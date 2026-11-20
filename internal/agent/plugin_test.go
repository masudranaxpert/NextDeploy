package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"panel/internal/db"
)

func newPluginTestManager(t *testing.T) (*Manager, *db.Store) {
	t.Helper()
	dir := t.TempDir()
	store, err := db.Open(filepath.Join(dir, "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	t.Setenv("OMP_BIN", "")
	oldSys := systemOMP
	systemOMP = func() string { return "omp" }
	t.Cleanup(func() { systemOMP = oldSys })
	m := &Manager{DB: store, dataDir: filepath.Join(dir, "data")}
	return m, store
}

// fakeReleases serves a GitHub-like release layout for one version.
func fakeReleases(t *testing.T, version string, binary []byte, sum string) *httptest.Server {
	t.Helper()
	asset := "omp-linux-musl-" + pluginArch()
	if sum == "" {
		h := sha256.Sum256(binary)
		sum = hex.EncodeToString(h[:])
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/latest", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/tag/"+version, http.StatusFound)
	})
	mux.HandleFunc("/download/"+version+"/SHA256SUMS.txt", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(sum + "  " + asset + "\n0000000000000000000000000000000000000000000000000000000000000000  other\n"))
	})
	mux.HandleFunc("/download/"+version+"/"+asset, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(binary)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	old := pluginBaseURL
	pluginBaseURL = srv.URL
	t.Cleanup(func() { pluginBaseURL = old })
	return srv
}

func waitInstalled(t *testing.T, m *Manager) PluginStatus {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		st := m.PluginStatus(context.Background())
		if !st.Installing {
			return st
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("install did not finish")
	return PluginStatus{}
}

func skipUnsupportedArch(t *testing.T) {
	if pluginArch() == "" {
		t.Skipf("no AI engine for %s", runtime.GOARCH)
	}
}

func TestEnableInstallsPinnedVersionAndDisableRemovesEverything(t *testing.T) {
	skipUnsupportedArch(t)
	m, store := newPluginTestManager(t)
	ctx := context.Background()
	fakeReleases(t, DefaultEngineVersion, []byte("#!/bin/sh\necho omp\n"), "")

	if m.Available(ctx) {
		t.Fatal("AI must be off before it is enabled")
	}
	if err := m.EnableAI(ctx, ""); err != nil {
		t.Fatal(err)
	}
	st := waitInstalled(t, m)
	if !st.Enabled || st.Source != "plugin" || st.Version != DefaultEngineVersion || st.Error != "" {
		t.Fatalf("after enable: %+v", st)
	}
	if store.GetSetting(ctx, SettingAIPluginVersion) != "" {
		t.Fatal("enabling the default version must not pin a setting")
	}

	// Tools omp downloads by itself, plus data that must survive a plain disable.
	home := filepath.Join(m.HomeDir(), ".omp")
	for _, p := range []string{"antigravity-cli/bin/1.5.10/agy", "natives/19.0.0/x.node", "logs/a.log", "antigravity-cli/gemini/logins/token"} {
		full := filepath.Join(home, p)
		_ = os.MkdirAll(filepath.Dir(full), 0o755)
		_ = os.WriteFile(full, []byte("x"), 0o644)
	}
	_ = os.MkdirAll(m.AgentDir(), 0o700)
	_ = os.WriteFile(filepath.Join(m.AgentDir(), "agent.db"), []byte("chats"), 0o600)
	_ = store.SetSetting(ctx, "ai_openai_api_key", "sk-test")

	if err := m.DisableAI(ctx, false); err != nil {
		t.Fatal(err)
	}
	if m.Available(ctx) || isFile(m.pluginBinary()) {
		t.Fatal("engine still present after disable")
	}
	for _, gone := range []string{"antigravity-cli/bin", "natives", "logs"} {
		if _, err := os.Stat(filepath.Join(home, gone)); !os.IsNotExist(err) {
			t.Fatalf("%s not removed", gone)
		}
	}
	if !isFile(filepath.Join(home, "antigravity-cli/gemini/logins/token")) || !isFile(filepath.Join(m.AgentDir(), "agent.db")) {
		t.Fatal("logins and chats must survive a plain disable")
	}
	if store.GetSetting(ctx, "ai_openai_api_key") == "" {
		t.Fatal("API keys must survive a plain disable")
	}

	if err := m.DisableAI(ctx, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(m.dataDir, "omp")); !os.IsNotExist(err) {
		t.Fatal("purge must delete omp data")
	}
	if store.GetSetting(ctx, "ai_openai_api_key") != "" {
		t.Fatal("purge must delete stored AI keys")
	}
	if store.GetSetting(ctx, SettingAIEnabled) != "0" {
		t.Fatal("AI must stay off after purge")
	}
}

func TestChecksumMismatchInstallsNothing(t *testing.T) {
	skipUnsupportedArch(t)
	m, _ := newPluginTestManager(t)
	ctx := context.Background()
	fakeReleases(t, DefaultEngineVersion, []byte("tampered"), "1111111111111111111111111111111111111111111111111111111111111111")

	if err := m.EnableAI(ctx, ""); err != nil {
		t.Fatal(err)
	}
	st := waitInstalled(t, m)
	if st.Installed || st.Enabled || st.Error == "" {
		t.Fatalf("tampered download must be rejected: %+v", st)
	}
	if entries, _ := os.ReadDir(m.pluginDir()); len(entries) != 0 {
		t.Fatalf("leftover files after a failed download: %v", entries)
	}
}

func TestEnableRejectsBadVersion(t *testing.T) {
	skipUnsupportedArch(t)
	m, _ := newPluginTestManager(t)
	if err := m.EnableAI(context.Background(), "../../etc"); err == nil {
		t.Fatal("expected an error for a bad version")
	}
}

func TestStartupMigration(t *testing.T) {
	skipUnsupportedArch(t)
	ctx := context.Background()

	t.Run("never used stays off and downloads nothing", func(t *testing.T) {
		m, store := newPluginTestManager(t)
		m.StartupPlugin(ctx)
		if store.GetSetting(ctx, SettingAIEnabled) != "0" || m.PluginStatus(ctx).Installing {
			t.Fatal("unused AI must be off")
		}
	})

	t.Run("used before is kept on and re-downloaded", func(t *testing.T) {
		m, store := newPluginTestManager(t)
		fakeReleases(t, DefaultEngineVersion, []byte("bin"), "")
		_ = os.MkdirAll(m.AgentDir(), 0o700)
		_ = os.WriteFile(filepath.Join(m.AgentDir(), "agent.db"), []byte("x"), 0o600)
		m.StartupPlugin(ctx)
		if store.GetSetting(ctx, SettingAIEnabled) != "1" {
			t.Fatal("a panel that used AI must keep it on")
		}
		if st := waitInstalled(t, m); !st.Enabled {
			t.Fatalf("engine not restored: %+v", st)
		}
	})

	t.Run("turned off stays off", func(t *testing.T) {
		m, store := newPluginTestManager(t)
		_ = store.SetSetting(ctx, SettingAIEnabled, "0")
		_ = os.MkdirAll(m.AgentDir(), 0o700)
		_ = os.WriteFile(filepath.Join(m.AgentDir(), "agent.db"), []byte("x"), 0o600)
		m.StartupPlugin(ctx)
		if store.GetSetting(ctx, SettingAIEnabled) != "0" || m.PluginStatus(ctx).Installing {
			t.Fatal("an admin's off must be respected")
		}
	})

	t.Run("installer marker turns it on", func(t *testing.T) {
		m, store := newPluginTestManager(t)
		fakeReleases(t, DefaultEngineVersion, []byte("bin"), "")
		_ = os.MkdirAll(m.pluginDir(), 0o755)
		_ = os.WriteFile(filepath.Join(m.pluginDir(), pluginInstallMarker), nil, 0o644)
		m.StartupPlugin(ctx)
		if store.GetSetting(ctx, SettingAIEnabled) != "1" {
			t.Fatal("marker must enable AI")
		}
		waitInstalled(t, m)
		if isFile(filepath.Join(m.pluginDir(), pluginInstallMarker)) {
			t.Fatal("marker must be removed")
		}
	})
}

func TestChecksumFor(t *testing.T) {
	sums := "abc  short\n" + "eef7f44072aeede18ea562f34405754363f0ea4772c6ccf9b94c9878561d28a0  omp-linux-musl-x64\n"
	if got := checksumFor(sums, "omp-linux-musl-x64"); got != "eef7f44072aeede18ea562f34405754363f0ea4772c6ccf9b94c9878561d28a0" {
		t.Fatalf("got %q", got)
	}
	if checksumFor(sums, "omp-linux-musl-arm64") != "" {
		t.Fatal("missing asset must give no checksum")
	}
}
