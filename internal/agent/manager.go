package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"

	"panel/internal/agent/omprpc"
	"panel/internal/db"
	"panel/internal/phphosting"
)

// AutonomyMode defines whether mutating actions require user confirmation.
type AutonomyMode string

const (
	AutonomySafe       AutonomyMode = "safe"       // Mutating actions require confirmation
	AutonomyAutonomous AutonomyMode = "autonomous" // Fully autonomous execution
)

// AgentEvent is sent over SSE to connected frontend clients.
type AgentEvent struct {
	Type         string           `json:"type"`                // ready, text_delta, thinking_delta, tool_start, tool_end, approval_required, agent_end, oauth_open_url, oauth_input_required, error
	MessageID    string           `json:"messageId,omitempty"` // correlation ID
	Delta        string           `json:"delta,omitempty"`     // streaming text or thinking chunk
	ToolName     string           `json:"toolName,omitempty"`  // active tool
	ToolArgs     string           `json:"toolArgs,omitempty"`  // arguments JSON string
	Result       string           `json:"result,omitempty"`    // tool execution output
	IsError      bool             `json:"isError,omitempty"`   // whether tool execution failed
	Approval     *ApprovalRequest `json:"approval,omitempty"`  // pending confirmation details
	URL          string           `json:"url,omitempty"`       // OAuth URL to open in browser
	Instructions string           `json:"instructions,omitempty"`
	Token        string           `json:"token,omitempty"`
	Placeholder  string           `json:"placeholder,omitempty"`
	Timestamp    int64            `json:"timestamp"`
}

// ApprovalRequest represents a pending confirmation for safe mode.
type ApprovalRequest struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Message string `json:"message"`
	Tool    string `json:"tool,omitempty"`
}

// Manager orchestrates the headless omp RPC engine and bridges it to NextDeploy.
type Manager struct {
	DB   *db.Store
	PHP  *phphosting.Manager
	Root string

	mu           sync.RWMutex
	client       *omprpc.Client
	autonomyMode AutonomyMode
	activePrompt atomic.Bool

	// Pending confirmations for safe mode
	confirmations map[string]chan bool

	// Pending inputs for OAuth and extension UI
	pendingInputs map[string]chan string

	// OMP available slash commands
	availableCommands []omprpc.AvailableSlashCommand

	// SSE subscriber channels
	subMu       sync.RWMutex
	subscribers map[chan AgentEvent]struct{}

	ompPath string
}

// NewManager creates a new agent manager instance.
func NewManager(database *db.Store, phpMgr *phphosting.Manager, root string) *Manager {
	return &Manager{
		DB:            database,
		PHP:           phpMgr,
		Root:          root,
		autonomyMode:  AutonomySafe,
		confirmations: make(map[string]chan bool),
		pendingInputs: make(map[string]chan string),
		subscribers:   make(map[chan AgentEvent]struct{}),
		ompPath:       findOMPBinary(),
	}
}

func findOMPBinary() string {
	if env := os.Getenv("OMP_BIN"); env != "" {
		if fi, err := os.Stat(env); err == nil && !fi.IsDir() {
			return env
		}
	}
	candidates := []string{
		"/usr/local/bin/omp",
		"/usr/bin/omp",
		os.Getenv("HOME") + "/.local/bin/omp",
		os.Getenv("HOME") + "/.nvm/versions/node/current/bin/omp",
	}
	for _, c := range candidates {
		if fi, err := os.Stat(c); err == nil && !fi.IsDir() {
			return c
		}
	}
	if p, err := exec.LookPath("omp"); err == nil {
		return p
	}
	return "omp"
}

// Subscribe returns a channel of events and a cancel function.
func (m *Manager) Subscribe() (chan AgentEvent, func()) {
	ch := make(chan AgentEvent, 128)
	m.subMu.Lock()
	m.subscribers[ch] = struct{}{}
	m.subMu.Unlock()

	cancel := func() {
		m.subMu.Lock()
		delete(m.subscribers, ch)
		close(ch)
		m.subMu.Unlock()
	}
	return ch, cancel
}

func (m *Manager) broadcast(ev AgentEvent) {
	ev.Timestamp = time.Now().UnixMilli()
	m.subMu.RLock()
	defer m.subMu.RUnlock()
	for ch := range m.subscribers {
		select {
		case ch <- ev:
		default:
		}
	}
}

// SetAutonomyMode changes safe vs autonomous behavior.
func (m *Manager) SetAutonomyMode(mode AutonomyMode) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.autonomyMode = mode
}

// GetAutonomyMode returns the current safety mode.
func (m *Manager) GetAutonomyMode() AutonomyMode {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.autonomyMode
}

// EnsureStarted starts omp in RPC-UI mode if not already running.
func (m *Manager) EnsureStarted(ctx context.Context) (*omprpc.Client, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.client != nil && m.client.Err() == nil {
		return m.client, nil
	}

	tools := m.buildHostTools()
	cmd := exec.Command(m.ompPath, "--mode", "rpc-ui", "--no-session")
	cmd.Dir = m.Root
	cmd.Stderr = os.Stderr

	env := os.Environ()
	hasAnyKey := false
	if m.DB != nil {
		keys := []struct {
			setting string
			envVar  string
		}{
			{"ai_openai_api_key", "OPENAI_API_KEY"},
			{"ai_anthropic_api_key", "ANTHROPIC_API_KEY"},
			{"ai_deepseek_api_key", "DEEPSEEK_API_KEY"},
			{"ai_gemini_api_key", "GEMINI_API_KEY"},
			{"ai_openrouter_api_key", "OPENROUTER_API_KEY"},
			{"ai_groq_api_key", "GROQ_API_KEY"},
			{"ai_xai_api_key", "XAI_API_KEY"},
			{"ai_zai_api_key", "ZAI_API_KEY"},
			{"ai_zhipu_api_key", "ZHIPU_API_KEY"},
			{"ai_custom_api_key", "CUSTOM_API_KEY"},
			{"ai_custom_base_url", "OPENAI_BASE_URL"},
			{"ai_ollama_host", "OLLAMA_HOST"},
		}
		for _, item := range keys {
			if k := m.DB.GetSetting(ctx, item.setting); k != "" {
				env = append(env, item.envVar+"="+k)
				hasAnyKey = true
			}
		}
	}
	// Bootstrap key so omp doesn't exit when no provider is authenticated yet
	if !hasAnyKey && os.Getenv("ANTHROPIC_API_KEY") == "" && os.Getenv("OPENAI_API_KEY") == "" {
		env = append(env, "ANTHROPIC_API_KEY=none")
	}
	cmd.Env = env

	client, err := omprpc.Start(ctx, cmd, omprpc.WithHostTools(tools...))
	if err != nil {
		return nil, fmt.Errorf("agent: failed to start omp rpc: %w", err)
	}

	if m.DB != nil {
		activeProvider := m.DB.GetSetting(ctx, "ai_active_provider")
		activeModel := m.DB.GetSetting(ctx, "ai_active_model")
		if activeModel != "" {
			_, _ = client.SetModel(ctx, omprpc.SetModelCommand{
				Provider: activeProvider,
				ModelID:  activeModel,
			})
		}
		thinkingLevel := m.DB.GetSetting(ctx, "ai_thinking_level")
		if thinkingLevel != "" {
			_ = client.SetThinkingLevel(ctx, omprpc.SetThinkingLevelCommand{
				Level: omprpc.ThinkingLevel(thinkingLevel),
			})
		}
	}

	m.client = client
	go m.processEvents(client)

	m.broadcast(AgentEvent{Type: "ready", Delta: "OMP Engine initialized successfully"})
	return client, nil
}

// processEvents consumes frames from the omp RPC engine.
func (m *Manager) processEvents(client *omprpc.Client) {
	defer client.Close()

	for frame := range client.Frames() {
		switch v := frame.Value.(type) {
		case omprpc.MessageUpdateEvent:
			m.handleMessageUpdate(v)

		case omprpc.ToolExecutionStartEvent:
			rawArgs, _ := json.Marshal(v.Args)
			m.broadcast(AgentEvent{
				Type:     "tool_start",
				ToolName: v.ToolName,
				ToolArgs: string(rawArgs),
			})

		case omprpc.ToolExecutionUpdateEvent:
			rawPartial, _ := json.Marshal(v.PartialResult)
			m.broadcast(AgentEvent{
				Type:     "tool_update",
				ToolName: v.ToolName,
				Result:   string(rawPartial),
			})

		case omprpc.ToolExecutionEndEvent:
			rawResult, _ := json.Marshal(v.Result)
			m.broadcast(AgentEvent{
				Type:     "tool_end",
				ToolName: v.ToolName,
				Result:   string(rawResult),
				IsError:  v.IsError != nil && *v.IsError,
			})

		case omprpc.AvailableCommandsUpdateEvent:
			m.mu.Lock()
			m.availableCommands = v.Commands
			m.mu.Unlock()
			m.broadcast(AgentEvent{
				Type: "commands_update",
			})

		case omprpc.ExtensionUiRequest:
			m.handleExtensionUI(client, v)

		case omprpc.PromptResultEvent:
			m.activePrompt.Store(false)
			m.broadcast(AgentEvent{Type: "agent_end"})

		case omprpc.AgentEndEvent:
			m.activePrompt.Store(false)
			m.broadcast(AgentEvent{Type: "agent_end"})
		}
	}
}

func (m *Manager) handleMessageUpdate(v omprpc.MessageUpdateEvent) {
	switch ev := v.AssistantMessageEvent.Value.(type) {
	case omprpc.AssistantTextDeltaEvent:
		m.broadcast(AgentEvent{Type: "text_delta", Delta: ev.Delta})
	case omprpc.AssistantThinkingDeltaEvent:
		m.broadcast(AgentEvent{Type: "thinking_delta", Delta: ev.Delta})
	}
}

func (m *Manager) handleExtensionUI(client *omprpc.Client, req omprpc.ExtensionUiRequest) {
	switch ui := req.Value.(type) {
	case omprpc.ConfirmUiRequest:
		m.mu.RLock()
		mode := m.autonomyMode
		m.mu.RUnlock()

		// In Autonomous Mode, auto-approve
		if mode == AutonomyAutonomous {
			_ = client.Send(omprpc.ConfirmUiResponse{ID: ui.ID, Confirmed: true})
			return
		}

		// In Safe Mode, broadcast approval request to UI and await response
		ch := make(chan bool, 1)
		m.mu.Lock()
		m.confirmations[ui.ID] = ch
		m.mu.Unlock()

		m.broadcast(AgentEvent{
			Type: "approval_required",
			Approval: &ApprovalRequest{
				ID:      ui.ID,
				Title:   ui.Title,
				Message: ui.Message,
			},
			Timestamp: time.Now().UnixMilli(),
		})

		go func(id string, c chan bool) {
			select {
			case approved := <-c:
				_ = client.Send(omprpc.ConfirmUiResponse{ID: id, Confirmed: approved})
			case <-time.After(5 * time.Minute):
				_ = client.Send(omprpc.ConfirmUiResponse{ID: id, Confirmed: false})
			}
			m.mu.Lock()
			delete(m.confirmations, id)
			m.mu.Unlock()
		}(ui.ID, ch)

	case omprpc.OpenUrlUiRequest:
		instructions := ""
		if ui.Instructions != nil {
			instructions = *ui.Instructions
		}
		m.broadcast(AgentEvent{
			Type:         "oauth_open_url",
			URL:          ui.URL,
			Instructions: instructions,
			Token:        ui.ID,
			Timestamp:    time.Now().UnixMilli(),
		})

	case omprpc.InputUiRequest:
		ch := make(chan string, 1)
		m.mu.Lock()
		m.pendingInputs[ui.ID] = ch
		m.mu.Unlock()

		placeholder := ""
		if ui.Placeholder != nil {
			placeholder = *ui.Placeholder
		}

		m.broadcast(AgentEvent{
			Type:        "oauth_input_required",
			Token:       ui.ID,
			Delta:       ui.Title,
			Placeholder: placeholder,
			Timestamp:   time.Now().UnixMilli(),
		})

		go func(id string, c chan string) {
			select {
			case val := <-c:
				_ = client.Send(omprpc.ValueUiResponse{ID: id, Value: val})
			case <-time.After(10 * time.Minute):
				_ = client.Send(omprpc.CancelUiResponse{ID: id})
			}
			m.mu.Lock()
			delete(m.pendingInputs, id)
			m.mu.Unlock()
		}(ui.ID, ch)
	}
}

// SubmitInput resolves a pending extension UI input request (such as an OAuth redirect code).
func (m *Manager) SubmitInput(id string, value string) bool {
	m.mu.Lock()
	ch, exists := m.pendingInputs[id]
	m.mu.Unlock()
	if !exists {
		return false
	}
	select {
	case ch <- value:
		return true
	default:
		return false
	}
}

// ConfirmApproval resolves a pending tool execution approval.
func (m *Manager) ConfirmApproval(id string, confirmed bool) bool {
	m.mu.Lock()
	ch, ok := m.confirmations[id]
	if ok {
		delete(m.confirmations, id)
	}
	m.mu.Unlock()

	if ok {
		select {
		case ch <- confirmed:
			return true
		default:
		}
	}
	return false
}

// SendPrompt feeds a task into the active omp RPC agent.
func (m *Manager) SendPrompt(ctx context.Context, message string) error {
	client, err := m.EnsureStarted(ctx)
	if err != nil {
		return err
	}

	m.activePrompt.Store(true)
	reqID := fmt.Sprintf("p_%d", time.Now().UnixNano())
	ctxWithID := omprpc.WithRequestID(ctx, reqID)

	_, err = client.Prompt(ctxWithID, omprpc.PromptCommand{
		Message: message,
	})
	if err != nil {
		m.activePrompt.Store(false)
		return fmt.Errorf("agent: prompt failed: %w", err)
	}
	return nil
}

// Abort cancels the active agent turn.
func (m *Manager) Abort(ctx context.Context) error {
	m.mu.RLock()
	client := m.client
	m.mu.RUnlock()

	if client != nil {
		m.activePrompt.Store(false)
		return client.Abort(ctx)
	}
	return nil
}

// GetStatus returns the operational state of the agent engine.
func (m *Manager) GetStatus() map[string]any {
	m.mu.RLock()
	running := m.client != nil && m.client.Err() == nil
	mode := m.autonomyMode
	m.mu.RUnlock()

	return map[string]any{
		"running":      running,
		"autonomyMode": string(mode),
		"active":       m.activePrompt.Load(),
		"binaryPath":   m.ompPath,
	}
}

// GetModels queries available models from omp and returns them along with the active model.
func (m *Manager) GetModels(ctx context.Context) ([]omprpc.ModelInfo, string, error) {
	client, err := m.EnsureStarted(ctx)
	if err != nil {
		return nil, "", err
	}

	models, err := client.GetAvailableModels(ctx)
	if err != nil {
		return nil, "", err
	}

	activeModel := ""
	if m.DB != nil {
		activeModel = m.DB.GetSetting(ctx, "ai_active_model")
	}
	return models, activeModel, nil
}

// SetModel selects the active AI model in omp and saves it in settings.
func (m *Manager) SetModel(ctx context.Context, provider, modelID string) error {
	client, err := m.EnsureStarted(ctx)
	if err != nil {
		return err
	}

	_, err = client.SetModel(ctx, omprpc.SetModelCommand{
		Provider: provider,
		ModelID:  modelID,
	})
	if err != nil {
		return err
	}

	if m.DB != nil {
		_ = m.DB.SetSetting(ctx, "ai_active_provider", provider)
		_ = m.DB.SetSetting(ctx, "ai_active_model", modelID)
	}
	return nil
}

// Restart stops the active client so the next operation spawns with updated environment.
func (m *Manager) Restart(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.client != nil {
		_ = m.client.Close()
		m.client = nil
	}
	return nil
}

// GetLoginProviders queries omp for available OAuth login providers and their status.
func (m *Manager) GetLoginProviders(ctx context.Context) ([]omprpc.LoginProvider, error) {
	client, err := m.EnsureStarted(ctx)
	if err != nil {
		return nil, err
	}
	return client.GetLoginProviders(ctx)
}

// Login triggers an OAuth login flow for the given provider via omp.
func (m *Manager) Login(ctx context.Context, providerID string) (string, error) {
	client, err := m.EnsureStarted(ctx)
	if err != nil {
		return "", err
	}
	return client.Login(ctx, omprpc.LoginCommand{ProviderID: providerID})
}

// GetState returns a snapshot of omp session state (current model, thinkingLevel, etc.).
func (m *Manager) GetState(ctx context.Context) (omprpc.SessionState, error) {
	client, err := m.EnsureStarted(ctx)
	if err != nil {
		return omprpc.SessionState{}, err
	}
	return client.GetState(ctx)
}

// SetThinkingLevel updates the thinking level in omp.
func (m *Manager) SetThinkingLevel(ctx context.Context, level string) error {
	client, err := m.EnsureStarted(ctx)
	if err != nil {
		return err
	}
	return client.SetThinkingLevel(ctx, omprpc.SetThinkingLevelCommand{
		Level: omprpc.ThinkingLevel(level),
	})
}

// GetAvailableCommands returns the list of slash commands registered with omp.
func (m *Manager) GetAvailableCommands() []omprpc.AvailableSlashCommand {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.availableCommands
}

// Close gracefully stops the omp subprocess.
func (m *Manager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.client != nil {
		err := m.client.Close()
		m.client = nil
		return err
	}
	return nil
}
