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
	Type      string `json:"type"`                // ready, text_delta, thinking_delta, tool_start, tool_end, approval_required, agent_end, error
	MessageID string `json:"messageId,omitempty"` // correlation ID
	Delta     string `json:"delta,omitempty"`     // streaming text or thinking chunk
	ToolName  string `json:"toolName,omitempty"`  // active tool
	ToolArgs  string `json:"toolArgs,omitempty"`  // arguments JSON string
	Result    string `json:"result,omitempty"`    // tool execution output
	IsError   bool   `json:"isError,omitempty"`   // whether tool execution failed
	Approval  *ApprovalRequest `json:"approval,omitempty"` // pending confirmation details
	Timestamp int64  `json:"timestamp"`
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

	client, err := omprpc.Start(ctx, cmd, omprpc.WithHostTools(tools...))
	if err != nil {
		return nil, fmt.Errorf("agent: failed to start omp rpc: %w", err)
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

		case omprpc.ToolExecutionEndEvent:
			rawResult, _ := json.Marshal(v.Result)
			m.broadcast(AgentEvent{
				Type:     "tool_end",
				ToolName: v.ToolName,
				Result:   string(rawResult),
				IsError:  v.IsError != nil && *v.IsError,
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
