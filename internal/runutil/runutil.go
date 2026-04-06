package runutil

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Result holds the outcome of a command execution.
type Result struct {
	OK     bool
	Output string
}

// Run executes a command in dir with optional extra env vars and captures stdout+stderr.
func Run(ctx context.Context, dir string, env []string, args ...string) Result {
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Dir = dir
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	output := strings.TrimSpace(out.String())
	if err != nil {
		if output == "" {
			output = err.Error()
		}
		return Result{OK: false, Output: output}
	}
	return Result{OK: true, Output: output}
}

// StatusText formats a Result as [ok] or [error] with output.
func StatusText(r Result) string {
	tag := "ok"
	if !r.OK {
		tag = "error"
	}
	return fmt.Sprintf("[%s]\n%s", tag, r.Output)
}
