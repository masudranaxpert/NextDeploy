package perflog

import (
	"log"
	"os"
	"strings"
	"sync"
	"time"
)

var (
	enabledOnce sync.Once
	enabled     bool
)

func Enabled() bool {
	enabledOnce.Do(func() {
		v := strings.TrimSpace(os.Getenv("PANEL_PERF_LOG"))
		enabled = v == "1" || strings.EqualFold(v, "true") || strings.EqualFold(v, "yes")
		if enabled {
			log.Printf("[perf] PANEL_PERF_LOG enabled — request and handler timing will be logged")
		}
	})
	return enabled
}

func Logf(format string, args ...any) {
	if !Enabled() {
		return
	}
	log.Printf("[perf] "+format, args...)
}

func FormatDur(d time.Duration) string {
	if d < time.Millisecond {
		return d.Round(time.Microsecond).String()
	}
	return d.Round(100 * time.Microsecond).String()
}

type Trace struct {
	scope  string
	start  time.Time
	last   time.Time
	fields []string
	steps  []string
}

func Start(scope string) *Trace {
	if !Enabled() {
		return nil
	}
	now := time.Now()
	return &Trace{scope: scope, start: now, last: now}
}

func (t *Trace) Field(key, val string) {
	if t == nil {
		return
	}
	key = strings.TrimSpace(key)
	val = strings.TrimSpace(val)
	if key == "" || val == "" {
		return
	}
	t.fields = append(t.fields, key+"="+val)
}

func (t *Trace) StepDur(name string, since time.Time) {
	if t == nil {
		return
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	t.steps = append(t.steps, name+"="+FormatDur(time.Since(since)))
	t.last = time.Now()
}

func (t *Trace) Finish() {
	if t == nil {
		return
	}
	total := time.Since(t.start)
	fields := strings.Join(t.fields, " ")
	steps := strings.Join(t.steps, " ")
	if fields == "" && steps == "" {
		Logf("%s total=%s", t.scope, FormatDur(total))
		return
	}
	if fields == "" {
		Logf("%s total=%s | %s", t.scope, FormatDur(total), steps)
		return
	}
	if steps == "" {
		Logf("%s total=%s %s", t.scope, FormatDur(total), fields)
		return
	}
	Logf("%s total=%s %s | %s", t.scope, FormatDur(total), fields, steps)
}

func ComposePS(project, source string, dur time.Duration, rows int, ok bool, detail string) {
	if !Enabled() {
		return
	}
	detail = strings.TrimSpace(detail)
	if detail != "" {
		Logf("ComposePS project=%s source=%s dur=%s rows=%d ok=%t detail=%s", project, source, FormatDur(dur), rows, ok, detail)
		return
	}
	Logf("ComposePS project=%s source=%s dur=%s rows=%d ok=%t", project, source, FormatDur(dur), rows, ok)
}

func DockerOp(name string, dur time.Duration, detail string) {
	if !Enabled() {
		return
	}
	detail = strings.TrimSpace(detail)
	if detail != "" {
		Logf("%s dur=%s %s", name, FormatDur(dur), detail)
		return
	}
	Logf("%s dur=%s", name, FormatDur(dur))
}
