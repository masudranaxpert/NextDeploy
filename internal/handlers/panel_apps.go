package handlers

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"panel/internal/db"
	"panel/internal/handlers/utils"
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
	if u.Role != db.RoleAdmin {
		count, err := p.DB.CountAppsOwnedByUser(ctx, u.ID)
		if err == nil && u.MaxApps > 0 && count >= u.MaxApps {
			return c.Status(400).SendString("maximum app limit reached")
		}
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
	if err := p.DB.CreateApp(ctx, id, name, u.ID); err != nil {
		_ = os.RemoveAll(p.Store.Path(id))
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
				RepoURL:       utils.NormalizeRepoURL(repoURL),
				RepoFullName:  utils.RepoFullNameFromURL(repoURL),
				Branch:        utils.NormalizeBranch(c.FormValue("branch")),
				AuthMode:      strings.TrimSpace(c.FormValue("auth_mode")),
				Token:         strings.TrimSpace(c.FormValue("token")),
				WebhookSecret: utils.RandomSecret(),
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
