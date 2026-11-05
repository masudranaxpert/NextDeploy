package workspace

import (
	"path/filepath"
	"strings"
)

// IsProtectedEnvFile returns true if the path points to an actual secret .env file
// (.env, .env.local, .env.production, etc.) while allowing template files like .env.example.
func IsProtectedEnvFile(relPath string) bool {
	base := strings.ToLower(filepath.Base(relPath))
	if base == ".env.example" || base == ".env.sample" || base == ".env.template" || base == ".env.dist" || base == ".env.defaults" {
		return false
	}
	return base == ".env" || strings.HasPrefix(base, ".env.") || strings.HasSuffix(base, ".env")
}
