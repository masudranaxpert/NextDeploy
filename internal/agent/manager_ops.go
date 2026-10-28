package agent

import (
	"context"
	"fmt"
	"os"
	"time"

	"panel/internal/agent/omprpc"
)

// requestApproval asks the user to confirm a mutating host tool call. In
// autonomous mode it returns true immediately; otherwise it blocks until the
// user answers, the call is cancelled, or five minutes pass.
func (m *Manager) requestApproval(ctx context.Context, tool, title, message string) bool {
	if m.GetAutonomyMode() == AutonomyAutonomous {
		return true
	}
	id := fmt.Sprintf("host_%s_%d", tool, time.Now().UnixNano())
	ch := make(chan bool, 1)
	m.mu.Lock()
	m.confirmations[id] = ch
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		delete(m.confirmations, id)
		m.mu.Unlock()
	}()

	m.broadcast(AgentEvent{
		Type:     "approval_required",
		Approval: &ApprovalRequest{ID: id, Title: title, Message: message, Tool: tool},
	})
	select {
	case ok := <-ch:
		return ok
	case <-ctx.Done():
		return false
	case <-time.After(5 * time.Minute):
		return false
	}
}

func (m *Manager) engine(ctx context.Context) (*omprpc.Client, error) {
	return m.EnsureStarted(ctx)
}

// NewSession starts a fresh omp conversation (clears context).
func (m *Manager) NewSession(ctx context.Context) error {
	c, err := m.engine(ctx)
	if err != nil {
		return err
	}
	res, err := c.NewSession(ctx, omprpc.NewSessionCommand{})
	if err != nil {
		return err
	}
	if res.Cancelled {
		return fmt.Errorf("new session was cancelled by an extension")
	}
	go m.pushState(c)
	return nil
}

// Compact summarises the conversation to free context.
func (m *Manager) Compact(ctx context.Context, instructions string) (omprpc.CompactionResult, error) {
	c, err := m.engine(ctx)
	if err != nil {
		return omprpc.CompactionResult{}, err
	}
	cmd := omprpc.CompactCommand{}
	if instructions != "" {
		cmd.CustomInstructions = &instructions
	}
	res, err := c.Compact(ctx, cmd)
	if err == nil {
		go m.pushState(c)
	}
	return res, err
}

// SessionStats returns token / cost / message counters for the session.
func (m *Manager) SessionStats(ctx context.Context) (omprpc.SessionStats, error) {
	c, err := m.engine(ctx)
	if err != nil {
		return omprpc.SessionStats{}, err
	}
	return c.GetSessionStats(ctx)
}

// QueueMessage delivers a message to a running turn: "steer" interrupts at the
// next opportunity, "followUp" runs after the turn finishes.
func (m *Manager) QueueMessage(ctx context.Context, message string, followUp bool) error {
	c, err := m.engine(ctx)
	if err != nil {
		return err
	}
	if followUp {
		return c.FollowUp(ctx, omprpc.FollowUpCommand{Message: message})
	}
	return c.Steer(ctx, omprpc.SteerCommand{Message: message})
}

// EngineSettings are the persisted omp toggles exposed in the UI.
type EngineSettings struct {
	FastMode       bool `json:"fastMode"`
	AutoCompaction bool `json:"autoCompaction"`
	AutoRetry      bool `json:"autoRetry"`
}

func (m *Manager) GetEngineSettings(ctx context.Context) EngineSettings {
	s := EngineSettings{AutoCompaction: true, AutoRetry: true}
	if m.DB != nil {
		if v := m.DB.GetSetting(ctx, "ai_fast_mode"); v != "" {
			s.FastMode = v == "1"
		}
		if v := m.DB.GetSetting(ctx, "ai_auto_compaction"); v != "" {
			s.AutoCompaction = v == "1"
		}
		if v := m.DB.GetSetting(ctx, "ai_auto_retry"); v != "" {
			s.AutoRetry = v == "1"
		}
	}
	return s
}

// SetEngineSetting toggles one of fast_mode, auto_compaction, auto_retry.
func (m *Manager) SetEngineSetting(ctx context.Context, key string, enabled bool) error {
	c, err := m.engine(ctx)
	if err != nil {
		return err
	}
	switch key {
	case "fast_mode":
		_, err = c.SetFastMode(ctx, omprpc.SetFastModeCommand{Enabled: enabled})
	case "auto_compaction":
		err = c.SetAutoCompaction(ctx, omprpc.SetAutoCompactionCommand{Enabled: enabled})
	case "auto_retry":
		err = c.SetAutoRetry(ctx, omprpc.SetAutoRetryCommand{Enabled: enabled})
	default:
		return fmt.Errorf("unknown setting %q", key)
	}
	if err != nil {
		return err
	}
	if m.DB != nil {
		v := "0"
		if enabled {
			v = "1"
		}
		_ = m.DB.SetSetting(ctx, "ai_"+key, v)
	}
	go m.pushState(c)
	return nil
}

// LogoutAccounts lists stored credentials for a provider.
func (m *Manager) LogoutAccounts(ctx context.Context, providerID string) ([]omprpc.LogoutAccount, error) {
	c, err := m.engine(ctx)
	if err != nil {
		return nil, err
	}
	return c.GetLogoutAccounts(ctx, omprpc.GetLogoutAccountsCommand{ProviderID: providerID})
}

// Logout removes one stored credential.
func (m *Manager) Logout(ctx context.Context, providerID string, credentialID int64) error {
	c, err := m.engine(ctx)
	if err != nil {
		return err
	}
	_, err = c.Logout(ctx, omprpc.LogoutCommand{ProviderID: providerID, CredentialID: credentialID})
	return err
}

// Fork copies the whole conversation into a new session file and switches to it.
func (m *Manager) Fork(ctx context.Context) error {
	if m.activePrompt.Load() {
		return fmt.Errorf("agent is busy; stop the current turn first")
	}
	c, err := m.engine(ctx)
	if err != nil {
		return err
	}
	res, err := c.Fork(ctx, omprpc.ForkCommand{})
	if err != nil {
		return err
	}
	if res.Cancelled {
		return fmt.Errorf("fork was cancelled")
	}
	go m.pushState(c)
	return nil
}

// Handoff summarises the conversation into a fresh session; "" means nothing was produced.
func (m *Manager) Handoff(ctx context.Context, instructions string) (string, error) {
	if m.activePrompt.Load() {
		return "", fmt.Errorf("agent is busy; stop the current turn first")
	}
	c, err := m.engine(ctx)
	if err != nil {
		return "", err
	}
	cmd := omprpc.HandoffCommand{}
	if instructions != "" {
		cmd.CustomInstructions = &instructions
	}
	res, err := c.Handoff(ctx, cmd)
	if err != nil || res == nil {
		return "", err
	}
	go m.pushState(c)
	return derefStr(res.SavedPath), nil
}

// ExportHTML renders the conversation as a standalone HTML file and returns
// its bytes; the temporary file is removed.
func (m *Manager) ExportHTML(ctx context.Context) ([]byte, error) {
	c, err := m.engine(ctx)
	if err != nil {
		return nil, err
	}
	f, err := os.CreateTemp("", "nextdeploy-export-*.html")
	if err != nil {
		return nil, err
	}
	path := f.Name()
	f.Close()
	defer os.Remove(path)
	if _, err := c.ExportHTML(ctx, omprpc.ExportHTMLCommand{OutputPath: &path}); err != nil {
		return nil, err
	}
	return os.ReadFile(path)
}
