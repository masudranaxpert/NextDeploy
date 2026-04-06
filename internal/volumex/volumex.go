package volumex

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"panel/internal/workspace"
)

var volNameRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]*$`)

func ValidVolumeName(name string) bool {
	name = strings.TrimSpace(name)
	return name != "" && volNameRe.MatchString(name)
}

// ParentRel delegates to workspace.ParentRel for consistency.
func ParentRel(rel string) string {
	return workspace.ParentRel(rel)
}

func List(ctx context.Context) ([]string, string) {
	cmd := exec.CommandContext(ctx, "docker", "volume", "ls", "-q")
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return nil, strings.TrimSpace(out.String())
	}
	var names []string
	for _, line := range strings.Split(out.String(), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			names = append(names, line)
		}
	}
	return names, ""
}

// ListForApp returns Docker volume names that likely belong to this app (compose project id / patterns).
func ListForApp(ctx context.Context, appID string) ([]string, string) {
	appID = strings.TrimSpace(appID)
	if appID == "" {
		return nil, ""
	}
	names, errMsg := List(ctx)
	if errMsg != "" {
		return nil, errMsg
	}
	u := strings.ReplaceAll(appID, "-", "_")
	var out []string
	for _, n := range names {
		if strings.Contains(n, appID) || strings.Contains(n, u) {
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out, ""
}

// RemoveMatching deletes Docker volumes whose names match ListForApp filters (best-effort).
func RemoveMatching(ctx context.Context, appID string) string {
	names, errMsg := ListForApp(ctx, appID)
	if errMsg != "" {
		return errMsg
	}
	var errs []string
	for _, n := range names {
		cmd := exec.CommandContext(ctx, "docker", "volume", "rm", "-f", n)
		var b bytes.Buffer
		cmd.Stderr = &b
		if err := cmd.Run(); err != nil {
			msg := strings.TrimSpace(b.String())
			if msg == "" {
				msg = err.Error()
			}
			errs = append(errs, n+": "+msg)
		}
	}
	return strings.Join(errs, "; ")
}

type Entry struct {
	Name  string
	IsDir bool
}

func safeVolSubpath(rel string) (string, error) {
	rel = filepath.ToSlash(strings.TrimSpace(rel))
	rel = strings.Trim(rel, "/")
	if rel == "" {
		return ".", nil
	}
	parts := strings.Split(rel, "/")
	var safe []string
	for _, p := range parts {
		if p == "" || p == "." || p == ".." {
			continue
		}
		safe = append(safe, p)
	}
	if len(safe) == 0 {
		return ".", nil
	}
	return strings.Join(safe, "/"), nil
}

func ListDir(ctx context.Context, vol, rel string) ([]Entry, string) {
	if !ValidVolumeName(vol) {
		return nil, "invalid volume name"
	}
	sub, err := safeVolSubpath(rel)
	if err != nil {
		return nil, err.Error()
	}
	target := "/mnt"
	if sub != "." && sub != "" {
		target = "/mnt/" + sub
	}
	cmd := exec.CommandContext(ctx, "docker", "run", "--rm", "-v", vol+":/mnt:ro", "alpine:3.20", "ls", "-1Ap", target)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(out.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, msg
	}
	var list []Entry
	for _, line := range strings.Split(out.String(), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || line == "./" || line == "../" {
			continue
		}
		isDir := strings.HasSuffix(line, "/")
		name := strings.TrimSuffix(line, "/")
		list = append(list, Entry{Name: name, IsDir: isDir})
	}
	return list, ""
}

func OpenTarStream(ctx context.Context, vol string) (io.ReadCloser, error) {
	if !ValidVolumeName(vol) {
		return nil, errors.New("invalid volume")
	}
	cmd := exec.CommandContext(ctx, "docker", "run", "--rm", "-v", vol+":/b:ro", "alpine:3.20", "tar", "czf", "-", "-C", "/b", ".")

	// Use an io.Pipe so the goroutine writes into one end while the caller
	// reads from the other. This avoids the StdoutPipe + Wait deadlock.
	pr, pw := io.Pipe()
	cmd.Stdout = pw

	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf

	if err := cmd.Start(); err != nil {
		_ = pw.Close()
		return pr, err
	}

	go func() {
		err := cmd.Wait()
		if err != nil {
			_ = pw.CloseWithError(fmt.Errorf("tar: %w: %s", err, strings.TrimSpace(stderrBuf.String())))
		} else {
			_ = pw.Close()
		}
	}()

	return pr, nil
}

func RestoreTarGz(ctx context.Context, vol string, in io.Reader) string {
	if !ValidVolumeName(vol) {
		return "invalid volume"
	}
	cmd := exec.CommandContext(ctx, "docker", "run", "-i", "--rm", "-v", vol+":/b", "alpine:3.20", "sh", "-c", "cd /b && tar xzf -")
	cmd.Stdin = in
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		s := strings.TrimSpace(out.String())
		if s == "" {
			return err.Error()
		}
		return s
	}
	return ""
}
