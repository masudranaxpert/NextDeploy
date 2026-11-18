package state

import (
	"fmt"
	"net"
	"path"
	"regexp"
	"strings"

	"panel/internal/db"
	"panel/internal/proxy"
)

const (
	defaultWorkspacesRoot = "/data/workspaces"
	defaultPHPRuntimeDir  = ".nd-php"
)

var (
	phpAuthUserRe = regexp.MustCompile(`^[A-Za-z0-9._-]{1,32}$`)
	phpHostRe     = regexp.MustCompile(`^(\*\.)?([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)
)

func resolveTLSMode(domain string, enableHTTPS, behindShield bool) proxy.TLSMode {
	if !enableHTTPS && !behindShield {
		return proxy.TLSNone
	}
	if behindShield || ShouldUseInternalTLS(domain) {
		return proxy.TLSInternal
	}
	return proxy.TLSAuto
}

func buildHosts(domain string, enableWWW bool) []string {
	if domain == "" {
		return nil
	}
	hosts := []string{domain}
	if enableWWW {
		hosts = append(hosts, "www."+domain)
	}
	return hosts
}

// ShouldUseInternalTLS reports whether domain is a local/dev or private wildcard
// hostname that requires an internal CA certificate rather than public ACME.
func ShouldUseInternalTLS(domain string) bool {
	return proxy.ShouldUseInternalTLS(domain)
}

// ExtractWildcardDomainIP extracts the embedded IPv4 address from a sslip.io or nip.io hostname.
func ExtractWildcardDomainIP(domain string) net.IP {
	return proxy.ExtractWildcardDomainIP(domain)
}

// FormatWildcardDomain builds a public sslip.io or nip.io hostname using dashed IPv4
// notation, matching proxy.FormatWildcardDomain.
func FormatWildcardDomain(slug, ipStr, provider string, panelID ...string) string {
	return proxy.FormatWildcardDomain(slug, ipStr, provider, panelID...)
}

// SlotPreviewDomain derives a slot-specific preview hostname from a primary domain.
func SlotPreviewDomain(domain, slot string) string {
	return proxy.SlotPreviewDomain(domain, slot)
}

func sanitizeProjectName(name string) string {
	name = strings.TrimSpace(strings.ToLower(name))
	if name == "" {
		return ""
	}
	var b strings.Builder
	lastDash := false
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if r == '-' || r == '_' || r == ' ' {
			if b.Len() == 0 || lastDash {
				continue
			}
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-_")
}

func sanitizeUpstreamToken(s string) string {
	return proxy.SanitizeUpstreamToken(s)
}

func upstreamName(appID, service string, port int, slot string) string {
	return proxy.UpstreamName(appID, service, port, slot)
}

func resolveAppProjectAndSlot(app db.App, overrideProj string) (slot, project string) {
	slot = strings.TrimSpace(app.ActiveSlot)
	overrideProj = strings.TrimSpace(overrideProj)
	if slot == "" && overrideProj != "" && !strings.HasSuffix(overrideProj, "-blue") && !strings.HasSuffix(overrideProj, "-green") {
		return "", overrideProj
	}
	if slot == "" {
		slot = "blue"
	}
	if overrideProj != "" {
		return slot, overrideProj
	}
	base := sanitizeProjectName(app.ID)
	if base == "" {
		base = sanitizeProjectName(app.Name)
	}
	if base == "" {
		return slot, ""
	}
	return slot, base + "-" + slot
}

func resolveSlotProject(app db.App, slot, overrideProj string) string {
	if overrideProj = strings.TrimSpace(overrideProj); overrideProj != "" {
		return overrideProj
	}
	base := sanitizeProjectName(app.ID)
	if base == "" {
		base = sanitizeProjectName(app.Name)
	}
	if base == "" {
		return ""
	}
	if slot == "" {
		return base
	}
	return base + "-" + slot
}

func resolveContainerName(project, service string, containers map[string]string) string {
	if containers != nil {
		if c := strings.TrimSpace(containers[service]); c != "" {
			return c
		}
	}
	if project != "" && service != "" {
		return project + "-" + service + "-1"
	}
	return service
}

func validPHPHost(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	return len(s) <= 253 && phpHostRe.MatchString(s)
}

func validAuthUser(name string) bool {
	return phpAuthUserRe.MatchString(name)
}

func cleanRelRootDir(r string) string {
	r = strings.ReplaceAll(strings.TrimSpace(r), "\\", "/")
	r = path.Clean("/" + r)
	r = strings.TrimPrefix(r, "/")
	if r == "." {
		return ""
	}
	return r
}

func defaultPHPDocRoot(appID, rootDir string) string {
	d := path.Join(defaultWorkspacesRoot, appID, "htdocs")
	if r := cleanRelRootDir(rootDir); r != "" {
		d = path.Join(d, r)
	}
	return d
}

func defaultPHPSocketPath(ownerID int64, version, appID string) string {
	vtag := strings.ReplaceAll(strings.TrimSpace(version), ".", "")
	return path.Join(defaultWorkspacesRoot, defaultPHPRuntimeDir, fmt.Sprintf("u%d-v%s", ownerID, vtag), "run", appID+".sock")
}
