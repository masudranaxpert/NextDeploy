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
	if len(args) > 0 && args[0] == "git" {
		// Allow panel (often UID 0) to run git inside workspaces/htdocs owned by site UIDs (20001+).
		wrapped := make([]string, 0, len(args)+2)
		wrapped = append(wrapped, "git", "-c", "safe.directory=*")
		wrapped = append(wrapped, args[1:]...)
		args = wrapped
	}
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
		"git", "-c", "core.symlinks=false", "clone", "--depth", "1", "--branch", branch, repoURL, filepath.Base(destDir))
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
	checkout := run(ctx, repoDir, env, "git", "-c", "core.symlinks=false", "checkout", "-f", "-B", branch, "FETCH_HEAD")
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

// Checkout checks out a specific commit or branch in the repository.
func Checkout(ctx context.Context, repoDir, ref string) Result {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return Result{OK: false, Output: "empty ref"}
	}
	return run(ctx, repoDir, nil, "git", "checkout", ref)
}

// IsDirty returns true when the working tree has any tracked modifications or untracked files
// not covered by .gitignore. Excludes .env since that is panel-managed and intentionally untracked.
func IsDirty(ctx context.Context, repoDir string) bool {
	res := run(ctx, repoDir, nil, "git", "status", "--porcelain")
	if !res.OK {
		return false // can't determine; treat as clean so we don't block deploy
	}
	for _, line := range strings.Split(res.Output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Skip panel-managed .env (always untracked; git pull also restores it anyway)
		if strings.HasSuffix(line, ".env") && (strings.HasPrefix(line, "?? ") || strings.HasPrefix(line, "!! ")) {
			continue
		}
		return true
	}
	return false
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

// Push initializes a git repo in dir if needed, commits any changes, and pushes to origin/<branch>.
func Push(ctx context.Context, dir, repoURL, branch, authMode, token, commitMsg, authorName, authorEmail string, force bool) Result {
	repoURL = strings.TrimSpace(repoURL)
	if repoURL == "" {
		return Result{OK: false, Output: "repository URL is required"}
	}
	if strings.TrimSpace(branch) == "" {
		branch = "main"
	}
	if strings.TrimSpace(commitMsg) == "" {
		commitMsg = "Sync from NextDeploy"
	}
	if strings.TrimSpace(authorName) == "" {
		authorName = "NextDeploy"
	}
	if strings.TrimSpace(authorEmail) == "" {
		authorEmail = "noreply@nextdeploy.local"
	}
	safeURL := SafeRepoURL(repoURL)
	authURL := AuthenticatedRepoURL(repoURL, authMode, token)
	env := []string{"GIT_TERMINAL_PROMPT=0"}

	if !RepoExists(dir) {
		initRes := run(ctx, dir, env, "git", "init", "-b", branch)
		if !initRes.OK {
			// Fallback for older git without -b
			initRes = run(ctx, dir, env, "git", "init")
			if !initRes.OK {
				return initRes
			}
			_ = run(ctx, dir, env, "git", "checkout", "-B", branch)
		}
	}

	_ = run(ctx, dir, env, "git", "config", "--local", "user.name", authorName)
	_ = run(ctx, dir, env, "git", "config", "--local", "user.email", authorEmail)

	remote := run(ctx, dir, env, "git", "remote", "get-url", "origin")
	if remote.OK {
		_ = run(ctx, dir, env, "git", "remote", "set-url", "origin", authURL)
	} else {
		_ = run(ctx, dir, env, "git", "remote", "add", "origin", authURL)
	}
	// Restore credential-free remote URL after push so tokens are never left in .git/config.
	defer func() {
		if safeURL != "" {
			_ = run(context.Background(), dir, env, "git", "remote", "set-url", "origin", safeURL)
		}
	}()

	if addRes := run(ctx, dir, env, "git", "-c", "core.fileMode=false", "add", "-A", "--", ".", ":!.nextdeploy", ":!.panel-meta", ":!.nd"); !addRes.OK {
		return addRes
	}
	statusRes := run(ctx, dir, env, "git", "-c", "core.fileMode=false", "status", "--porcelain")
	if statusRes.OK && strings.TrimSpace(statusRes.Output) != "" {
		if commitRes := run(ctx, dir, env, "git", "commit", "-m", commitMsg); !commitRes.OK {
			return commitRes
		}
	} else if CurrentCommit(ctx, dir) == "" {
		// Empty repo with no commits yet
		if commitRes := run(ctx, dir, env, "git", "commit", "--allow-empty", "-m", commitMsg); !commitRes.OK {
			return commitRes
		}
	}

	pushArgs := []string{"git", "push", "-u", "origin", "HEAD:" + branch}
	if force {
		pushArgs = []string{"git", "push", "--force", "-u", "origin", "HEAD:" + branch}
	}
	pushRes := run(ctx, dir, env, pushArgs...)
	if !pushRes.OK {
		if authURL != safeURL {
			pushRes.Output = strings.ReplaceAll(pushRes.Output, authURL, safeURL)
		}
		lower := strings.ToLower(pushRes.Output)
		if !force && (strings.Contains(lower, "fetch first") || strings.Contains(lower, "non-fast-forward") || strings.Contains(lower, "rejected")) {
			pushRes.Output = strings.TrimSpace(pushRes.Output) + "\nHint: remote branch already has commits (such as an initial README). Retry with --force if you want to overwrite it."
		}
	}
	return pushRes
}
