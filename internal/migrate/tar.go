package migrate

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
)

func tarExtractMembers(ctx context.Context, archivePath, destDir string, members []string) error {
	args := []string{"xzf", archivePath, "-C", destDir}
	args = append(args, members...)
	return runTar(ctx, args, "extract members")
}

func tarExtractGz(ctx context.Context, archivePath, destDir string) error {
	return runTar(ctx, []string{"xzf", archivePath, "-C", destDir}, "extract archive")
}

func tarCreateGz(ctx context.Context, outPath, baseDir string, members []string) error {
	args := []string{"czf", outPath, "-C", baseDir}
	args = append(args, members...)
	return runTar(ctx, args, "create archive")
}

func runTar(ctx context.Context, args []string, label string) error {
	cmd := exec.CommandContext(ctx, "tar", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return errTar{label: label, msg: msg}
	}
	return nil
}

type errTar struct {
	label string
	msg   string
}

func (e errTar) Error() string {
	return e.label + ": " + e.msg
}
