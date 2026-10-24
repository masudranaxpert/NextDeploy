package handlers

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"panel/internal/db"
	"panel/internal/dockerapi"
	"panel/internal/dockerx"
	"panel/internal/gitx"

	"github.com/gofiber/fiber/v2"
)

// SlotImageExists checks whether the Docker image for a given app slot still exists on the host
// (or if the slot container itself still exists).
func (p *Panel) SlotImageExists(ctx context.Context, app db.App, appID, slot string) bool {
	slot = strings.TrimSpace(strings.ToLower(slot))
	if slot != "blue" && slot != "green" {
		return false
	}
	slotProject := strings.ToLower(p.SlotProjectName(app, appID, slot))
	containers, _ := dockerapi.ListContainers(ctx)
	for _, c := range containers {
		if strings.ToLower(strings.TrimSpace(c.ComposeProject)) == slotProject || strings.HasPrefix(strings.ToLower(c.Name), slotProject+"-") {
			return true
		}
	}
	images, _ := dockerapi.ListImages(ctx)
	for _, img := range images {
		for _, tag := range img.RepoTags {
			t := strings.ToLower(strings.TrimSpace(tag))
			if strings.HasPrefix(t, slotProject+"-") || strings.HasPrefix(t, slotProject+":") || strings.HasPrefix(t, slotProject+"/") {
				return true
			}
		}
	}
	return false
}

// PinSlot pins or unpins the active production slot for an app.
// POST /apps/:id/slots/pin
func (p *Panel) PinSlot(c *fiber.Ctx) error {
	appID := c.Params("id")
	app, err := p.DB.GetApp(c.UserContext(), appID)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"ok": false, "error": "app not found"})
	}

	var body struct {
		Slot string `json:"slot"`
	}
	_ = c.BodyParser(&body)
	reqSlot := strings.TrimSpace(strings.ToLower(body.Slot))
	if reqSlot == "" {
		reqSlot = strings.TrimSpace(strings.ToLower(c.FormValue("slot")))
	}

	newPin := ""
	if reqSlot == "unpin" || reqSlot == "none" {
		newPin = ""
	} else if reqSlot == "blue" || reqSlot == "green" {
		newPin = reqSlot
	} else {
		// Toggle pin on current active slot
		if strings.TrimSpace(app.PinnedSlot) != "" {
			newPin = ""
		} else {
			active := strings.TrimSpace(app.ActiveSlot)
			if active == "" {
				active = "blue"
			}
			newPin = active
		}
	}

	if err := p.DB.UpdateAppPinnedSlot(c.UserContext(), appID, newPin); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"ok": false, "error": err.Error()})
	}

	if newPin != "" {
		p.RecordAuditLog(c, "pin_slot", "app", appID, fmt.Sprintf("Pinned production to slot %s", newPin))
	} else {
		p.RecordAuditLog(c, "unpin_slot", "app", appID, "Unpinned production slot")
	}

	return c.JSON(fiber.Map{
		"ok":          true,
		"pinned_slot": newPin,
	})
}

// PromoteSlot promotes the preview/standby slot to active production and stops the old slot.
// POST /apps/:id/slots/promote
func (p *Panel) PromoteSlot(c *fiber.Ctx) error {
	appID := c.Params("id")
	app, err := p.DB.GetApp(c.UserContext(), appID)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"ok": false, "error": "app not found"})
	}

	ctx, cancel := context.WithTimeout(c.UserContext(), 2*time.Minute)
	defer cancel()

	currentSlot := strings.TrimSpace(app.ActiveSlot)
	if currentSlot == "" {
		currentSlot = "blue"
	}
	targetSlot := strings.TrimSpace(app.StandbySlot)
	if targetSlot == "" || targetSlot == currentSlot {
		if currentSlot == "blue" {
			targetSlot = "green"
		} else {
			targetSlot = "blue"
		}
	}

	if !p.SlotImageExists(ctx, app, appID, targetSlot) {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"ok":    false,
			"error": fmt.Sprintf("preview slot %s has no built image or running container to promote", targetSlot),
		})
	}

	// Regenerate full production compose override (previewOnly = false) for targetSlot
	if _, err := p.SyncAppSlotComposeCtx(ctx, appID, targetSlot, false); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"ok":    false,
			"error": fmt.Sprintf("failed to sync production labels for slot %s: %v", targetSlot, err),
		})
	}

	slotProject := p.SlotProjectName(app, appID, targetSlot)
	slotPaths := p.EffectiveComposePathsForSlot(ctx, app, appID, targetSlot)
	dir := p.AppSourcePath(ctx, appID)
	envFiles := p.ComposeEnvFiles(ctx, appID)

	upRes := dockerx.ComposeUpNoBuild(ctx, dir, slotPaths, slotProject, nil, envFiles)
	if !upRes.OK {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"ok":    false,
			"error": fmt.Sprintf("failed to promote slot %s: %s", targetSlot, upRes.Output),
		})
	}

	// Update active slot in DB and clear production pin so normal zero-downtime flow resumes (or user can pin new slot)
	_ = p.DB.UpdateAppSlots(ctx, appID, targetSlot, currentSlot)
	_ = p.DB.UpdateAppPinnedSlot(ctx, appID, "")

	// Stop and remove the previous production slot so only the newly promoted slot serves traffic
	for _, otherSlot := range []string{"blue", "green"} {
		if otherSlot == targetSlot {
			continue
		}
		prevProj := p.SlotProjectName(app, appID, otherSlot)
		prevPaths := p.EffectiveComposePathsForSlot(ctx, app, appID, otherSlot)
		_ = dockerx.ComposeDown(ctx, dir, prevPaths, prevProj, nil, envFiles)
		_ = dockerapi.RemoveContainersByComposeProject(ctx, prevProj)
	}
	p.StopOtherComposeStacks(ctx, app, appID, slotProject)
	_ = p.syncRootStackCompose(ctx)
	p.applyRootStackComposeBackground(p.nextDeployComposePath(), "caddy")

	panelEnv, _ := p.DB.GetPanelEnv(ctx, appID)
	var commitSHA string
	if cfg, err := p.DB.GetAppGitConfig(ctx, appID); err == nil {
		commitSHA = cfg.LastDeployRef
	}
	p.RecordAuditLog(c, "promote_slot", "app", appID, fmt.Sprintf("Promoted preview slot %s to production", targetSlot))
	_ = p.DB.InsertDeployLogWithSnapshot(ctx, appID, "Promote to Production", true,
		fmt.Sprintf("Promoted preview slot %s to active production. Stopped previous slot %s.", targetSlot, currentSlot),
		targetSlot, commitSHA, "", panelEnv)

	p.InvalidateAfterAppDeployChange(appID)

	return c.JSON(fiber.Map{
		"ok":          true,
		"active_slot": targetSlot,
		"pinned_slot": "",
		"message":     fmt.Sprintf("Promoted slot %s to production", strings.ToUpper(targetSlot)),
	})
}

// RollbackDeployment restores an app to a previous deployment release.
// If the target slot container is available in warm standby, cutover is instant (< 100ms).
func (p *Panel) RollbackDeployment(c *fiber.Ctx) error {
	appID := c.Params("id")
	logIDStr := c.Params("depId")
	if logIDStr == "" {
		logIDStr = c.Params("logId")
	}
	logID, err := strconv.ParseInt(logIDStr, 10, 64)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"ok": false, "error": "invalid deployment id"})
	}

	app, err := p.DB.GetApp(c.UserContext(), appID)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"ok": false, "error": "app not found"})
	}

	dlog, err := p.DB.GetDeployLog(c.UserContext(), appID, logID)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"ok": false, "error": "deployment record not found"})
	}

	ctx, cancel := context.WithTimeout(c.UserContext(), 2*time.Minute)
	defer cancel()

	targetSlot := strings.TrimSpace(dlog.Slot)
	currentSlot := app.ActiveSlot

	// 1. Instant Rollback to alternate Slot (Blue <-> Green)
	if targetSlot != "" && targetSlot != currentSlot {
		if !p.SlotImageExists(ctx, app, appID, targetSlot) {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"ok":    false,
				"error": fmt.Sprintf("Cannot rollback: the Docker image for slot %s was deleted or is no longer available on the host.", strings.ToUpper(targetSlot)),
			})
		}

		// Ensure the target slot has full production labels (previewOnly = false)
		_, _ = p.SyncAppSlotComposeCtx(ctx, appID, targetSlot, false)

		slotProject := p.SlotProjectName(app, appID, targetSlot)
		slotPaths := p.EffectiveComposePathsForSlot(ctx, app, appID, targetSlot)
		dir := p.AppSourcePath(ctx, appID)
		envFiles := p.ComposeEnvFiles(ctx, appID)

		// Apply compose up without re-building so existing slot image is preserved
		upRes := dockerx.ComposeUpNoBuild(ctx, dir, slotPaths, slotProject, nil, envFiles)
		if !upRes.OK {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"ok":    false,
				"error": fmt.Sprintf("failed to activate standby slot %s: %s", targetSlot, upRes.Output),
			})
		}

		// Restore environment variables snapshot if saved
		if strings.TrimSpace(dlog.EnvSnapshot) != "" {
			_ = p.DB.UpdatePanelEnv(ctx, appID, dlog.EnvSnapshot)
		}

		// Atomic cutover in DB & clear any production pin
		_ = p.DB.UpdateAppSlots(ctx, appID, targetSlot, currentSlot)
		_ = p.DB.UpdateAppPinnedSlot(ctx, appID, "")
		for _, otherSlot := range []string{"blue", "green"} {
			if otherSlot == targetSlot {
				continue
			}
			prevProj := p.SlotProjectName(app, appID, otherSlot)
			prevPaths := p.EffectiveComposePathsForSlot(ctx, app, appID, otherSlot)
			_ = dockerx.ComposeDown(ctx, dir, prevPaths, prevProj, nil, envFiles)
			_ = dockerapi.RemoveContainersByComposeProject(ctx, prevProj)
		}
		p.StopOtherComposeStacks(ctx, app, appID, slotProject)
		_ = p.syncRootStackCompose(ctx)
	p.applyRootStackComposeBackground(p.nextDeployComposePath(), "caddy")

		p.RecordAuditLog(c, "rollback_deployment", "app", appID, fmt.Sprintf("Rolled back to release #%d (Slot %s)", logID, targetSlot))
		_ = p.DB.InsertDeployLogWithSnapshot(ctx, appID, "Rollback", true,
			fmt.Sprintf("Rolled back release to deployment #%d (Slot %s). Atomic cutover completed.", logID, targetSlot),
			targetSlot, dlog.CommitSHA, dlog.ImageDigest, dlog.EnvSnapshot)

		p.InvalidateAfterAppDeployChange(appID)

		return c.JSON(fiber.Map{
			"ok":          true,
			"active_slot": targetSlot,
			"pinned_slot": "",
			"message":     fmt.Sprintf("Rolled back to release #%d (Slot %s)", logID, targetSlot),
		})
	}

	// 2. Deep Rollback to Historical Release via Git Commit SHA
	if dlog.CommitSHA != "" && p.IsGitApp(ctx, appID) {
		repoDir := p.AppCheckoutPath(appID)
		if gitx.RepoExists(repoDir) {
			checkRes := gitx.Checkout(ctx, repoDir, dlog.CommitSHA)
			if !checkRes.OK {
				return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
					"ok":    false,
					"error": fmt.Sprintf("git checkout failed: %s", checkRes.Output),
				})
			}
			if strings.TrimSpace(dlog.EnvSnapshot) != "" {
				_ = p.DB.UpdatePanelEnv(ctx, appID, dlog.EnvSnapshot)
			}
			p.RecordAuditLog(c, "rollback_deployment", "app", appID, fmt.Sprintf("Rolled back to commit %s", dlog.CommitSHA))
			return c.JSON(fiber.Map{
				"ok":      true,
				"message": fmt.Sprintf("Checked out commit %s for rollback deploy", dlog.CommitSHA),
			})
		}
	}

	return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
		"ok":    false,
		"error": "deployment record does not contain slot or commit snapshot for rollback",
	})
}
