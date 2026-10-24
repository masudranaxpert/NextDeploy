package runutil

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

// Result holds the outcome of a command execution.
type Result struct {
	OK       bool
	Output   string
	ExitCode int
}

// Run executes a command in dir with optional extra env vars and captures stdout+stderr.
func Run(ctx context.Context, dir string, env []string, args ...string) Result {
	return RunWithStdin(ctx, dir, env, nil, args...)
}

// RunWithStdin executes a command with optional stdin reader, extra env vars, and captures stdout+stderr.
func RunWithStdin(ctx context.Context, dir string, env []string, stdin io.Reader, args ...string) Result {
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Dir = dir
	if stdin != nil {
		cmd.Stdin = stdin
	}
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}
	if configDir, ok := ctx.Value("docker_config").(string); ok && configDir != "" {
		if len(cmd.Env) == 0 {
			cmd.Env = os.Environ()
		}
		cmd.Env = append(cmd.Env, "DOCKER_CONFIG="+configDir)
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
		exitCode := -1
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
		return Result{OK: false, Output: output, ExitCode: exitCode}
	}
	return Result{OK: true, Output: output, ExitCode: 0}
}

// StatusText formats a Result as [ok] or [error] with output.
func StatusText(r Result) string {
	tag := "ok"
	if !r.OK {
		tag = "error"
	}
	return fmt.Sprintf("[%s]\n%s", tag, r.Output)
}
