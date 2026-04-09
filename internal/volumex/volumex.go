package volumex

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
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

// ListForApp returns Docker volume names that likely belong to this app.
// composeProjects lists compose project names (e.g. from COMPOSE_PROJECT_NAME or app slug) so volumes
// like "myproject_data" match even when the panel app id is a UUID.
func ListForApp(ctx context.Context, appID string, composeProjects []string) ([]string, string) {
	appID = strings.TrimSpace(appID)
	if appID == "" {
		return nil, ""
	}
	names, errMsg := List(ctx)
	if errMsg != "" {
		return nil, errMsg
	}
	u := strings.ReplaceAll(appID, "-", "_")
	seen := map[string]struct{}{}
	var out []string
	add := func(vol string) {
		if _, ok := seen[vol]; ok {
			return
		}
		seen[vol] = struct{}{}
		out = append(out, vol)
	}
	for _, n := range names {
		if strings.Contains(n, appID) || strings.Contains(n, u) {
			add(n)
			continue
		}
		for _, pref := range composeProjects {
			pref = strings.TrimSpace(pref)
			if pref == "" {
				continue
			}
			if n == pref || strings.HasPrefix(n, pref+"_") {
				add(n)
				break
			}
		}
	}
	sort.Strings(out)
	return out, ""
}

// RemoveMatching deletes Docker volumes whose names match ListForApp filters (best-effort).
func RemoveMatching(ctx context.Context, appID string) string {
	names, errMsg := ListForApp(ctx, appID, nil)
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

type volumeInspect struct {
	Name       string `json:"Name"`
	Driver     string `json:"Driver"`
	Mountpoint string `json:"Mountpoint"`
}

func getVolumeMountpoint(ctx context.Context, vol string) (string, error) {
	if !ValidVolumeName(vol) {
		return "", errors.New("invalid volume name")
	}
	cmd := exec.CommandContext(ctx, "docker", "volume", "inspect", vol)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("docker volume inspect failed: %w", err)
	}
	var inspectData []volumeInspect
	if err := json.Unmarshal(out.Bytes(), &inspectData); err != nil {
		return "", fmt.Errorf("failed to parse volume inspect output: %w", err)
	}
	if len(inspectData) == 0 {
		return "", errors.New("volume not found")
	}
	mountpoint := strings.TrimSpace(inspectData[0].Mountpoint)
	if mountpoint == "" {
		return "", errors.New("volume has no mountpoint")
	}
	return mountpoint, nil
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
	
	mountpoint, err := getVolumeMountpoint(ctx, vol)
	if err != nil {
		return nil, fmt.Sprintf("failed to get volume mountpoint: %v", err)
	}
	
	targetPath := mountpoint
	if sub != "." && sub != "" {
		targetPath = filepath.Join(mountpoint, sub)
	}
	
	entries, err := os.ReadDir(targetPath)
	if err != nil {
		return nil, fmt.Sprintf("failed to read directory: %v", err)
	}
	
	var list []Entry
	for _, entry := range entries {
		list = append(list, Entry{
			Name:  entry.Name(),
			IsDir: entry.IsDir(),
		})
	}
	
	return list, ""
}

// BackupToTemp creates a gzipped tar archive of vol into a temp file, returning
// the path. The caller is responsible for removing the file after use.
// This is synchronous — it blocks until docker tar finishes, which ensures the
// full archive is ready before the HTTP response begins.
func BackupToTemp(ctx context.Context, vol string) (path string, err error) {
	if !ValidVolumeName(vol) {
		return "", errors.New("invalid volume")
	}
	f, err := os.CreateTemp("", "vol-backup-*.tar.gz")
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := f.Name()
	defer func() {
		f.Close()
		if err != nil {
			os.Remove(tmpPath)
		}
	}()

	var stderrBuf bytes.Buffer
	cmd := exec.CommandContext(ctx, "docker", "run", "--rm", "-v", vol+":/b:ro",
		"alpine:3.20", "tar", "czf", "-", "-C", "/b", ".")
	cmd.Stdout = f
	cmd.Stderr = &stderrBuf

	if runErr := cmd.Run(); runErr != nil {
		errMsg := strings.TrimSpace(stderrBuf.String())
		if errMsg == "" {
			errMsg = runErr.Error()
		}
		return "", fmt.Errorf("docker tar: %s", errMsg)
	}
	return tmpPath, nil
}

func RestoreTarGz(ctx context.Context, vol string, in io.Reader) string {
	if !ValidVolumeName(vol) {
		return "invalid volume"
	}
	// Use --no-same-owner and --no-same-permissions for security
	// Add --exclude to prevent symlink attacks and path traversal
	cmd := exec.CommandContext(ctx, "docker", "run", "-i", "--rm", "-v", vol+":/b", 
		"alpine:3.20", "sh", "-c", 
		"cd /b && tar xzf - --no-same-owner --no-same-permissions --exclude='../*' --exclude='*/../*'")
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
