package proxy

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

func (s *Syncer) executeBatch(ctx context.Context, reasons []Reason, force bool) (ApplyResult, error) {
	start := s.now()
	drv := s.cfg.Driver
	drvName := drv.Name()

	st, err := s.cfg.BuildState(ctx)
	if err != nil {
		s.record(ctx, "", drvName, "error", err.Error())
		return ApplyResult{Driver: drvName, Duration: s.now().Sub(start), AppliedAt: s.now().UTC()}, err
	}
	if st == nil {
		st = &State{}
	}
	gen := st.ComputeGeneration()
	st.Generation = gen

	s.mu.Lock()
	hasApplied, lastGen, lastFiles, lastState := s.hasApplied, s.lastGen, s.lastFiles, s.lastState
	s.mu.Unlock()

	if !force && hasApplied && gen == lastGen {
		return ApplyResult{
			Generation: gen,
			Driver:     drvName,
			Changed:    false,
			Reloaded:   false,
			Duration:   s.now().Sub(start),
			Message:    "unchanged",
			AppliedAt:  s.now().UTC(),
		}, nil
	}

	files, err := drv.Render(st)
	if err != nil {
		s.record(ctx, gen, drvName, "error", err.Error())
		return ApplyResult{Generation: gen, Driver: drvName, Changed: true, Duration: s.now().Sub(start), AppliedAt: s.now().UTC()}, err
	}

	stagedDir, targetBase, cleanupStage, err := s.prepareStage(gen, files)
	if err != nil {
		s.record(ctx, gen, drvName, "error", err.Error())
		return ApplyResult{Generation: gen, Driver: drvName, Changed: true, Duration: s.now().Sub(start), AppliedAt: s.now().UTC()}, err
	}
	defer cleanupStage()

	if err := drv.Validate(ctx, stagedDir); err != nil {
		if s.cfg.Stage != nil {
			s.cfg.Stage.RemoveStaged(stagedDir)
		}
		s.record(ctx, gen, drvName, "error", err.Error())
		return ApplyResult{Generation: gen, Driver: drvName, Changed: true, Duration: s.now().Sub(start), AppliedAt: s.now().UTC()}, err
	}

	var prevTarget string
	if s.cfg.Stage != nil && targetBase != "" {
		prevTarget, err = s.cfg.Stage.Activate(targetBase)
		if err != nil {
			s.cfg.Stage.RemoveStaged(stagedDir)
			s.record(ctx, gen, drvName, "error", err.Error())
			return ApplyResult{Generation: gen, Driver: drvName, Changed: true, Duration: s.now().Sub(start), AppliedAt: s.now().UTC()}, err
		}
	}

	res, applyErr := s.applyRendered(ctx, st, files, reasons)
	res.Generation, res.Driver, res.Changed = gen, drvName, true
	if applyErr != nil {
		if res.RolledBack {
			res.Duration = s.now().Sub(start)
			res.AppliedAt = s.now().UTC()
			s.record(ctx, gen, drvName, "rolled_back", applyErr.Error())
			return res, applyErr
		}
		return s.handleApplyFailure(ctx, gen, drvName, start, res, applyErr, hasApplied, lastGen, lastFiles, lastState, reasons, targetBase, prevTarget)
	}
	if s.cfg.HealthCheck != nil {
		if herr := s.cfg.HealthCheck(ctx, drv, st, res); herr != nil {
			return s.handleApplyFailure(ctx, gen, drvName, start, res, herr, hasApplied, lastGen, lastFiles, lastState, reasons, targetBase, prevTarget)
		}
	}

	s.mu.Lock()
	s.hasApplied, s.lastGen, s.lastFiles, s.lastState = true, gen, cloneFiles(files), st
	s.mu.Unlock()
	if s.cfg.Stage != nil && targetBase != "" {
		_ = s.cfg.Stage.Prune(targetBase, prevTarget)
	}
	res.Duration = s.now().Sub(start)
	if res.AppliedAt.IsZero() {
		res.AppliedAt = s.now().UTC()
	}
	s.record(ctx, gen, drvName, "ok", "")
	return res, nil
}

func (s *Syncer) prepareStage(gen string, files Files) (stagedDir, targetBase string, cleanup func(), err error) {
	cleanup = func() {}
	if len(files) == 0 {
		return "", "", cleanup, nil
	}
	if s.cfg.Stage != nil {
		stagedDir, targetBase, err = s.cfg.Stage.Stage(gen, files)
		return stagedDir, targetBase, cleanup, err
	}
	tmpDir, err := os.MkdirTemp("", "nd-proxy-stage-*")
	if err != nil {
		return "", "", cleanup, fmt.Errorf("create temp stage dir: %w", err)
	}
	for rel, content := range files {
		cleanRel, verr := sanitizeRelFilePath(rel)
		if verr != nil {
			_ = os.RemoveAll(tmpDir)
			return "", "", cleanup, verr
		}
		if werr := WriteFileAtomic(filepath.Join(tmpDir, cleanRel), []byte(content), 0o644); werr != nil {
			_ = os.RemoveAll(tmpDir)
			return "", "", cleanup, werr
		}
	}
	return tmpDir, "", func() { _ = os.RemoveAll(tmpDir) }, nil
}

func (s *Syncer) applyRendered(ctx context.Context, st *State, files Files, reasons []Reason) (ApplyResult, error) {
	if s.cfg.ComposeMu != nil {
		s.cfg.ComposeMu.Lock()
		defer s.cfg.ComposeMu.Unlock()
	}
	if len(files) == 0 && s.cfg.LabelApply != nil {
		return s.cfg.LabelApply(ctx, st, reasons)
	}
	return s.cfg.Driver.Apply(ctx, files)
}

func (s *Syncer) handleApplyFailure(
	ctx context.Context,
	gen, drvName string,
	start time.Time,
	res ApplyResult,
	applyErr error,
	hasApplied bool,
	lastGen string,
	lastFiles Files,
	lastState *State,
	reasons []Reason,
	targetBase, prevTarget string,
) (ApplyResult, error) {
	// Detach from shutdown cancellation so rollback reload and audit recording complete cleanly.
	rbCtx, rbCancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	defer rbCancel()

	if s.cfg.Stage != nil && targetBase != "" {
		if linkErr := s.cfg.Stage.Rollback(prevTarget); linkErr != nil {
			applyErr = errors.Join(applyErr, fmt.Errorf("restore symlink failed: %w", linkErr))
		}
	}
	canRollback := hasApplied || prevTarget != ""
	status := "error"
	finalErr := applyErr
	if canRollback {
		var rbErr error
		switch {
		case s.cfg.Rollback != nil:
			rbErr = s.cfg.Rollback(rbCtx, lastGen, lastFiles)
		case len(lastFiles) > 0:
			_, rbErr = s.applyRendered(rbCtx, lastState, lastFiles, reasons)
		case s.cfg.Stage != nil && prevTarget != "":
			restoredFiles, rerr := s.cfg.Stage.ReadTargetFiles(prevTarget)
			if rerr != nil {
				rbErr = rerr
			} else {
				_, rbErr = s.applyRendered(rbCtx, nil, restoredFiles, reasons)
			}
		case len(lastFiles) == 0 && s.cfg.LabelApply != nil && lastState != nil:
			_, rbErr = s.applyRendered(rbCtx, lastState, nil, reasons)
		default:
			rbErr = errors.New("no previous generation state available to reload")
		}
		if rbErr == nil {
			res.RolledBack = true
			status = "rolled_back"
		} else {
			s.mu.Lock()
			s.hasApplied = false
			s.mu.Unlock()
			finalErr = errors.Join(applyErr, fmt.Errorf("rollback failed: %w", rbErr))
		}
	}
	res.Duration = s.now().Sub(start)
	res.AppliedAt = s.now().UTC()
	s.record(rbCtx, gen, drvName, status, finalErr.Error())
	return res, finalErr
}

func (s *Syncer) record(ctx context.Context, gen, drvName, status, errMsg string) {
	if s.cfg.RecordApply == nil {
		return
	}
	recCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	_ = s.cfg.RecordApply(recCtx, gen, drvName, status, errMsg)
}

func cloneFiles(in Files) Files {
	if len(in) == 0 {
		return nil
	}
	out := make(Files, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
