package dockerapi

import (
	"context"
	"fmt"

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
// container may be a name or ID. Default command is /bin/sh when cmd is empty.
func ExecPTY(ctx context.Context, container string, cmd []string, height, width uint) (*PTYExecSession, error) {
	if height == 0 {
		height = 24
	}
	if width == 0 {
		width = 80
	}
	if len(cmd) == 0 {
		cmd = []string{"/bin/sh"}
	}
	cli, err := newAPIClient()
	if err != nil {
		return nil, err
	}
	execCfg := types.ExecConfig{
		AttachStdin:  true,
		AttachStdout: true,
		AttachStderr: true,
		Tty:          true,
		Cmd:          cmd,
		ConsoleSize:  &[2]uint{height, width},
	}
	cr, err := cli.ContainerExecCreate(ctx, container, execCfg)
	if err != nil {
		_ = cli.Close()
		return nil, err
	}
	execID := cr.ID
	attachResp, err := cli.ContainerExecAttach(ctx, execID, types.ExecStartCheck{
		Tty:         true,
		ConsoleSize: &[2]uint{height, width},
	})
	if err != nil {
		_ = cli.Close()
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

// Close releases the hijacked connection and the Docker API client.
func (s *PTYExecSession) Close() error {
	if s == nil {
		return nil
	}
	s.Hijack.Close()
	if s.cli != nil {
		return s.cli.Close()
	}
	return nil
}
