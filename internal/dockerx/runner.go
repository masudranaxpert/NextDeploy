package dockerx

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"panel/internal/runutil"
)

// Result is an alias to the shared runutil.Result for backward compatibility.
type Result = runutil.Result

// ComposePsRow matches `docker compose ps --format json` line objects (Compose V2).
type ComposePsRow struct {
	Name    string `json:"Name"`
	Service string `json:"Service"`
	State   string `json:"State"`
	Status  string `json:"Status"`
}

func run(ctx context.Context, dir string, args ...string) runutil.Result {
	return runutil.Run(ctx, dir, nil, args...)
}

// runCompose runs docker compose; envFiles are repeated --env-file (later overrides earlier).
func runCompose(ctx context.Context, projectDir string, composeFiles []string, project string, logW io.Writer, envFiles []string, rest ...string) Result {
	args := composeBin(projectDir, composeFiles, project, envFiles, rest...)
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Dir = projectDir
	var buf bytes.Buffer
	out := io.Writer(&buf)
	if logW != nil {
		out = io.MultiWriter(&buf, logW)
	}
	cmd.Stdout = out
	cmd.Stderr = out
	err := cmd.Run()
	output := strings.TrimSpace(buf.String())
	if err != nil {
		if output == "" {
			output = err.Error()
		}
		return Result{OK: false, Output: output}
	}
	return Result{OK: true, Output: output}
}

func composeFileArg(projectDir, composeFile string) string {
	cleanDir := filepath.Clean(projectDir)
	cleanFile := filepath.Clean(composeFile)
	rel, err := filepath.Rel(cleanDir, cleanFile)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		rel = filepath.Base(composeFile)
	}
	return filepath.ToSlash(rel)
}

// composeBin builds a docker compose argv. --project-directory makes host paths in volumes
// (e.g. ./nginx.conf) resolve from project root on the daemon, not only from the compose file path.
func composeBin(projectDir string, composeFiles []string, projectName string, envFiles []string, rest ...string) []string {
	pd := filepath.Clean(projectDir)
	a := []string{
		"docker", "compose",
		"--project-directory", pd,
		"-p", projectName,
	}
	for _, ef := range envFiles {
		ef = strings.TrimSpace(ef)
		if ef == "" {
			continue
		}
		a = append(a, "--env-file", ef)
	}
	for _, composeFile := range composeFiles {
		if strings.TrimSpace(composeFile) == "" {
			continue
		}
		a = append(a, "-f", composeFileArg(projectDir, composeFile))
	}
	return append(a, rest...)
}

// fixLineEndings normalizes CRLF to LF in top-level *.sh under projectDir so Docker/Linux entrypoints run.
func fixLineEndings(projectDir string) {
	matches, err := filepath.Glob(filepath.Join(projectDir, "*.sh"))
	if err != nil {
		return
	}
	for _, f := range matches {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		fixed := bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))
		if bytes.Equal(data, fixed) {
			continue
		}
		mode := os.FileMode(0o644)
		if st, err := os.Stat(f); err == nil {
			mode = st.Mode()
		}
		_ = os.WriteFile(f, fixed, mode)
	}
}

// logW receives a live copy of stdout+stderr; nil disables streaming.
func ComposeUp(ctx context.Context, projectDir string, composeFiles []string, project string, logW io.Writer, envFiles []string) Result {
	fixLineEndings(projectDir)
	return runCompose(ctx, projectDir, composeFiles, project, logW, envFiles, "up", "-d", "--build")
}

// ComposeApply runs `docker compose up -d` (no rebuild) — used to apply label/config
// changes without rebuilding images, e.g. after a domain add/edit/delete.
func ComposeApply(ctx context.Context, projectDir string, composeFiles []string, project string, logW io.Writer, envFiles []string) Result {
	return ComposeApplyServices(ctx, projectDir, composeFiles, project, logW, envFiles)
}

// ComposeApplyServices runs `docker compose up -d` for zero or more service names.
// With no services, all services defined in the compose files are reconciled.
func ComposeApplyServices(ctx context.Context, projectDir string, composeFiles []string, project string, logW io.Writer, envFiles []string, services ...string) Result {
	fixLineEndings(projectDir)
	args := append([]string{"up", "-d"}, services...)
	return runCompose(ctx, projectDir, composeFiles, project, logW, envFiles, args...)
}

func ComposeDown(ctx context.Context, projectDir string, composeFiles []string, project string, logW io.Writer, envFiles []string) Result {
	return runCompose(ctx, projectDir, composeFiles, project, logW, envFiles, "down")
}

// ComposeDownVolumes runs compose down including volumes and orphan containers.
func ComposeDownVolumes(ctx context.Context, projectDir string, composeFiles []string, project string, logW io.Writer, envFiles []string) Result {
	return runCompose(ctx, projectDir, composeFiles, project, logW, envFiles, "down", "--volumes", "--remove-orphans")
}

// ComposeDownDeleteProject runs compose down with volumes, orphans, and removes all service images for this project (--rmi all).
func ComposeDownDeleteProject(ctx context.Context, projectDir string, composeFiles []string, project string, logW io.Writer, envFiles []string) Result {
	return runCompose(ctx, projectDir, composeFiles, project, logW, envFiles, "down", "--volumes", "--remove-orphans", "--rmi", "all")
}

func ComposeRestart(ctx context.Context, projectDir string, composeFiles []string, project string, logW io.Writer, envFiles []string) Result {
	return runCompose(ctx, projectDir, composeFiles, project, logW, envFiles, "restart")
}

// ComposePullUp pulls latest images then brings the stack up (redeploy without rebuild).
func ComposePullUp(ctx context.Context, projectDir string, composeFiles []string, project string, logW io.Writer, envFiles []string) Result {
	fixLineEndings(projectDir)
	pull := runCompose(ctx, projectDir, composeFiles, project, logW, envFiles, "pull")
	if !pull.OK {
		return pull
	}
	return runCompose(ctx, projectDir, composeFiles, project, logW, envFiles, "up", "-d", "--build")
}

func ComposeLogs(ctx context.Context, projectDir string, composeFiles []string, project string, tail int, envFiles []string) Result {
	if tail <= 0 {
		tail = 80
	}
	return run(ctx, projectDir, composeBin(projectDir, composeFiles, project, envFiles, "logs", "--no-color", "--tail", fmt.Sprintf("%d", tail))...)
}

func ComposePS(ctx context.Context, projectDir string, composeFiles []string, project string, envFiles []string) ([]ComposePsRow, Result) {
	r := run(ctx, projectDir, composeBin(projectDir, composeFiles, project, envFiles, "ps", "-a", "--format", "json")...)
	if !r.OK {
		return nil, r
	}
	trimmed := strings.TrimSpace(r.Output)
	if strings.HasPrefix(trimmed, "[") {
		var batch []ComposePsRow
		if err := json.Unmarshal([]byte(trimmed), &batch); err == nil {
			return batch, Result{OK: true, Output: ""}
		}
	}
	var rows []ComposePsRow
	sc := bufio.NewScanner(strings.NewReader(r.Output))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var row ComposePsRow
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			continue
		}
		if row.Name != "" {
			rows = append(rows, row)
		}
	}
	return rows, Result{OK: true, Output: ""}
}

func ContainerRestart(ctx context.Context, name string) Result {
	name = strings.TrimSpace(name)
	if name == "" {
		return Result{OK: false, Output: "no container name"}
	}
	return run(ctx, ".", "docker", "restart", name)
}

func ContainerRemove(ctx context.Context, name string) Result {
	name = strings.TrimSpace(name)
	if name == "" {
		return Result{OK: false, Output: "no container name"}
	}
	return run(ctx, ".", "docker", "rm", "-f", name)
}

func DockerLogs(ctx context.Context, container string, tail int) Result {
	if tail <= 0 {
		tail = 400
	}
	if strings.TrimSpace(container) == "" {
		return Result{OK: false, Output: "no container selected"}
	}
	// Keep Docker timestamps so the browser toggle has a stable source.
	// No --no-color so ANSI from apps is preserved for browser rendering.
	return run(ctx, ".", "docker", "logs", "-t", "--tail", fmt.Sprintf("%d", tail), container)
}

// ComposeServiceLogs runs `docker compose logs` for one service. Service names stay valid after
// `compose up --force-recreate`; container Names often embed a short id that becomes stale.
func ComposeServiceLogs(ctx context.Context, projectDir string, composeFiles []string, project string, envFiles []string, service string, tail int) Result {
	service = strings.TrimSpace(service)
	if service == "" {
		return Result{OK: false, Output: "no service selected"}
	}
	if tail <= 0 {
		tail = 400
	}
	return run(ctx, projectDir, composeBin(projectDir, composeFiles, project, envFiles, "logs", "-t", "--tail", fmt.Sprintf("%d", tail), service)...)
}

// ComposeServiceLogsFollowCmd builds `docker compose logs -f` for a single service (streaming).
func ComposeServiceLogsFollowCmd(ctx context.Context, projectDir string, composeFiles []string, project string, envFiles []string, service string) (*exec.Cmd, error) {
	service = strings.TrimSpace(service)
	if service == "" {
		return nil, fmt.Errorf("no service selected")
	}
	args := composeBin(projectDir, composeFiles, project, envFiles, "logs", "-f", "--tail", "0", service)
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Dir = projectDir
	return cmd, nil
}

func DockerPruneUnused(ctx context.Context) Result {
	containerRes := run(ctx, ".", "docker", "container", "prune", "-f")
	imageRes := run(ctx, ".", "docker", "image", "prune", "-a", "-f")
	var parts []string
	if strings.TrimSpace(containerRes.Output) != "" {
		parts = append(parts, "[containers]\n"+containerRes.Output)
	}
	if strings.TrimSpace(imageRes.Output) != "" {
		parts = append(parts, "[images]\n"+imageRes.Output)
	}
	ok := containerRes.OK && imageRes.OK
	out := strings.Join(parts, "\n\n")
	if strings.TrimSpace(out) == "" {
		out = "No cleanup output."
	}
	return Result{OK: ok, Output: out}
}

// DockerExec runs a non-interactive shell command inside a running container (sh -c).
// Falls back to direct execution if the container doesn't have a shell.
func DockerExec(ctx context.Context, container, shellCmd string) Result {
	container = strings.TrimSpace(container)
	shellCmd = strings.TrimSpace(shellCmd)
	if container == "" {
		return Result{OK: false, Output: "no container selected"}
	}
	if shellCmd == "" {
		return Result{OK: false, Output: "empty command"}
	}
	// Try with sh -c first
	r := run(ctx, ".", "docker", "exec", "-i", container, "sh", "-c", shellCmd)
	if !r.OK && strings.Contains(r.Output, "executable file not found") {
		// Fallback: try running the command directly without shell
		parts := strings.Fields(shellCmd)
		if len(parts) > 0 {
			r = run(ctx, ".", append([]string{"docker", "exec", "-i", container}, parts...)...)
		}
	}
	return r
}

func Build(ctx context.Context, projectDir, dockerfile string) Result {
	df := filepath.Base(dockerfile)
	tag := fmt.Sprintf("panel-local/%s:latest", sanitizeTag(filepath.Base(projectDir)))
	args := []string{"docker", "build", "-t", tag, "-f", df, "."}
	return run(ctx, projectDir, args...)
}

func sanitizeTag(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "image"
	}
	return out
}
