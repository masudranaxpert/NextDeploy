package gitx

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type Result struct {
	OK     bool
	Output string
}

func run(ctx context.Context, dir string, env []string, args ...string) Result {
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

func SafeRepoURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	u.User = nil
	return u.String()
}

func AuthenticatedRepoURL(rawURL, authMode, token string) string {
	rawURL = strings.TrimSpace(rawURL)
	token = strings.TrimSpace(token)
	if rawURL == "" || token == "" || authMode == "public" {
		return rawURL
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	if strings.EqualFold(authMode, "github_app") {
		u.User = url.UserPassword("x-access-token", token)
		return u.String()
	}
	u.User = url.UserPassword("x-access-token", token)
	return u.String()
}

func Clone(ctx context.Context, destDir, repoURL, branch, authMode, token string) Result {
	repoURL = AuthenticatedRepoURL(repoURL, authMode, token)
	if strings.TrimSpace(branch) == "" {
		branch = "main"
	}
	parent := filepath.Dir(destDir)
	if err := os.MkdirAll(parent, 0o750); err != nil {
		return Result{OK: false, Output: err.Error()}
	}
	return run(ctx, parent, []string{"GIT_TERMINAL_PROMPT=0"},
		"git", "clone", "--depth", "1", "--branch", branch, repoURL, filepath.Base(destDir))
}

func Pull(ctx context.Context, repoDir, branch, authMode, token string) Result {
	if strings.TrimSpace(branch) == "" {
		branch = "main"
	}
	env := []string{"GIT_TERMINAL_PROMPT=0"}
	remote := run(ctx, repoDir, nil, "git", "remote", "get-url", "origin")
	if remote.OK {
		authURL := AuthenticatedRepoURL(strings.TrimSpace(remote.Output), authMode, token)
		_ = run(ctx, repoDir, nil, "git", "remote", "set-url", "origin", authURL)
	}
	fetch := run(ctx, repoDir, env, "git", "fetch", "--depth", "1", "origin", branch)
	if !fetch.OK {
		return fetch
	}
	checkout := run(ctx, repoDir, env, "git", "checkout", "-B", branch, "FETCH_HEAD")
	if !checkout.OK {
		return checkout
	}
	clean := run(ctx, repoDir, env, "git", "clean", "-fd")
	if !clean.OK {
		return clean
	}
	return Result{OK: true, Output: join(fetch.Output, checkout.Output, clean.Output)}
}

func CurrentCommit(ctx context.Context, repoDir string) string {
	res := run(ctx, repoDir, nil, "git", "rev-parse", "HEAD")
	if !res.OK {
		return ""
	}
	return strings.TrimSpace(res.Output)
}

func EnsureSafeRemote(ctx context.Context, repoDir, authMode, token string) {
	remote := run(ctx, repoDir, nil, "git", "remote", "get-url", "origin")
	if !remote.OK {
		return
	}
	authURL := AuthenticatedRepoURL(strings.TrimSpace(remote.Output), authMode, token)
	if authURL == "" {
		return
	}
	_ = run(ctx, repoDir, nil, "git", "remote", "set-url", "origin", authURL)
}

func RepoExists(repoDir string) bool {
	st, err := os.Stat(filepath.Join(repoDir, ".git"))
	return err == nil && st.IsDir()
}

func join(parts ...string) string {
	var out []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, "\n\n")
}

func WebhookURL(baseURL, appID string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" || appID == "" {
		return ""
	}
	return fmt.Sprintf("%s/webhooks/github/%s", baseURL, appID)
}
