package handlers

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"panel/internal/dockerx"
)

type migrateRunningStack struct {
	appID    string
	appName  string
	project  string
	dir      string
	paths    []string
	envFiles []string
}

func composeDownBenign(output string) bool {
	low := strings.ToLower(strings.TrimSpace(output))
	if low == "" {
		return true
	}
	return strings.Contains(low, "no resource found") ||
		strings.Contains(low, "warning") && strings.Contains(low, "no resource")
}

func (p *Panel) migrateQuiescePanel(ctx context.Context, logf func(string)) (func(context.Context), error) {
	apps, err := p.DB.ListApps(ctx)
	if err != nil {
		return nil, fmt.Errorf("list apps: %w", err)
	}
	if len(apps) == 0 {
		return nil, nil
	}

	logf(fmt.Sprintf("stopping %d app(s) across the panel for a consistent snapshot", len(apps)))

	var toRestart []migrateRunningStack
	rollback := func() {
		if len(toRestart) == 0 {
			return
		}
		rbCtx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
		defer cancel()
		logf("rolling back: restarting apps stopped before failed quiesce")
		for _, stack := range toRestart {
			v, _ := p.ComposeMu.LoadOrStore(stack.appID, &sync.Mutex{})
			mu := v.(*sync.Mutex)
			mu.Lock()
			_ = dockerx.ComposeUp(rbCtx, stack.dir, stack.paths, stack.project, nil, stack.envFiles)
			mu.Unlock()
		}
	}

	for _, app := range apps {
		if err := ctx.Err(); err != nil {
			rollback()
			return nil, err
		}

		v, _ := p.ComposeMu.LoadOrStore(app.ID, &sync.Mutex{})
		mu := v.(*sync.Mutex)
		mu.Lock()

		dir := p.ComposeWorkspaceRoot(ctx, app.ID)
		paths := p.EffectiveComposePaths(ctx, app, app.ID)
		if len(paths) == 0 {
			mu.Unlock()
			continue
		}
		envFiles := p.ComposeEnvFiles(ctx, app.ID)
		project := p.ActiveComposeProjectName(ctx, app, app.ID)
		if project == "" {
			mu.Unlock()
			continue
		}

		rows, res := dockerx.ComposePS(ctx, dir, paths, project, envFiles)
		wasRunning := res.OK && countComposeOkRunning(rows) > 0

		logf("stopping " + app.Name)
		p.StopOtherComposeStacks(ctx, app, app.ID, project)
		downRes := dockerx.ComposeDown(ctx, dir, paths, project, nil, envFiles)
		if !downRes.OK && !composeDownBenign(downRes.Output) {
			mu.Unlock()
			rollback()
			return nil, fmt.Errorf("stop %s: %s", app.Name, strings.TrimSpace(downRes.Output))
		}

		if wasRunning {
			toRestart = append(toRestart, migrateRunningStack{
				appID: app.ID, appName: app.Name, project: project,
				dir: dir, paths: paths, envFiles: envFiles,
			})
		}
		mu.Unlock()
	}

	if len(toRestart) == 0 {
		logf("panel quiesced (no running stacks to restore later)")
	} else {
		logf(fmt.Sprintf("panel quiesced (%d stack(s) will be restarted after export)", len(toRestart)))
	}

	return func(restoreCtx context.Context) {
		if len(toRestart) == 0 {
			return
		}
		restoreCtx, cancel := context.WithTimeout(restoreCtx, 30*time.Minute)
		defer cancel()
		logf(fmt.Sprintf("restarting %d app stack(s)", len(toRestart)))
		for _, stack := range toRestart {
			select {
			case <-restoreCtx.Done():
				logf("restore cancelled: " + restoreCtx.Err().Error())
				return
			default:
			}
			v, _ := p.ComposeMu.LoadOrStore(stack.appID, &sync.Mutex{})
			mu := v.(*sync.Mutex)
			mu.Lock()
			logf("starting " + stack.appName)
			upRes := dockerx.ComposeUp(restoreCtx, stack.dir, stack.paths, stack.project, nil, stack.envFiles)
			if !upRes.OK {
				logf("restart " + stack.appName + " failed: " + strings.TrimSpace(upRes.Output))
			}
			mu.Unlock()
		}
		logf("panel restore finished")
	}, nil
}
