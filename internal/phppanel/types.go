package phppanel

import (
	"path/filepath"
	"strings"

	"panel/internal/db"
)

const (
	TemplateID          = "php_panel"
	DefaultComposeFile  = "compose.yml"
	defaultRootPassword = "changeme_please"
)

var SupportedPHPVersions = []string{"7.4", "8.1", "8.2", "8.3"}

func NormalizePHPVersion(v string) string {
	switch strings.TrimSpace(v) {
	case "7.4", "8.1", "8.2", "8.3":
		return strings.TrimSpace(v)
	default:
		return "8.3"
	}
}

func ServiceForVersion(v string) string {
	switch NormalizePHPVersion(v) {
	case "7.4":
		return "php_fpm_74"
	case "8.1":
		return "php_fpm_81"
	case "8.2":
		return "php_fpm_82"
	default:
		return "php_fpm_83"
	}
}

func SitePublicRoot(appID, slug string) string {
	return filepath.ToSlash(filepath.Join("/data/workspaces", appID, "sites", slug, "public_html"))
}

func AppIsPHPPanel(app db.App) bool {
	return strings.TrimSpace(app.TemplateID) == TemplateID
}
