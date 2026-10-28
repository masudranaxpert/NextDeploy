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
