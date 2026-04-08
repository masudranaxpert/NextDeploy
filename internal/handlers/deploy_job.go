package handlers

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"panel/internal/dockerx"
)

const deployLogTimeLayout = "2006-Jan-02 15:04:05.000000"

// deployRun holds live output for the Deployment tab while compose runs in the background.
type deployRun struct {
	mu        sync.Mutex
	Running   bool
	Action    string
	Output    bytes.Buffer
	lineCarry []byte // incomplete line tail from docker stream
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

// deployRunWriter streams docker CLI output into the live deploy buffer with a timestamp per line.
type deployRunWriter struct {
	r *deployRun
}

func (r *deployRun) writeTimestampedLineLocked(line string) {
	line = strings.TrimRight(line, "\r\n")
	if line == "" {
		return
	}
	r.Output.WriteString(time.Now().Format(deployLogTimeLayout))
	r.Output.WriteString(" ")
	r.Output.WriteString(line)
	r.Output.WriteByte('\n')
}

func (r *deployRun) writeTimestampedBlockLocked(s string) {
	for _, line := range strings.Split(s, "\n") {
		r.writeTimestampedLineLocked(line)
	}
}

// flushLogLineCarryLocked emits a partial line left in the stream (caller must hold r.mu).
func (r *deployRun) flushLogLineCarryLocked() {
	if len(r.lineCarry) == 0 {
		return
	}
	r.writeTimestampedLineLocked(string(r.lineCarry))
	r.lineCarry = nil
}

func (w *deployRunWriter) Write(p []byte) (int, error) {
	w.r.mu.Lock()
	defer w.r.mu.Unlock()
	data := append(w.r.lineCarry, p...)
	w.r.lineCarry = nil
	written := len(p)
	for len(data) > 0 {
		idx := bytes.IndexByte(data, '\n')
		if idx < 0 {
			w.r.lineCarry = append([]byte(nil), data...)
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

func (p *Panel) startComposeJob(id, project string, composePaths []string, action string, fn func(context.Context, string, []string, string, io.Writer, []string) dockerx.Result, gitSyncOut string) error {
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
	r.lineCarry = nil
	if s := strings.TrimSpace(gitSyncOut); s != "" {
		r.writeTimestampedLineLocked("[git sync]")
		r.writeTimestampedBlockLocked(s)
		r.Output.WriteByte('\n')
	}
	r.writeTimestampedLineLocked(fmt.Sprintf("Starting %s — docker compose is running in the background (builds may take several minutes).", action))
	r.writeTimestampedLineLocked("----------------------------------------")
	r.mu.Unlock()

	go func() {
		runCtx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		stream := &deployRunWriter{r: r}
		envFiles := p.composeEnvFiles(runCtx, id)
		res := fn(runCtx, dir, composePaths, project, stream, envFiles)
		r.mu.Lock()
		r.flushLogLineCarryLocked()
		if res.OK {
			r.writeTimestampedLineLocked("Compose command finished successfully.")
		} else {
			r.writeTimestampedLineLocked("Compose exited with a non-zero status (see output above).")
		}
		r.writeTimestampedLineLocked("----------------------------------------")
		saved := r.Output.String()
		r.Running = false
		r.mu.Unlock()
		_ = p.DB.InsertDeployLog(context.Background(), id, action, res.OK, saved)
	}()
	return nil
}
