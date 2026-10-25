package dockerapi

import (
	"context"
	"fmt"
	"strings"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
)

// PTYExecSession is an interactive TTY exec attached to a container.
type PTYExecSession struct {
	ExecID string
	Hijack types.HijackedResponse
	cli    *client.Client
}

// ExecPTY starts a new shell (or cmd) with a TTY inside the container and returns a bidirectional stream.
// container may be a name or ID. Default command is /bin/sh -i when cmd is empty (inherits container env).
func ExecPTY(ctx context.Context, container string, cmd []string, height, width uint) (*PTYExecSession, error) {
	return ExecPTYWithOptions(ctx, container, cmd, height, width, PTYOptions{})
}

// PTYOptions customises the exec: run as another user, in another directory, with extra env.
type PTYOptions struct {
	User       string
	WorkingDir string
	Env        []string
}

// ExecPTYWithOptions is ExecPTY with a user, working directory and extra environment.
func ExecPTYWithOptions(ctx context.Context, container string, cmd []string, height, width uint, opts PTYOptions) (*PTYExecSession, error) {
	if height == 0 {
		height = 24
	}
	if width == 0 {
		width = 80
	}
	if len(cmd) == 0 {
		cmd = []string{"/bin/sh", "-i"}
	}
	cli, err := apiClient()
	if err != nil {
		return nil, err
	}
	execCfg := types.ExecConfig{
		AttachStdin:  true,
		AttachStdout: true,
		AttachStderr: true,
		Tty:          true,
		Cmd:          cmd,
		Env: []string{
			"TERM=xterm-256color",
			"COLORTERM=truecolor",
			"PS1=\\[\\033[01;32m\\]\\u@\\h\\[\\033[00m\\]:\\[\\033[01;34m\\]\\W\\[\\033[00m\\]\\$ ",
		},
		ConsoleSize:  &[2]uint{height, width},
		User:         opts.User,
		WorkingDir:   opts.WorkingDir,
	}
	execCfg.Env = mergeEnv(execCfg.Env, opts.Env)
	cr, err := cli.ContainerExecCreate(ctx, container, execCfg)
	if err != nil {

		return nil, err
	}
	execID := cr.ID
	attachResp, err := cli.ContainerExecAttach(ctx, execID, types.ExecStartCheck{
		Tty:         true,
		ConsoleSize: &[2]uint{height, width},
	})
	if err != nil {

		return nil, err
	}
	return &PTYExecSession{
		ExecID: execID,
		Hijack: attachResp,
		cli:    cli,
	}, nil
}

// Resize updates the TTY dimensions (from xterm fit).
func (s *PTYExecSession) Resize(ctx context.Context, height, width uint) error {
	if s == nil || s.cli == nil {
		return fmt.Errorf("nil session")
	}
	if height == 0 || width == 0 {
		return nil
	}
	return s.cli.ContainerExecResize(ctx, s.ExecID, types.ResizeOptions{Height: height, Width: width})
}

func (s *PTYExecSession) Close() error {
	if s == nil {
		return nil
	}
	s.Hijack.Close()
	return nil
}

// ExecPTYAutoShell tries common interactive shells until Docker accepts exec+attach.
// Minimal images often only have /bin/sh; some need -i for a stable TTY session.
func ExecPTYAutoShell(ctx context.Context, container string, height, width uint) (*PTYExecSession, error) {
	candidates := [][]string{
		{"/bin/sh", "-i"},
		{"/bin/sh"},
		{"/bin/bash", "-i"},
		{"/bin/bash"},
		{"sh", "-i"},
		{"sh"},
	}
	var lastErr error
	for _, cmd := range candidates {
		s, err := ExecPTY(ctx, container, cmd, height, width)
		if err != nil {
			lastErr = err
			continue
		}
		return s, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no shell could be started in container")
	}
	return nil, lastErr
}

// mergeEnv appends extra to base; an entry in extra replaces a base entry with the same key.
func mergeEnv(base, extra []string) []string {
	if len(extra) == 0 {
		return base
	}
	override := make(map[string]bool, len(extra))
	for _, e := range extra {
		if i := strings.IndexByte(e, '='); i > 0 {
			override[e[:i]] = true
		}
	}
	out := make([]string, 0, len(base)+len(extra))
	for _, e := range base {
		if i := strings.IndexByte(e, '='); i > 0 && override[e[:i]] {
			continue
		}
		out = append(out, e)
	}
	return append(out, extra...)
}
