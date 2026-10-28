package handlers

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"time"

	"panel/internal/agent"

	"github.com/gofiber/fiber/v2"
)

// GET /api/v1/copilot/status
func (p *Panel) CopilotStatus(c *fiber.Ctx) error {
	if p.Copilot == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
			"error": "Copilot engine not configured",
		})
	}
	return c.JSON(p.Copilot.GetStatus())
}

// GET /api/v1/copilot/stream (SSE)
func (p *Panel) CopilotStream(c *fiber.Ctx) error {
	if p.Copilot == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
			"error": "Copilot engine not configured",
		})
	}

	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	c.Set("Connection", "keep-alive")
	c.Set("Transfer-Encoding", "chunked")

	c.Context().SetBodyStreamWriter(func(w *bufio.Writer) {
		ch, cancel := p.Copilot.Subscribe()
		defer cancel()

		// Heartbeat ticker to keep SSE connection alive
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				if _, err := fmt.Fprintf(w, ": heartbeat\n\n"); err != nil {
					return
				}
				if err := w.Flush(); err != nil {
					return
				}
			case ev, ok := <-ch:
				if !ok {
					return
				}
				data, err := json.Marshal(ev)
				if err != nil {
					continue
				}
				if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
					return
				}
				if err := w.Flush(); err != nil {
					return
				}
			}
		}
	})

	return nil
}

// POST /api/v1/copilot/prompt
func (p *Panel) CopilotPrompt(c *fiber.Ctx) error {
	if p.Copilot == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
			"error": "Copilot engine not configured",
		})
	}

	var req struct {
		Message string `json:"message"`
	}
	if err := c.BodyParser(&req); err != nil || req.Message == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid message",
		})
	}

	// Launch prompt asynchronously; events will stream via SSE
	go func(msg string) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		_ = p.Copilot.SendPrompt(ctx, msg)
	}(req.Message)

	return c.JSON(fiber.Map{"status": "queued"})
}

// POST /api/v1/copilot/confirm
func (p *Panel) CopilotConfirm(c *fiber.Ctx) error {
	if p.Copilot == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
			"error": "Copilot engine not configured",
		})
	}

	var req struct {
		ID        string `json:"id"`
		Confirmed bool   `json:"confirmed"`
	}
	if err := c.BodyParser(&req); err != nil || req.ID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid confirmation payload",
		})
	}

	resolved := p.Copilot.ConfirmApproval(req.ID, req.Confirmed)
	return c.JSON(fiber.Map{"resolved": resolved})
}

// POST /api/v1/copilot/abort
func (p *Panel) CopilotAbort(c *fiber.Ctx) error {
	if p.Copilot == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
			"error": "Copilot engine not configured",
		})
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := p.Copilot.Abort(ctx); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	return c.JSON(fiber.Map{"status": "aborted"})
}

// POST /api/v1/copilot/mode
func (p *Panel) CopilotSetMode(c *fiber.Ctx) error {
	if p.Copilot == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
			"error": "Copilot engine not configured",
		})
	}

	var req struct {
		Mode string `json:"mode"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid request",
		})
	}

	switch agent.AutonomyMode(req.Mode) {
	case agent.AutonomyAutonomous:
		p.Copilot.SetAutonomyMode(agent.AutonomyAutonomous)
	case agent.AutonomySafe:
		p.Copilot.SetAutonomyMode(agent.AutonomySafe)
	default:
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Mode must be 'safe' or 'autonomous'",
		})
	}

	return c.JSON(fiber.Map{
		"mode": p.Copilot.GetAutonomyMode(),
	})
}

// GET /api/v1/copilot/models
func (p *Panel) CopilotGetModels(c *fiber.Ctx) error {
	if p.Copilot == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "Copilot not configured"})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	models, activeModel, err := p.Copilot.GetModels(ctx)
	thinkingLevel := "low"
	if state, errState := p.Copilot.GetState(ctx); errState == nil {
		if state.Model != nil && state.Model.ID != "" {
			activeModel = state.Model.ID
		}
		if state.ThinkingLevel != nil {
			thinkingLevel = string(*state.ThinkingLevel)
		}
	}

	if err != nil || len(models) == 0 {
		return c.JSON(fiber.Map{
			"active":        p.DB.GetSetting(ctx, "ai_active_model"),
			"thinkingLevel": thinkingLevel,
			"models":        []fiber.Map{},
		})
	}
	return c.JSON(fiber.Map{
		"active":        activeModel,
		"thinkingLevel": thinkingLevel,
		"models":        models,
	})
}

// POST /api/v1/copilot/models
func (p *Panel) CopilotSetModel(c *fiber.Ctx) error {
	if p.Copilot == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "Copilot not configured"})
	}
	var req struct {
		Provider string `json:"provider"`
		ModelID  string `json:"modelId"`
	}
	if err := c.BodyParser(&req); err != nil || req.ModelID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid model request"})
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := p.Copilot.SetModel(ctx, req.Provider, req.ModelID); err != nil {
		_ = p.DB.SetSetting(ctx, "ai_active_provider", req.Provider)
		_ = p.DB.SetSetting(ctx, "ai_active_model", req.ModelID)
	}
	return c.JSON(fiber.Map{"ok": true, "modelId": req.ModelID})
}

// GET /api/v1/copilot/providers
func (p *Panel) CopilotGetProviders(c *fiber.Ctx) error {
	ctx := c.Context()
	maskKey := func(key string) string {
		if len(key) <= 8 {
			if len(key) > 0 {
				return "••••••••"
			}
			return ""
		}
		return key[:4] + "••••" + key[len(key)-4:]
	}

	loginProviders := []fiber.Map{}
	if p.Copilot != nil {
		ctxTimeout, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if provs, err := p.Copilot.GetLoginProviders(ctxTimeout); err == nil {
			for _, lp := range provs {
				if lp.Available {
					loginProviders = append(loginProviders, fiber.Map{
						"id":            lp.ID,
						"name":          lp.Name,
						"authenticated": lp.Authenticated,
					})
				}
			}
		}
	}

	return c.JSON(fiber.Map{
		"loginProviders": loginProviders,
		"openai": fiber.Map{
			"configured": p.DB.GetSetting(ctx, "ai_openai_api_key") != "",
			"maskedKey":  maskKey(p.DB.GetSetting(ctx, "ai_openai_api_key")),
		},
		"anthropic": fiber.Map{
			"configured": p.DB.GetSetting(ctx, "ai_anthropic_api_key") != "",
			"maskedKey":  maskKey(p.DB.GetSetting(ctx, "ai_anthropic_api_key")),
		},
		"deepseek": fiber.Map{
			"configured": p.DB.GetSetting(ctx, "ai_deepseek_api_key") != "",
			"maskedKey":  maskKey(p.DB.GetSetting(ctx, "ai_deepseek_api_key")),
		},
		"gemini": fiber.Map{
			"configured": p.DB.GetSetting(ctx, "ai_gemini_api_key") != "",
			"maskedKey":  maskKey(p.DB.GetSetting(ctx, "ai_gemini_api_key")),
		},
		"ollama": fiber.Map{
			"configured": p.DB.GetSetting(ctx, "ai_ollama_host") != "",
			"host":       p.DB.GetSetting(ctx, "ai_ollama_host"),
		},
	})
}

// POST /api/v1/copilot/login
func (p *Panel) CopilotStartLogin(c *fiber.Ctx) error {
	if p.Copilot == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "Copilot not configured"})
	}
	var req struct {
		ProviderID string `json:"providerId"`
	}
	if err := c.BodyParser(&req); err != nil || req.ProviderID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "providerId is required"})
	}

	go func(providerID string) {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
		defer cancel()
		_, _ = p.Copilot.Login(ctx, providerID)
	}(req.ProviderID)

	return c.JSON(fiber.Map{"ok": true, "message": "Login initiated"})
}

// POST /api/v1/copilot/input
func (p *Panel) CopilotSubmitInput(c *fiber.Ctx) error {
	if p.Copilot == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "Copilot not configured"})
	}
	var req struct {
		ID    string `json:"id"`
		Value string `json:"value"`
	}
	if err := c.BodyParser(&req); err != nil || req.ID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "id and value are required"})
	}

	ok := p.Copilot.SubmitInput(req.ID, req.Value)
	return c.JSON(fiber.Map{"ok": ok})
}

// POST /api/v1/copilot/thinking
func (p *Panel) CopilotSetThinking(c *fiber.Ctx) error {
	if p.Copilot == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "Copilot not configured"})
	}
	var req struct {
		Level string `json:"level"`
	}
	if err := c.BodyParser(&req); err != nil || req.Level == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "level is required"})
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := p.Copilot.SetThinkingLevel(ctx, req.Level); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"ok": true, "level": req.Level})
}

// GET /api/v1/copilot/commands
func (p *Panel) CopilotGetCommands(c *fiber.Ctx) error {
	if p.Copilot == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "Copilot not configured"})
	}
	return c.JSON(fiber.Map{
		"commands": p.Copilot.GetAvailableCommands(),
	})
}

// POST /api/v1/copilot/providers
func (p *Panel) CopilotSaveProviders(c *fiber.Ctx) error {
	var req map[string]string
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid payload"})
	}

	ctx := c.Context()
	for key, val := range req {
		dbKey := "ai_" + key
		_ = p.DB.SetSetting(ctx, dbKey, val)
	}

	if p.Copilot != nil {
		_ = p.Copilot.Restart(ctx)
	}

	return c.JSON(fiber.Map{"ok": true})
}
