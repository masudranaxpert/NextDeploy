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

	"panel/internal/dockerx"
)

const (
	deployLogTimeLayout      = "2006-Jan-02 15:04:05.000000"
	maxDeployRunOutputBytes  = 2 * 1024 * 1024
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
		r.Mu.Unlock()
		return "", fmt.Errorf("busy")
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
		res := fn(runCtx, dir, composePaths, project, stream, envFiles)
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

		_ = p.DB.InsertDeployLog(context.Background(), id, action, res.OK, saved)
		if res.OK {
			p.InvalidateAfterAppDeployChange(id)
		} else {
			p.InvalidateAfterDockerChange()
		}
	}()
	return jobID, nil
}
