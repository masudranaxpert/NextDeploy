package gitx

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"panel/internal/runutil"
)

// Result is an alias to the shared runutil.Result for backward compatibility.
type Result = runutil.Result

func run(ctx context.Context, dir string, env []string, args ...string) runutil.Result {
	return runutil.Run(ctx, dir, env, args...)
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
	s := u.String()
	s = strings.ReplaceAll(s, "x-access-token:", "x-access-token:***")
	s = strings.ReplaceAll(s, "oauth2:", "oauth2:***")
	return s
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
	// GitLab HTTPS expects oauth2:TOKEN (or any non-empty username + PAT); x-access-token is GitHub-specific.
	if strings.EqualFold(authMode, "gitlab_token") {
		u.User = url.UserPassword("oauth2", token)
		return u.String()
	}
	u.User = url.UserPassword("x-access-token", token)
	return u.String()
}

func Clone(ctx context.Context, destDir, repoURL, branch, authMode, token string) Result {
	safeURL := SafeRepoURL(repoURL)
	repoURL = AuthenticatedRepoURL(repoURL, authMode, token)
	if strings.TrimSpace(branch) == "" {
		branch = "main"
	}
	parent := filepath.Dir(destDir)
	if err := os.MkdirAll(parent, 0o750); err != nil {
		return Result{OK: false, Output: err.Error()}
	}
	res := run(ctx, parent, []string{"GIT_TERMINAL_PROMPT=0"},
		"git", "clone", "--depth", "1", "--branch", branch, repoURL, filepath.Base(destDir))
	if !res.OK && repoURL != safeURL {
		res.Output = strings.ReplaceAll(res.Output, repoURL, safeURL)
	}
	return res
}

func Pull(ctx context.Context, repoDir, branch, authMode, token string) Result {
	if strings.TrimSpace(branch) == "" {
		branch = "main"
	}
	env := []string{"GIT_TERMINAL_PROMPT=0"}
	remote := run(ctx, repoDir, nil, "git", "remote", "get-url", "origin")
	var authURL, safeURL string
	if remote.OK {
		authURL = AuthenticatedRepoURL(strings.TrimSpace(remote.Output), authMode, token)
		safeURL = SafeRepoURL(authURL)
		_ = run(ctx, repoDir, nil, "git", "remote", "set-url", "origin", authURL)
	}
	fetch := run(ctx, repoDir, env, "git", "fetch", "--depth", "1", "origin", branch)
	if !fetch.OK {
		if authURL != "" && authURL != safeURL {
			fetch.Output = strings.ReplaceAll(fetch.Output, authURL, safeURL)
		}
		return fetch
	}
	checkout := run(ctx, repoDir, env, "git", "checkout", "-f", "-B", branch, "FETCH_HEAD")
	if !checkout.OK {
		if authURL != "" && authURL != safeURL {
			checkout.Output = strings.ReplaceAll(checkout.Output, authURL, safeURL)
		}
		return checkout
	}
	// Do not remove panel-managed .env (often untracked if not gitignored); compose uses this path as --env-file.
	clean := run(ctx, repoDir, env, "git", "clean", "-fd", "--exclude=.env")
	if !clean.OK {
		if authURL != "" && authURL != safeURL {
			clean.Output = strings.ReplaceAll(clean.Output, authURL, safeURL)
		}
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

// CurrentCommitSubject returns the first line of the latest commit message (git log -1 %s).
func CurrentCommitSubject(ctx context.Context, repoDir string) string {
	res := run(ctx, repoDir, nil, "git", "log", "-1", "--pretty=%s")
	if !res.OK {
		return ""
	}
	s := strings.TrimSpace(res.Output)
	if len(s) > 200 {
		return s[:197] + "..."
	}
	return s
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

// legacyGitDirPresent reports whether repoDir has a classic .git directory or a .git gitdir file.
func legacyGitDirPresent(repoDir string) bool {
	gitPath := filepath.Join(repoDir, ".git")
	st, err := os.Lstat(gitPath)
	if err != nil {
		return false
	}
	if st.IsDir() {
		return true
	}
	if !st.Mode().IsRegular() {
		return false
	}
	b, err := os.ReadFile(gitPath)
	if err != nil {
		return false
	}
	s := strings.TrimSpace(string(b))
	if !strings.HasPrefix(s, "gitdir: ") {
		return false
	}
	link := strings.TrimSpace(strings.TrimPrefix(s, "gitdir: "))
	if link == "" {
		return false
	}
	if !filepath.IsAbs(link) {
		link = filepath.Join(repoDir, link)
	}
	st2, err := os.Stat(filepath.Clean(link))
	return err == nil && st2.IsDir()
}

// RepoExists returns true if repoDir is a usable Git working tree (authoritative check via git,
// with fallbacks for minimal environments and .git-as-file layouts).
func RepoExists(repoDir string) bool {
	repoDir = filepath.Clean(repoDir)
	st, err := os.Stat(repoDir)
	if err != nil || !st.IsDir() {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	res := run(ctx, repoDir, []string{"GIT_TERMINAL_PROMPT=0"}, "git", "rev-parse", "--is-inside-work-tree")
	if res.OK && strings.TrimSpace(res.Output) == "true" {
		return true
	}
	return legacyGitDirPresent(repoDir)
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
