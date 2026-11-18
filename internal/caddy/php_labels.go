package caddy

import (
	"fmt"
	"strconv"
	"strings"

	"panel/internal/db"
	"panel/internal/proxy"
)

// SiteSecurity is the per-site protection that Caddy enforces before a request reaches PHP.
type SiteSecurity = proxy.SiteSecurity

// NormalizeIPList trims, validates (IPv4/IPv6 address or CIDR) and de-duplicates a list.
func NormalizeIPList(in []string) ([]string, error) {
	return proxy.NormalizeIPList(in)
}

// PHPSiteLabels builds the caddy-docker-proxy labels for a PHP/static site served straight
// from disk. root is the document root as Caddy sees it; socket is the PHP-FPM unix socket
// path as Caddy sees it ("" for static sites).
func PHPSiteLabels(domains []db.AppDomain, root, socket string, sec SiteSecurity) map[string]string {
	return PHPSiteLabelsWithRouting(domains, root, socket, sec, SiteRouting{})
}

// PHPSiteLabelsWithRouting builds caddy-docker-proxy labels including per-site routing & fallback rules.
func PHPSiteLabelsWithRouting(domains []db.AppDomain, root, socket string, sec SiteSecurity, routing SiteRouting) map[string]string {
	out := map[string]string{}
	domains = sortedDomains(domains)
	for i, d := range domains {
		prefix := "caddy"
		if len(domains) > 1 {
			prefix = "caddy_" + strconv.Itoa(i)
		}
		appendPHPSiteLabels(out, prefix, d, root, socket, sec, routing)
	}
	return out
}

func appendPHPSiteLabels(labels map[string]string, prefix string, d db.AppDomain, root, socket string, sec SiteSecurity, routing SiteRouting) {
	domain := cleanQuotedValue(d.Domain)
	if domain == "" {
		return
	}
	var sites []string
	if d.EnableHTTPS || d.ShieldNodeID != nil {
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
	if (d.EnableHTTPS || d.ShieldNodeID != nil) && (d.ShieldNodeID != nil || ShouldUseInternalTLS(domain)) {
		labels[prefix+".tls"] = "internal"
	}
	labels[prefix+".root"] = "* " + root
	labels[prefix+".encode"] = "zstd gzip"
	labels[prefix+".header.X-Content-Type-Options"] = "nosniff"
	labels[prefix+".header.Referrer-Policy"] = "strict-origin-when-cross-origin"
	labels[prefix+".@media.path"] = "*.ico *.gif *.jpg *.jpeg *.png *.svg *.woff *.woff2 *.webp *.avif *.ttf *.eot"
	labels[prefix+".header_0"] = `@media Cache-Control "public, max-age=2592000, immutable"`
	labels[prefix+".@code.path"] = "*.css *.js"
	labels[prefix+".header_1"] = `@code Cache-Control "public, max-age=604800, stale-while-revalidate=86400"`
	// Never serve VCS metadata or dotenv files, but keep /.well-known reachable.
	// (any depth, so a link or subfolder cannot expose them)
	labels[prefix+".@hidden.path_regexp"] = `(^|/)\.(git|svn|hg|env|nextdeploy-db|ht[a-z]*)(/|$|\.)`
	labels[prefix+".respond_0"] = "@hidden 404"
	appendSecurityLabels(sec, labels, prefix)
	hasCustomRules := appendRoutingLabels(routing, labels, prefix, socket != "")
	if socket != "" {
		// Auto-serve WordPress static HTML disk cache (WP Fastest Cache, WP Super Cache,
		// Cache Enabler, WP Rocket) directly via Caddy file_server in <1ms without touching PHP or DB.
		// Bypassed for logged-in users, WooCommerce cart, POST requests, and admin/API endpoints.
		labels[prefix+".@wp_cache.0_not.header_regexp"] = `Cookie "comment_author|wordpress_[a-f0-9]+|wp-postpass|wordpress_logged_in|woocommerce_items_in_cart|wp_woocommerce_session"`
		labels[prefix+".@wp_cache.1_not.path_regexp"] = `(?i)(/wp-admin/|/xmlrpc.php|/wp-(app|cron|login|register|mail).php|/wp-json/|/feed/|sitemap.*.xml)`
		labels[prefix+".@wp_cache.2_not.method"] = "POST"
		labels[prefix+".@wp_cache.3_not.expression"] = "{query} != ''"
		labels[prefix+".@wp_cache.file.try_files"] = "/wp-content/cache/all{path}/index.html /wp-content/cache/supercache/{host}{uri}/index-https.html /wp-content/cache/supercache/{host}{uri}/index.html /wp-content/cache/cache-enabler/{host}{uri}/https-index.html /wp-content/cache/cache-enabler/{host}{uri}/index.html /wp-content/cache/wp-rocket/{host}{uri}/index-https.html /wp-content/cache/wp-rocket/{host}{uri}/index.html"
		if hasCustomRules {
			// Run WP cache lookup inside route after user-defined block/redir/rewrite rules.
			labels[fmt.Sprintf("%s.route.%03d_rewrite", prefix, routingEnabledCount(routing))] = "@wp_cache {http.matchers.file.relative}"
		} else {
			labels[prefix+".rewrite"] = "@wp_cache {http.matchers.file.relative}"
		}

		labels[prefix+".php_fastcgi"] = "unix/" + socket
		labels[prefix+".php_fastcgi.dial_timeout"] = "5s"
		labels[prefix+".php_fastcgi.read_timeout"] = "120s"
		labels[prefix+".php_fastcgi.write_timeout"] = "120s"
		if !d.TrustedProxy && d.ShieldNodeID == nil {
			applyUntrustedDirectProxyHeaders(labels, prefix+".php_fastcgi")
		}
		switch routing.FallbackMode {
		case FallbackStrict404:
			labels[prefix+".php_fastcgi.try_files"] = "{path} {path}/index.php =404"
		case FallbackCustom:
			if entry := strings.TrimPrefix(strings.TrimSpace(routing.FallbackEntry), "/"); entry != "" {
				labels[prefix+".php_fastcgi.try_files"] = "{path} {path}/index.php /" + entry
			}
		}
		if d.ShieldNodeID != nil || d.EnableHTTPS {
			labels[prefix+".php_fastcgi.env"] = "HTTPS on"
		}
	} else if routing.FallbackMode == FallbackCustom {
		if entry := strings.TrimPrefix(strings.TrimSpace(routing.FallbackEntry), "/"); entry != "" {
			labels[prefix+".@nd_spa.not.file"] = `{{""}}`
			labels[prefix+".rewrite_fallback"] = "@nd_spa /" + entry
		}
	}
	labels[prefix+".file_server.precompressed"] = "gzip zstd"
	labels[prefix+".file_server"] = `{{""}}`
	labels[prefix+".file_server.hide"] = ".git .env* *.sql *.db *.sqlite *.key *.sock"
}

// scannerAgents are tools whose only use against a site is probing it.
const scannerAgents = `(?i)(sqlmap|nikto|nmap|masscan|acunetix|nessus|openvas|wpscan|dirbuster|gobuster|havij|zgrab)`

func appendSecurityLabels(s SiteSecurity, labels map[string]string, prefix string) {
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
