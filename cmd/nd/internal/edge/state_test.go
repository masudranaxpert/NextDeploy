package edge

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWriteFileAtomicCrashSimulation(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "config.json")
	if err := WriteFileAtomic(target, []byte("original-data"), 0o600); err != nil {
		t.Fatalf("initial write: %v", err)
	}

	for _, stage := range []AtomicWriteStage{StageAfterWrite, StageAfterSync, StageBeforeRename} {
		t.Cleanup(func() { testAtomicHook = nil })
		testAtomicHook = func(s AtomicWriteStage, tmpPath, targetPath string) error {
			if s == stage {
				return errors.New("simulated power loss")
			}
			return nil
		}

		err := WriteFileAtomic(target, []byte("corrupted-partial"), 0o600)
		if err == nil {
			t.Fatalf("stage %d: expected error", stage)
		}
		got, err := os.ReadFile(target)
		if err != nil {
			t.Fatalf("read target after failed write: %v", err)
		}
		if string(got) != "original-data" {
			t.Fatalf("stage %d: target mutated on crash: got %q", stage, string(got))
		}
		// Ensure temp files were cleaned up.
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("readdir: %v", err)
		}
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), ".tmp-") {
				t.Fatalf("stage %d: leftover temp file %s", stage, e.Name())
			}
		}
	}
	testAtomicHook = nil
}

func TestLockContentionEBoot6(t *testing.T) {
	paths := NewPaths(t.TempDir())
	l1, err := AcquireLock(paths.LockFile)
	if err != nil {
		t.Fatalf("first AcquireLock failed: %v", err)
	}
	defer l1.Release()

	l2, err := AcquireLock(paths.LockFile)
	if !errors.Is(err, ErrLockHeld) {
		if l2 != nil {
			_ = l2.Release()
		}
		t.Fatalf("expected ErrLockHeld, got %v", err)
	}

	if err := l1.Release(); err != nil {
		t.Fatalf("release lock: %v", err)
	}
	l3, err := AcquireLock(paths.LockFile)
	if err != nil {
		t.Fatalf("re-acquire after release failed: %v", err)
	}
	_ = l3.Release()
}

func TestCorruptConfigEState3(t *testing.T) {
	paths := NewPaths(t.TempDir())
	if _, err := LoadConfig(paths.ConfigFile); !errors.Is(err, ErrConfigNotFound) {
		t.Fatalf("expected ErrConfigNotFound, got %v", err)
	}

	// Write corrupt JSON.
	if err := WriteFileAtomic(paths.ConfigFile, []byte("{bad-json"), 0o600); err != nil {
		t.Fatalf("seed corrupt config: %v", err)
	}
	_, err := LoadConfig(paths.ConfigFile)
	var corruptErr *CorruptFileError
	if !errors.As(err, &corruptErr) {
		t.Fatalf("expected *CorruptFileError, got %v", err)
	}
	// E-STATE-3: corrupt config must NOT be deleted or overwritten.
	raw, readErr := os.ReadFile(paths.ConfigFile)
	if readErr != nil || string(raw) != "{bad-json" {
		t.Fatalf("corrupt config was modified or deleted: raw=%q err=%v", string(raw), readErr)
	}
}

func TestCorruptPanelStateEState1(t *testing.T) {
	paths := NewPaths(t.TempDir())
	stPath := paths.StateFile("panel-1")
	if err := WriteFileAtomic(stPath, []byte("not json"), 0o600); err != nil {
		t.Fatalf("seed corrupt state: %v", err)
	}

	now := time.Date(2026, 11, 11, 4, 0, 0, 0, time.UTC)
	st, err := LoadPanelState(stPath, "panel-1", "vps-1", now)
	var corruptErr *CorruptFileError
	if !errors.As(err, &corruptErr) {
		t.Fatalf("expected *CorruptFileError, got %v", err)
	}
	if st == nil || st.PanelID != "panel-1" {
		t.Fatalf("expected fresh PanelState, got %+v", st)
	}
	if _, statErr := os.Stat(stPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("expected original corrupt state file to be quarantined, statErr=%v", statErr)
	}
	if _, statErr := os.Stat(corruptErr.Quarantine); statErr != nil {
		t.Fatalf("expected quarantine file %s to exist: %v", corruptErr.Quarantine, statErr)
	}
}

func TestCorruptClaimsRebuildEState2(t *testing.T) {
	paths := NewPaths(t.TempDir())
	panelsDir := paths.PanelGenDir(1)
	if err := os.MkdirAll(panelsDir, 0o755); err != nil {
		t.Fatalf("mkdir panelsDir: %v", err)
	}
	caddyA := `# Managed by nd edge
(nd_up_aaaaaaaa) {
	reverse_proxy https://1.2.3.4:443
}

app.example.com, www.app.example.com {
	import nd_common
	import nd_up_aaaaaaaa
}
`
	if err := WriteFileAtomic(filepath.Join(panelsDir, "panel-a.caddy"), []byte(caddyA), 0o644); err != nil {
		t.Fatalf("write panel-a.caddy: %v", err)
	}
	if err := WriteFileAtomic(filepath.Join(panelsDir, "00-placeholder.caddy"), []byte("# placeholder\n"), 0o644); err != nil {
		t.Fatalf("write placeholder: %v", err)
	}
	if err := WriteFileAtomic(paths.ClaimsFile, []byte("{broken"), 0o600); err != nil {
		t.Fatalf("write corrupt claims: %v", err)
	}

	now := time.Date(2026, 11, 11, 4, 0, 0, 0, time.UTC)
	cs, err := LoadClaims(paths.ClaimsFile, panelsDir, now)
	var corruptErr *CorruptFileError
	if !errors.As(err, &corruptErr) {
		t.Fatalf("expected *CorruptFileError, got %v", err)
	}
	if cs.Claims["app.example.com"].PanelID != "panel-a" {
		t.Fatalf("expected app.example.com rebuilt for panel-a, got %+v", cs.Claims)
	}
	if cs.Claims["www.app.example.com"].PanelID != "panel-a" {
		t.Fatalf("expected www.app.example.com rebuilt for panel-a, got %+v", cs.Claims)
	}
	if _, statErr := os.Stat(corruptErr.Quarantine); statErr != nil {
		t.Fatalf("expected quarantined claims file: %v", statErr)
	}
}

func TestClaimsFirstWinsReleaseTieBreakAndMove(t *testing.T) {
	now := time.Date(2026, 11, 11, 4, 0, 0, 0, time.UTC)
	cs := NewClaimsStore()

	t1 := now.Add(-2 * time.Hour)
	t2 := now.Add(-1 * time.Hour)

	// Tie on initial claim: earlier RegisteredAt wins (panel-a).
	owned, conflicts := cs.ResolveClaims([]Claimant{
		{PanelID: "panel-b", PanelName: "vps-b", RegisteredAt: t2, DesiredHosts: map[string]struct{}{"dup.com": {}}},
		{PanelID: "panel-a", PanelName: "vps-a", RegisteredAt: t1, DesiredHosts: map[string]struct{}{"dup.com": {}}},
	}, now)
	if _, ok := owned["panel-a"]["dup.com"]; !ok {
		t.Fatalf("expected panel-a to win tie-break")
	}
	if len(conflicts["panel-b"]) != 1 || conflicts["panel-b"][0].OwnerPanel != "vps-a" {
		t.Fatalf("expected conflict on panel-b pointing to vps-a, got %+v", conflicts["panel-b"])
	}

	// First-wins even if an even older panel-0 later asks for dup.com.
	t0 := now.Add(-3 * time.Hour)
	owned, conflicts = cs.ResolveClaims([]Claimant{
		{PanelID: "panel-0", PanelName: "vps-0", RegisteredAt: t0, DesiredHosts: map[string]struct{}{"dup.com": {}}},
		{PanelID: "panel-a", PanelName: "vps-a", RegisteredAt: t1, DesiredHosts: map[string]struct{}{"dup.com": {}}},
	}, now.Add(time.Minute))
	if _, ok := owned["panel-a"]["dup.com"]; !ok {
		t.Fatalf("expected existing owner panel-a to retain claim")
	}
	if len(conflicts["panel-0"]) != 1 {
		t.Fatalf("expected panel-0 to get conflict, got %+v", conflicts["panel-0"])
	}

	// Domain move (E-MP-2): panel-a drops dup.com -> panel-b acquires immediately.
	owned, conflicts = cs.ResolveClaims([]Claimant{
		{PanelID: "panel-a", PanelName: "vps-a", RegisteredAt: t1, DesiredHosts: map[string]struct{}{}},
		{PanelID: "panel-b", PanelName: "vps-b", RegisteredAt: t2, DesiredHosts: map[string]struct{}{"dup.com": {}}},
	}, now.Add(2*time.Minute))
	if _, ok := owned["panel-b"]["dup.com"]; !ok {
		t.Fatalf("expected panel-b to acquire dup.com after panel-a dropped it")
	}
	if len(conflicts["panel-b"]) != 0 {
		t.Fatalf("expected no conflicts after move, got %+v", conflicts["panel-b"])
	}

	// Same RegisteredAt tie-break -> lexical PanelID wins.
	cs2 := NewClaimsStore()
	owned2, _ := cs2.ResolveClaims([]Claimant{
		{PanelID: "panel-z", PanelName: "z", RegisteredAt: t1, DesiredHosts: map[string]struct{}{"tie.com": {}}},
		{PanelID: "panel-m", PanelName: "m", RegisteredAt: t1, DesiredHosts: map[string]struct{}{"tie.com": {}}},
	}, now)
	if _, ok := owned2["panel-m"]["tie.com"]; !ok {
		t.Fatalf("expected lexical panel-m to win equal-timestamp tie")
	}
}
