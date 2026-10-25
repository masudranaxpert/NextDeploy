package handlers

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"panel/internal/db"
	"panel/internal/quota"
	"panel/internal/sysinfo"
	"panel/internal/volumex"
	"panel/internal/workspace"

	"github.com/gofiber/fiber/v2"
)

func randomAppSuffix() string {
	buf := make([]byte, 2)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%04x", time.Now().UnixNano()%65536)
	}
	return hex.EncodeToString(buf)
}

func (p *Panel) RandomAppSuffix() string {
	return randomAppSuffix()
}

func (p *Panel) ValidateAppSlug(raw string) (string, error) {
	return validateAppSlug(raw)
}

func (p *Panel) CreateApp(c *fiber.Ctx) error {
	ctx := c.UserContext()
	u, ok := currentUser(c)
	if !ok {
		return c.Status(401).SendString("unauthorized")
	}
	slug, err := validateAppSlug(c.FormValue("name"))
	if err != nil {
		return c.Status(400).SendString(err.Error())
	}
	exists, err := p.DB.AppNameExistsForUser(ctx, slug, u.ID)
	if err != nil {
		return c.Status(500).SendString(err.Error())
	}
	if exists {
		return c.Status(400).SendString("an app with this name already exists")
	}
	if err := quota.CheckAppSlot(ctx, p.DB, u); err != nil {
		return c.Status(400).SendString("maximum app limit reached")
	}
	var id string
	for {
		suffix := randomAppSuffix()
		id = fmt.Sprintf("%s-%s", slug, suffix)
		if _, err := p.DB.GetApp(ctx, id); errors.Is(err, sql.ErrNoRows) {
			break
		}
	}
	name := slug
	if err := os.MkdirAll(p.Store.Path(id), 0750); err != nil {
		return c.Status(500).SendString(err.Error())
	}
	if err := p.Store.WriteMeta(id, name); err != nil {
		return c.Status(500).SendString(err.Error())
	}

	// Always ensure a default docker-compose.yml exists in the workspace
	composePath := filepath.Join(p.Store.Path(id), "docker-compose.yml")
	composeContent := strings.TrimSpace(c.FormValue("compose_content"))
	if composeContent == "" {
		composeContent = fmt.Sprintf("services:\n  app:\n    image: nginx:alpine\n    restart: unless-stopped\n    ports:\n      - \"80\"\n")
	}
	_ = os.WriteFile(composePath, []byte(composeContent+"\n"), 0644)

	if err := quota.CreateApp(ctx, p.DB, u, id, name); err != nil {
		_ = os.RemoveAll(p.Store.Path(id))
		if errors.Is(err, db.ErrAppLimitReached) || strings.Contains(strings.ToLower(err.Error()), "app limit reached") {
			return c.Status(400).SendString("maximum app limit reached")
		}
		if strings.Contains(strings.ToLower(err.Error()), "already exists") {
			return c.Status(400).SendString(err.Error())
		}
		return c.Status(500).SendString(err.Error())
	}
	sourceType := strings.TrimSpace(c.FormValue("source_type"))
	if sourceType == "github" || sourceType == "git" {
		_ = p.DB.SetAppSourceType(c.UserContext(), id, "git")
		repoURL := strings.TrimSpace(c.FormValue("repo_url"))
		if repoURL != "" {
			cfg := db.AppGitConfig{
				AppID:         id,
				Provider:      "github",
				RepoURL:       NormalizeRepoURL(repoURL),
				RepoFullName:  RepoFullNameFromURL(repoURL),
				Branch:        NormalizeBranch(c.FormValue("branch")),
				AuthMode:      strings.TrimSpace(c.FormValue("auth_mode")),
				Token:         strings.TrimSpace(c.FormValue("token")),
				WebhookSecret: RandomSecret(),
				AutoDeploy:    true,
			}
			if cfg.AuthMode == "" {
				cfg.AuthMode = "public"
			}
			if err := p.DB.UpsertAppGitConfig(c.UserContext(), cfg); err != nil {
				return c.Status(500).SendString(err.Error())
			}
			if err := os.MkdirAll(filepath.Join(p.Store.ReservedPath(id), "repo"), 0750); err != nil {
				return c.Status(500).SendString(err.Error())
			}
		}
	}
	p.RecordAuditLog(c, "create_app", "app", id, "Created app: "+name+" with source: "+sourceType)
	if WantsJSON(c) {
		return c.JSON(fiber.Map{"ok": true, "id": id, "name": name})
	}
	return c.Redirect(fmt.Sprintf("/apps/%s", id))
}

func (p *Panel) SaveAppCompose(c *fiber.Ctx) error {
	id := c.Params("id")
	if _, err := p.DB.GetApp(c.UserContext(), id); err != nil {
		return c.Status(fiber.StatusNotFound).SendString("app not found")
	}
	raw := workspace.NormalizeComposeRel(c.FormValue("compose_file"))
	if err := p.DB.UpdateComposeFile(c.UserContext(), id, raw); err != nil {
		return c.Status(500).SendString(err.Error())
	}
	if err := p.SyncAppCaddyOverride(c, id); err != nil {
		return c.Status(500).SendString(err.Error())
	}
	if WantsJSON(c) {
		return c.JSON(fiber.Map{"ok": true, "compose_file": raw})
	}
	return c.Redirect(fmt.Sprintf("/apps/%s?tab=overview", id))
}

func (p *Panel) SaveAppEnv(c *fiber.Ctx) error {
	id := c.Params("id")
	if _, err := p.DB.GetApp(c.UserContext(), id); err != nil {
		return c.Status(fiber.StatusNotFound).SendString("app not found")
	}
	content := c.FormValue("env")
	if err := p.DB.UpdatePanelEnv(c.UserContext(), id, content); err != nil {
		return c.Status(500).SendString(err.Error())
	}
	root := p.ComposeWorkspaceRoot(c.UserContext(), id)
	if err := p.SyncWorkspaceEnvFromPanel(id, root, content); err != nil {
		return c.Status(500).SendString(err.Error())
	}
	_ = p.SyncAppCaddyOverride(c, id)
	if WantsJSON(c) {
		return c.JSON(fiber.Map{"ok": true})
	}
	return c.Redirect(fmt.Sprintf("/apps/%s?tab=environment", id))
}

func (p *Panel) InvalidateAfterUserChange() {
	p.InvalidateMonitorResourceCache()
	p.TriggerBandwidthQoSReconcile()
}

func (p *Panel) InvalidateAfterAppWorkspaceChange(appID string) {
	if appID == "" {
		return
	}
	p.InvalidateAppStorageCache(appID)
}

func (p *Panel) InvalidateAfterDockerChange() {
	invalidateBootCaches()
	volumex.InvalidateSharedMatcher()
	sysinfo.InvalidateDiskCache()
	go p.refreshMonitorSnapshot()
	p.TriggerBandwidthQoSReconcile()
}

func (p *Panel) InvalidateAfterAppDeployChange(appID string) {
	p.InvalidateAfterAppWorkspaceChange(appID)
	p.InvalidateAfterDockerChange()
}

func (p *Panel) AddCollaborator(c *fiber.Ctx) error {
	ctx := c.UserContext()
	appID := c.Params("id")
	u, ok := c.Locals("auth_user").(db.User)
	if !ok {
		return c.Status(fiber.StatusUnauthorized).SendString("Unauthorized")
	}

	app, err := p.DB.GetApp(ctx, appID)
	if err != nil {
		return c.Status(fiber.StatusNotFound).SendString("App not found")
	}

	if u.Role != db.RoleAdmin && app.OwnerID != u.ID {
		return c.Status(fiber.StatusForbidden).SendString("Forbidden")
	}

	var req struct {
		Username    string   `json:"username"`
		Role        string   `json:"role"`
		Permissions []string `json:"permissions"`
	}
	_ = c.BodyParser(&req)

	collabUsername := strings.TrimSpace(req.Username)
	if collabUsername == "" {
		collabUsername = strings.TrimSpace(c.FormValue("username"))
	}
	role := strings.TrimSpace(req.Role)
	if role == "" {
		role = strings.TrimSpace(c.FormValue("role"))
	}
	if role != db.CollabRoleDeveloper && role != db.CollabRoleViewer {
		role = db.CollabRoleViewer
	}

	permsJSON := "[]"
	if len(req.Permissions) > 0 {
		b, _ := json.Marshal(req.Permissions)
		permsJSON = string(b)
	} else if pForm := strings.TrimSpace(c.FormValue("permissions")); pForm != "" {
		permsJSON = pForm
	}

	targetUser, err := p.DB.GetUserByUsername(ctx, collabUsername)
	if err != nil {
		if WantsJSON(c) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"ok": false, "error": "User not found"})
		}
		SetFlash(c, "Error: User not found.")
		return c.Redirect(fmt.Sprintf("/apps/%s?tab=collaborators", appID))
	}

	if targetUser.ID == app.OwnerID {
		if WantsJSON(c) {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"ok": false, "error": "User is already the owner of this app"})
		}
		SetFlash(c, "Error: User is already the owner of this app.")
		return c.Redirect(fmt.Sprintf("/apps/%s?tab=collaborators", appID))
	}

	if err := p.DB.AddCollaborator(ctx, appID, targetUser.ID, role, permsJSON); err != nil {
		if WantsJSON(c) {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"ok": false, "error": err.Error()})
		}
		SetFlash(c, "Error adding collaborator: "+err.Error())
		return c.Redirect(fmt.Sprintf("/apps/%s?tab=collaborators", appID))
	}

	p.RecordAuditLog(c, "add_collaborator", "app", appID, fmt.Sprintf("Added collaborator %s with role %s", collabUsername, role))
	if WantsJSON(c) {
		return c.JSON(fiber.Map{"ok": true})
	}
	SetFlash(c, "Collaborator added successfully.")
	return c.Redirect(fmt.Sprintf("/apps/%s?tab=collaborators", appID))
}

func (p *Panel) DeleteCollaborator(c *fiber.Ctx) error {
	ctx := c.UserContext()
	appID := c.Params("id")
	collabUserID, err := c.ParamsInt("uid")
	if err != nil {
		return c.Status(fiber.StatusBadRequest).SendString("Invalid user ID")
	}

	u, ok := c.Locals("auth_user").(db.User)
	if !ok {
		return c.Status(fiber.StatusUnauthorized).SendString("Unauthorized")
	}

	app, err := p.DB.GetApp(ctx, appID)
	if err != nil {
		return c.Status(fiber.StatusNotFound).SendString("App not found")
	}

	if u.Role != db.RoleAdmin && app.OwnerID != u.ID {
		return c.Status(fiber.StatusForbidden).SendString("Forbidden")
	}

	if err := p.DB.RemoveCollaborator(ctx, appID, int64(collabUserID)); err != nil {
		if WantsJSON(c) {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"ok": false, "error": err.Error()})
		}
		SetFlash(c, "Error removing collaborator: "+err.Error())
		return c.Redirect(fmt.Sprintf("/apps/%s?tab=collaborators", appID))
	}

	p.RecordAuditLog(c, "delete_collaborator", "app", appID, fmt.Sprintf("Removed collaborator user ID %d", collabUserID))
	if WantsJSON(c) {
		return c.JSON(fiber.Map{"ok": true})
	}
	SetFlash(c, "Collaborator removed successfully.")
	return c.Redirect(fmt.Sprintf("/apps/%s?tab=collaborators", appID))
}

func (p *Panel) TransferAppOwnership(c *fiber.Ctx) error {
	ctx := c.UserContext()
	appID := c.Params("id")
	u, ok := c.Locals("auth_user").(db.User)
	if !ok {
		return c.Status(fiber.StatusUnauthorized).SendString("Unauthorized")
	}

	app, err := p.DB.GetApp(ctx, appID)
	if err != nil {
		return c.Status(fiber.StatusNotFound).SendString("App not found")
	}

	if u.Role != db.RoleAdmin && app.OwnerID != u.ID {
		return c.Status(fiber.StatusForbidden).SendString("Forbidden")
	}

	newOwnerUsername := strings.TrimSpace(c.FormValue("username"))
	targetUser, err := p.DB.GetUserByUsername(ctx, newOwnerUsername)
	if err != nil {
		if WantsJSON(c) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"ok": false, "error": "User not found"})
		}
		SetFlash(c, "Error: User not found.")
		return c.Redirect(fmt.Sprintf("/apps/%s?tab=collaborators", appID))
	}

	if targetUser.ID == app.OwnerID {
		if WantsJSON(c) {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"ok": false, "error": "User is already the owner"})
		}
		SetFlash(c, "Error: User is already the owner.")
		return c.Redirect(fmt.Sprintf("/apps/%s?tab=collaborators", appID))
	}

	collabs, err := p.DB.ListCollaborators(ctx, appID)
	if err != nil {
		if WantsJSON(c) {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"ok": false, "error": "Error loading collaborators"})
		}
		SetFlash(c, "Error loading collaborators.")
		return c.Redirect(fmt.Sprintf("/apps/%s?tab=collaborators", appID))
	}
	isCollab := false
	for _, cb := range collabs {
		if cb.UserID == targetUser.ID {
			isCollab = true
			break
		}
	}
	if !isCollab {
		if WantsJSON(c) {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"ok": false, "error": "Target user must be a collaborator of this app first"})
		}
		SetFlash(c, "Error: Target user must be a collaborator of this app first.")
		return c.Redirect(fmt.Sprintf("/apps/%s?tab=collaborators", appID))
	}

	if err := p.DB.TransferAppOwnership(ctx, appID, targetUser.ID); err != nil {
		if WantsJSON(c) {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"ok": false, "error": err.Error()})
		}
		SetFlash(c, "Error transferring ownership: "+err.Error())
		return c.Redirect(fmt.Sprintf("/apps/%s?tab=collaborators", appID))
	}

	_ = p.DB.RemoveCollaborator(ctx, appID, targetUser.ID)

	if p.PHP != nil && p.DB.IsPHPSite(ctx, appID) {
		// Re-home the PHP runtime so the new owner's quota applies.
		if err := p.PHP.TransferSite(ctx, appID, targetUser.ID); err != nil {
			log.Printf("php transfer %s: %v", appID, err)
			if WantsJSON(c) {
				return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"ok": false, "error": "ownership changed, but the PHP runtime could not be moved: " + err.Error()})
			}
		}
	}

	p.RecordAuditLog(c, "transfer_ownership", "app", appID, fmt.Sprintf("Transferred ownership of app %s to %s", appID, newOwnerUsername))
	if WantsJSON(c) {
		return c.JSON(fiber.Map{"ok": true})
	}
	SetFlash(c, "Ownership transferred successfully.")

	if u.Role != db.RoleAdmin {
		return c.Redirect("/apps")
	}
	return c.Redirect(fmt.Sprintf("/apps/%s?tab=collaborators", appID))
}
