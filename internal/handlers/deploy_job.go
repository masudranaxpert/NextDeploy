package handlers

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"panel/internal/dockerx"
)

// deployRun holds live output for the Deployment tab while compose runs in the background.
type deployRun struct {
	mu      sync.Mutex
	Running bool
	Action  string
	Output  bytes.Buffer
}

func (p *Panel) getDeployRun(appID string) *deployRun {
	p.deployMu.Lock()
	defer p.deployMu.Unlock()
	if p.deployRuns == nil {
		p.deployRuns = make(map[string]*deployRun)
	}
	if r, ok := p.deployRuns[appID]; ok {
		return r
	}
	r := &deployRun{}
	p.deployRuns[appID] = r
	return r
}

// deploySnapshot returns buffered live log text, last action label, and running flag.
func (p *Panel) deploySnapshot(appID string) (out, action string, running bool) {
	r := p.getDeployRun(appID)
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.Output.String(), r.Action, r.Running
}

// deployRunWriter streams docker CLI output into the live deploy buffer (concurrent-safe with deploySnapshot).
type deployRunWriter struct {
	r *deployRun
}

func (w *deployRunWriter) Write(p []byte) (int, error) {
	w.r.mu.Lock()
	defer w.r.mu.Unlock()
	return w.r.Output.Write(p)
}

func (p *Panel) startComposeJob(id, project string, composePaths []string, action string, fn func(context.Context, string, []string, string, io.Writer) dockerx.Result) error {
	dir := p.appSourcePath(context.Background(), id)
	r := p.getDeployRun(id)
	r.mu.Lock()
	if r.Running {
		r.mu.Unlock()
		return fmt.Errorf("busy")
	}
	r.Running = true
	r.Action = action
	r.Output.Reset()
	r.Output.WriteString(fmt.Sprintf("[%s] %s — docker compose is running in the background (builds may take several minutes).\n\n", time.Now().Format("15:04:05"), action))
	r.mu.Unlock()

	go func() {
		runCtx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		stream := &deployRunWriter{r: r}
		res := fn(runCtx, dir, composePaths, project, stream)
		out := formatOut(res)
		_ = p.DB.InsertDeployLog(context.Background(), id, action, res.OK, out)
		r.mu.Lock()
		if res.OK {
			r.Output.WriteString("\n\n[ok] compose finished\n")
		} else {
			r.Output.WriteString("\n\n[error] compose finished\n")
		}
		r.Running = false
		r.mu.Unlock()
	}()
	return nil
}
