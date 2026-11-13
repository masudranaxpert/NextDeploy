package handlers

import (
	"fmt"
	"net/url"
	"strings"

	"panel/internal/db"
)

func NormalizeBranch(branch string) string {
	branch = strings.TrimSpace(branch)
	branch = strings.TrimPrefix(branch, "refs/heads/")
	if branch == "" {
		return "main"
	}
	return branch
}

func NormalizeRepoURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "git@github.com:") {
		raw = strings.TrimPrefix(raw, "git@github.com:")
		raw = strings.TrimSuffix(raw, ".git")
		return "https://github.com/" + raw + ".git"
	}
	if strings.HasPrefix(raw, "git@gitlab.com:") {
		raw = strings.TrimPrefix(raw, "git@gitlab.com:")
		raw = strings.TrimSuffix(raw, ".git")
		return "https://gitlab.com/" + raw + ".git"
	}
	// Expand shorthand "owner/repo" (or "github.com/owner/repo") into an HTTPS clone URL.
	if !strings.Contains(raw, "://") && !strings.Contains(raw, ":\\") && !strings.HasPrefix(raw, "/") && !strings.HasPrefix(raw, ".") && !strings.ContainsAny(raw, " \t\n") {
		if strings.HasPrefix(raw, "github.com/") || strings.HasPrefix(raw, "gitlab.com/") {
			raw = "https://" + raw
		} else if parts := strings.Split(strings.TrimSuffix(raw, ".git"), "/"); len(parts) == 2 && parts[0] != "" && parts[1] != "" {
			raw = "https://github.com/" + parts[0] + "/" + parts[1] + ".git"
		}
	}
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	u.User = nil
	if u.Scheme == "" {
		return raw
	}
	if !strings.HasSuffix(u.Path, ".git") {
		u.Path += ".git"
	}
	return u.String()
}

func RepoFullNameFromURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	p := strings.Trim(strings.TrimSuffix(u.Path, ".git"), "/")
	if p == "" {
		return ""
	}
	return p
}

func CommitPageURL(cfg db.AppGitConfig, fullSHA string) string {
	fullSHA = strings.TrimSpace(fullSHA)
	if fullSHA == "" {
		return ""
	}
	fn := strings.TrimSpace(cfg.RepoFullName)
	if fn == "" {
		fn = RepoFullNameFromURL(cfg.RepoURL)
	}
	if fn == "" {
		return ""
	}
	raw := strings.TrimSpace(cfg.RepoURL)
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return ""
	}
	host := strings.ToLower(u.Host)
	prov := strings.ToLower(strings.TrimSpace(cfg.Provider))
	if prov == "github" || strings.Contains(host, "github") {
		return fmt.Sprintf("https://%s/%s/commit/%s", u.Host, fn, fullSHA)
	}
	if prov == "gitlab" || strings.Contains(host, "gitlab") {
		return fmt.Sprintf("https://%s/%s/-/commit/%s", u.Host, fn, fullSHA)
	}
	return ""
}
