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

type recordedApply struct {
	gen    string
	driver string
	status string
	errMsg string
}

type mockSyncDriver struct {
	mu           sync.Mutex
	name         string
	renderFn     func(*State) (Files, error)
	validateFn   func(context.Context, string) error
	applyFn      func(context.Context, Files) (ApplyResult, error)
	renderCount  int
	validateDirs []string
	appliedFiles []Files
}

func (m *mockSyncDriver) Name() string          { return m.name }
func (m *mockSyncDriver) ContainerName() string { return "nd-proxy" }
func (m *mockSyncDriver) Capabilities() Capabilities {
	return Capabilities{AutoTLS: true}
}
func (m *mockSyncDriver) StackService(StackOptions) (map[string]any, map[string]any, error) {
	return nil, nil, nil
}
func (m *mockSyncDriver) AppLabels([]Site, UpstreamRef) map[string]string { return nil }
func (m *mockSyncDriver) Render(st *State) (Files, error) {
	m.mu.Lock()
	m.renderCount++
	m.mu.Unlock()
	if m.renderFn != nil {
		return m.renderFn(st)
	}
	return Files{"site.conf": fmt.Sprintf("# gen=%s\n", st.Generation)}, nil
}
func (m *mockSyncDriver) Validate(ctx context.Context, stagedDir string) error {
	m.mu.Lock()
	m.validateDirs = append(m.validateDirs, stagedDir)
	m.mu.Unlock()
	if m.validateFn != nil {
		return m.validateFn(ctx, stagedDir)
	}
	return nil
}
func (m *mockSyncDriver) Apply(ctx context.Context, files Files) (ApplyResult, error) {
	m.mu.Lock()
	m.appliedFiles = append(m.appliedFiles, cloneFiles(files))
	m.mu.Unlock()
	if m.applyFn != nil {
		return m.applyFn(ctx, files)
	}
	return ApplyResult{Reloaded: true, AppliedAt: time.Now().UTC()}, nil
}
func (m *mockSyncDriver) Status(context.Context) (Status, error)      { return Status{}, nil }
func (m *mockSyncDriver) Logs(context.Context, int) ([]string, error) { return nil, nil }
func (m *mockSyncDriver) Restart(context.Context) error               { return nil }

func TestNewSyncerValidationAndClose(t *testing.T) {
	if _, err := NewSyncer(SyncerConfig{}); !errors.Is(err, ErrDriverRequired) {
		t.Fatalf("expected ErrDriverRequired, got %v", err)
	}
	if _, err := NewSyncer(SyncerConfig{Driver: &mockSyncDriver{name: "angie"}}); !errors.Is(err, ErrBuilderRequired) {
		t.Fatalf("expected ErrBuilderRequired, got %v", err)
	}

	s, err := NewSyncer(SyncerConfig{
		Driver:     &mockSyncDriver{name: "caddy"},
		BuildState: func(context.Context) (*State, error) { return &State{}, nil },
	})
	if err != nil {
		t.Fatalf("NewSyncer: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := s.SyncNow(context.Background(), Reason{Kind: "after_close"}); !errors.Is(err, ErrSyncerClosed) {
		t.Fatalf("expected ErrSyncerClosed after Close, got %v", err)
	}
}

func TestSyncAsyncCoalescesBurst(t *testing.T) {
	drv := &mockSyncDriver{
		name:     "caddy",
		renderFn: func(*State) (Files, error) { return Files{}, nil },
	}
	var labelCalls atomic.Int32
	var coalescedCount atomic.Int32
	done := make(chan struct{})

	var records []recordedApply
	var recMu sync.Mutex

	s, err := NewSyncer(SyncerConfig{
		Driver: drv,
		BuildState: func(context.Context) (*State, error) {
			return &State{ACMEEmail: "ops@example.com"}, nil
		},
		LabelApply: func(_ context.Context, _ *State, reasons []Reason) (ApplyResult, error) {
			labelCalls.Add(1)
			coalescedCount.Store(int32(len(reasons)))
			close(done)
			return ApplyResult{Reloaded: true}, nil
		},
		RecordApply: func(_ context.Context, gen, driver, status, errMsg string) error {
			recMu.Lock()
			records = append(records, recordedApply{gen: gen, driver: driver, status: status, errMsg: errMsg})
			recMu.Unlock()
			return nil
		},
		Debounce: 25 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewSyncer: %v", err)
	}
	defer s.Close()

	for i := 0; i < 12; i++ {
		s.SyncAsync(Reason{Kind: "app_domain", ID: fmt.Sprintf("app-%d", i)})
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for coalesced SyncAsync batch")
	}

	time.Sleep(40 * time.Millisecond)
	if got := labelCalls.Load(); got != 1 {
		t.Fatalf("expected 1 coalesced LabelApply call, got %d", got)
	}
	if got := coalescedCount.Load(); got != 12 {
		t.Fatalf("expected 12 coalesced reasons, got %d", got)
	}
	recMu.Lock()
	defer recMu.Unlock()
	if len(records) != 1 || records[0].status != "ok" {
		t.Fatalf("unexpected apply records: %+v", records)
	}
}

func TestSyncNowValidationFailureLeavesLiveUntouched(t *testing.T) {
	baseDir := t.TempDir()
	stage, err := NewStageStore(StageConfig{BaseDir: baseDir, KeepGenerations: 3})
	if err != nil {
		t.Fatalf("NewStageStore: %v", err)
	}

	domain := "v1.example.com"
	var failValidate atomic.Bool
	drv := &mockSyncDriver{
		name: "angie",
		renderFn: func(st *State) (Files, error) {
			return Files{"site.conf": "server_name " + st.Sites[0].Hosts[0] + ";\n"}, nil
		},
		validateFn: func(_ context.Context, stagedDir string) error {
			if failValidate.Load() {
				if _, rerr := os.ReadFile(filepath.Join(stagedDir, "site.conf")); rerr != nil {
					return fmt.Errorf("expected staged file in %s: %w", stagedDir, rerr)
				}
				return errors.New("angie -t syntax error")
			}
			return nil
		},
	}

	var records []recordedApply
	s, err := NewSyncer(SyncerConfig{
		Driver: drv,
		Stage:  stage,
		BuildState: func(context.Context) (*State, error) {
			return &State{Sites: []Site{{ID: "s1", Hosts: []string{domain}}}}, nil
		},
		RecordApply: func(_ context.Context, gen, driver, status, errMsg string) error {
			records = append(records, recordedApply{gen: gen, driver: driver, status: status, errMsg: errMsg})
			return nil
		},
	})
	if err != nil {
		t.Fatalf("NewSyncer: %v", err)
	}
	defer s.Close()

	res1, err := s.SyncNow(context.Background(), Reason{Kind: "init"})
	if err != nil {
		t.Fatalf("initial SyncNow failed: %v", err)
	}
	liveTarget1, err := ReadDirLink(stage.LinkPath())
	if err != nil {
		t.Fatalf("ReadDirLink v1: %v", err)
	}

	domain = "broken.example.com"
	failValidate.Store(true)
	_, err = s.SyncNow(context.Background(), Reason{Kind: "app_domain", ID: "s1"})
	if err == nil || !strings.Contains(err.Error(), "angie -t syntax error") {
		t.Fatalf("expected synchronous validation error, got %v", err)
	}

	liveTargetAfter, err := ReadDirLink(stage.LinkPath())
	if err != nil {
		t.Fatalf("ReadDirLink after failed validate: %v", err)
	}
	if liveTargetAfter != liveTarget1 {
		t.Fatalf("live target changed on failed validation: got %q, want %q", liveTargetAfter, liveTarget1)
	}
	if s.LastGeneration() != res1.Generation {
		t.Fatalf("LastGeneration changed on failed validation: got %q, want %q", s.LastGeneration(), res1.Generation)
	}
	if len(drv.appliedFiles) != 1 {
		t.Fatalf("Apply must not be called on Validate failure; called %d times", len(drv.appliedFiles))
	}
	if len(records) != 2 || records[1].status != "error" {
		t.Fatalf("expected recorded error status, got %+v", records)
	}
}

func TestSyncIdempotentGenerationSkipAndForce(t *testing.T) {
	drv := &mockSyncDriver{name: "ferron"}
	s, err := NewSyncer(SyncerConfig{
		Driver: drv,
		BuildState: func(context.Context) (*State, error) {
			return &State{ACMEEmail: "ops@example.com"}, nil
		},
	})
	if err != nil {
		t.Fatalf("NewSyncer: %v", err)
	}
	defer s.Close()

	res1, err := s.SyncNow(context.Background(), Reason{Kind: "init"})
	if err != nil || !res1.Changed || !res1.Reloaded {
		t.Fatalf("first SyncNow unexpected: res=%+v err=%v", res1, err)
	}

	res2, err := s.SyncNow(context.Background(), Reason{Kind: "noop"})
	if err != nil || res2.Changed || res2.Reloaded || res2.Generation != res1.Generation {
		t.Fatalf("unchanged SyncNow must skip apply: res=%+v err=%v", res2, err)
	}
	if len(drv.appliedFiles) != 1 {
		t.Fatalf("expected 1 Apply call after skip, got %d", len(drv.appliedFiles))
	}

	res3, err := s.ForceSyncNow(context.Background(), Reason{Kind: "force"})
	if err != nil || !res3.Changed || !res3.Reloaded {
		t.Fatalf("ForceSyncNow unexpected: res=%+v err=%v", res3, err)
	}
	if len(drv.appliedFiles) != 2 {
		t.Fatalf("expected 2 Apply calls after ForceSyncNow, got %d", len(drv.appliedFiles))
	}
}

func TestSyncNowWhileInFlightBuildsFreshStateAndHonorsCancel(t *testing.T) {
	startedBatch1 := make(chan struct{})
	releaseBatch1 := make(chan struct{})
	var version atomic.Int32
	version.Store(1)

	drv := &mockSyncDriver{
		name: "angie",
		applyFn: func(context.Context, Files) (ApplyResult, error) {
			if version.Load() == 1 {
				select {
				case <-startedBatch1:
				default:
					close(startedBatch1)
				}
				<-releaseBatch1
			}
			return ApplyResult{Reloaded: true}, nil
		},
	}

	s, err := NewSyncer(SyncerConfig{
		Driver: drv,
		BuildState: func(context.Context) (*State, error) {
			v := version.Load()
			return &State{ACMEEmail: fmt.Sprintf("v%d@example.com", v)}, nil
		},
	})
	if err != nil {
		t.Fatalf("NewSyncer: %v", err)
	}
	defer s.Close()

	batch1Done := make(chan error, 1)
	go func() {
		_, err := s.SyncNow(context.Background(), Reason{Kind: "first"})
		batch1Done <- err
	}()
	<-startedBatch1

	version.Store(2)
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, cerr := s.SyncNow(canceledCtx, Reason{Kind: "canceled"}); !errors.Is(cerr, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", cerr)
	}

	batch2Done := make(chan ApplyResult, 1)
	go func() {
		res, _ := s.SyncNow(context.Background(), Reason{Kind: "second"})
		batch2Done <- res
	}()

	close(releaseBatch1)
	if err := <-batch1Done; err != nil {
		t.Fatalf("batch1 err: %v", err)
	}
	res2 := <-batch2Done
	wantV2Gen := (&State{ACMEEmail: "v2@example.com"}).ComputeGeneration()
	if res2.Generation != wantV2Gen {
		t.Fatalf("in-flight SyncNow returned stale generation %q, want %q", res2.Generation, wantV2Gen)
	}
}

func TestSyncShutdownContextDoesNotBreakRollbackOrRecordApply(t *testing.T) {
	var host string = "v1.example.com"
	var mu sync.Mutex
	var failNext atomic.Bool
	inHealthCheck := make(chan struct{})
	proceedHealthCheck := make(chan struct{})
	var rollbackCtxErr error
	var recordCtxErrs []error
	var recCount atomic.Int32

	drv := &mockSyncDriver{
		name: "angie",
		renderFn: func(st *State) (Files, error) {
			return Files{"site.conf": st.Sites[0].Hosts[0]}, nil
		},
	}

	var s *Syncer
	var err error
	s, err = NewSyncer(SyncerConfig{
		Driver: drv,
		BuildState: func(context.Context) (*State, error) {
			mu.Lock()
			h := host
			mu.Unlock()
			return &State{Sites: []Site{{ID: "s1", Hosts: []string{h}}}}, nil
		},
		HealthCheck: func(context.Context, Driver, *State, ApplyResult) error {
			if failNext.Load() {
				close(inHealthCheck)
				<-proceedHealthCheck
				return errors.New("health failed during shutdown")
			}
			return nil
		},
		Rollback: func(ctx context.Context, _ string, _ Files) error {
			mu.Lock()
			rollbackCtxErr = ctx.Err()
			mu.Unlock()
			return nil
		},
		RecordApply: func(ctx context.Context, _, _, _, _ string) error {
			mu.Lock()
			recordCtxErrs = append(recordCtxErrs, ctx.Err())
			mu.Unlock()
			recCount.Add(1)
			return nil
		},
	})
	if err != nil {
		t.Fatalf("NewSyncer: %v", err)
	}

	if _, err := s.SyncNow(context.Background(), Reason{Kind: "init"}); err != nil {
		t.Fatalf("init SyncNow: %v", err)
	}

	mu.Lock()
	host = "v2.example.com"
	mu.Unlock()
	failNext.Store(true)

	batch2Done := make(chan ApplyResult, 1)
	go func() {
		res, _ := s.SyncNow(context.Background(), Reason{Kind: "v2"})
		batch2Done <- res
	}()

	<-inHealthCheck
	// Cancel baseCtx via Close() in a goroutine while releasing HealthCheck right after.
	closeDone := make(chan error, 1)
	go func() {
		closeDone <- s.Close()
	}()
	time.Sleep(15 * time.Millisecond)
	close(proceedHealthCheck)

	if err := <-closeDone; err != nil {
		t.Fatalf("Close: %v", err)
	}
	res2 := <-batch2Done
	if !res2.RolledBack {
		t.Fatalf("expected RolledBack=true despite shutdown cancellation, got %+v", res2)
	}
	mu.Lock()
	defer mu.Unlock()
	if rollbackCtxErr != nil {
		t.Fatalf("Rollback received canceled ctx: %v", rollbackCtxErr)
	}
	if len(recordCtxErrs) != 2 || recordCtxErrs[1] != nil {
		t.Fatalf("RecordApply received canceled ctx: %+v", recordCtxErrs)
	}
}

func TestSyncDriverInternalRollbackAvoidsDoubleApply(t *testing.T) {
	var failDriver atomic.Bool
	var applyCount atomic.Int32
	var records []recordedApply

	drv := &mockSyncDriver{
		name: "angie",
		applyFn: func(context.Context, Files) (ApplyResult, error) {
			applyCount.Add(1)
			if failDriver.Load() {
				return ApplyResult{RolledBack: true}, errors.New("reload failed; driver rolled back")
			}
			return ApplyResult{Reloaded: true}, nil
		},
	}
	host := "v1.example.com"
	s, err := NewSyncer(SyncerConfig{
		Driver: drv,
		BuildState: func(context.Context) (*State, error) {
			return &State{Sites: []Site{{ID: "s1", Hosts: []string{host}}}}, nil
		},
		RecordApply: func(_ context.Context, gen, driver, status, errMsg string) error {
			records = append(records, recordedApply{gen: gen, driver: driver, status: status, errMsg: errMsg})
			return nil
		},
	})
	if err != nil {
		t.Fatalf("NewSyncer: %v", err)
	}
	defer s.Close()

	if _, err := s.SyncNow(context.Background(), Reason{Kind: "init"}); err != nil {
		t.Fatalf("v1 SyncNow: %v", err)
	}
	host = "v2.example.com"
	failDriver.Store(true)

	res, err := s.SyncNow(context.Background(), Reason{Kind: "v2"})
	if err == nil || !res.RolledBack {
		t.Fatalf("expected RolledBack=true and error, got res=%+v err=%v", res, err)
	}
	if got := applyCount.Load(); got != 2 {
		t.Fatalf("expected exactly 2 Driver.Apply calls (no double rollback), got %d", got)
	}
	if len(records) != 2 || records[1].status != "rolled_back" {
		t.Fatalf("expected rolled_back record, got %+v", records)
	}
}
