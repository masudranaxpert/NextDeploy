package phphosting

import (
	"os"
)

// VersionMeta holds security lifecycle and CVE vulnerability information for a PHP version.
type VersionMeta struct {
	Version      string   `json:"version"`
	Status       string   `json:"status"`        // "active", "security-only", "eol"
	StatusLabel  string   `json:"status_label"`  // e.g. "Active Support", "Security Only", "EOL / Critical CVEs"
	HasCVE       bool     `json:"has_cve"`       // true if version has known critical CVEs or is past official EOL
	CVSS         string   `json:"cvss,omitempty"`// e.g. "9.2 (Critical)"
	CVEs         []string `json:"cves,omitempty"`// e.g. ["CVE-2026-109618", "CVE-2026-109619", "CVE-2024-4577"]
	CVEWarning   string   `json:"cve_warning"`   // security notice or advisory
	EOLDate      string   `json:"eol_date"`      // official php.net EOL date
	PatchedIn    string   `json:"patched_in,omitempty"` // minimum safe upstream release
	IsDefault    bool     `json:"is_default"`    // true for recommended default version (8.3)
}

// VersionCatalog holds the official lifecycle and security vulnerability data for all supported PHP versions.
var VersionCatalog = map[string]VersionMeta{
	"7.4": {
		Version:     "7.4",
		Status:      "eol",
		StatusLabel: "EOL / Critical CVEs",
		HasCVE:      true,
		CVSS:        "9.2 (Critical)",
		CVEs:        []string{"CVE-2026-109618", "CVE-2026-109619", "CVE-2026-115560", "CVE-2024-4577"},
		CVEWarning:  "CRITICAL: Unpatched PHP2Shell RCE (CVE-2026-109618, CVE-2026-109619, CVSS 9.2), Session RCE (CVE-2026-115560), and CGI Argument Injection (CVE-2024-4577). EOL since Nov 2022. Upgrading to PHP 8.3+ is strongly recommended.",
		EOLDate:     "Nov 28, 2022",
		IsDefault:   false,
	},
	"8.0": {
		Version:     "8.0",
		Status:      "eol",
		StatusLabel: "EOL / Critical CVEs",
		HasCVE:      true,
		CVSS:        "9.2 (Critical)",
		CVEs:        []string{"CVE-2026-109618", "CVE-2026-109619", "CVE-2026-115560"},
		CVEWarning:  "CRITICAL: Vulnerable to PHP2Shell RCE (CVE-2026-109618, CVE-2026-109619, CVSS 9.2) and Session RCE (CVE-2026-115560). EOL since Nov 2023 without upstream patches. Immediate upgrade recommended.",
		EOLDate:     "Nov 26, 2023",
		IsDefault:   false,
	},
	"8.1": {
		Version:     "8.1",
		Status:      "eol",
		StatusLabel: "EOL / Critical CVEs",
		HasCVE:      true,
		CVSS:        "9.2 (Critical)",
		CVEs:        []string{"CVE-2026-109618", "CVE-2026-109619", "CVE-2026-115560"},
		CVEWarning:  "CRITICAL: Vulnerable to PHP2Shell RCE (CVE-2026-109618, CVE-2026-109619, CVSS 9.2) and Session RCE (CVE-2026-115560). Support ended Dec 2025. Upgrade to PHP 8.3+ recommended.",
		EOLDate:     "Dec 31, 2025",
		IsDefault:   false,
	},
	"8.2": {
		Version:     "8.2",
		Status:      "security-only",
		StatusLabel: "Security Only",
		HasCVE:      false,
		CVEs:        []string{"Patched in 8.2.35"},
		CVEWarning:  "Patched against PHP2Shell in 8.2.35. Security support ends Dec 31, 2026.",
		EOLDate:     "Dec 31, 2026",
		PatchedIn:   "8.2.35",
		IsDefault:   false,
	},
	"8.3": {
		Version:     "8.3",
		Status:      "active",
		StatusLabel: "Active Support (Recommended)",
		HasCVE:      false,
		CVEs:        []string{"Patched in 8.3.36"},
		CVEWarning:  "Patched against PHP2Shell in 8.3.36. Official support until Dec 31, 2027.",
		EOLDate:     "Dec 31, 2027",
		PatchedIn:   "8.3.36",
		IsDefault:   true,
	},
	"8.4": {
		Version:     "8.4",
		Status:      "active",
		StatusLabel: "Latest Stable",
		HasCVE:      false,
		CVEs:        []string{"Patched in 8.4.27"},
		CVEWarning:  "Patched against PHP2Shell in 8.4.27. Official support until Dec 31, 2028.",
		EOLDate:     "Dec 31, 2028",
		PatchedIn:   "8.4.27",
		IsDefault:   false,
	},
	"8.5": {
		Version:     "8.5",
		Status:      "active",
		StatusLabel: "Active Support (Latest)",
		HasCVE:      false,
		CVEs:        []string{"Patched in 8.5.12"},
		CVEWarning:  "Patched against PHP2Shell in 8.5.12. Official support until Dec 31, 2029.",
		EOLDate:     "Dec 31, 2029",
		PatchedIn:   "8.5.12",
		IsDefault:   false,
	},
}

// MetaForVersion returns the VersionMeta for a given version, falling back to a sensible default.
func MetaForVersion(version string) VersionMeta {
	if meta, ok := VersionCatalog[version]; ok {
		return meta
	}
	return VersionMeta{
		Version:     version,
		Status:      "active",
		StatusLabel: "PHP " + version,
		HasCVE:      false,
		CVEWarning:  "",
		EOLDate:     "",
		IsDefault:   false,
	}
}

// ImageRepo returns the configured Docker Hub image repository for pre-built PHP runtimes.
func ImageRepo() string {
	if v := os.Getenv("NEXTDEPLOY_PHP_IMAGE_REPO"); v != "" {
		return v
	}
	return "masudranaxpert/nextdeploy-php"
}
