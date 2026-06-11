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

	"panel/internal/dockerapi"
	"panel/internal/runutil"
)

type Result = runutil.Result

type ComposePsRow struct {
	Name    string `json:"Name"`
	Service string `json:"Service"`
	State   string `json:"State"`
	Status  string `json:"Status"`
	// WorkingDir is com.docker.compose.project.working_dir (SDK path only; empty from CLI fallback).
	WorkingDir string `json:"-"`
}

func run(ctx context.Context, dir string, args ...string) runutil.Result {
	return runutil.Run(ctx, dir, nil, args...)
}

// runCompose runs docker compose; envFiles are repeated --env-file (later overrides earlier).
func runCompose(ctx context.Context, projectDir string, composeFiles []string, project string, logW io.Writer, envFiles []string, rest ...string) Result {
	args := composeBin(projectDir, composeFiles, project, envFiles, rest...)
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Dir = projectDir
	if configDir, ok := ctx.Value("docker_config").(string); ok && configDir != "" {
		cmd.Env = append(os.Environ(), "DOCKER_CONFIG="+configDir)
	}
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

func ComposeUp(ctx context.Context, projectDir string, composeFiles []string, project string, logW io.Writer, envFiles []string) Result {
	fixLineEndings(projectDir)
	return runCompose(ctx, projectDir, composeFiles, project, logW, envFiles, "up", "-d", "--build")
}

func ComposeApply(ctx context.Context, projectDir string, composeFiles []string, project string, logW io.Writer, envFiles []string) Result {
	return ComposeApplyServices(ctx, projectDir, composeFiles, project, logW, envFiles)
}

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
	project = strings.TrimSpace(project)
	if project != "" {
		sdkRows, err := dockerapi.ComposePS(ctx, project)
		if err == nil {
			rows := make([]ComposePsRow, 0, len(sdkRows))
			for _, sr := range sdkRows {
				rows = append(rows, ComposePsRow{
					Name:       sr.Name,
					Service:    sr.Service,
					State:      sr.State,
					Status:     sr.Status,
					WorkingDir: sr.WorkingDir,
				})
			}
			return rows, Result{OK: true, Output: ""}
		}
	}

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
	if err := dockerapi.RestartContainerByName(ctx, name); err == nil {
		return Result{OK: true, Output: ""}
	}
	return run(ctx, ".", "docker", "restart", name)
}

// ContainerStart runs `docker start` for an existing stopped container.
func ContainerStart(ctx context.Context, name string) Result {
	name = strings.TrimSpace(name)
	if name == "" {
		return Result{OK: false, Output: "no container name"}
	}
	if err := dockerapi.StartContainerByName(ctx, name); err == nil {
		return Result{OK: true, Output: ""}
	}
	return run(ctx, ".", "docker", "start", name)
}

// ContainerStop runs `docker stop` with a 10s grace period.
func ContainerStop(ctx context.Context, name string) Result {
	name = strings.TrimSpace(name)
	if name == "" {
		return Result{OK: false, Output: "no container name"}
	}
	if err := dockerapi.StopContainerByName(ctx, name); err == nil {
		return Result{OK: true, Output: ""}
	}
	return run(ctx, ".", "docker", "stop", "-t", "10", name)
}

func ContainerRemove(ctx context.Context, name string) Result {
	name = strings.TrimSpace(name)
	if name == "" {
		return Result{OK: false, Output: "no container name"}
	}
	if err := dockerapi.RemoveContainerByName(ctx, name); err == nil {
		return Result{OK: true, Output: ""}
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
	return run(ctx, ".", "docker", "logs", "-t", "--tail", fmt.Sprintf("%d", tail), container)
}

type PruneOptions struct {
	Containers bool
	Images     bool
	BuildCache bool
}

func DefaultPruneOptions() PruneOptions {
	return PruneOptions{Containers: true, Images: true, BuildCache: false}
}

func DockerPruneUnused(ctx context.Context) Result {
	return DockerPruneWithOptions(ctx, DefaultPruneOptions())
}

func DockerPruneWithOptions(ctx context.Context, opts PruneOptions) Result {
	if !opts.Containers && !opts.Images && !opts.BuildCache {
		return Result{OK: true, Output: "No cleanup options selected."}
	}

	var parts []string
	ok := true

	if opts.Containers {
		r := run(ctx, ".", "docker", "container", "prune", "-f")
		if strings.TrimSpace(r.Output) != "" {
			parts = append(parts, "[containers]\n"+r.Output)
		}
		ok = ok && r.OK
	}
	if opts.Images {
		r := run(ctx, ".", "docker", "image", "prune", "-a", "-f")
		if strings.TrimSpace(r.Output) != "" {
			parts = append(parts, "[images]\n"+r.Output)
		}
		ok = ok && r.OK
	}
	if opts.BuildCache {
		// `builder prune -a -f` removes all build cache layers including ones
		// still referenced by image manifests, matching the "-a" on images.
		r := run(ctx, ".", "docker", "builder", "prune", "-a", "-f")
		if strings.TrimSpace(r.Output) != "" {
			parts = append(parts, "[build cache]\n"+r.Output)
		}
		ok = ok && r.OK
	}

	out := strings.Join(parts, "\n\n")
	if strings.TrimSpace(out) == "" {
		out = "No cleanup output."
	}
	return Result{OK: ok, Output: out}
}

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
