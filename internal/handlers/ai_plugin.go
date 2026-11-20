package handlers

import (
	"strings"

	"panel/internal/agent"

	"github.com/gofiber/fiber/v2"
)

// RequireAIMiddleware answers every AI request with a clear message while the optional AI
// plugin is off or not downloaded yet. It runs after the auth middlewares.
func (p *Panel) RequireAIMiddleware(c *fiber.Ctx) error {
	if p.Copilot == nil || !p.Copilot.Available(c.UserContext()) {
		st := agent.PluginStatus{}
		if p.Copilot != nil {
			st = p.Copilot.PluginStatus(c.UserContext())
		}
		msg := agent.ErrAINotInstalled.Error()
		if st.Installing {
			msg = "AI is being installed; try again when the download finishes"
		}
		return c.Status(fiber.StatusConflict).JSON(fiber.Map{
			"ok": false, "error": msg, "ai_installed": false, "plugin": st,
		})
	}
	return c.Next()
}

// GET /api/v1/ai/plugin
func (p *Panel) AIPluginStatus(c *fiber.Ctx) error {
	if p.Copilot == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"ok": false, "error": "AI module unavailable"})
	}
	return c.JSON(fiber.Map{"ok": true, "plugin": p.Copilot.PluginStatus(c.UserContext())})
}

// POST /api/v1/ai/plugin/enable  {"version": "latest" | "v18.13.2"}
func (p *Panel) AIPluginEnable(c *fiber.Ctx) error {
	if p.Copilot == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"ok": false, "error": "AI module unavailable"})
	}
	var req struct {
		Version string `json:"version"`
	}
	_ = c.BodyParser(&req)
	if req.Version == "" {
		req.Version = strings.TrimSpace(c.FormValue("version"))
	}
	ctx := c.UserContext()
	if err := p.Copilot.EnableAI(ctx, req.Version); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"ok": false, "error": err.Error()})
	}
	p.RecordAuditLog(c, "ai.enable", "ai_plugin", "omp", strings.TrimSpace(req.Version))
	return c.JSON(fiber.Map{"ok": true, "plugin": p.Copilot.PluginStatus(ctx)})
}

// POST /api/v1/ai/plugin/disable
func (p *Panel) AIPluginDisable(c *fiber.Ctx) error {
	if p.Copilot == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"ok": false, "error": "AI module unavailable"})
	}
	var req struct {
		Purge bool `json:"purge"`
	}
	_ = c.BodyParser(&req)
	ctx := c.UserContext()
	if err := p.Copilot.DisableAI(ctx, req.Purge); err != nil {
		return c.Status(fiber.StatusConflict).JSON(fiber.Map{"ok": false, "error": err.Error()})
	}
	detail := ""
	if req.Purge {
		detail = "purge"
	}
	p.RecordAuditLog(c, "ai.disable", "ai_plugin", "omp", detail)
	return c.JSON(fiber.Map{"ok": true, "plugin": p.Copilot.PluginStatus(ctx)})
}
