package workspace

import (
	"path"
	"path/filepath"
	"strings"
)

// NormalizeComposeRel returns a safe relative compose file path (forward slashes, no ./ or ..).
func NormalizeComposeRel(rel string) string {
	clean := path.Clean("/" + filepath.ToSlash(strings.TrimSpace(rel)))
	clean = strings.TrimPrefix(clean, "/")
	if clean == "" || clean == "." {
		return "docker-compose.yml"
	}
	return clean
}
