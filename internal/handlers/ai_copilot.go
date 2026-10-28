package handlers

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"panel/internal/agent"
	"panel/internal/agent/omprpc"

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

		// Initial connected frame so browser immediately acknowledges SSE open
		_, _ = fmt.Fprintf(w, ": connected\n\n")
		_ = w.Flush()

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
		Images  []struct {
			Data     string `json:"data"` // base64
			MimeType string `json:"mimeType"`
		} `json:"images"`
	}
	if err := c.BodyParser(&req); err != nil || (strings.TrimSpace(req.Message) == "" && len(req.Images) == 0) || len(req.Message) > 256<<10 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid message",
		})
	}
	var images []omprpc.ImageContent
	total := 0
	for _, im := range req.Images {
		if !strings.HasPrefix(im.MimeType, "image/") || len(images) >= 8 {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid image attachment"})
		}
		total += len(im.Data)
		if total > 30<<20 {
			return c.Status(fiber.StatusRequestEntityTooLarge).JSON(fiber.Map{"error": "Images are too large"})
		}
		images = append(images, omprpc.ImageContent{Data: im.Data, MimeType: im.MimeType})
	}

	// Launch prompt asynchronously; events will stream via SSE
	go func(msg string, images []omprpc.ImageContent) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		_ = p.Copilot.SendPrompt(ctx, msg, images)
	}(req.Message, images)

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

	mode, ok := agent.ParseAutonomyMode(req.Mode)
	if !ok {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "mode must be always-ask, always-ask-cached, write or yolo",
		})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := p.Copilot.ApplyAutonomyMode(ctx, mode); err != nil {
		return c.Status(fiber.StatusConflict).JSON(fiber.Map{"error": err.Error()})
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
	if p.DB != nil {
		if saved := p.DB.GetSetting(ctx, "ai_thinking_level"); saved != "" {
			thinkingLevel = saved
		}
	}
	var contextUsage any = nil
	if state, errState := p.Copilot.GetState(ctx); errState == nil {
		if state.Model != nil && state.Model.ID != "" {
			activeModel = state.Model.ID
		}
		if state.ThinkingLevel != nil {
			thinkingLevel = string(*state.ThinkingLevel)
		}
		if state.ContextUsage != nil {
			contextUsage = state.ContextUsage
		}
	}

	availableThinkingLevels := []string{}
	if levels, errLevels := p.Copilot.GetAvailableThinkingLevels(ctx); errLevels == nil {
		availableThinkingLevels = levels
	}

	if err != nil || len(models) == 0 {
		return c.JSON(fiber.Map{
			"active":                  "",
			"thinkingLevel":           thinkingLevel,
			"availableThinkingLevels": availableThinkingLevels,
			"contextUsage":            contextUsage,
			"models":                  []fiber.Map{},
		})
	}
	return c.JSON(fiber.Map{
		"active":                  activeModel,
		"thinkingLevel":           thinkingLevel,
		"availableThinkingLevels": availableThinkingLevels,
		"contextUsage":            contextUsage,
		"models":                  models,
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
		return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{"error": err.Error()})
	}
	thinkingLevel := ""
	if st, err := p.Copilot.GetState(ctx); err == nil && st.ThinkingLevel != nil {
		thinkingLevel = string(*st.ThinkingLevel)
	}

	availableLevels := []string{}
	if levels, errLevels := p.Copilot.GetAvailableThinkingLevels(ctx); errLevels == nil {
		availableLevels = levels
	}
	return c.JSON(fiber.Map{
		"ok":                      true,
		"modelId":                 req.ModelID,
		"thinkingLevel":           thinkingLevel,
		"availableThinkingLevels": availableLevels,
	})
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

	out := fiber.Map{"loginProviders": loginProviders}
	all, _ := p.DB.GetAllSettings(ctx)
	for k, v := range all {
		if v == "" || !strings.HasPrefix(k, "ai_") {
			continue
		}
		name := strings.TrimPrefix(k, "ai_")
		if strings.HasSuffix(name, "_api_key") {
			out[strings.TrimSuffix(name, "_api_key")] = fiber.Map{"configured": true, "maskedKey": maskKey(v)}
		} else if name == "ollama_host" {
			out["ollama"] = fiber.Map{"configured": true, "host": v}
		}
	}
	return c.JSON(out)
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
	if p.DB != nil {
		_ = p.DB.SetSetting(ctx, "ai_thinking_level", req.Level)
	}
	return c.JSON(fiber.Map{"ok": true, "level": req.Level})
}

// GET /api/v1/copilot/thinking/levels
func (p *Panel) CopilotGetThinkingLevels(c *fiber.Ctx) error {
	if p.Copilot == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "Copilot not configured"})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	levels, err := p.Copilot.GetAvailableThinkingLevels(ctx)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{
		"levels": levels,
	})
}

// GET /api/v1/copilot/commands
func (p *Panel) CopilotGetCommands(c *fiber.Ctx) error {
	if p.Copilot == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "Copilot not configured"})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	cmds := p.Copilot.GetAvailableCommands(ctx)
	if cmds == nil {
		cmds = []omprpc.AvailableSlashCommand{}
	}
	return c.JSON(fiber.Map{"commands": cmds})
}

// POST /api/v1/copilot/providers
func (p *Panel) CopilotSaveProviders(c *fiber.Ctx) error {
	var req map[string]string
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid payload"})
	}

	ctx := c.Context()
	for key := range req {
		if !providerSettingPattern.MatchString(key) {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "unknown provider setting: " + key})
		}
	}
	for key, val := range req {
		if err := p.DB.SetSetting(ctx, "ai_"+key, strings.TrimSpace(val)); err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
		}
	}

	if p.Copilot != nil {
		_ = p.Copilot.Restart(ctx)
	}

	return c.JSON(fiber.Map{"ok": true})
}

// Provider credentials/hosts the UI may store; anything else is rejected so the
// endpoint cannot be used to overwrite arbitrary panel settings.
var providerSettingPattern = regexp.MustCompile(`^([a-z0-9][a-z0-9-]{0,40}_api_key|custom_base_url|ollama_host)$`)
var copilotSecretNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`)

type CopilotSecretItem struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Masked string `json:"masked"`
	Length int    `json:"length"`
}

// GET /api/v1/copilot/secrets
func (p *Panel) CopilotGetSecrets(c *fiber.Ctx) error {
	ctx := c.Context()
	all, err := p.DB.GetAllSettings(ctx)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	secrets := make([]CopilotSecretItem, 0)
	for k, v := range all {
		if !strings.HasPrefix(k, "ai_secret_") {
			continue
		}
		name := strings.TrimPrefix(k, "ai_secret_")
		if !copilotSecretNamePattern.MatchString(name) {
			continue
		}
		masked := "••••••••"
		if len(v) > 8 {
			masked = v[:2] + "••••••••" + v[len(v)-2:]
		}
		secrets = append(secrets, CopilotSecretItem{
			Name:   name,
			Value:  v,
			Masked: masked,
			Length: len(v),
		})
	}
	sort.Slice(secrets, func(i, j int) bool {
		return secrets[i].Name < secrets[j].Name
	})

	return c.JSON(fiber.Map{"secrets": secrets})
}

// POST /api/v1/copilot/secrets
func (p *Panel) CopilotSaveSecret(c *fiber.Ctx) error {
	var req struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid payload"})
	}

	name := strings.TrimSpace(req.Name)
	if !copilotSecretNamePattern.MatchString(name) {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Secret name must start with a letter/underscore and contain only letters, numbers, or underscores (max 128 characters)",
		})
	}

	val := strings.TrimSpace(req.Value)
	if val == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Secret value cannot be empty"})
	}

	ctx := c.Context()
	if err := p.DB.SetSetting(ctx, "ai_secret_"+name, val); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	if p.Copilot != nil {
		_ = p.Copilot.Restart(ctx)
	}

	return c.JSON(fiber.Map{"ok": true, "name": name})
}

// DELETE /api/v1/copilot/secrets/:name
func (p *Panel) CopilotDeleteSecret(c *fiber.Ctx) error {
	name := strings.TrimSpace(c.Params("name"))
	if !copilotSecretNamePattern.MatchString(name) {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid secret name"})
	}

	ctx := c.Context()
	if err := p.DB.DeleteSetting(ctx, "ai_secret_"+name); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	if p.Copilot != nil {
		_ = p.Copilot.Restart(ctx)
	}

	return c.JSON(fiber.Map{"ok": true, "name": name})
}

func copilotUnavailable(c *fiber.Ctx) error {
	return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "Copilot not configured"})
}

// GET /api/v1/copilot/workdir
func (p *Panel) CopilotGetWorkDir(c *fiber.Ctx) error {
	if p.Copilot == nil {
		return copilotUnavailable(c)
	}
	return c.JSON(p.Copilot.WorkDir())
}

// POST /api/v1/copilot/workdir {kind: root|site|temp, appId?}
func (p *Panel) CopilotSetWorkDir(c *fiber.Ctx) error {
	if p.Copilot == nil {
		return copilotUnavailable(c)
	}
	var req struct {
		Kind  string `json:"kind"`
		AppID string `json:"appId"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request"})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	wd, err := p.Copilot.SetWorkDir(ctx, req.Kind, req.AppID)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(wd)
}

// POST /api/v1/copilot/session/new
func (p *Panel) CopilotNewSession(c *fiber.Ctx) error {
	if p.Copilot == nil {
		return copilotUnavailable(c)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := p.Copilot.NewSession(ctx); err != nil {
		return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"ok": true})
}

// POST /api/v1/copilot/compact {instructions?}
func (p *Panel) CopilotCompact(c *fiber.Ctx) error {
	if p.Copilot == nil {
		return copilotUnavailable(c)
	}
	var req struct {
		Instructions string `json:"instructions"`
	}
	_ = c.BodyParser(&req)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	res, err := p.Copilot.Compact(ctx, req.Instructions)
	if err != nil && (strings.Contains(err.Error(), "Already compacted") || strings.Contains(err.Error(), "Nothing to compact")) {
		return c.JSON(fiber.Map{"ok": true, "skipped": true})
	}
	if err != nil {
		return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"ok": true, "summary": res.Summary, "tokensBefore": res.TokensBefore})
}

// GET /api/v1/copilot/stats
func (p *Panel) CopilotStats(c *fiber.Ctx) error {
	if p.Copilot == nil {
		return copilotUnavailable(c)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	st, err := p.Copilot.SessionStats(ctx)
	if err != nil {
		return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(st)
}

// POST /api/v1/copilot/queue {message, mode: steer|followUp}
func (p *Panel) CopilotQueue(c *fiber.Ctx) error {
	if p.Copilot == nil {
		return copilotUnavailable(c)
	}
	var req struct {
		Message string `json:"message"`
		Mode    string `json:"mode"`
	}
	if err := c.BodyParser(&req); err != nil || strings.TrimSpace(req.Message) == "" || len(req.Message) > 256<<10 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid message"})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := p.Copilot.QueueMessage(ctx, req.Message, req.Mode == "followUp"); err != nil {
		return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"ok": true})
}

// GET /api/v1/copilot/settings
func (p *Panel) CopilotGetSettings(c *fiber.Ctx) error {
	if p.Copilot == nil {
		return copilotUnavailable(c)
	}
	return c.JSON(p.Copilot.GetEngineSettings(c.Context()))
}

// POST /api/v1/copilot/settings {key: fast_mode|auto_compaction|auto_retry, enabled}
func (p *Panel) CopilotSetSettings(c *fiber.Ctx) error {
	if p.Copilot == nil {
		return copilotUnavailable(c)
	}
	var req struct {
		Key     string `json:"key"`
		Enabled bool   `json:"enabled"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request"})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := p.Copilot.SetEngineSetting(ctx, req.Key, req.Enabled); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(p.Copilot.GetEngineSettings(ctx))
}

// GET /api/v1/copilot/accounts?provider=<id>
func (p *Panel) CopilotGetAccounts(c *fiber.Ctx) error {
	if p.Copilot == nil {
		return copilotUnavailable(c)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	accts, err := p.Copilot.LogoutAccounts(ctx, c.Query("provider"))
	if err != nil {
		return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"accounts": accts})
}

// POST /api/v1/copilot/logout {providerId, credentialId}
func (p *Panel) CopilotLogout(c *fiber.Ctx) error {
	if p.Copilot == nil {
		return copilotUnavailable(c)
	}
	var req struct {
		ProviderID   string `json:"providerId"`
		CredentialID int64  `json:"credentialId"`
	}
	if err := c.BodyParser(&req); err != nil || req.ProviderID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "providerId and credentialId are required"})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := p.Copilot.Logout(ctx, req.ProviderID, req.CredentialID); err != nil {
		return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"ok": true})
}

// GET /api/v1/copilot/files
func (p *Panel) CopilotFiles(c *fiber.Ctx) error {
	if p.Copilot == nil {
		return copilotUnavailable(c)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	files, err := p.Copilot.ListFiles(ctx)
	if err != nil {
		return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"path": p.Copilot.WorkDir().Path, "files": files})
}

// GET /api/v1/copilot/sessions
func (p *Panel) CopilotListSessions(c *fiber.Ctx) error {
	if p.Copilot == nil {
		return copilotUnavailable(c)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	list, err := p.Copilot.ListSessions(ctx)
	if err != nil {
		return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"sessions": list})
}

// POST /api/v1/copilot/sessions/open {path}
func (p *Panel) CopilotOpenSession(c *fiber.Ctx) error {
	if p.Copilot == nil {
		return copilotUnavailable(c)
	}
	var req struct {
		Path string `json:"path"`
	}
	if err := c.BodyParser(&req); err != nil || req.Path == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "path is required"})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := p.Copilot.OpenSession(ctx, req.Path); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"ok": true})
}

// DELETE /api/v1/copilot/sessions {path}
func (p *Panel) CopilotDeleteSession(c *fiber.Ctx) error {
	if p.Copilot == nil {
		return copilotUnavailable(c)
	}
	var req struct {
		Path string `json:"path"`
	}
	if err := c.BodyParser(&req); err != nil || req.Path == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "path is required"})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := p.Copilot.DeleteSession(ctx, req.Path); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"ok": true})
}

// GET /api/v1/copilot/history
func (p *Panel) CopilotHistory(c *fiber.Ctx) error {
	if p.Copilot == nil {
		return copilotUnavailable(c)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	msgs, err := p.Copilot.History(ctx)
	if err != nil {
		return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"messages": msgs})
}

// GET /api/v1/copilot/subagents
func (p *Panel) CopilotSubagents(c *fiber.Ctx) error {
	if p.Copilot == nil {
		return copilotUnavailable(c)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	subs, err := p.Copilot.Subagents(ctx)
	if err != nil {
		return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"subagents": subs})
}

// POST /api/v1/copilot/subagents/cancel {id}
func (p *Panel) CopilotCancelSubagent(c *fiber.Ctx) error {
	if p.Copilot == nil {
		return copilotUnavailable(c)
	}
	var req struct {
		ID string `json:"id"`
	}
	if err := c.BodyParser(&req); err != nil || req.ID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "id is required"})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ok, err := p.Copilot.CancelSubagent(ctx, req.ID)
	if err != nil {
		return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"cancelled": ok})
}

// POST /api/v1/copilot/upload (multipart "file")
func (p *Panel) CopilotUpload(c *fiber.Ctx) error {
	if p.Copilot == nil {
		return copilotUnavailable(c)
	}
	fh, err := c.FormFile("file")
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "file is required"})
	}
	f, err := fh.Open()
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}
	defer f.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	path, n, err := p.Copilot.SaveUpload(ctx, fh.Filename, f)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"path": path, "name": fh.Filename, "size": n})
}

// POST /api/v1/copilot/answers {id, cancel?, answers:[{id, selectedOptions, customInput}]}
func (p *Panel) CopilotAnswers(c *fiber.Ctx) error {
	if p.Copilot == nil {
		return copilotUnavailable(c)
	}
	var req struct {
		ID      string `json:"id"`
		Cancel  bool   `json:"cancel"`
		Answers []struct {
			ID              string   `json:"id"`
			SelectedOptions []string `json:"selectedOptions"`
			CustomInput     *string  `json:"customInput"`
		} `json:"answers"`
	}
	if err := c.BodyParser(&req); err != nil || req.ID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "id is required"})
	}
	var answers []omprpc.AskAnswer
	if !req.Cancel {
		answers = []omprpc.AskAnswer{}
		for _, a := range req.Answers {
			sel := a.SelectedOptions
			if sel == nil {
				sel = []string{}
			}
			answers = append(answers, omprpc.AskAnswer{ID: a.ID, SelectedOptions: sel, CustomInput: a.CustomInput})
		}
	}
	return c.JSON(fiber.Map{"ok": p.Copilot.SubmitAnswers(req.ID, answers)})
}

// POST /api/v1/copilot/fork
func (p *Panel) CopilotFork(c *fiber.Ctx) error {
	if p.Copilot == nil {
		return copilotUnavailable(c)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := p.Copilot.Fork(ctx); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"ok": true})
}

// POST /api/v1/copilot/handoff {instructions?}
func (p *Panel) CopilotHandoff(c *fiber.Ctx) error {
	if p.Copilot == nil {
		return copilotUnavailable(c)
	}
	var req struct {
		Instructions string `json:"instructions"`
	}
	_ = c.BodyParser(&req)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	saved, err := p.Copilot.Handoff(ctx, req.Instructions)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"ok": true, "savedPath": saved})
}

// GET /api/v1/copilot/export
func (p *Panel) CopilotExport(c *fiber.Ctx) error {
	if p.Copilot == nil {
		return copilotUnavailable(c)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	html, err := p.Copilot.ExportHTML(ctx)
	if err != nil {
		return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{"error": err.Error()})
	}
	c.Set("Content-Type", "text/html; charset=utf-8")
	c.Set("Content-Disposition", `attachment; filename="conversation.html"`)
	return c.Send(html)
}
