package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// WorkDir describes where the omp subprocess runs.
type WorkDir struct {
	Kind  string `json:"kind"` // root | site | temp
	AppID string `json:"appId,omitempty"`
	Path  string `json:"path"`
	// Sites lists the workspaces under the root that can be selected.
	Sites []string `json:"sites,omitempty"`
}

// resolveCwdLocked returns the directory omp should start in. m.mu must be held.
func (m *Manager) resolveCwdLocked() (string, error) {
	switch m.cwdKind {
	case "site":
		return m.siteDir(m.cwdAppID)
	case "temp":
		if m.tempDir == "" {
			dir, err := os.MkdirTemp("", "nextdeploy-ai-")
			if err != nil {
				return "", fmt.Errorf("agent: create temp workspace: %w", err)
			}
			m.tempDir = dir
		}
		return m.tempDir, nil
	}
	if fi, err := os.Stat(m.Root); err != nil || !fi.IsDir() {
		return "", fmt.Errorf("agent: workspace root not found: %s", m.Root)
	}
	return m.Root, nil
}

func (m *Manager) removeTempDirLocked() {
	if m.tempDir != "" {
		_ = os.RemoveAll(m.tempDir)
		m.tempDir = ""
	}
}

// WorkDir reports the current working directory selection.
func (m *Manager) WorkDir() WorkDir {
	m.mu.RLock()
	defer m.mu.RUnlock()
	wd := WorkDir{Kind: m.cwdKind, AppID: m.cwdAppID, Path: m.Root}
	switch m.cwdKind {
	case "site":
		wd.Path = filepath.Join(m.Root, m.cwdAppID)
	case "temp":
		wd.Path = m.tempDir
	}
	wd.Sites = m.listSites()
	return wd
}

func (m *Manager) listSites() []string {
	entries, err := os.ReadDir(m.Root)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() && ValidAppID(e.Name()) {
			out = append(out, e.Name())
		}
	}
	return out
}

// SetWorkDir switches omp to the workspace root, one site's workspace or a
// throw-away temp directory. The subprocess is restarted so omp really runs
// in that directory (omp has no runtime chdir).
func (m *Manager) SetWorkDir(ctx context.Context, kind, appID string) (WorkDir, error) {
	if m.activePrompt.Load() {
		return WorkDir{}, fmt.Errorf("agent is busy; stop the current turn before changing directory")
	}
	m.mu.Lock()
	switch kind {
	case "root", "temp":
		appID = ""
	case "site":
		if _, err := m.siteDir(appID); err != nil {
			m.mu.Unlock()
			return WorkDir{}, err
		}
	default:
		m.mu.Unlock()
		return WorkDir{}, fmt.Errorf("kind must be root, site or temp")
	}
	if m.client != nil {
		_ = m.client.Close()
		m.client = nil
	}
	m.removeTempDirLocked() // choosing "temp" again gives a fresh directory
	m.cwdKind, m.cwdAppID = kind, appID
	m.mu.Unlock()

	if m.DB != nil {
		_ = m.DB.SetSetting(ctx, "ai_workdir_kind", kind)
		_ = m.DB.SetSetting(ctx, "ai_workdir_app", appID)
	}
	if _, err := m.EnsureStarted(ctx); err != nil {
		return WorkDir{}, err
	}
	m.broadcast(AgentEvent{Type: "workdir"})
	return m.WorkDir(), nil
}

// FileEntry is one item of the working-directory tree shown in the UI.
type FileEntry struct {
	Name    string `json:"name"`
	RelPath string `json:"rel_path"`
	IsDir   bool   `json:"is_dir"`
	Size    int64  `json:"size"`
}

var skipDirs = map[string]bool{".git": true, "node_modules": true, "vendor": true, ".cache": true}

// ListFiles walks the directory omp is running in (depth-limited) so the UI
// shows exactly what the agent sees.
func (m *Manager) ListFiles(ctx context.Context) ([]FileEntry, error) {
	if _, err := m.EnsureStarted(ctx); err != nil {
		return nil, err
	}
	base := m.WorkDir().Path
	if base == "" {
		return []FileEntry{}, nil
	}
	const maxDepth, maxEntries = 4, 2000
	out := []FileEntry{}
	err := filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(base, path)
		if rel == "." {
			return nil
		}
		depth := strings.Count(rel, string(os.PathSeparator)) + 1
		if d.IsDir() && (skipDirs[d.Name()] || depth > maxDepth) {
			return filepath.SkipDir
		}
		if len(out) >= maxEntries {
			return filepath.SkipAll
		}
		e := FileEntry{Name: d.Name(), RelPath: filepath.ToSlash(rel), IsDir: d.IsDir()}
		if !d.IsDir() {
			if fi, err := d.Info(); err == nil {
				e.Size = fi.Size()
			}
		}
		out = append(out, e)
		return nil
	})
	return out, err
}
