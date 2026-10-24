package handlers

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"panel/internal/dockerapi"
	"panel/internal/dockerx"
)

const (
	deployLogTimeLayout     = "2006-Jan-02 15:04:05.000000"
	maxDeployRunOutputBytes = 2 * 1024 * 1024
)

type DeployRun struct {
	Mu         sync.Mutex
	JobID      string
	Running    bool
	Action     string
	Output     bytes.Buffer
	LineCarry  []byte
	OK         bool
	FinishedAt time.Time
}

// DeployJobRecord stores snapshot info for deploy jobs trackable by agents/polling.
type DeployJobRecord struct {
	JobID      string    `json:"job_id"`
	AppID      string    `json:"app_id"`
	Action     string    `json:"action"`
	Running    bool      `json:"running"`
	OK         bool      `json:"ok"`
	Output     string    `json:"output"`
	CreatedAt  time.Time `json:"created_at"`
	FinishedAt time.Time `json:"finished_at,omitempty"`
}

func (p *Panel) RemoveDeployRun(appID string) {
	p.deployMu.Lock()
	defer p.deployMu.Unlock()
	delete(p.deployRuns, appID)
}

func (p *Panel) GetDeployRun(appID string) *DeployRun {
	p.deployMu.Lock()
	defer p.deployMu.Unlock()
	if p.deployRuns == nil {
		p.deployRuns = make(map[string]*DeployRun)
	}
	if r, ok := p.deployRuns[appID]; ok {
		return r
	}
	r := &DeployRun{}
	p.deployRuns[appID] = r
	return r
}

func (p *Panel) DeploySnapshot(appID string) (out, action string, running bool) {
	r := p.GetDeployRun(appID)
	r.Mu.Lock()
	defer r.Mu.Unlock()
	return r.Output.String(), r.Action, r.Running
}

// GetDeployJob returns the job status by job ID.
func (p *Panel) GetDeployJob(jobID string) (DeployJobRecord, bool) {
	p.deployMu.Lock()
	defer p.deployMu.Unlock()
	if p.deployJobs == nil {
		return DeployJobRecord{}, false
	}
	rec, ok := p.deployJobs[jobID]
	if !ok {
		return DeployJobRecord{}, false
	}
	out := *rec
	if out.Running {
		if r, has := p.deployRuns[out.AppID]; has && r.JobID == jobID {
			r.Mu.Lock()
			out.Output = r.Output.String()
			r.Mu.Unlock()
		}
	}
	return out, true
}

// LatestDeployJobForApp returns the most recent deploy job for an app.
func (p *Panel) LatestDeployJobForApp(appID string) (DeployJobRecord, bool) {
	p.deployMu.Lock()
	defer p.deployMu.Unlock()
	if p.deployJobs == nil {
		return DeployJobRecord{}, false
	}
	var latest *DeployJobRecord
	for _, job := range p.deployJobs {
		if job.AppID == appID {
			if latest == nil || job.CreatedAt.After(latest.CreatedAt) {
				latest = job
			}
		}
	}
	if latest == nil {
		return DeployJobRecord{}, false
	}
	out := *latest
	if out.Running {
		if r, has := p.deployRuns[appID]; has && r.JobID == out.JobID {
			r.Mu.Lock()
			out.Output = r.Output.String()
			r.Mu.Unlock()
		}
	}
	return out, true
}

type deployRunWriter struct {
	r *DeployRun
}

func (r *DeployRun) writeTimestampedLineLocked(line string) {
	line = strings.TrimRight(line, "\r\n")
	if line == "" {
		return
	}
	if r.Output.Len() >= maxDeployRunOutputBytes {
		return
	}
	r.Output.WriteString(time.Now().Format(deployLogTimeLayout))
	r.Output.WriteString(" ")
	r.Output.WriteString(line)
	r.Output.WriteByte('\n')
}

func (r *DeployRun) writeTimestampedBlockLocked(s string) {
	for _, line := range strings.Split(s, "\n") {
		r.writeTimestampedLineLocked(line)
	}
}

func (r *DeployRun) flushLogLineCarryLocked() {
	if len(r.LineCarry) == 0 {
		return
	}
	r.writeTimestampedLineLocked(string(r.LineCarry))
	r.LineCarry = nil
}

func (w *deployRunWriter) Write(p []byte) (int, error) {
	w.r.Mu.Lock()
	defer w.r.Mu.Unlock()
	data := append(w.r.LineCarry, p...)
	w.r.LineCarry = nil
	written := len(p)
	for len(data) > 0 {
		idx := bytes.IndexByte(data, '\n')
		if idx < 0 {
			w.r.LineCarry = append([]byte(nil), data...)
			return written, nil
		}
		line := data[:idx]
		data = data[idx+1:]
		if len(line) > 0 && line[len(line)-1] == '\r' {
			line = line[:len(line)-1]
		}
		w.r.writeTimestampedLineLocked(string(line))
	}
	return written, nil
}

func (p *Panel) StartComposeJob(id, project string, composePaths []string, action string, fn func(context.Context, string, []string, string, io.Writer, []string) dockerx.Result, gitSyncOut string) (string, error) {
	dir := p.AppSourcePath(context.Background(), id)
	r := p.GetDeployRun(id)
	r.Mu.Lock()
	if r.Running {
		activeJobID := r.JobID
		r.Mu.Unlock()
		return "", fmt.Errorf("busy: deployment job %q is already in progress for app %q. Poll deploy_status with job_id=%q then retry once finished", activeJobID, id, activeJobID)
	}
	jobID := fmt.Sprintf("job_%s_%d", id, time.Now().UnixNano()/1e6)
	r.JobID = jobID
	r.Running = true
	r.Action = action
	r.Output.Reset()
	r.LineCarry = nil
	r.OK = false
	r.FinishedAt = time.Time{}
	if s := strings.TrimSpace(gitSyncOut); s != "" {
		r.writeTimestampedLineLocked("[git sync]")
		r.writeTimestampedBlockLocked(s)
		r.Output.WriteByte('\n')
	}
	r.writeTimestampedLineLocked(fmt.Sprintf("Starting %s — docker compose is running in the background (builds may take several minutes).", action))
	r.writeTimestampedLineLocked("----------------------------------------")
	r.Mu.Unlock()

	p.deployMu.Lock()
	if p.deployJobs == nil {
		p.deployJobs = make(map[string]*DeployJobRecord)
	}
	jobRec := &DeployJobRecord{
		JobID:     jobID,
		AppID:     id,
		Action:    action,
		Running:   true,
		CreatedAt: time.Now(),
	}
	p.deployJobs[jobID] = jobRec
	p.deployMu.Unlock()

	go func() {
		runCtx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		dockerConfigPath := filepath.Join(p.Store.ReservedPath(id), ".docker")
		if _, err := os.Stat(filepath.Join(dockerConfigPath, "config.json")); err == nil {
			runCtx = context.WithValue(runCtx, "docker_config", dockerConfigPath)
		}
		stream := &deployRunWriter{r: r}
		envFiles := p.ComposeEnvFiles(runCtx, id)

		isDeployAction := (action == "Deploy" || action == "Redeploy (pull + up)" || strings.HasPrefix(action, "Webhook") || action == "Redeploy")

		var res dockerx.Result
		var deployedSlot, commitSHA, panelEnv string

		if isDeployAction {
			app, _ := p.DB.GetApp(runCtx, id)
			currentSlot := strings.TrimSpace(app.ActiveSlot)
			pinnedSlot := strings.TrimSpace(app.PinnedSlot)
			isPinnedMode := pinnedSlot == "blue" || pinnedSlot == "green"

			targetSlot := "green"
			if isPinnedMode {
				currentSlot = pinnedSlot
				if pinnedSlot == "green" {
					targetSlot = "blue"
				} else {
					targetSlot = "green"
				}
			} else {
				if currentSlot == "green" {
					targetSlot = "blue"
				} else if currentSlot == "" {
					targetSlot = "blue"
				}
			}
			deployedSlot = targetSlot

			bindings, syncErr := p.SyncAppSlotComposeCtx(runCtx, id, targetSlot, isPinnedMode)
			if syncErr != nil {
				r.writeTimestampedLineLocked("[error] Failed to generate slot compose: " + syncErr.Error())
			}
			slotProject := p.SlotProjectName(app, id, targetSlot)
			slotPaths := p.EffectiveComposePathsForSlot(runCtx, app, id, targetSlot)

			if isPinnedMode {
				r.writeTimestampedLineLocked(fmt.Sprintf("[pinned-mode] Production is pinned on Slot %s. Deploying preview release to Slot %s...", pinnedSlot, targetSlot))
			} else {
				r.writeTimestampedLineLocked(fmt.Sprintf("[zero-downtime] Deploying Slot %s (active production: %s)...", targetSlot, currentSlot))
			}
			res = fn(runCtx, dir, slotPaths, slotProject, stream, envFiles)

			if res.OK {
				r.writeTimestampedLineLocked("[readiness] Verifying health and socket readiness...")
				targetPort := 80
				domains, _ := p.DB.ListAppDomains(runCtx, id)
				if len(domains) > 0 && domains[0].Port > 0 {
					targetPort = domains[0].Port
				} else if len(bindings) > 0 && bindings[0].ContainerPort > 0 {
					targetPort = bindings[0].ContainerPort
				}

				rows, psRes := dockerx.ComposePS(runCtx, dir, slotPaths, slotProject, envFiles)
				if psRes.OK && len(rows) > 0 {
					for _, row := range rows {
						if err := p.ProbeContainerReadiness(runCtx, ReadinessProbeOptions{
							ContainerName: row.Name,
							TargetPort:    targetPort,
							Timeout:       60 * time.Second,
							LogW:          stream,
						}); err != nil {
							r.writeTimestampedLineLocked(fmt.Sprintf("[readiness] ✖ Probe failed for %s: %v. Aborting deployment, active slot %s untouched.", row.Name, err, currentSlot))
							_ = dockerx.ComposeDown(runCtx, dir, slotPaths, slotProject, nil, envFiles)
							res.OK = false
							break
						}
					}
				}

				if res.OK {
					if isPinnedMode {
						// Keep active_slot pinned, update standby_slot to the preview slot, and leave both running!
						_ = p.DB.UpdateAppSlots(runCtx, id, pinnedSlot, targetSlot)
						r.Mu.Lock()
						r.writeTimestampedLineLocked(fmt.Sprintf("[pinned-mode] ✓ Preview slot %s is live and healthy! Production remains pinned on slot %s.", targetSlot, pinnedSlot))
						r.Mu.Unlock()
						_ = p.syncRootStackCompose(runCtx)
						p.applyRootStackComposeBackground(p.nextDeployComposePath(), "caddy")
					} else {
						_ = p.DB.UpdateAppSlots(runCtx, id, targetSlot, currentSlot)
						r.Mu.Lock()
						r.writeTimestampedLineLocked(fmt.Sprintf("[zero-downtime] ✓ Atomic cutover completed. Active slot is now %s.", targetSlot))
						r.Mu.Unlock()
						// Tear down every other compose project candidate (blue/green/legacy) so only targetSlot runs
						for _, otherSlot := range []string{"blue", "green"} {
							if otherSlot == targetSlot {
								continue
							}
							r.Mu.Lock()
							r.writeTimestampedLineLocked(fmt.Sprintf("[connection-draining] Stopping inactive slot %s so Caddy only routes to %s...", otherSlot, targetSlot))
							r.Mu.Unlock()
							prevProj := p.SlotProjectName(app, id, otherSlot)
							prevPaths := p.EffectiveComposePathsForSlot(runCtx, app, id, otherSlot)
							_ = dockerx.ComposeDown(runCtx, dir, prevPaths, prevProj, nil, envFiles)
							_ = dockerapi.RemoveContainersByComposeProject(runCtx, prevProj)
							r.Mu.Lock()
							r.writeTimestampedLineLocked(fmt.Sprintf("[connection-draining] Slot %s stopped cleanly.", otherSlot))
							r.Mu.Unlock()
						}
						p.StopOtherComposeStacks(runCtx, app, id, slotProject)
						_ = p.syncRootStackCompose(runCtx)
						p.applyRootStackComposeBackground(p.nextDeployComposePath(), "caddy")
					}
				}
			} else {
				r.writeTimestampedLineLocked("[zero-downtime] Compose build/up failed on target slot. Keeping active slot running.")
				_ = dockerx.ComposeDown(runCtx, dir, slotPaths, slotProject, nil, envFiles)
			}

			if cfg, err := p.DB.GetAppGitConfig(runCtx, id); err == nil {
				commitSHA = cfg.LastDeployRef
			}
			panelEnv, _ = p.DB.GetPanelEnv(runCtx, id)
		} else {
			res = fn(runCtx, dir, composePaths, project, stream, envFiles)
			if action == "Stop" {
				app, _ := p.DB.GetApp(runCtx, id)
				if app.StandbySlot != "" {
					standbyProj := p.SlotProjectName(app, id, app.StandbySlot)
					standbyPaths := p.EffectiveComposePathsForSlot(runCtx, app, id, app.StandbySlot)
					_ = dockerx.ComposeDown(runCtx, dir, standbyPaths, standbyProj, nil, envFiles)
				}
			}
		}

		r.Mu.Lock()
		r.flushLogLineCarryLocked()
		if res.OK {
			r.writeTimestampedLineLocked("Compose command finished successfully.")
		} else {
			r.writeTimestampedLineLocked("Compose exited with a non-zero status (see output above).")
		}
		r.writeTimestampedLineLocked("----------------------------------------")
		saved := r.Output.String()
		now := time.Now()
		r.Running = false
		r.OK = res.OK
		r.FinishedAt = now
		r.Mu.Unlock()

		p.deployMu.Lock()
		if jobRec != nil {
			jobRec.Running = false
			jobRec.OK = res.OK
			jobRec.Output = saved
			jobRec.FinishedAt = now
		}
		p.deployMu.Unlock()

		if isDeployAction {
			_ = p.DB.InsertDeployLogWithSnapshot(context.Background(), id, action, res.OK, saved, deployedSlot, commitSHA, "", panelEnv)
		} else {
			_ = p.DB.InsertDeployLog(context.Background(), id, action, res.OK, saved)
		}

		if res.OK {
			p.InvalidateAfterAppDeployChange(id)
		} else {
			p.InvalidateAfterDockerChange()
		}
	}()
	return jobID, nil
}
