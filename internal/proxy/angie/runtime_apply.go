package angie

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"panel/internal/proxy"
)

// Validate runs `angie -t` against a staged configuration directory before live activation.
// When the proxy container is running and stagedDir resides under DataDir, validation
// executes inside the running container; otherwise it runs via a throwaway container.
func (d *Driver) Validate(ctx context.Context, stagedDir string) error {
	ctx = contextOrBackground(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	stagedDir = strings.TrimSpace(stagedDir)
	if stagedDir == "" {
		return errors.New("angie validate: staged directory is required")
	}
	confPath := filepath.Join(stagedDir, "angie.conf")
	if _, err := os.Stat(confPath); err != nil {
		return fmt.Errorf("angie validate: missing angie.conf in %s: %w", stagedDir, err)
	}
	if d != nil && d.hooks.RunValidate != nil {
		return d.hooks.RunValidate(ctx, stagedDir)
	}

	name := d.ContainerName()
	if rel, ok := d.relativeToDataDir(stagedDir); ok {
		if info, err := d.inspectContainer(ctx, name); err == nil && info.Running {
			containerConf := containerConfigRoot + "/" + filepath.ToSlash(rel) + "/angie.conf"
			out, execErr := d.execInContainer(ctx, name, "angie", "-t", "-c", containerConf)
			if execErr == nil {
				return nil
			}
			// During an engine switch the shared proxy container may still run another engine
			// (e.g. Ferron) that has no angie binary: validate in a throwaway container instead.
			if !isMissingBinaryError(out + " " + execErr.Error()) {
				return fmt.Errorf("angie -t failed: %s (%w)", strings.TrimSpace(out), execErr)
			}
		}
	}

	return d.validateWithEphemeralContainer(ctx, stagedDir)
}

func (d *Driver) validateWithEphemeralContainer(ctx context.Context, stagedDir string) error {
	absStaged, err := filepath.Abs(stagedDir)
	if err != nil {
		return fmt.Errorf("angie validate: resolve staged dir: %w", err)
	}
	image := AngieImage
	if d != nil && strings.TrimSpace(d.image) != "" {
		image = strings.TrimSpace(d.image)
	}

	args := []string{
		"run", "--rm", "--quiet",
		"-v", d.toHost(absStaged) + ":/etc/angie/nd/staged:ro",
	}
	if d != nil {
		for hostDir, mountTarget := range map[string]string{
			strings.TrimSpace(d.pkiSitesDir):    "/etc/nd-pki/sites:ro",
			strings.TrimSpace(d.pkiImportedDir): "/etc/nd-pki/imported:ro",
		} {
			if hostDir == "" {
				continue
			}
			if st, serr := os.Stat(hostDir); serr == nil && st.IsDir() {
				if absDir, aerr := filepath.Abs(hostDir); aerr == nil {
					args = append(args, "-v", d.toHost(absDir)+":"+mountTarget)
				}
			}
		}
	}
	args = append(args, image, "angie", "-t", "-c", "/etc/angie/nd/staged/angie.conf")

	cmd := exec.CommandContext(ctx, "docker", args...)
	out, runErr := cmd.CombinedOutput()
	if runErr != nil {
		return fmt.Errorf("angie -t failed: %s (%w)", strings.TrimSpace(string(out)), runErr)
	}
	return nil
}

// Apply activates rendered configuration files atomically using StageStore (when configured),
// reloads the running Angie master process, and rolls back on failure.
func (d *Driver) Apply(ctx context.Context, files proxy.Files) (proxy.ApplyResult, error) {
	ctx = contextOrBackground(ctx)
	start := time.Now()
	if err := ctx.Err(); err != nil {
		return proxy.ApplyResult{Driver: DriverName, AppliedAt: time.Now().UTC()}, err
	}
	if len(files) == 0 {
		return proxy.ApplyResult{Driver: DriverName, AppliedAt: time.Now().UTC()}, errors.New("angie apply: empty file set")
	}
	if d != nil && d.hooks.Apply != nil {
		res, err := d.hooks.Apply(ctx, files)
		if res.Driver == "" {
			res.Driver = DriverName
		}
		if res.AppliedAt.IsZero() {
			res.AppliedAt = time.Now().UTC()
		}
		return res, err
	}

	gen := filesDigest(files)
	if d != nil && d.stage != nil {
		return d.applyWithStageStore(ctx, gen, files, start)
	}

	if err := d.reloadContainer(ctx); err != nil {
		return proxy.ApplyResult{
			Generation: gen,
			Driver:     DriverName,
			Changed:    true,
			Reloaded:   false,
			Duration:   time.Since(start),
			AppliedAt:  time.Now().UTC(),
		}, err
	}
	return proxy.ApplyResult{
		Generation: gen,
		Driver:     DriverName,
		Changed:    true,
		Reloaded:   true,
		Duration:   time.Since(start),
		Message:    "reloaded",
		AppliedAt:  time.Now().UTC(),
	}, nil
}

func (d *Driver) applyWithStageStore(ctx context.Context, gen string, files proxy.Files, start time.Time) (proxy.ApplyResult, error) {
	// When Syncer.cfg.Stage already activated the target generation before calling Apply,
	// avoid a redundant stage/symlink swap and proceed directly to reload.
	if activeDirMatches(d.stage.LinkPath(), files) {
		if err := d.reloadContainer(ctx); err != nil {
			return proxy.ApplyResult{
				Generation: gen,
				Driver:     DriverName,
				Changed:    true,
				Reloaded:   false,
				Duration:   time.Since(start),
				AppliedAt:  time.Now().UTC(),
			}, err
		}
		return proxy.ApplyResult{
			Generation: gen,
			Driver:     DriverName,
			Changed:    true,
			Reloaded:   true,
			Duration:   time.Since(start),
			Message:    "reloaded active generation",
			AppliedAt:  time.Now().UTC(),
		}, nil
	}

	stagedDir, targetBase, err := d.stage.Stage(gen, files)
	if err != nil {
		return proxy.ApplyResult{
			Generation: gen,
			Driver:     DriverName,
			Changed:    true,
			Duration:   time.Since(start),
			AppliedAt:  time.Now().UTC(),
		}, err
	}
	if err := d.Validate(ctx, stagedDir); err != nil {
		d.stage.RemoveStaged(stagedDir)
		return proxy.ApplyResult{
			Generation: gen,
			Driver:     DriverName,
			Changed:    true,
			Duration:   time.Since(start),
			AppliedAt:  time.Now().UTC(),
		}, err
	}
	prevTarget, err := d.stage.Activate(targetBase)
	if err != nil {
		d.stage.RemoveStaged(stagedDir)
		return proxy.ApplyResult{
			Generation: gen,
			Driver:     DriverName,
			Changed:    true,
			Duration:   time.Since(start),
			AppliedAt:  time.Now().UTC(),
		}, err
	}
	if err := d.reloadContainer(ctx); err != nil {
		rbErr := d.stage.Rollback(prevTarget)
		rolledBack := rbErr == nil && prevTarget != ""
		finalErr := err
		if rbErr != nil {
			finalErr = errors.Join(err, fmt.Errorf("rollback failed: %w", rbErr))
		}
		return proxy.ApplyResult{
			Generation: gen,
			Driver:     DriverName,
			Changed:    true,
			Reloaded:   false,
			RolledBack: rolledBack,
			Duration:   time.Since(start),
			AppliedAt:  time.Now().UTC(),
		}, finalErr
	}
	_ = d.stage.Prune(targetBase, prevTarget)
	return proxy.ApplyResult{
		Generation: gen,
		Driver:     DriverName,
		Changed:    true,
		Reloaded:   true,
		Duration:   time.Since(start),
		Message:    "applied generation " + targetBase,
		AppliedAt:  time.Now().UTC(),
	}, nil
}

func (d *Driver) reloadContainer(ctx context.Context) error {
	name := d.ContainerName()
	if d != nil && d.hooks.Reload != nil {
		return d.hooks.Reload(ctx, name)
	}
	out, err := d.execInContainer(ctx, name, "angie", "-s", "reload", "-c", containerLiveConfig)
	if err != nil && strings.Contains(out, "angie.pid") {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(300 * time.Millisecond):
		}
		out, err = d.execInContainer(ctx, name, "angie", "-s", "reload", "-c", containerLiveConfig)
	}
	if err != nil && isPIDSignalError(out) {
		// The master was started from a config with a different (or no) `pid` directive, e.g.
		// an older bootstrap config: it can never be signalled. The live "sites" link already
		// points at the new generation, so a container restart starts the master on it.
		if rerr := d.Restart(ctx); rerr == nil {
			return nil
		} else {
			return fmt.Errorf("angie -s reload failed (%s) and restart failed: %w", strings.TrimSpace(out), rerr)
		}
	}
	if err != nil {
		return fmt.Errorf("angie -s reload failed: %s (%w)", strings.TrimSpace(out), err)
	}
	return nil
}

// isMissingBinaryError reports whether an exec failed because the container has no angie
// binary (it runs another proxy engine), as opposed to a configuration error.
func isMissingBinaryError(out string) bool {
	o := strings.ToLower(out)
	return strings.Contains(o, "executable file not found") ||
		strings.Contains(o, "exit status 127") ||
		strings.Contains(o, "exit status 126") ||
		strings.Contains(o, "no such file or directory: unknown")
}

// isPIDSignalError reports whether `angie -s reload` failed because the master's pid file is
// missing, empty or stale, rather than because of a config error.
func isPIDSignalError(out string) bool {
	o := strings.ToLower(out)
	return strings.Contains(o, "angie.pid") &&
		(strings.Contains(o, "invalid pid") || strings.Contains(o, "no such file") || strings.Contains(o, "no such process"))
}
