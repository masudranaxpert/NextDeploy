package phphosting

import (
	"bufio"
	"context"
	"embed"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

//go:embed docker/Dockerfile docker/nd-fpm.conf docker/nd-php.ini
var dockerAssets embed.FS

// BuildState is the progress of an image build.
type BuildState struct {
	Version   string    `json:"version"`
	Running   bool      `json:"running"`
	Error     string    `json:"error,omitempty"`
	Log       string    `json:"log"`
	StartedAt time.Time `json:"started_at"`
	EndedAt   time.Time `json:"ended_at,omitempty"`
}

type buildRegistry struct {
	mu     sync.Mutex
	states map[string]*BuildState
}

func (r *buildRegistry) init() { r.states = map[string]*BuildState{} }

func (r *buildRegistry) get(version string) (BuildState, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	st, ok := r.states[version]
	if !ok {
		return BuildState{}, false
	}
	return *st, true
}

const maxBuildLog = 64 * 1024

// BuildStatus returns the latest build state for a version.
func (m *Manager) BuildStatus(version string) (BuildState, bool) { return m.builds.get(version) }

// StartBuild builds nextdeploy/php:<version> in the background. It is a no-op error when a
// build for the version is already running.
func (m *Manager) StartBuild(version string) error {
	if !IsSupportedVersion(version) {
		return fmt.Errorf("unsupported PHP version %q", version)
	}
	m.builds.mu.Lock()
	if st, ok := m.builds.states[version]; ok && st.Running {
		m.builds.mu.Unlock()
		return fmt.Errorf("a build for PHP %s is already running", version)
	}
	st := &BuildState{Version: version, Running: true, StartedAt: time.Now()}
	m.builds.states[version] = st
	m.builds.mu.Unlock()

	appendLog := func(s string) {
		m.builds.mu.Lock()
		st.Log += s
		if len(st.Log) > maxBuildLog {
			st.Log = st.Log[len(st.Log)-maxBuildLog:]
		}
		m.builds.mu.Unlock()
	}
	finish := func(err error) {
		m.builds.mu.Lock()
		st.Running = false
		st.EndedAt = time.Now()
		if err != nil {
			st.Error = err.Error()
		}
		m.builds.mu.Unlock()
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Minute)
		defer cancel()

		targetImage := ImageName(version)

		// 1. Fast path: Attempt to pull pre-built image from Docker Hub first
		appendLog(fmt.Sprintf("Checking for pre-built image on Docker Hub (%s)...\n", targetImage))
		pullCmd := exec.CommandContext(ctx, "docker", "pull", targetImage)
		pullCmd.Env = os.Environ()
		prPull, pwPull := io.Pipe()
		pullCmd.Stdout, pullCmd.Stderr = pwPull, pwPull
		pullDone := make(chan struct{})
		go func() {
			sc := bufio.NewScanner(prPull)
			sc.Buffer(make([]byte, 64*1024), 1024*1024)
			for sc.Scan() {
				appendLog(sc.Text() + "\n")
			}
			close(pullDone)
		}()
		pullErr := pullCmd.Run()
		_ = pwPull.Close()
		<-pullDone

		if pullErr == nil {
			appendLog(fmt.Sprintf("\nSuccessfully pulled pre-built image %s.\n", targetImage))
			m.invalidateInstalled()
			finish(nil)
			return
		}

		// 2. Fallback: Local compilation with docker build if pre-built image is unavailable
		appendLog(fmt.Sprintf("\nPre-built image not available on Docker Hub; starting local build with extensions...\n"))

		dir, err := os.MkdirTemp("", "nd-php-build-")
		if err != nil {
			finish(err)
			return
		}
		defer os.RemoveAll(dir)
		for _, f := range []string{"Dockerfile", "nd-fpm.conf", "nd-php.ini"} {
			data, err := dockerAssets.ReadFile("docker/" + f)
			if err != nil {
				finish(err)
				return
			}
			if err := os.WriteFile(filepath.Join(dir, f), data, 0644); err != nil {
				finish(err)
				return
			}
		}
		cmd := exec.CommandContext(ctx, "docker", "build", "--pull", "--build-arg", "PHP_VERSION="+version, "-t", targetImage, dir)
		cmd.Env = os.Environ()
		pr, pw := io.Pipe()
		cmd.Stdout, cmd.Stderr = pw, pw
		done := make(chan struct{})
		go func() {
			sc := bufio.NewScanner(pr)
			sc.Buffer(make([]byte, 64*1024), 1024*1024)
			for sc.Scan() {
				appendLog(sc.Text() + "\n")
			}
			close(done)
		}()
		runErr := cmd.Run()
		_ = pw.Close()
		<-done
		if runErr != nil {
			finish(fmt.Errorf("docker build failed: %v", runErr))
			return
		}
		appendLog("\nBuild finished.\n")
		m.invalidateInstalled()
		finish(nil)
	}()
	return nil
}

// RemoveImage deletes an installed version; callers must make sure no site uses it.
func (m *Manager) RemoveImage(ctx context.Context, version string) error {
	if !IsSupportedVersion(version) {
		return fmt.Errorf("unsupported PHP version %q", version)
	}
	res := docker(ctx, "rmi", ImageName(version))
	if !res.OK && !strings.Contains(strings.ToLower(res.Output), "no such image") {
		return fmt.Errorf("%s", res.Output)
	}
	if ImageRepo() != "nextdeploy/php" {
		_ = docker(ctx, "rmi", "nextdeploy/php:"+version)
	}
	m.builds.mu.Lock()
	delete(m.builds.states, version)
	m.builds.mu.Unlock()
	m.invalidateInstalled()
	return nil
}

// InstalledVersions lists the PHP versions whose image exists locally. It uses a single
// docker call and a short cache because the SPA bootstrap asks for it frequently.
func (m *Manager) InstalledVersions(ctx context.Context) []string {
	m.instMu.Lock()
	defer m.instMu.Unlock()
	if time.Since(m.instAt) < 10*time.Second && m.instVals != nil {
		return append([]string{}, m.instVals...)
	}
	have := map[string]bool{}
	res := docker(ctx, "image", "ls", ImageRepo(), "--format", "{{.Tag}}")
	if res.OK {
		for _, t := range strings.Fields(res.Output) {
			have[t] = true
		}
	}
	if ImageRepo() != "nextdeploy/php" {
		resLegacy := docker(ctx, "image", "ls", "nextdeploy/php", "--format", "{{.Tag}}")
		if resLegacy.OK {
			for _, t := range strings.Fields(resLegacy.Output) {
				have[t] = true
			}
		}
	}
	out := []string{}
	for _, v := range SupportedVersions {
		if have[v] {
			out = append(out, v)
		}
	}
	m.instVals, m.instAt = out, time.Now()
	return append([]string{}, out...)
}

func (m *Manager) invalidateInstalled() {
	m.instMu.Lock()
	m.instAt = time.Time{}
	m.instMu.Unlock()
}
