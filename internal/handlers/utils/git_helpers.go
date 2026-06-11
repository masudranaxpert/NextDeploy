package utils

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"
	"time"

	"panel/internal/db"
)

func RandomSecret() string {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("nd-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}

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
