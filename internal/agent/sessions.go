package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"panel/internal/agent/omprpc"
)

// SessionInfo is one conversation file omp keeps for a working directory.
type SessionInfo struct {
	Path      string `json:"path"`
	ID        string `json:"id"`
	Title     string `json:"title"`
	UpdatedAt int64  `json:"updatedAt"` // unix ms
	Current   bool   `json:"current"`
	Kind      string `json:"kind"`            // root | site: where the conversation runs
	AppID     string `json:"appId,omitempty"` // for kind "site"
	Scope     string `json:"scope"`           // human label: "Workspace root" or the site name
}

// scopeOf classifies a session's working directory relative to the workspace root.
func (m *Manager) scopeOf(cwd string) (kind, appID string, ok bool) {
	root, err := filepath.EvalSymlinks(m.Root)
	if err != nil {
		root = m.Root
	}
	rel, err := filepath.Rel(root, cwd)
	switch {
	case err != nil || strings.HasPrefix(rel, ".."):
		return "", "", false // temp or foreign directory: not selectable from the panel
	case rel == ".":
		return "root", "", true
	case !strings.Contains(rel, string(os.PathSeparator)) && ValidAppID(rel):
		return "site", rel, true
	}
	return "", "", false
}

func (m *Manager) sessionsRoot() string { return filepath.Join(m.AgentDir(), "sessions") }

// safeSessionPath accepts only .jsonl files inside omp's sessions directory.
func (m *Manager) safeSessionPath(p string) (string, error) {
	root, err := filepath.Abs(m.sessionsRoot())
	if err != nil {
		return "", err
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(abs, root+string(os.PathSeparator)) || !strings.HasSuffix(abs, ".jsonl") {
		return "", fmt.Errorf("not a session file")
	}
	return abs, nil
}

// readSessionHeader reads the title record and the session record (first two lines).
func readSessionHeader(path string) (title, id, cwd string, ok bool) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	r := bufio.NewReaderSize(f, 16<<10)
	for i := 0; i < 3; i++ {
		line, err := r.ReadBytes('\n')
		if len(line) == 0 && err != nil {
			break
		}
		var h struct {
			Type  string `json:"type"`
			Title string `json:"title"`
			ID    string `json:"id"`
			Cwd   string `json:"cwd"`
		}
		if json.Unmarshal(line, &h) != nil {
			continue
		}
		switch h.Type {
		case "title":
			title = h.Title
		case "session":
			id, cwd, ok = h.ID, h.Cwd, true
		}
		if ok {
			break
		}
	}
	return
}

// ListSessions returns the saved conversations for the current working
// directory, newest first.
func (m *Manager) ListSessions(ctx context.Context) ([]SessionInfo, error) {
	current := ""
	if st, err := m.GetState(ctx); err == nil && st.SessionFile != nil {
		current = *st.SessionFile
	}

	var out []SessionInfo
	known := map[string]bool{}
	_ = filepath.WalkDir(m.sessionsRoot(), func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".jsonl") {
			return nil
		}
		// Conversations are <root>/<cwd>/<file>.jsonl; deeper files are subagent transcripts.
		if rel, rerr := filepath.Rel(m.sessionsRoot(), path); rerr != nil || strings.Count(rel, string(os.PathSeparator)) != 1 {
			return nil
		}
		title, id, scwd, ok := readSessionHeader(path)
		if !ok {
			return nil
		}
		known[id] = true
		if real, err := filepath.EvalSymlinks(scwd); err == nil {
			scwd = real
		}
		kind, appID, inScope := m.scopeOf(scwd)
		if !inScope {
			return nil
		}
		fi, err := d.Info()
		if err != nil {
			return nil
		}
		if path != current && !hasMessages(path) {
			return nil // an untouched, empty conversation
		}
		if title == "" {
			title = firstUserText(path)
		}
		if title == "" {
			title = "New conversation"
		}
		scope := "Workspace root"
		if kind == "site" {
			scope = appID
		}
		out = append(out, SessionInfo{Path: path, ID: id, Title: title, UpdatedAt: fi.ModTime().UnixMilli(), Current: path == current, Kind: kind, AppID: appID, Scope: scope})
		return nil
	})
	m.pruneOrphanUploads(known)
	// File names start with the creation timestamp: stable order even when omp touches a file.
	sort.Slice(out, func(i, j int) bool { return filepath.Base(out[i].Path) > filepath.Base(out[j].Path) })
	if len(out) > 200 {
		out = out[:200]
	}
	if out == nil {
		out = []SessionInfo{}
	}
	return out, nil
}

// OpenSession switches omp to another saved conversation.
func (m *Manager) OpenSession(ctx context.Context, path string) error {
	if m.activePrompt.Load() {
		return fmt.Errorf("agent is busy; stop the current turn first")
	}
	abs, err := m.safeSessionPath(path)
	if err != nil {
		return err
	}
	// A conversation belongs to the directory it ran in: move omp there first.
	if _, _, scwd, ok := readSessionHeader(abs); ok {
		if real, err := filepath.EvalSymlinks(scwd); err == nil {
			scwd = real
		}
		if kind, appID, inScope := m.scopeOf(scwd); inScope {
			if cur := m.WorkDir(); cur.Kind != kind || cur.AppID != appID {
				if _, err := m.SetWorkDir(ctx, kind, appID); err != nil {
					return err
				}
			}
		}
	}
	c, err := m.EnsureStarted(ctx)
	if err != nil {
		return err
	}
	res, err := c.SwitchSession(ctx, omprpc.SwitchSessionCommand{SessionPath: abs})
	if err != nil && strings.Contains(err.Error(), "restore model") {
		// The conversation's saved model is gone (login removed, model retired):
		// keep the history and continue it with the model that is active now.
		if st, serr := c.GetState(ctx); serr == nil && st.Model != nil {
			res, err = c.SwitchSession(ctx, omprpc.SwitchSessionCommand{
				SessionPath: abs,
				Provider:    omprpc.Ptr(st.Model.Provider),
				ModelID:     omprpc.Ptr(st.Model.ID),
			})
		}
	}
	if err != nil {
		return err
	}
	if res.Cancelled {
		return fmt.Errorf("switch was cancelled")
	}
	go m.pushState(c)
	return nil
}

// DeleteSession removes a saved conversation; deleting the live one first
// starts a fresh conversation so omp is never left pointing at a missing file.
func (m *Manager) DeleteSession(ctx context.Context, path string) error {
	abs, err := m.safeSessionPath(path)
	if err != nil {
		return err
	}
	if st, err := m.GetState(ctx); err == nil && st.SessionFile != nil && *st.SessionFile == abs {
		if m.activePrompt.Load() {
			return fmt.Errorf("agent is busy; stop the current turn first")
		}
		if err := m.NewSession(ctx); err != nil {
			return err
		}
	}
	_, sid, _, _ := readSessionHeader(abs)
	if err := os.Remove(abs); err != nil && !os.IsNotExist(err) {
		return err
	}
	if ValidAppID(sid) {
		_ = os.RemoveAll(filepath.Join(m.AgentDir(), "uploads", sid)) // files the user attached to this chat
	}
	_ = os.RemoveAll(strings.TrimSuffix(abs, ".jsonl")) // artifacts dir next to the file
	return nil
}

// HistoryToolCall / HistoryMessage are the UI-facing shape of omp messages.
type HistoryToolCall struct {
	ID      string          `json:"id"`
	Name    string          `json:"name"`
	Args    map[string]any  `json:"args,omitempty"`
	Result  string          `json:"result,omitempty"`
	Details json.RawMessage `json:"details,omitempty"`
	Status  string          `json:"status"`
}

// HistoryImage is an image the user attached to a message.
type HistoryImage struct {
	MimeType string `json:"mimeType"`
	Data     string `json:"data"` // base64
}

type HistoryMessage struct {
	ID        string            `json:"id"`
	Role      string            `json:"role"`           // user | assistant | system
	Kind      string            `json:"kind,omitempty"` // for system: compaction | branch | note
	Content   string            `json:"content"`
	Thinking  string            `json:"thinking,omitempty"`
	ToolCalls []HistoryToolCall `json:"toolCalls,omitempty"`
	Attached  []string          `json:"attached,omitempty"`
	Images    []HistoryImage    `json:"images,omitempty"`
	Timestamp int64             `json:"timestamp"`
}

func textOf(content json.RawMessage) string {
	var s string
	if json.Unmarshal(content, &s) == nil {
		return s
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(content, &blocks) != nil {
		return ""
	}
	var b strings.Builder
	for _, bl := range blocks {
		if bl.Type == "text" {
			b.WriteString(bl.Text)
		}
	}
	return b.String()
}

func tsOf(raw json.RawMessage) int64 {
	var n float64
	if json.Unmarshal(raw, &n) == nil && n > 0 {
		return int64(n)
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
			return t.UnixMilli()
		}
	}
	return 0
}

// History returns the live conversation mapped to UI messages: consecutive
// assistant messages and their tool results collapse into one assistant turn.
func (m *Manager) History(ctx context.Context) ([]HistoryMessage, error) {
	c, err := m.EnsureStarted(ctx)
	if err != nil {
		return nil, err
	}
	// Page through the history instead of one huge frame: a long conversation
	// would otherwise exceed the transport's frame limit.
	var all []json.RawMessage
	cursor := ""
	for page := 0; page < 200; page++ {
		params := map[string]any{"limit": 100}
		if cursor != "" {
			params["cursor"] = cursor
		}
		raw, err := c.Call(ctx, "get_messages_page", params, 30*time.Second)
		if err != nil {
			return nil, err
		}
		var pg struct {
			Messages   []json.RawMessage `json:"messages"`
			NextCursor *string           `json:"nextCursor"`
		}
		if err := json.Unmarshal(raw, &pg); err != nil {
			return nil, err
		}
		all = append(all, pg.Messages...)
		if pg.NextCursor == nil || *pg.NextCursor == "" {
			break
		}
		cursor = *pg.NextCursor
	}
	joined, _ := json.Marshal(map[string]any{"messages": all})
	return mapHistory(joined)
}

func mapHistory(raw json.RawMessage) ([]HistoryMessage, error) {
	var data struct {
		Messages []map[string]json.RawMessage `json:"messages"`
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, err
	}
	str := func(m map[string]json.RawMessage, k string) string {
		var v string
		_ = json.Unmarshal(m[k], &v)
		return v
	}
	boolean := func(m map[string]json.RawMessage, k string) bool {
		var v bool
		_ = json.Unmarshal(m[k], &v)
		return v
	}

	out := []HistoryMessage{}
	var cur *HistoryMessage // open assistant turn
	flush := func() {
		if cur != nil {
			out = append(out, *cur)
			cur = nil
		}
	}
	openTurn := func(id string, ts int64) *HistoryMessage {
		if cur == nil {
			cur = &HistoryMessage{ID: id, Role: "assistant", Timestamp: ts}
		}
		return cur
	}
	for i, msg := range data.Messages {
		id := fmt.Sprintf("h_%d", i)
		ts := tsOf(msg["timestamp"])
		switch str(msg, "role") {
		case "user":
			flush()
			um := HistoryMessage{ID: id, Role: "user", Content: textOf(msg["content"]), Timestamp: ts}
			var blocks []struct {
				Type     string `json:"type"`
				Data     string `json:"data"`
				MimeType string `json:"mimeType"`
			}
			if json.Unmarshal(msg["content"], &blocks) == nil {
				for _, b := range blocks {
					if b.Type == "image" && b.Data != "" {
						um.Images = append(um.Images, HistoryImage{MimeType: b.MimeType, Data: b.Data})
					}
				}
			}
			if um.Content != "" || len(um.Attached) > 0 || len(um.Images) > 0 {
				out = append(out, um)
			}
		case "assistant":
			t := openTurn(id, ts)
			var blocks []struct {
				Type      string         `json:"type"`
				Text      string         `json:"text"`
				Thinking  string         `json:"thinking"`
				ID        string         `json:"id"`
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
			}
			_ = json.Unmarshal(msg["content"], &blocks)
			for _, b := range blocks {
				switch b.Type {
				case "text":
					t.Content += b.Text
				case "thinking":
					t.Thinking += b.Thinking
				case "redactedThinking":
					if t.Thinking == "" {
						t.Thinking = "_The provider redacted this reasoning._"
					}
				case "toolCall":
					t.ToolCalls = append(t.ToolCalls, HistoryToolCall{ID: b.ID, Name: b.Name, Args: b.Arguments, Status: "completed"})
				}
			}
			switch str(msg, "stopReason") {
			case "error":
				e := str(msg, "errorMessage")
				if e == "" {
					e = "the model request failed"
				}
				t.Content += "\n\n_Error: " + e + "_"
			case "aborted":
				t.Content += "\n\n_Stopped._"
			}
		case "toolResult":
			if cur != nil {
				var isErr bool
				_ = json.Unmarshal(msg["isError"], &isErr)
				for i := range cur.ToolCalls {
					if cur.ToolCalls[i].ID == str(msg, "toolCallId") {
						cur.ToolCalls[i].Result = textOf(msg["content"])
						cur.ToolCalls[i].Details = msg["details"]
						if isErr {
							cur.ToolCalls[i].Status = "failed"
						}
					}
				}
			}
		case "bashExecution", "pythonExecution":
			// Commands the user ran themselves (!cmd / $code) appear as tool rows.
			t := openTurn(id, ts)
			name, key := "bash", "command"
			if str(msg, "role") == "pythonExecution" {
				name, key = "eval", "code"
			}
			status := "completed"
			var code *int
			_ = json.Unmarshal(msg["exitCode"], &code)
			if boolean(msg, "cancelled") || (code != nil && *code != 0) {
				status = "failed"
			}
			t.ToolCalls = append(t.ToolCalls, HistoryToolCall{
				ID: id, Name: name, Args: map[string]any{key: str(msg, key)}, Result: str(msg, "output"), Status: status,
			})
		case "compactionSummary":
			flush()
			out = append(out, HistoryMessage{ID: id, Role: "system", Kind: "compaction", Content: str(msg, "summary"), Timestamp: ts})
		case "branchSummary":
			flush()
			out = append(out, HistoryMessage{ID: id, Role: "system", Kind: "branch", Content: str(msg, "summary"), Timestamp: ts})
		case "custom", "hookMessage":
			if boolean(msg, "display") {
				if t := textOf(msg["content"]); t != "" {
					flush()
					out = append(out, HistoryMessage{ID: id, Role: "system", Kind: "note", Content: t, Timestamp: ts})
				}
			}
		case "fileMention":
			var files []struct {
				Path string `json:"path"`
			}
			if json.Unmarshal(msg["files"], &files) == nil {
				for k := len(out) - 1; k >= 0; k-- {
					if out[k].Role == "user" {
						for _, f := range files {
							out[k].Attached = append(out[k].Attached, f.Path)
						}
						break
					}
				}
			}
		}
	}
	flush()
	return out, nil
}

// Subagents returns the running/finished subagents omp reports (background tasks).
func (m *Manager) Subagents(ctx context.Context) ([]omprpc.SubagentSnapshot, error) {
	c, err := m.EnsureStarted(ctx)
	if err != nil {
		return nil, err
	}
	return c.GetSubagents(ctx)
}

// CancelSubagent hard-kills one running subagent.
func (m *Manager) CancelSubagent(ctx context.Context, id string) (bool, error) {
	c, err := m.EnsureStarted(ctx)
	if err != nil {
		return false, err
	}
	return c.CancelSubagent(ctx, omprpc.CancelSubagentCommand{SubagentID: id})
}

const maxUploadBytes = 25 << 20

var unsafeName = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// SaveUpload stores a user-attached file under the current conversation's
// upload folder (removed with the conversation) and returns its absolute path,
// which the agent can read through omp's @file mentions.
func (m *Manager) SaveUpload(ctx context.Context, name string, r io.Reader) (string, int64, error) {
	st, err := m.GetState(ctx)
	if err != nil {
		return "", 0, err
	}
	if !ValidAppID(st.SessionID) {
		return "", 0, fmt.Errorf("no active conversation")
	}
	base := unsafeName.ReplaceAllString(filepath.Base(name), "_")
	if base == "" || base == "." || base == ".." {
		base = "upload"
	}
	dir := filepath.Join(m.AgentDir(), "uploads", st.SessionID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", 0, err
	}
	dst := filepath.Join(dir, base)
	if _, err := os.Stat(dst); err == nil {
		dst = filepath.Join(dir, fmt.Sprintf("%d_%s", time.Now().UnixNano(), base))
	}
	f, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", 0, err
	}
	n, err := io.Copy(f, io.LimitReader(r, maxUploadBytes+1))
	f.Close()
	if err == nil && n > maxUploadBytes {
		err = fmt.Errorf("file is larger than %d MB", maxUploadBytes>>20)
	}
	if err != nil {
		_ = os.Remove(dst)
		return "", 0, err
	}
	return dst, n, nil
}

// hasMessages reports whether the session file holds at least one message.
func hasMessages(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	r := bufio.NewReaderSize(f, 64<<10)
	for i := 0; i < 400; i++ {
		line, err := r.ReadBytes('\n')
		if strings.Contains(string(line), `"type":"message"`) {
			return true
		}
		if err != nil {
			return false
		}
	}
	return true
}

// pruneOrphanUploads removes upload folders whose conversation no longer
// exists (deleted outside the panel, or never saved). Fresh folders are kept
// for a while: the conversation file may not be written yet.
func (m *Manager) pruneOrphanUploads(known map[string]bool) {
	root := filepath.Join(m.AgentDir(), "uploads")
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() || known[e.Name()] || !ValidAppID(e.Name()) {
			continue
		}
		if fi, err := e.Info(); err == nil && time.Since(fi.ModTime()) > time.Hour {
			_ = os.RemoveAll(filepath.Join(root, e.Name()))
		}
	}
}

// firstUserText is the fallback title: omp names a conversation with a model
// call, which may not have happened (offline, no model yet).
func firstUserText(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	r := bufio.NewReaderSize(f, 64<<10)
	for i := 0; i < 400; i++ {
		line, err := r.ReadBytes('\n')
		if strings.Contains(string(line), `"role":"user"`) {
			var rec struct {
				Message struct {
					Role    string          `json:"role"`
					Content json.RawMessage `json:"content"`
				} `json:"message"`
			}
			if json.Unmarshal(line, &rec) == nil && rec.Message.Role == "user" {
				t := strings.TrimSpace(strings.Join(strings.Fields(textOf(rec.Message.Content)), " "))
				if len(t) > 60 {
					t = t[:60] + "…"
				}
				if t != "" {
					return t
				}
			}
		}
		if err != nil {
			break
		}
	}
	return ""
}
