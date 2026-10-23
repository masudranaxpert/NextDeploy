package handlers

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"panel/internal/db"
	"panel/internal/dockerapi"
	"panel/internal/dockerx"
	"panel/internal/resmatch"
	"panel/internal/runutil"
	"panel/internal/volumex"

	"github.com/gofiber/fiber/v2"
)

func (p *Panel) ComposeUp(c *fiber.Ctx) error {
	return p.enqueueCompose(c, "Deploy", dockerx.ComposeUp)
}

func (p *Panel) ComposeDown(c *fiber.Ctx) error {
	return p.enqueueCompose(c, "Stop", dockerx.ComposeDown)
}

func (p *Panel) ComposeRestart(c *fiber.Ctx) error {
	return p.enqueueCompose(c, "Stack restart", dockerx.ComposeRestart)
}

func (p *Panel) ComposeRedeploy(c *fiber.Ctx) error {
	return p.enqueueCompose(c, "Redeploy (pull + up)", dockerx.ComposePullUp)
}

func (p *Panel) GlobalImageRemove(c *fiber.Ctx) error {
	imageID := strings.TrimSpace(c.FormValue("image_id"))
	if imageID == "" {
		imageID = strings.TrimSpace(c.FormValue("id"))
	}
	if imageID == "" {
		return c.Status(400).JSON(fiber.Map{"ok": false, "error": "image_id required"})
	}
	allowed, err := p.isImageAccessAllowed(c, imageID)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"ok": false, "error": err.Error()})
	}
	if !allowed {
		return c.Status(403).JSON(fiber.Map{"ok": false, "error": "forbidden"})
	}
	p.RecordAuditLog(c, "remove_image", "image", imageID, "Removed Docker image")
	if err := dockerapi.RemoveImageByID(c.UserContext(), imageID); err != nil {
		return c.Status(500).JSON(fiber.Map{"ok": false, "error": err.Error()})
	}
	p.InvalidateAfterDockerChange()
	if WantsJSON(c) {
		return c.JSON(fiber.Map{"ok": true})
	}
	return c.Redirect("/images")
}

func (p *Panel) GlobalContainerRemove(c *fiber.Ctx) error {
	name := strings.TrimSpace(c.FormValue("name"))
	if name == "" {
		return c.Status(400).JSON(fiber.Map{"ok": false, "error": "name required"})
	}
	allowed, err := p.isContainerAccessAllowed(c, name)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"ok": false, "error": err.Error()})
	}
	if !allowed {
		return c.Status(403).JSON(fiber.Map{"ok": false, "error": "forbidden"})
	}
	p.RecordAuditLog(c, "remove_container", "container", name, "Removed Docker container")
	if err := dockerapi.RemoveContainerByName(c.UserContext(), name); err != nil {
		return c.Status(500).JSON(fiber.Map{"ok": false, "error": err.Error()})
	}
	p.InvalidateAfterDockerChange()
	if WantsJSON(c) {
		return c.JSON(fiber.Map{"ok": true})
	}
	return c.Redirect("/containers")
}

func (p *Panel) GlobalContainerRemoveSelected(c *fiber.Ctx) error {
	var names []string
	c.Request().PostArgs().VisitAll(func(key, val []byte) {
		if string(key) != "name" {
			return
		}
		if n := strings.TrimSpace(string(val)); n != "" {
			names = append(names, n)
		}
	})
	if len(names) == 0 {
		return c.Status(400).JSON(fiber.Map{"ok": false, "error": "no containers selected"})
	}
	ctx := c.UserContext()
	for _, name := range names {
		allowed, err := p.isContainerAccessAllowed(c, name)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"ok": false, "error": err.Error()})
		}
		if !allowed {
			return c.Status(403).JSON(fiber.Map{"ok": false, "error": "forbidden"})
		}
	}
	p.RecordAuditLog(c, "remove_selected_containers", "container", strings.Join(names, ", "), "Removed selected containers")
	for _, name := range names {
		_ = dockerapi.RemoveContainerByName(ctx, name)
	}
	p.InvalidateAfterDockerChange()
	if WantsJSON(c) {
		return c.JSON(fiber.Map{"ok": true})
	}
	return c.Redirect("/containers")
}

func (p *Panel) GlobalContainerRestart(c *fiber.Ctx) error {
	name := strings.TrimSpace(c.FormValue("name"))
	if name == "" {
		return c.Status(400).JSON(fiber.Map{"ok": false, "error": "name required"})
	}
	allowed, err := p.isContainerAccessAllowed(c, name)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"ok": false, "error": err.Error()})
	}
	if !allowed {
		return c.Status(403).JSON(fiber.Map{"ok": false, "error": "forbidden"})
	}
	p.RecordAuditLog(c, "restart_container", "container", name, "Restarted Docker container")
	if err := dockerapi.RestartContainerByName(c.UserContext(), name); err != nil {
		return c.Status(500).JSON(fiber.Map{"ok": false, "error": err.Error()})
	}
	p.InvalidateAfterDockerChange()
	if WantsJSON(c) {
		return c.JSON(fiber.Map{"ok": true})
	}
	return c.Redirect("/containers")
}

func (p *Panel) GlobalVolumeRemove(c *fiber.Ctx) error {
	name := strings.TrimSpace(c.FormValue("name"))
	if name == "" {
		return c.Status(400).JSON(fiber.Map{"ok": false, "error": "name required"})
	}
	allowed, err := p.isVolumeAccessAllowed(c, name)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"ok": false, "error": err.Error()})
	}
	if !allowed {
		return c.Status(403).JSON(fiber.Map{"ok": false, "error": "forbidden"})
	}
	p.RecordAuditLog(c, "remove_volume", "volume", name, "Removed Docker volume")
	if err := dockerapi.RemoveVolumeByName(c.UserContext(), name); err != nil {
		return c.Status(500).JSON(fiber.Map{"ok": false, "error": err.Error()})
	}
	p.InvalidateAfterDockerChange()
	if WantsJSON(c) {
		return c.JSON(fiber.Map{"ok": true})
	}
	return c.Redirect("/volumes")
}

func (p *Panel) GlobalImagePrune(c *fiber.Ctx) error {
	u, ok := c.Locals("auth_user").(db.User)
	if !ok || u.Role != db.RoleAdmin {
		return c.Status(403).JSON(fiber.Map{"ok": false, "error": "forbidden"})
	}
	p.RecordAuditLog(c, "prune_images", "system", "docker", "Pruned unused Docker images")
	if err := dockerapi.PruneImages(c.UserContext()); err != nil {
		return c.Status(500).JSON(fiber.Map{"ok": false, "error": err.Error()})
	}
	p.InvalidateAfterDockerChange()
	if WantsJSON(c) {
		return c.JSON(fiber.Map{"ok": true})
	}
	return c.Redirect("/images")
}

func (p *Panel) GlobalContainerPrune(c *fiber.Ctx) error {
	u, ok := c.Locals("auth_user").(db.User)
	if !ok || u.Role != db.RoleAdmin {
		return c.Status(403).JSON(fiber.Map{"ok": false, "error": "forbidden"})
	}
	p.RecordAuditLog(c, "prune_containers", "system", "docker", "Pruned stopped Docker containers")
	if err := dockerapi.PruneContainers(c.UserContext()); err != nil {
		return c.Status(500).JSON(fiber.Map{"ok": false, "error": err.Error()})
	}
	p.InvalidateAfterDockerChange()
	if WantsJSON(c) {
		return c.JSON(fiber.Map{"ok": true})
	}
	return c.Redirect("/containers")
}

func (p *Panel) isContainerAccessAllowed(c *fiber.Ctx, containerName string) (bool, error) {
	ctx := c.UserContext()
	u, ok := c.Locals("auth_user").(db.User)
	if !ok {
		return false, nil
	}
	if u.Role == db.RoleAdmin {
		return true, nil
	}
	project, _, err := dockerapi.ContainerComposeLabels(ctx, containerName)
	if err != nil || project == "" {
		return false, nil
	}
	apps, err := p.DB.ListAppsForUser(ctx, u.ID)
	if err != nil {
		return false, err
	}
	for _, app := range apps {
		if app.ID == project {
			return true, nil
		}
	}
	return false, nil
}

func (p *Panel) isImageAccessAllowed(c *fiber.Ctx, imageID string) (bool, error) {
	ctx := c.UserContext()
	u, ok := c.Locals("auth_user").(db.User)
	if !ok {
		return false, nil
	}
	if u.Role == db.RoleAdmin {
		return true, nil
	}
	apps, err := p.DB.ListAppsForUser(ctx, u.ID)
	if err != nil {
		return false, err
	}
	allContainers, _ := dockerapi.ListContainers(ctx)
	allowedProjectIDs := make(map[string]bool)
	usedImageNames := make(map[string]bool)
	for _, app := range apps {
		allowedProjectIDs[app.ID] = true
	}
	for _, cRow := range allContainers {
		if allowedProjectIDs[cRow.ComposeProject] {
			usedImageNames[strings.TrimSpace(cRow.Image)] = true
		}
	}
	images, _ := dockerapi.ListImages(ctx)
	var targetImg *dockerapi.ImageRow
	for _, img := range images {
		if img.ID == imageID {
			targetImg = &img
			break
		}
	}
	if targetImg == nil {
		return false, nil
	}
	for tag := range usedImageNames {
		if strings.Contains(targetImg.Tags, tag) {
			return true, nil
		}
	}
	var allUserProjects []string
	for _, app := range apps {
		allUserProjects = append(allUserProjects, app.ID)
		allUserProjects = append(allUserProjects, strings.ReplaceAll(app.ID, "-", "_"))
		for _, cand := range p.ComposeProjectCandidates(ctx, app, app.ID) {
			allUserProjects = append(allUserProjects, cand)
		}
	}
	for _, proj := range apps {
		for _, t := range targetImg.RepoTags {
			if t == "" || t == "<none>" {
				continue
			}
			if resmatch.MatchesImageRepo(imageRepoBase(t), proj.ID, allUserProjects) {
				return true, nil
			}
		}
	}
	return false, nil
}

func (p *Panel) isVolumeAccessAllowed(c *fiber.Ctx, volumeName string) (bool, error) {
	ctx := c.UserContext()
	u, ok := c.Locals("auth_user").(db.User)
	if !ok {
		return false, nil
	}
	if u.Role == db.RoleAdmin {
		return true, nil
	}
	apps, err := p.DB.ListAppsForUser(ctx, u.ID)
	if err != nil {
		return false, err
	}
	allVolNames, listErr := volumex.List(ctx)
	if listErr != "" {
		return false, fmt.Errorf("%s", listErr)
	}
	allProjects := p.AllPanelComposeProjects(ctx)
	matcher := volumex.SharedMatcher(ctx)
	for _, app := range apps {
		projCandidates := append([]string{app.ID, strings.ReplaceAll(app.ID, "-", "_"), app.Name}, p.ComposeProjectCandidates(ctx, app, app.ID)...)
		q := p.AppVolumeQuery(ctx, app, allProjects, projCandidates...)
		appVols, _ := matcher.ListForAppFromNames(ctx, q, allVolNames)
		for _, v := range appVols {
			if v == volumeName {
				return true, nil
			}
		}
	}
	return false, nil
}

func imageRepoBase(repo string) string {
	repo = strings.TrimSpace(repo)
	if i := strings.LastIndex(repo, ":"); i > 0 {
		repo = repo[:i]
	}
	if i := strings.LastIndex(repo, "/"); i >= 0 {
		repo = repo[i+1:]
	}
	return repo
}

func (p *Panel) enqueueCompose(c *fiber.Ctx, action string, fn func(context.Context, string, []string, string, io.Writer, []string) dockerx.Result) error {
	id := c.Params("id")
	wantJSON := WantsJSON(c)

	v, _ := p.ComposeMu.LoadOrStore(id, &sync.Mutex{})
	mu := v.(*sync.Mutex)
	mu.Lock()
	defer mu.Unlock()

	app, err := p.DB.GetApp(c.UserContext(), id)
	if err != nil {
		if wantJSON {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"ok": false, "error": "app not found"})
		}
		return c.Status(fiber.StatusNotFound).SendString("app not found")
	}
	var gitSyncPreamble string
	if p.IsGitApp(c.UserContext(), id) {
		ctx, cancel := context.WithTimeout(c.UserContext(), 15*time.Minute)
		out, err := p.SyncGitAppSource(ctx, id)
		if err != nil {
			cancel()
			msg := "[error]\nGit sync failed.\n\n" + err.Error()
			if strings.TrimSpace(out) != "" {
				msg += "\n\n" + out
			}
			_ = p.DB.InsertDeployLog(c.UserContext(), id, action, false, msg)
			if wantJSON {
				return c.Status(400).JSON(fiber.Map{"ok": false, "error": "Git sync failed: " + err.Error(), "output": msg})
			}
			return c.Redirect(fmt.Sprintf("/apps/%s?tab=deployment", id))
		}
		cancel()
		gitSyncPreamble = strings.TrimSpace(out)
		if gitSyncPreamble == "" {
			gitSyncPreamble = "Repository sync completed."
		}
		p.InvalidateAfterAppWorkspaceChange(id)
	}
	cp := p.ComposeFilePath(c.UserContext(), app, id)
	if _, err := os.Stat(cp); err != nil {
		hasDockerfile, hasCompose := p.Store.HasDockerArtifacts(id)
		if !hasDockerfile || hasCompose {
			msg := "[error]\nCompose file not found. Set path on Overview or upload the file / sync the repository first."
			_ = p.DB.InsertDeployLog(c.UserContext(), id, action, false, msg)
			if wantJSON {
				return c.Status(400).JSON(fiber.Map{"ok": false, "error": "Compose file or Dockerfile not found in workspace. Upload your files or sync Git repository first.", "output": msg})
			}
			return c.Redirect(fmt.Sprintf("/apps/%s?tab=deployment", id))
		}
	}
	r := p.GetDeployRun(id)
	r.Mu.Lock()
	isBusy := r.Running
	activeJobID := r.JobID
	r.Mu.Unlock()
	if isBusy {
		errMsg := fmt.Sprintf("busy: deployment job %q is already in progress for app %q. Wait for it to finish before starting another action", activeJobID, id)
		if wantJSON {
			return c.Status(409).JSON(fiber.Map{"ok": false, "error": errMsg})
		}
		return c.Redirect(fmt.Sprintf("/apps/%s?tab=deployment&busy=1", id))
	}

	pinnedSlot := strings.TrimSpace(app.PinnedSlot)
	isPinnedMode := pinnedSlot == "blue" || pinnedSlot == "green"

	targetSlot := "blue"
	if isPinnedMode {
		if pinnedSlot == "blue" {
			targetSlot = "green"
		} else {
			targetSlot = "blue"
		}
	} else if app.ActiveSlot == "blue" {
		targetSlot = "green"
	}

	if action == "Deploy" || action == "Redeploy (pull + up)" {
		if _, err := p.SyncAppSlotComposeCtx(c.UserContext(), id, targetSlot, isPinnedMode); err != nil {
			msg := "[error]\n" + err.Error()
			_ = p.DB.InsertDeployLog(c.UserContext(), id, action, false, msg)
			if wantJSON {
				return c.Status(400).JSON(fiber.Map{"ok": false, "error": err.Error(), "output": msg})
			}
			return c.Redirect(fmt.Sprintf("/apps/%s?tab=deployment", id))
		}
	} else {
		activeSlot := app.ActiveSlot
		if activeSlot == "" {
			activeSlot = "blue"
		}
		if _, err := p.SyncAppSlotComposeCtx(c.UserContext(), id, activeSlot, false); err != nil {
			msg := "[error]\n" + err.Error()
			_ = p.DB.InsertDeployLog(c.UserContext(), id, action, false, msg)
			if wantJSON {
				return c.Status(400).JSON(fiber.Map{"ok": false, "error": err.Error(), "output": msg})
			}
			return c.Redirect(fmt.Sprintf("/apps/%s?tab=deployment", id))
		}
	}

	activeSlot := app.ActiveSlot
	if activeSlot == "" {
		activeSlot = "blue"
	}
	project := p.SlotProjectName(app, id, activeSlot)
	paths := p.EffectiveComposePathsForSlot(c.UserContext(), app, id, activeSlot)
	if action == "Deploy" || action == "Redeploy (pull + up)" {
		project = p.SlotProjectName(app, id, targetSlot)
		paths = p.EffectiveComposePathsForSlot(c.UserContext(), app, id, targetSlot)
	}

	p.RecordAuditLog(c, "compose_"+strings.ToLower(strings.ReplaceAll(action, " ", "_")), "app", id, "Triggered compose action: "+action)
	jobID, err := p.StartComposeJob(id, project, paths, action, fn, gitSyncPreamble)
	if err != nil {
		if wantJSON {
			return c.Status(409).JSON(fiber.Map{"ok": false, "error": err.Error()})
		}
		return c.Redirect(fmt.Sprintf("/apps/%s?tab=deployment&busy=1", id))
	}
	if wantJSON {
		return c.JSON(fiber.Map{"ok": true, "job_id": jobID, "action": action, "slot": targetSlot})
	}
	return c.Redirect(fmt.Sprintf("/apps/%s?tab=deployment", id))
}

func (p *Panel) DeployProgressPartial(c *fiber.Ctx) error {
	id := c.Params("id")
	if _, err := p.DB.GetApp(c.UserContext(), id); err != nil {
		return c.Status(404).JSON(fiber.Map{"ok": false, "error": "not found"})
	}
	r := p.GetDeployRun(id)
	r.Mu.Lock()
	out := r.Output.String()
	running := r.Running
	act := r.Action
	r.Mu.Unlock()
	return c.JSON(fiber.Map{
		"ok":      true,
		"id":      id,
		"output":  out,
		"running": running,
		"action":  act,
	})
}

func formatOut(r dockerx.Result) string {
	return runutil.StatusText(runutil.Result{OK: r.OK, Output: r.Output})
}
