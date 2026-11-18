package proxy

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type spyLocker struct {
	locks   atomic.Int32
	unlocks atomic.Int32
	mu      sync.Mutex
}

func (l *spyLocker) Lock() {
	l.locks.Add(1)
	l.mu.Lock()
}

func (l *spyLocker) Unlock() {
	l.unlocks.Add(1)
	l.mu.Unlock()
}

func TestSyncHealthFailureTriggersAutomaticRollback(t *testing.T) {
	baseDir := t.TempDir()
	stage, err := NewStageStore(StageConfig{BaseDir: baseDir, KeepGenerations: 3})
	if err != nil {
		t.Fatalf("NewStageStore: %v", err)
	}

	var mu sync.Mutex
	host := "v1.example.com"
	var failHealth atomic.Bool
	var records []recordedApply
	composeLock := &spyLocker{}

	drv := &mockSyncDriver{
		name: "angie",
		renderFn: func(st *State) (Files, error) {
			return Files{"site.conf": st.Sites[0].Hosts[0]}, nil
		},
	}

	s, err := NewSyncer(SyncerConfig{
		Driver:    drv,
		Stage:     stage,
		ComposeMu: composeLock,
		BuildState: func(context.Context) (*State, error) {
			mu.Lock()
			h := host
			mu.Unlock()
			return &State{Sites: []Site{{ID: "s1", Hosts: []string{h}}}}, nil
		},
		HealthCheck: func(context.Context, Driver, *State, ApplyResult) error {
			if failHealth.Load() {
				return errors.New("probe 502 bad gateway")
			}
			return nil
		},
		RecordApply: func(_ context.Context, gen, driver, status, errMsg string) error {
			mu.Lock()
			records = append(records, recordedApply{gen: gen, driver: driver, status: status, errMsg: errMsg})
			mu.Unlock()
			return nil
		},
	})
	if err != nil {
		t.Fatalf("NewSyncer: %v", err)
	}
	defer s.Close()

	res1, err := s.SyncNow(context.Background(), Reason{Kind: "init"})
	if err != nil {
		t.Fatalf("SyncNow v1 failed: %v", err)
	}
	v1Target, err := ReadDirLink(stage.LinkPath())
	if err != nil {
		t.Fatalf("ReadDirLink v1: %v", err)
	}

	mu.Lock()
	host = "v2.example.com"
	mu.Unlock()
	failHealth.Store(true)

	res2, err := s.SyncNow(context.Background(), Reason{Kind: "update"})
	if err == nil || !strings.Contains(err.Error(), "probe 502") {
		t.Fatalf("expected health failure error, got %v", err)
	}
	if !res2.RolledBack {
		t.Fatalf("expected RolledBack=true on health check failure, got %+v", res2)
	}

	afterTarget, err := ReadDirLink(stage.LinkPath())
	if err != nil {
		t.Fatalf("ReadDirLink after rollback: %v", err)
	}
	if afterTarget != v1Target {
		t.Fatalf("expected live link restored to %q, got %q", v1Target, afterTarget)
	}
	if s.LastGeneration() != res1.Generation {
		t.Fatalf("LastGeneration should stay %q after rollback, got %q", res1.Generation, s.LastGeneration())
	}
	if len(drv.appliedFiles) != 3 || drv.appliedFiles[2]["site.conf"] != "v1.example.com" {
		t.Fatalf("expected rollback Apply with v1 files, got %+v", drv.appliedFiles)
	}
	if composeLock.locks.Load() != 3 || composeLock.unlocks.Load() != 3 {
		t.Fatalf("expected ComposeMu balanced across 3 Apply calls, got locks=%d unlocks=%d", composeLock.locks.Load(), composeLock.unlocks.Load())
	}

	mu.Lock()
	defer mu.Unlock()
	if len(records) != 2 || records[1].status != "rolled_back" {
		t.Fatalf("expected recorded rolled_back status, got %+v", records)
	}
}

func TestStageStoreRelativeSymlinkAndPruning(t *testing.T) {
	baseDir := t.TempDir()
	stage, err := NewStageStore(StageConfig{
		BaseDir:         baseDir,
		LinkName:        "sites",
		KeepGenerations: 2,
	})
	if err != nil {
		t.Fatalf("NewStageStore: %v", err)
	}

	if err := SwapDirLinkAtomic(stage.LinkPath(), "/abs/path"); err == nil {
		t.Fatal("expected error for absolute symlink target")
	}
	if err := SwapDirLinkAtomic(stage.LinkPath(), "../escape"); err == nil {
		t.Fatal("expected error for parent traversal symlink target")
	}
	if _, _, err := stage.Stage("gen0", Files{"../evil.conf": "x"}); err == nil {
		t.Fatal("expected error for staged file traversal")
	}

	var targets []string
	for i := 1; i <= 5; i++ {
		gen := fmt.Sprintf("gen%dabcdef1234", i)
		_, targetBase, err := stage.Stage(gen, Files{"site.conf": fmt.Sprintf("v=%d\n", i)})
		if err != nil {
			t.Fatalf("Stage %d: %v", i, err)
		}
		if filepath.IsAbs(targetBase) || strings.ContainsAny(targetBase, `/\`) {
			t.Fatalf("targetBase %q must be relative single segment", targetBase)
		}
		prev, err := stage.Activate(targetBase)
		if err != nil {
			t.Fatalf("Activate %d: %v", i, err)
		}
		if err := stage.Prune(targetBase, prev); err != nil {
			t.Fatalf("Prune %d: %v", i, err)
		}
		targets = append(targets, targetBase)
		time.Sleep(5 * time.Millisecond)
	}

	activeTarget := targets[len(targets)-1]
	prevTarget := targets[len(targets)-2]

	if rawLink, rerr := os.Readlink(stage.LinkPath()); rerr == nil {
		if filepath.IsAbs(rawLink) || rawLink != activeTarget {
			t.Fatalf("os.Readlink = %q, want relative %q", rawLink, activeTarget)
		}
	}
	gotLink, err := ReadDirLink(stage.LinkPath())
	if err != nil || gotLink != activeTarget {
		t.Fatalf("ReadDirLink = (%q, %v), want %q", gotLink, err, activeTarget)
	}

	if _, err := os.Stat(filepath.Join(baseDir, activeTarget)); err != nil {
		t.Fatalf("active generation directory missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(baseDir, prevTarget)); err != nil {
		t.Fatalf("previous generation directory missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(baseDir, targets[0])); !os.IsNotExist(err) {
		t.Fatalf("expected oldest generation %q to be pruned, stat err=%v", targets[0], err)
	}
}

func TestSyncPostRestartFirstApplyFailureReloadsDiskPrevTarget(t *testing.T) {
	baseDir := t.TempDir()
	stage, err := NewStageStore(StageConfig{BaseDir: baseDir, KeepGenerations: 3})
	if err != nil {
		t.Fatalf("NewStageStore: %v", err)
	}

	// Simulate pre-existing v1 generation on disk prior to panel restart.
	_, v1Target, err := stage.Stage("gen-v1", Files{"site.conf": "server_name old.example.com;\n"})
	if err != nil {
		t.Fatalf("Stage v1: %v", err)
	}
	if _, err := stage.Activate(v1Target); err != nil {
		t.Fatalf("Activate v1: %v", err)
	}

	var mu sync.Mutex
	var records []recordedApply
	drv := &mockSyncDriver{
		name: "angie",
		renderFn: func(st *State) (Files, error) {
			return Files{"site.conf": "server_name " + st.Sites[0].Hosts[0] + ";\n"}, nil
		},
	}

	// Fresh Syncer (hasApplied == false, lastFiles == nil).
	s, err := NewSyncer(SyncerConfig{
		Driver: drv,
		Stage:  stage,
		BuildState: func(context.Context) (*State, error) {
			return &State{Sites: []Site{{ID: "s1", Hosts: []string{"new.example.com"}}}}, nil
		},
		HealthCheck: func(context.Context, Driver, *State, ApplyResult) error {
			return errors.New("probe 502 after reload")
		},
		RecordApply: func(_ context.Context, gen, driver, status, errMsg string) error {
			mu.Lock()
			records = append(records, recordedApply{gen: gen, driver: driver, status: status, errMsg: errMsg})
			mu.Unlock()
			return nil
		},
	})
	if err != nil {
		t.Fatalf("NewSyncer: %v", err)
	}
	defer s.Close()

	res, err := s.SyncNow(context.Background(), Reason{Kind: "restart_update"})
	if err == nil || !strings.Contains(err.Error(), "probe 502") {
		t.Fatalf("expected health check failure, got %v", err)
	}
	if !res.RolledBack {
		t.Fatalf("expected RolledBack=true after reloading restored v1 files from disk, got %+v", res)
	}
	afterLink, err := ReadDirLink(stage.LinkPath())
	if err != nil || afterLink != v1Target {
		t.Fatalf("expected symlink restored to %q, got (%q, %v)", v1Target, afterLink, err)
	}
	if len(drv.appliedFiles) != 2 || drv.appliedFiles[1]["site.conf"] != "server_name old.example.com;\n" {
		t.Fatalf("expected second Apply call to reload v1 files from disk, got %+v", drv.appliedFiles)
	}
	mu.Lock()
	if len(records) != 1 || records[0].status != "rolled_back" {
		t.Fatalf("expected rolled_back record, got %+v", records)
	}
	mu.Unlock()

	// Now simulate corrupt/missing prevTarget directory: must NOT falsely report RolledBack=true.
	if err := os.RemoveAll(filepath.Join(baseDir, v1Target)); err != nil {
		t.Fatalf("RemoveAll v1Target: %v", err)
	}
	resMissing, err := s.SyncNow(context.Background(), Reason{Kind: "missing_prev_dir"})
	if err == nil || !strings.Contains(err.Error(), "rollback failed") {
		t.Fatalf("expected rollback failure error when prevTarget missing, got %v", err)
	}
	if resMissing.RolledBack {
		t.Fatalf("must not report RolledBack=true when reload could not run: %+v", resMissing)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(records) != 2 || records[1].status != "error" {
		t.Fatalf("expected error record when rollback reload failed, got %+v", records)
	}
}
