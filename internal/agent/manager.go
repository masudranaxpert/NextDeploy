package agent

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"panel/internal/agent/omprpc"
	"panel/internal/db"
	"panel/internal/phphosting"
)

// AutonomyMode is omp's own `tools.approvalMode`; the panel does not invent
// a second approval system. Legacy "safe"/"autonomous" map onto it.
type AutonomyMode string

const (
	AutonomyAskAlways  AutonomyMode = "always-ask"        // confirm every tool call
	AutonomyAskCached  AutonomyMode = "always-ask-cached" // confirm once per tool/path, then remember
	AutonomySafe       AutonomyMode = "write"             // confirm changes, read freely
	AutonomyAutonomous AutonomyMode = "yolo"              // never ask
)

// ParseAutonomyMode accepts omp's values and the legacy panel names.
func ParseAutonomyMode(s string) (AutonomyMode, bool) {
	switch s {
	case "always-ask", "always-ask-cached", "write", "yolo":
		return AutonomyMode(s), true
	case "safe":
		return AutonomySafe, true
	case "autonomous":
		return AutonomyAutonomous, true
	}
	return "", false
}

// AgentEvent is sent over SSE to connected frontend clients.
type AgentEvent struct {
	Type          string           `json:"type"`                // ready, text_delta, thinking_delta, tool_start, tool_end, approval_required, agent_end, oauth_open_url, oauth_input_required, error, commands_update, thinking_level_changed
	MessageID     string           `json:"messageId,omitempty"` // correlation ID
	Delta         string           `json:"delta,omitempty"`     // streaming text or thinking chunk
	ThinkingLevel string           `json:"thinkingLevel,omitempty"`
	ToolName      string           `json:"toolName,omitempty"` // active tool
	ToolArgs      string           `json:"toolArgs,omitempty"` // arguments JSON string
	Result        string           `json:"result,omitempty"`   // tool execution output
	IsError       bool             `json:"isError,omitempty"`  // whether tool execution failed
	Approval      *ApprovalRequest `json:"approval,omitempty"` // pending confirmation details
	URL           string           `json:"url,omitempty"`      // OAuth URL to open in browser
	Instructions  string           `json:"instructions,omitempty"`
	Token         string           `json:"token,omitempty"`
	Placeholder   string           `json:"placeholder,omitempty"`
	ToolCallID    string           `json:"toolCallId,omitempty"`
	Level         string           `json:"level,omitempty"`  // notice severity
	Data          any              `json:"data,omitempty"`   // structured payload (state, subagents, compaction, retry)
	Intent        string           `json:"intent,omitempty"` // the model's one-line description of a tool call
	Timestamp     int64            `json:"timestamp"`
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

	// Pending multi-question "ask" dialogs
	pendingAsks map[string]chan []omprpc.AskAnswer

	// OMP available slash commands
	availableCommands []omprpc.AvailableSlashCommand

	// SSE subscriber channels
	subMu       sync.RWMutex
	subscribers map[chan AgentEvent]struct{}

	ompPath string

	// DataDir is the panel's persistent volume. omp keeps its logins, skills,
	// settings and conversation history under <DataDir>/omp/agent so they
	// survive container rebuilds. Empty means omp's default (~/.omp/agent).
	dataDir string

	// Idle reaper: omp is stopped when unused (it is resumed from its session
	// file on the next request) so an idle panel costs no memory.
	lastActivity atomic.Int64 // unix ms
	reaperOnce   sync.Once
	stop         chan struct{}

	// needsLogin is true when omp only started thanks to the placeholder key.
	needsLogin bool

	// Conversation titling (see nameSession): the latest prompt, per-session
	// retry state, and the id of the internal /rename turn being awaited.
	titleSeed  string
	titleState map[string]*titleInfo
	renameID   string
	renameDone chan struct{}

	// Working directory the omp subprocess runs in (see workdir.go).
	cwdKind  string // "root" | "site" | "temp"
	cwdAppID string
	tempDir  string
}

// NewManager creates a new agent manager instance.
func NewManager(database *db.Store, phpMgr *phphosting.Manager, root string) *Manager {
	m := &Manager{
		DB:            database,
		PHP:           phpMgr,
		Root:          root,
		autonomyMode:  AutonomySafe,
		confirmations: make(map[string]chan bool),
		pendingInputs: make(map[string]chan string),
		pendingAsks:   make(map[string]chan []omprpc.AskAnswer),
		titleState:    make(map[string]*titleInfo),
		subscribers:   make(map[chan AgentEvent]struct{}),
		ompPath:       findOMPBinary(),
		cwdKind:       "root",
		stop:          make(chan struct{}),
	}
	m.touch()
	if database != nil {
		ctx := context.Background()
		if mode, ok := ParseAutonomyMode(database.GetSetting(ctx, "ai_approval_mode")); ok {
			m.autonomyMode = mode
		}
		switch database.GetSetting(ctx, "ai_workdir_kind") {
		case "site":
			if id := database.GetSetting(ctx, "ai_workdir_app"); ValidAppID(id) {
				if _, err := m.siteDir(id); err == nil {
					m.cwdKind, m.cwdAppID = "site", id
				}
			}
		case "temp":
			m.cwdKind = "temp"
		}
	}
	return m
}

const bootstrapEnv = "ANTHROPIC_API_KEY=nextdeploy-no-credentials"

var appIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)

// ValidAppID reports whether id is a safe single path segment for a workspace.
func ValidAppID(id string) bool {
	return appIDPattern.MatchString(id) && id != "." && id != ".."
}

// siteDir resolves a workspace directory for app id, refusing anything outside Root.
func (m *Manager) siteDir(id string) (string, error) {
	if !ValidAppID(id) {
		return "", fmt.Errorf("invalid app id")
	}
	dir := filepath.Join(m.Root, id)
	if rel, err := filepath.Rel(m.Root, dir); err != nil || rel != id {
		return "", fmt.Errorf("invalid app id")
	}
	fi, err := os.Stat(dir)
	if err != nil || !fi.IsDir() {
		return "", fmt.Errorf("workspace not found: %s", id)
	}
	return dir, nil
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
	ch := make(chan AgentEvent, 512)
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
	critical := ev.Type == "approval_required" || ev.Type == "agent_end" || ev.Type == "error" ||
		ev.Type == "oauth_open_url" || ev.Type == "oauth_input_required"
	m.subMu.RLock()
	defer m.subMu.RUnlock()
	for ch := range m.subscribers {
		select {
		case ch <- ev:
		default:
			if critical {
				select {
				case ch <- ev:
				case <-time.After(2 * time.Second):
				}
			}
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
	m.touch()
	m.reaperOnce.Do(func() { go m.reapIdle() })

	if m.client != nil && m.client.Err() == nil {
		return m.client, nil
	}

	tools := m.buildHostTools()
	cwd, err := m.resolveCwdLocked()
	if err != nil {
		return nil, err
	}
	env := append(os.Environ(), "PI_RPC_EMIT_TITLE=1") // omp suppresses its generated conversation title in RPC mode otherwise
	if dir := m.AgentDir(); m.dataDir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("agent: create omp state dir: %w", err)
		}
		env = append(env, "PI_CODING_AGENT_DIR="+dir)
		// HOME inside the volume too: ~/.ssh keys, ~/.agents/skills, git and docker
		// config the agent sets up survive container rebuilds.
		home := m.HomeDir()
		if err := os.MkdirAll(home, 0o700); err != nil {
			return nil, fmt.Errorf("agent: create omp home: %w", err)
		}
		env = append(env, "HOME="+home)
	}
	if m.DB != nil {
		if all, err := m.DB.GetAllSettings(ctx); err == nil {
			env = append(env, ProviderEnv(all)...)
		}
	}

	start := func(env []string, extra ...string) (*omprpc.Client, error) {
		args := []string{"--mode", "rpc-ui", "--cwd", cwd}
		if overlay, err := m.writeOverlay(); err == nil {
			args = append(args, "--config", overlay)
		}
		if path, err := m.writeIdentity(); err == nil {
			args = append(args, "--append-system-prompt", path)
		}
		cmd := exec.Command(m.ompPath, append(args, extra...)...)
		cmd.Dir = cwd
		cmd.Stderr = os.Stderr
		cmd.Env = env
		return omprpc.Start(ctx, cmd, omprpc.WithHostTools(tools...))
	}
	launch := func(env []string) (*omprpc.Client, error) {
		if m.cwdKind == "temp" {
			return start(env, "--no-session") // scratch dir: nothing worth keeping
		}
		// Resume the last conversation for this directory. omp refuses to start
		// when that conversation's model is no longer available (e.g. the login
		// was removed), so fall back to a fresh conversation instead of failing.
		if c, err := start(env, "--continue"); err == nil {
			return c, nil
		}
		return start(env)
	}
	client, err := launch(env)
	m.needsLogin = false
	if err != nil {
		// With no credentials at all omp exits ("No default model selected")
		// before the RPC handshake, which would also hide the login flow. A
		// placeholder key lets it start; GetLoginProviders never reports that
		// placeholder as a real login.
		client, err = launch(append(env, bootstrapEnv))
		m.needsLogin = err == nil
	}
	if err != nil {
		return nil, fmt.Errorf("agent: failed to start omp rpc: %w", err)
	}

	// Surface subagent progress and apply persisted engine toggles.
	_, _ = client.SetSubagentSubscription(ctx, omprpc.SetSubagentSubscriptionCommand{Level: omprpc.SubagentSubscriptionLevelProgress})
	if m.DB != nil {
		if v := m.DB.GetSetting(ctx, "ai_fast_mode"); v != "" {
			_, _ = client.SetFastMode(ctx, omprpc.SetFastModeCommand{Enabled: v == "1"})
		}
		if v := m.DB.GetSetting(ctx, "ai_auto_compaction"); v != "" {
			_ = client.SetAutoCompaction(ctx, omprpc.SetAutoCompactionCommand{Enabled: v == "1"})
		}
		if v := m.DB.GetSetting(ctx, "ai_auto_retry"); v != "" {
			_ = client.SetAutoRetry(ctx, omprpc.SetAutoRetryCommand{Enabled: v == "1"})
		}
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
	go m.pushState(client)

	// Eagerly prefetch available slash commands catalog from omp
	go func() {
		bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if cmds, err := client.GetAvailableCommands(bgCtx); err == nil && len(cmds) > 0 {
			m.mu.Lock()
			m.availableCommands = cmds
			m.mu.Unlock()
			m.broadcast(AgentEvent{Type: "commands_update"})
		}
	}()

	m.broadcast(AgentEvent{Type: "ready", Delta: "OMP Engine initialized successfully"})
	return client, nil
}

// processEvents consumes frames from the omp RPC engine.
func (m *Manager) processEvents(client *omprpc.Client) {
	defer client.Close()

	for frame := range client.Frames() {
		m.touch()
		switch v := frame.Value.(type) {
		case omprpc.MessageUpdateEvent:
			m.handleMessageUpdate(v)

		case omprpc.ToolExecutionStartEvent:
			m.broadcast(AgentEvent{
				Type:       "tool_start",
				ToolCallID: v.ToolCallID,
				ToolName:   v.ToolName,
				ToolArgs:   string(v.Args),
				Intent:     derefStr(v.Intent),
			})

		case omprpc.ToolExecutionUpdateEvent:
			m.broadcast(AgentEvent{
				Type:       "tool_update",
				ToolCallID: v.ToolCallID,
				ToolName:   v.ToolName,
				Result:     string(v.PartialResult),
			})

		case omprpc.ToolExecutionEndEvent:
			m.broadcast(AgentEvent{
				Type:       "tool_end",
				ToolCallID: v.ToolCallID,
				ToolName:   v.ToolName,
				Result:     string(v.Result),
				IsError:    v.IsError != nil && *v.IsError,
			})
			if strings.Contains(strings.ToLower(v.ToolName), "todo") {
				go m.pushState(client)
			}

		case omprpc.AvailableCommandsUpdateEvent:
			m.mu.Lock()
			m.availableCommands = v.Commands
			m.mu.Unlock()
			m.broadcast(AgentEvent{Type: "commands_update"})

		case omprpc.ThinkingLevelChangedEvent:
			lvl := ""
			if v.ThinkingLevel != nil {
				lvl = string(*v.ThinkingLevel)
			} else if v.Configured != nil {
				lvl = string(*v.Configured)
			}
			m.broadcast(AgentEvent{Type: "thinking_level_changed", ThinkingLevel: lvl})

		case omprpc.ExtensionUiRequest:
			m.handleExtensionUI(client, v)

		case omprpc.PromptResultEvent:
			if !v.AgentInvoked || v.Status != omprpc.PromptStatusCompleted {
				m.activePrompt.Store(false)
			}
			if v.ID != nil && m.finishRename(*v.ID) {
				break // our own internal /rename, nothing for the UI to show
			}
			if v.Error != nil {
				m.broadcast(AgentEvent{Type: "error", Delta: v.Error.Message})
			}
			if !v.AgentInvoked {
				m.broadcast(AgentEvent{Type: "agent_end"})
			}

		case omprpc.AgentEndEvent:
			// isTerminal:false means async delivery will resume the session.
			if v.IsTerminal == nil || *v.IsTerminal {
				m.activePrompt.Store(false)
				m.broadcast(AgentEvent{Type: "agent_end"})
				go m.nameSession(client)
				go m.pushState(client)
			}

		case omprpc.NoticeEvent:
			m.broadcast(AgentEvent{Type: "notice", Level: string(v.Level), Delta: v.Message})

		case omprpc.SessionInfoUpdateEvent:
			m.broadcast(AgentEvent{Type: "session_info"})

		case omprpc.CommandOutputEvent:
			if !m.renameActive() {
				m.broadcast(AgentEvent{Type: "command_output", Delta: v.Text})
			}

		case omprpc.ModelChangedEvent, omprpc.ConfigUpdateEvent, omprpc.TodoReminderEvent, omprpc.TodoAutoClearEvent:
			go m.pushState(client)

		case omprpc.AutoCompactionStartEvent:
			m.broadcast(AgentEvent{Type: "compaction_start", Data: map[string]any{"reason": v.Reason, "action": v.Action}})

		case omprpc.AutoCompactionEndEvent:
			msg := ""
			if v.ErrorMessage != nil {
				msg = *v.ErrorMessage
			}
			m.broadcast(AgentEvent{Type: "compaction_end", Data: map[string]any{"aborted": v.Aborted, "willRetry": v.WillRetry, "error": msg}})
			go m.pushState(client)

		case omprpc.RetryFallbackAppliedEvent:
			m.broadcast(AgentEvent{Type: "notice", Level: "warning", Delta: "Switched model: " + v.From + " → " + v.To})

		case omprpc.AutoRetryStartEvent:
			m.broadcast(AgentEvent{Type: "retry_start", Delta: v.ErrorMessage, Data: map[string]any{"attempt": v.Attempt, "maxAttempts": v.MaxAttempts, "delayMs": v.DelayMs}})

		case omprpc.AutoRetryEndEvent:
			final := ""
			if v.FinalError != nil {
				final = *v.FinalError
			}
			m.broadcast(AgentEvent{Type: "retry_end", Delta: final, Data: map[string]any{"success": v.Success, "attempt": v.Attempt}})

		case omprpc.SubagentLifecycleEvent:
			m.broadcast(AgentEvent{Type: "subagent", Data: v.Payload})

		case omprpc.SubagentProgressEvent:
			m.broadcast(AgentEvent{Type: "subagent_progress", Data: v.Payload})
		}
	}
	// An intentional stop (restart, idle reaper) clears m.client first; only a
	// crash leaves it pointing here and needs the UI unblocked.
	m.mu.RLock()
	crashed := m.client == client
	m.mu.RUnlock()
	if crashed {
		m.activePrompt.Store(false)
		m.broadcast(AgentEvent{Type: "engine_exit"})
	}
}

// pushState broadcasts a snapshot of the omp session (model, thinking, context, todos).
func (m *Manager) pushState(client *omprpc.Client) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	st, err := client.GetState(ctx)
	if err != nil {
		return
	}
	data := map[string]any{
		"isStreaming":  st.IsStreaming,
		"isCompacting": st.IsCompacting,
		"fastMode":     st.FastModeEnabled,
		"autoCompact":  st.AutoCompactionEnabled,
		"todoPhases":   st.TodoPhases,
		"contextUsage": st.ContextUsage,
		"queued":       st.QueuedMessageCount,
	}
	if st.Model != nil {
		data["model"] = st.Model.ID
		data["provider"] = st.Model.Provider
	}
	if st.ThinkingLevel != nil {
		data["thinkingLevel"] = string(*st.ThinkingLevel)
	}
	m.broadcast(AgentEvent{Type: "state", Data: data})
}

func (m *Manager) handleMessageUpdate(v omprpc.MessageUpdateEvent) {
	switch ev := v.AssistantMessageEvent.Value.(type) {
	case omprpc.AssistantTextDeltaEvent:
		m.broadcast(AgentEvent{Type: "text_delta", Delta: ev.Delta})
	case omprpc.AssistantThinkingDeltaEvent:
		m.broadcast(AgentEvent{Type: "thinking_delta", Delta: ev.Delta})
	case omprpc.AssistantErrorEvent:
		msg := derefStr(ev.Error.ErrorMessage)
		m.broadcast(AgentEvent{Type: "assistant_error", Level: string(ev.Reason), Delta: msg})
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
			Data:        map[string]any{"secret": ui.Secret != nil && *ui.Secret},
			Timestamp:   time.Now().UnixMilli(),
		})
		m.awaitInput(client, ui.ID, ch, ui.Timeout)

	case omprpc.SelectUiRequest:
		ch := make(chan string, 1)
		m.mu.Lock()
		m.pendingInputs[ui.ID] = ch
		m.mu.Unlock()
		m.broadcast(AgentEvent{
			Type:  "ui_select",
			Token: ui.ID,
			Delta: ui.Title,
			Data:  map[string]any{"options": ui.Options, "details": ui.OptionDetails},
		})
		m.awaitInput(client, ui.ID, ch, ui.Timeout)

	case omprpc.EditorUiRequest:
		ch := make(chan string, 1)
		m.mu.Lock()
		m.pendingInputs[ui.ID] = ch
		m.mu.Unlock()
		prefill := ""
		if ui.Prefill != nil {
			prefill = *ui.Prefill
		}
		m.broadcast(AgentEvent{Type: "ui_editor", Token: ui.ID, Delta: ui.Title, Placeholder: prefill})
		m.awaitInput(client, ui.ID, ch, nil)

	case omprpc.AskUiRequest:
		ch := make(chan []omprpc.AskAnswer, 1)
		m.mu.Lock()
		m.pendingAsks[ui.ID] = ch
		m.mu.Unlock()
		m.broadcast(AgentEvent{Type: "ui_ask", Token: ui.ID, Data: map[string]any{"questions": ui.Questions}})
		d := 10 * time.Minute
		if ui.Timeout != nil && *ui.Timeout > 0 {
			d = time.Duration(*ui.Timeout) * time.Millisecond
		}
		go func(id string) {
			select {
			case answers := <-ch:
				if answers != nil {
					_ = client.Send(omprpc.AnswersUiResponse{ID: id, Answers: answers})
				} else {
					_ = client.Send(omprpc.CancelUiResponse{ID: id})
				}
			case <-time.After(d):
				_ = client.Send(omprpc.CancelUiResponse{ID: id, TimedOut: omprpc.Ptr(true)})
			}
			m.mu.Lock()
			delete(m.pendingAsks, id)
			m.mu.Unlock()
		}(ui.ID)

	case omprpc.SetTitleUiRequest:
		m.broadcast(AgentEvent{Type: "session_info", Delta: ui.Title})

	case omprpc.NotifyUiRequest:
		level := "info"
		if ui.NotifyType != nil {
			level = string(*ui.NotifyType)
		}
		m.broadcast(AgentEvent{Type: "notice", Level: level, Delta: ui.Message})

	case omprpc.CancelUiRequest:
		m.mu.Lock()
		delete(m.pendingInputs, ui.TargetID)
		delete(m.pendingAsks, ui.TargetID)
		delete(m.confirmations, ui.TargetID)
		m.mu.Unlock()
		m.broadcast(AgentEvent{Type: "ui_cancel", Token: ui.TargetID})
	}
}

// awaitInput waits for the user's reply to a pending extension UI request and
// forwards it (or a cancellation on timeout) back to omp.
func (m *Manager) awaitInput(client *omprpc.Client, id string, ch chan string, timeoutMs *int64) {
	d := 10 * time.Minute
	if timeoutMs != nil && *timeoutMs > 0 {
		d = time.Duration(*timeoutMs) * time.Millisecond
	}
	go func() {
		select {
		case val := <-ch:
			_ = client.Send(omprpc.ValueUiResponse{ID: id, Value: val})
		case <-time.After(d):
			_ = client.Send(omprpc.CancelUiResponse{ID: id, TimedOut: omprpc.Ptr(true)})
		}
		m.mu.Lock()
		delete(m.pendingInputs, id)
		m.mu.Unlock()
	}()
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

// SendPrompt feeds a task into the active omp RPC agent. While a turn is
// streaming the message is delivered as a steering message instead.
func (m *Manager) SendPrompt(ctx context.Context, message string, images []omprpc.ImageContent) error {
	client, err := m.EnsureStarted(ctx)
	if err != nil {
		m.broadcast(AgentEvent{Type: "error", Delta: err.Error()})
		return err
	}

	if !strings.HasPrefix(strings.TrimSpace(message), "/") {
		m.mu.Lock()
		m.titleSeed = message
		m.mu.Unlock()
	}
	cmd := omprpc.PromptCommand{Message: message, Images: images}
	if m.activePrompt.Swap(true) {
		cmd.StreamingBehavior = omprpc.Ptr(omprpc.StreamingBehaviorSteer)
	}
	reqID := fmt.Sprintf("p_%d", time.Now().UnixNano())
	if _, err = client.Prompt(omprpc.WithRequestID(ctx, reqID), cmd); err != nil {
		m.activePrompt.Store(false)
		m.broadcast(AgentEvent{Type: "error", Delta: err.Error()})
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
	needsLogin := m.needsLogin
	m.mu.RUnlock()

	return map[string]any{
		"running":      running,
		"autonomyMode": string(mode),
		"active":       m.activePrompt.Load(),
		"binaryPath":   m.ompPath,
		"needsLogin":   needsLogin,
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
	m.mu.RLock()
	placeholder := m.needsLogin
	m.mu.RUnlock()
	if placeholder { // the placeholder key must not make Anthropic models look usable
		kept := models[:0]
		for _, mo := range models {
			if mo.Provider != "anthropic" {
				kept = append(kept, mo)
			}
		}
		models = kept
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
	provs, err := client.GetLoginProviders(ctx)
	if err != nil {
		return nil, err
	}
	m.mu.RLock()
	placeholder := m.needsLogin
	m.mu.RUnlock()
	if placeholder {
		for i := range provs {
			if provs[i].ID == "anthropic" {
				provs[i].Authenticated = false
			}
		}
	}
	return provs, nil
}

// Login triggers an OAuth login flow for the given provider via omp.
func (m *Manager) Login(ctx context.Context, providerID string) (string, error) {
	client, err := m.EnsureStarted(ctx)
	if err != nil {
		return "", err
	}
	out, err := client.Login(ctx, omprpc.LoginCommand{ProviderID: providerID})
	ev := AgentEvent{Type: "login_done", Delta: providerID}
	if err != nil {
		ev.IsError, ev.Result = true, err.Error()
	} else if m.NeedsLogin() {
		// The engine only started thanks to the placeholder key; restart it so it
		// runs with the real credentials and the full model list.
		_ = m.Restart(ctx)
		_, _ = m.EnsureStarted(ctx)
	}
	m.broadcast(ev)
	return out, err
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

// GetAvailableThinkingLevels returns the supported thinking levels for the active model in omp.
func (m *Manager) GetAvailableThinkingLevels(ctx context.Context) ([]string, error) {
	client, err := m.EnsureStarted(ctx)
	if err != nil {
		return nil, err
	}
	levels, err := client.GetAvailableThinkingLevels(ctx)
	if err != nil {
		return nil, err
	}
	res := make([]string, len(levels))
	for i, l := range levels {
		res[i] = string(l)
	}
	return res, nil
}

// GetAvailableCommands returns the list of slash commands registered with omp.
func (m *Manager) GetAvailableCommands(ctx context.Context) []omprpc.AvailableSlashCommand {
	m.mu.RLock()
	cmds := m.availableCommands
	m.mu.RUnlock()
	if len(cmds) > 0 {
		return cmds
	}

	client, err := m.EnsureStarted(ctx)
	if err == nil {
		if fetched, err := client.GetAvailableCommands(ctx); err == nil && len(fetched) > 0 {
			m.mu.Lock()
			m.availableCommands = fetched
			m.mu.Unlock()
			return fetched
		}
	}
	return nil
}

// Close gracefully stops the omp subprocess.
func (m *Manager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	var err error
	if m.client != nil {
		c := m.client
		m.client = nil
		err = c.Close()
	}
	m.removeTempDirLocked()
	select {
	case <-m.stop:
	default:
		close(m.stop)
	}
	return err
}

// ProviderEnv converts stored "ai_<provider>_api_key" settings (and the custom
// base URL / Ollama host) into the environment variables omp reads.
func ProviderEnv(settings map[string]string) []string {
	var env []string
	for k, v := range settings {
		if v == "" || !strings.HasPrefix(k, "ai_") {
			continue
		}
		name := strings.TrimPrefix(k, "ai_")
		switch {
		case name == "custom_base_url":
			env = append(env, "OPENAI_BASE_URL="+v)
		case name == "ollama_host":
			env = append(env, "OLLAMA_HOST="+v)
		case strings.HasSuffix(name, "_api_key"):
			id := strings.TrimSuffix(name, "_api_key")
			if appIDPattern.MatchString(id) {
				env = append(env, strings.ToUpper(strings.ReplaceAll(id, "-", "_"))+"_API_KEY="+v)
			}
		}
	}
	return env
}

// SetDataDir points omp's persistent state at <dir>/omp/agent.
func (m *Manager) SetDataDir(dir string) {
	m.mu.Lock()
	m.dataDir = dir
	m.mu.Unlock()
}

// AgentDir is where omp stores auth, skills, settings and sessions.
func (m *Manager) AgentDir() string {
	if m.dataDir != "" {
		return filepath.Join(m.dataDir, "omp", "agent")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".omp", "agent")
}

func derefStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// SubmitAnswers resolves a pending "ask" dialog; nil answers cancels it.
func (m *Manager) SubmitAnswers(id string, answers []omprpc.AskAnswer) bool {
	m.mu.Lock()
	ch, ok := m.pendingAsks[id]
	if ok {
		delete(m.pendingAsks, id)
	}
	m.mu.Unlock()
	if !ok {
		return false
	}
	ch <- answers
	return true
}

// NeedsLogin reports whether omp is running on the placeholder key only.
func (m *Manager) NeedsLogin() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.needsLogin
}

// HomeDir is the HOME omp runs with (inside the data volume).
func (m *Manager) HomeDir() string {
	return filepath.Join(m.dataDir, "omp", "home")
}

func (m *Manager) touch() { m.lastActivity.Store(time.Now().UnixMilli()) }

func (m *Manager) idleTimeout(ctx context.Context) time.Duration {
	mins := 15
	if m.DB != nil {
		if v := m.DB.GetSetting(ctx, "ai_idle_minutes"); v != "" {
			if n, err := strconv.Atoi(v); err == nil {
				mins = n
			}
		}
	}
	return time.Duration(mins) * time.Minute // 0 disables
}

// reapIdle stops omp after a quiet period. It never stops a running turn,
// pending background work, or an open approval/question.
func (m *Manager) reapIdle() {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-m.stop:
			return
		case <-t.C:
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		limit := m.idleTimeout(ctx)
		cancel()
		if limit <= 0 || time.Since(time.UnixMilli(m.lastActivity.Load())) < limit || m.activePrompt.Load() {
			continue
		}
		m.mu.Lock()
		busy := len(m.confirmations)+len(m.pendingInputs)+len(m.pendingAsks) > 0
		client := m.client
		m.mu.Unlock()
		if client == nil || busy {
			continue
		}
		sctx, scancel := context.WithTimeout(context.Background(), 5*time.Second)
		st, err := client.GetState(sctx)
		scancel()
		if err != nil || st.IsStreaming || st.IsCompacting || st.HasPendingAsyncWork {
			continue
		}
		m.mu.Lock()
		if m.client == client {
			m.client = nil
			_ = client.Close()
		}
		m.mu.Unlock()
	}
}

// writeOverlay writes the per-launch omp config overlay carrying the approval mode.
func (m *Manager) writeOverlay() (string, error) {
	dir := os.TempDir()
	if m.dataDir != "" {
		dir = m.AgentDir()
	}
	path := filepath.Join(dir, "nextdeploy-overlay.yml")
	body := "tools:\n  approvalMode: " + string(m.autonomyMode) + "\n"
	return path, os.WriteFile(path, []byte(body), 0o600)
}

// ApplyAutonomyMode persists a new approval mode and restarts omp so it takes
// effect (omp reads it at startup). Refused while a turn is running.
func (m *Manager) ApplyAutonomyMode(ctx context.Context, mode AutonomyMode) error {
	if m.GetAutonomyMode() == mode {
		return nil
	}
	if m.activePrompt.Load() {
		return fmt.Errorf("agent is busy; stop the current turn before changing the approval mode")
	}
	m.mu.Lock()
	m.autonomyMode = mode
	if m.client != nil {
		c := m.client
		m.client = nil
		_ = c.Close()
	}
	m.mu.Unlock()
	if m.DB != nil {
		_ = m.DB.SetSetting(ctx, "ai_approval_mode", string(mode))
	}
	if _, err := m.EnsureStarted(ctx); err != nil {
		return err
	}
	m.broadcast(AgentEvent{Type: "workdir"}) // clients reload their view of the engine
	return nil
}

type titleInfo struct {
	tries    int
	fallback string // the plain first-prompt title we set when omp generated none
}

func (m *Manager) renameActive() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.renameID != ""
}

// finishRename reports whether id is the internal /rename turn and, if so, wakes its waiter.
func (m *Manager) finishRename(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.renameID == "" || id != m.renameID {
		return false
	}
	m.renameID = ""
	if m.renameDone != nil {
		close(m.renameDone)
		m.renameDone = nil
	}
	return true
}

// nameSession titles a conversation the way omp's own CLI does: by running its
// /rename command, which generates a title with the model when none is given
// (omp does not do this on its own over RPC). omp declines to title a trivial
// first turn ("hi"); then the first prompt is used for now and /rename is tried
// again after the next turns (up to 3 times).
func (m *Manager) nameSession(client *omprpc.Client) {
	m.mu.Lock()
	seed := m.titleSeed
	m.mu.Unlock()
	if seed == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	st, err := client.GetState(ctx)
	if err != nil {
		return
	}
	m.mu.Lock()
	ti := m.titleState[st.SessionID]
	if ti == nil {
		ti = &titleInfo{}
		m.titleState[st.SessionID] = ti
	}
	m.mu.Unlock()
	name := ""
	if st.SessionName != nil {
		name = *st.SessionName
	}
	if (name != "" && name != ti.fallback) || ti.tries >= 3 {
		return // already has a real title (or we gave up)
	}
	ti.tries++

	id := fmt.Sprintf("nd_rename_%d", time.Now().UnixNano())
	done := make(chan struct{})
	m.mu.Lock()
	m.renameID, m.renameDone = id, done
	m.mu.Unlock()
	if _, err := client.Prompt(omprpc.WithRequestID(ctx, id), omprpc.PromptCommand{Message: "/rename"}); err == nil {
		select {
		case <-done:
		case <-time.After(20 * time.Second):
		}
	}
	m.mu.Lock()
	m.renameID, m.renameDone = "", nil
	m.mu.Unlock()

	title := ""
	if st, err := client.GetState(ctx); err == nil && st.SessionName != nil && *st.SessionName != "" && *st.SessionName != ti.fallback {
		title = *st.SessionName
	}
	if title == "" && name == "" {
		title = cleanTitle(seed)
		ti.fallback = title
		if title == "" || client.SetSessionName(ctx, omprpc.SetSessionNameCommand{Name: title}) != nil {
			return
		}
	}
	if title != "" {
		m.broadcast(AgentEvent{Type: "session_info", Delta: title})
	}
}

// cleanTitle reduces model/user text to one short plain line.
func cleanTitle(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	s = strings.Trim(strings.Join(strings.Fields(s), " "), "\"'`*#. ")
	if r := []rune(s); len(r) > 60 {
		s = string(r[:60]) + "…"
	}
	return s
}

const identityPrompt = `# You are NextDeploy AI (ND AI)

You are the assistant built into the NextDeploy control panel, running on the panel's own server (inside its container, with the Docker socket).
When asked who you are, say you are NextDeploy AI (ND AI), not a generic coding assistant.
Your job is to help the admin run the server: sites and containers, deploys, domains, Caddy, backups, databases, logs, performance and security problems.
Prefer the NextDeploy host tools (list_sites, list_containers, container_logs, list_deploy_logs, get_site_logs, get_audit_log, ...) over raw shell when they fit.
Answer in the language the user writes in (Bangla, Banglish or English).
`

// writeIdentity writes the per-launch system-prompt addendum that names the assistant.
func (m *Manager) writeIdentity() (string, error) {
	dir := os.TempDir()
	if m.dataDir != "" {
		dir = m.AgentDir()
	}
	path := filepath.Join(dir, "nextdeploy-identity.md")
	return path, os.WriteFile(path, []byte(identityPrompt), 0o600)
}
