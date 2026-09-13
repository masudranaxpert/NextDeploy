package migrate

import (
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
)

func ParallelWorkers() int {
	n := runtime.NumCPU()
	if n < 2 {
		n = 2
	}
	if n > 6 {
		n = 6
	}
	if v := strings.TrimSpace(os.Getenv("MIGRATE_PARALLEL")); v != "" {
		if i, err := strconv.Atoi(v); err == nil && i > 0 {
			if i > 16 {
				i = 16
			}
			n = i
		}
	}
	return n
}

type safeLogger struct {
	mu sync.Mutex
	fn func(string)
}

func newSafeLogger(fn func(string)) *safeLogger {
	return &safeLogger{fn: fn}
}

func (l *safeLogger) log(msg string) {
	if l == nil || l.fn == nil {
		return
	}
	l.mu.Lock()
	l.fn(msg)
	l.mu.Unlock()
}
