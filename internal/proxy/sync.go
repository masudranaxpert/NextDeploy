package proxy

import (
	"context"
	"errors"
	"sync"
	"time"
)

const defaultSyncDebounce = 300 * time.Millisecond

var (
	// ErrSyncerClosed is returned by SyncNow when the Syncer has already been closed.
	ErrSyncerClosed = errors.New("proxy syncer closed")
	// ErrDriverRequired is returned by NewSyncer when Driver is nil.
	ErrDriverRequired = errors.New("proxy driver is required")
	// ErrBuilderRequired is returned by NewSyncer when BuildState is nil.
	ErrBuilderRequired = errors.New("state builder is required")
)

// BuildStateFunc builds the complete desired-state IR from current store state.
type BuildStateFunc func(ctx context.Context) (*State, error)

// HealthCheckFunc verifies proxy health after Apply completes.
type HealthCheckFunc func(ctx context.Context, drv Driver, st *State, res ApplyResult) error

// RecordApplyFunc persists the outcome of a proxy apply attempt.
type RecordApplyFunc func(ctx context.Context, generation, driver, status, errMsg string) error

// LabelApplyFunc applies label-driven proxy updates when Driver.Render returns empty Files.
type LabelApplyFunc func(ctx context.Context, st *State, reasons []Reason) (ApplyResult, error)

// RollbackFunc restores a prior proxy generation after a failed apply or health check.
type RollbackFunc func(ctx context.Context, prevGen string, prevFiles Files) error

// SyncerConfig configures a Syncer instance. Driver and BuildState are required.
//
// Stage is optional: when nil, Syncer writes files to a temporary staging directory
// for Driver.Validate and delegates live atomic swap/reload to Driver.Apply. Set Stage
// only when Syncer (rather than Driver.Apply) owns generation symlink switching.
type SyncerConfig struct {
	Driver      Driver
	BuildState  BuildStateFunc
	LabelApply  LabelApplyFunc
	HealthCheck HealthCheckFunc
	Rollback    RollbackFunc
	RecordApply RecordApplyFunc
	Stage       *StageStore
	ComposeMu   sync.Locker
	Debounce    time.Duration
	Now         func() time.Time
}

type batchOutcome struct {
	res ApplyResult
	err error
}

// Syncer coalesces proxy state change triggers into serialized Render -> Validate ->
// Apply -> Health batches, skips unchanged IR generations, and rolls back on failure.
type Syncer struct {
	cfg      SyncerConfig
	debounce time.Duration
	now      func() time.Time

	baseCtx    context.Context
	baseCancel context.CancelFunc

	mu               sync.Mutex
	closed           bool
	running          bool
	pending          bool
	pendingImmediate bool
	pendingForce     bool
	pendingAt        time.Time
	pendingReasons   []Reason
	waiters          []chan batchOutcome
	timer            *time.Timer
	wg               sync.WaitGroup

	hasApplied bool
	lastGen    string
	lastFiles  Files
	lastState  *State
}

// NewSyncer validates cfg and returns an initialized Syncer.
func NewSyncer(cfg SyncerConfig) (*Syncer, error) {
	if cfg.Driver == nil {
		return nil, ErrDriverRequired
	}
	if cfg.BuildState == nil {
		return nil, ErrBuilderRequired
	}
	deb := cfg.Debounce
	if deb <= 0 {
		deb = defaultSyncDebounce
	}
	nowFn := cfg.Now
	if nowFn == nil {
		nowFn = time.Now
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Syncer{
		cfg:        cfg,
		debounce:   deb,
		now:        nowFn,
		baseCtx:    ctx,
		baseCancel: cancel,
	}, nil
}

// SyncAsync schedules a debounced proxy sync batch, coalescing rapid triggers
// within the configured debounce window.
func (s *Syncer) SyncAsync(reason Reason) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.pending = true
	s.pendingAt = s.now()
	s.pendingReasons = append(s.pendingReasons, reason)
	if !s.running && !s.pendingImmediate {
		s.resetTimerLocked(s.debounce)
	}
}

// SyncNow triggers or joins the next sync batch immediately (bypassing debounce)
// and blocks until Render -> Validate -> Apply -> Health completes or ctx cancels.
func (s *Syncer) SyncNow(ctx context.Context, reason Reason) (ApplyResult, error) {
	return s.syncWait(ctx, reason, false)
}

// ForceSyncNow behaves like SyncNow but bypasses the idempotent generation skip.
func (s *Syncer) ForceSyncNow(ctx context.Context, reason Reason) (ApplyResult, error) {
	return s.syncWait(ctx, reason, true)
}

func (s *Syncer) syncWait(ctx context.Context, reason Reason, force bool) (ApplyResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return ApplyResult{}, err
	}

	ch := make(chan batchOutcome, 1)
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return ApplyResult{}, ErrSyncerClosed
	}
	s.pending = true
	s.pendingImmediate = true
	if force {
		s.pendingForce = true
	}
	s.pendingReasons = append(s.pendingReasons, reason)
	s.waiters = append(s.waiters, ch)
	if s.timer != nil {
		s.timer.Stop()
		s.timer = nil
	}
	if !s.running {
		s.running = true
		s.wg.Add(1)
		go s.runWorker()
	}
	s.mu.Unlock()

	select {
	case out := <-ch:
		return out.res, out.err
	case <-ctx.Done():
		return ApplyResult{}, ctx.Err()
	}
}

// LastGeneration returns the most recently successfully applied IR generation hash.
func (s *Syncer) LastGeneration() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastGen
}

// Close stops pending timers, cancels in-flight syncs, unblocks waiting SyncNow callers,
// and waits for any running batch to finish.
func (s *Syncer) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		s.wg.Wait()
		return nil
	}
	s.closed = true
	if s.timer != nil {
		s.timer.Stop()
		s.timer = nil
	}
	waiters := s.waiters
	s.waiters = nil
	s.pending = false
	s.pendingReasons = nil
	s.mu.Unlock()

	s.baseCancel()
	for _, w := range waiters {
		w <- batchOutcome{err: ErrSyncerClosed}
	}
	s.wg.Wait()
	return nil
}

func (s *Syncer) resetTimerLocked(d time.Duration) {
	if s.timer != nil {
		s.timer.Stop()
	}
	s.timer = time.AfterFunc(d, s.onTimer)
}

func (s *Syncer) onTimer() {
	s.mu.Lock()
	if s.closed || !s.pending || s.running {
		s.mu.Unlock()
		return
	}
	s.timer = nil
	s.running = true
	s.wg.Add(1)
	s.mu.Unlock()
	go s.runWorker()
}

func (s *Syncer) runWorker() {
	defer s.wg.Done()
	for {
		s.mu.Lock()
		if s.closed || !s.pending {
			s.running = false
			s.mu.Unlock()
			return
		}
		reasons := s.pendingReasons
		waiters := s.waiters
		force := s.pendingForce
		s.pending = false
		s.pendingImmediate = false
		s.pendingForce = false
		s.pendingReasons = nil
		s.waiters = nil
		if s.timer != nil {
			s.timer.Stop()
			s.timer = nil
		}
		s.mu.Unlock()

		res, err := s.executeBatch(s.baseCtx, reasons, force)
		out := batchOutcome{res: res, err: err}
		for _, w := range waiters {
			w <- out
		}

		s.mu.Lock()
		if s.closed || !s.pending {
			s.running = false
			s.mu.Unlock()
			return
		}
		if s.pendingImmediate {
			s.mu.Unlock()
			continue
		}
		rem := s.debounce - s.now().Sub(s.pendingAt)
		if rem <= 0 {
			s.mu.Unlock()
			continue
		}
		s.resetTimerLocked(rem)
		s.running = false
		s.mu.Unlock()
		return
	}
}
