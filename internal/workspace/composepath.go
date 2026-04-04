package workspace

import (
	"path/filepath"
	"strings"
)

// NormalizeComposeRel returns a safe relative compose file path (forward slashes, no ./ or ..).
func NormalizeComposeRel(rel string) string {
	rel = strings.TrimSpace(rel)
	rel = strings.ReplaceAll(rel, "\\", "/")
	rel = strings.TrimPrefix(rel, "./")
	rel = filepath.ToSlash(rel)
	rel = strings.Trim(rel, "/")
	if rel == "" {
		return "docker-compose.yml"
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
		return "docker-compose.yml"
	}
	return strings.Join(safe, "/")
}
