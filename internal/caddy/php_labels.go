package caddy

import (
	"fmt"
	"net/netip"
	"strconv"
	"strings"

	"panel/internal/db"
)

// PHPSiteLabels builds the caddy-docker-proxy labels for a PHP/static site served straight
// from disk. root is the document root as Caddy sees it; socket is the PHP-FPM unix socket
// path as Caddy sees it ("" for static sites).
func PHPSiteLabels(domains []db.AppDomain, root, socket string, sec SiteSecurity) map[string]string {
	out := map[string]string{}
	domains = sortedDomains(domains)
	for i, d := range domains {
		prefix := "caddy"
		if len(domains) > 1 {
			prefix = "caddy_" + strconv.Itoa(i)
		}
		appendPHPSiteLabels(out, prefix, d, root, socket, sec)
	}
	return out
}

func appendPHPSiteLabels(labels map[string]string, prefix string, d db.AppDomain, root, socket string, sec SiteSecurity) {
	domain := cleanQuotedValue(d.Domain)
	if domain == "" {
		return
	}
	var sites []string
	if d.EnableHTTPS {
		sites = append(sites, domain)
		if d.EnableWWW {
			sites = append(sites, "www."+domain)
		}
	} else {
		sites = append(sites, "http://"+domain)
		if d.EnableWWW {
			sites = append(sites, "http://www."+domain)
		}
	}
	labels[prefix] = strings.Join(sites, ", ")
	if d.EnableHTTPS && ShouldUseInternalTLS(domain) {
		labels[prefix+".tls"] = "internal"
	}
	labels[prefix+".root"] = "* " + root
	labels[prefix+".encode"] = "zstd gzip"
	labels[prefix+".header.X-Content-Type-Options"] = "nosniff"
	labels[prefix+".header.Referrer-Policy"] = "strict-origin-when-cross-origin"
	// Never serve VCS metadata or dotenv files, but keep /.well-known reachable.
	// (any depth, so a link or subfolder cannot expose them)
	labels[prefix+".@hidden.path_regexp"] = `(^|/)\.(git|svn|hg|env|nextdeploy-db|ht[a-z]*)(/|$|\.)`
	labels[prefix+".respond_0"] = "@hidden 404"
	sec.appendLabels(labels, prefix)
	if socket != "" {
		labels[prefix+".php_fastcgi"] = "unix/" + socket
	}
	labels[prefix+".file_server"] = `{{""}}`
}

// SiteSecurity is the per-site protection that Caddy enforces before a request reaches PHP.
type SiteSecurity struct {
	BlockedIPs      []string `json:"blocked_ips"`      // addresses or CIDR ranges that always get 403
	AllowOnly       []string `json:"allow_only"`       // when not empty, every other address gets 403
	BasicAuthUser   string   `json:"basic_auth_user"`  // password protection for the whole site
	BasicAuthHash   string   `json:"basic_auth_hash"`  // bcrypt hash; never sent to the browser
	BlockXMLRPC     bool     `json:"block_xmlrpc"`     // /xmlrpc.php
	BlockUploadPHP  bool     `json:"block_upload_php"` // no PHP execution inside upload folders
	BlockScanners   bool     `json:"block_scanners"`   // known vulnerability scanners by User-Agent
	HSTS            bool     `json:"hsts"`             // Strict-Transport-Security
	FrameProtection bool     `json:"frame_protection"` // X-Frame-Options: SAMEORIGIN
}

const maxSecurityEntries = 500

// NormalizeIPList trims, validates (IPv4/IPv6 address or CIDR) and de-duplicates a list.
func NormalizeIPList(in []string) ([]string, error) {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, raw := range in {
		v := strings.TrimSpace(raw)
		if v == "" {
			continue
		}
		var canon string
		if strings.Contains(v, "/") {
			p, err := netip.ParsePrefix(v)
			if err != nil {
				return nil, fmt.Errorf("%q is not a valid IP range", v)
			}
			canon = p.Masked().String()
		} else {
			a, err := netip.ParseAddr(v)
			if err != nil {
				return nil, fmt.Errorf("%q is not a valid IP address", v)
			}
			canon = a.Unmap().String()
		}
		if !seen[canon] {
			seen[canon] = true
			out = append(out, canon)
		}
	}
	if len(out) > maxSecurityEntries {
		return nil, fmt.Errorf("at most %d entries", maxSecurityEntries)
	}
	return out, nil
}

// scannerAgents are tools whose only use against a site is probing it.
const scannerAgents = `(?i)(sqlmap|nikto|nmap|masscan|acunetix|nessus|openvas|wpscan|dirbuster|gobuster|havij|zgrab)`

func (s SiteSecurity) appendLabels(labels map[string]string, prefix string) {
	n := 1
	respond := func(matcher, status string) {
		labels[prefix+".respond_"+strconv.Itoa(n)] = matcher + " " + status
		n++
	}
	if len(s.BlockedIPs) > 0 {
		labels[prefix+".@blocked_ip.client_ip"] = strings.Join(s.BlockedIPs, " ")
		respond("@blocked_ip", "403")
	}
	if len(s.AllowOnly) > 0 {
		labels[prefix+".@not_allowed.not.client_ip"] = strings.Join(s.AllowOnly, " ")
		respond("@not_allowed", "403")
	}
	if s.BlockScanners {
		labels[prefix+".@scanner.header_regexp"] = "User-Agent " + scannerAgents
		respond("@scanner", "403")
	}
	if s.BlockXMLRPC {
		labels[prefix+".@xmlrpc.path"] = "/xmlrpc.php"
		respond("@xmlrpc", "403")
	}
	if s.BlockUploadPHP {
		labels[prefix+".@upload_php.path_regexp"] = `(?i)/(uploads|upload|files|media|wp-content/uploads)/.*\.(php|phtml|phar|php[0-9])$`
		respond("@upload_php", "403")
	}
	if s.BasicAuthUser != "" && s.BasicAuthHash != "" {
		labels[prefix+".basic_auth."+s.BasicAuthUser] = s.BasicAuthHash
	}
	if s.HSTS {
		labels[prefix+".header.Strict-Transport-Security"] = `"max-age=31536000; includeSubDomains"`
	}
	if s.FrameProtection {
		labels[prefix+".header.X-Frame-Options"] = "SAMEORIGIN"
	}
}
