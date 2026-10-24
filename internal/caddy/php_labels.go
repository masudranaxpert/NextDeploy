package caddy

import (
	"strconv"
	"strings"

	"panel/internal/db"
)

// PHPSiteLabels builds the caddy-docker-proxy labels for a PHP/static site served straight
// from disk. root is the document root as Caddy sees it; socket is the PHP-FPM unix socket
// path as Caddy sees it ("" for static sites).
func PHPSiteLabels(domains []db.AppDomain, root, socket string) map[string]string {
	out := map[string]string{}
	domains = sortedDomains(domains)
	for i, d := range domains {
		prefix := "caddy"
		if len(domains) > 1 {
			prefix = "caddy_" + strconv.Itoa(i)
		}
		appendPHPSiteLabels(out, prefix, d, root, socket)
	}
	return out
}

func appendPHPSiteLabels(labels map[string]string, prefix string, d db.AppDomain, root, socket string) {
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
	labels[prefix+".respond"] = "@hidden 404"
	if socket != "" {
		labels[prefix+".php_fastcgi"] = "unix/" + socket
	}
	labels[prefix+".file_server"] = `{{""}}`
}
