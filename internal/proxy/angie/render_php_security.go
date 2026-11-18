package angie

import (
	"fmt"
	"strings"

	"panel/internal/proxy"
)

const scannerUserAgentsRegex = "(sqlmap|nikto|nmap|masscan|acunetix|nessus|openvas|wpscan|dirbuster|gobuster|havij|zgrab)"

func writePHPSecurity(b *strings.Builder, site proxy.Site, stem string, files proxy.Files) error {
	sec := site.Security
	if sec != nil {
		for _, ip := range sec.BlockedIPs {
			ip = strings.TrimSpace(ip)
			if ip == "" || proxy.HasUnsafeLabelChars(ip) {
				continue
			}
			fmt.Fprintf(b, "    deny %s;\n", ip)
		}
		if len(sec.AllowOnly) > 0 {
			for _, ip := range sec.AllowOnly {
				ip = strings.TrimSpace(ip)
				if ip == "" || proxy.HasUnsafeLabelChars(ip) {
					continue
				}
				fmt.Fprintf(b, "    allow %s;\n", ip)
			}
			b.WriteString("    deny all;\n")
		}
		if len(sec.BlockedIPs) > 0 || len(sec.AllowOnly) > 0 {
			b.WriteString("\n")
		}

		user := strings.TrimSpace(sec.BasicAuthUser)
		hash := strings.TrimSpace(sec.BasicAuthHash)
		if user != "" && hash != "" {
			if strings.ContainsAny(user, ":/\\\r\n\t ") || strings.ContainsAny(hash, "\r\n\t ") {
				return fmt.Errorf("angie site %q: unsafe basic auth credentials", site.ID)
			}
			htRel := "htpasswd/" + stem + ".htpasswd"
			files[htRel] = user + ":" + hash + "\n"
			b.WriteString("    auth_basic \"Restricted\";\n")
			fmt.Fprintf(b, "    auth_basic_user_file %s;\n\n", htRel)
		}

		if sec.BlockScanners {
			fmt.Fprintf(b, "    if ($http_user_agent ~* \"%s\") {\n        return 403;\n    }\n", scannerUserAgentsRegex)
		}
	}

	b.WriteString("    if ($uri ~* \"(^|/)\\.(git|svn|hg|env|nextdeploy-db|ht[a-z]*)(/|$|\\.)\") {\n        return 404;\n    }\n")
	if hideExtRe := hideExtensionRegex(site.PHP.HidePatterns); hideExtRe != "" {
		fmt.Fprintf(b, "    if ($uri ~* \"%s\") {\n        return 404;\n    }\n", hideExtRe)
	}
	if sec != nil {
		if sec.BlockXMLRPC {
			b.WriteString("    if ($uri = \"/xmlrpc.php\") {\n        return 403;\n    }\n")
		}
		if sec.BlockUploadPHP {
			b.WriteString("    if ($uri ~* \"/(uploads|upload|files|media|wp-content/uploads)/.*\\.(php|phtml|phar|php[0-9])$\") {\n        return 403;\n    }\n")
		}
	}
	b.WriteString("\n")
	return nil
}

func writePHPHideLocations(b *strings.Builder, spec *proxy.PHPSpec, sec *proxy.SecuritySpec) {
	b.WriteString("    location ~* (^|/)\\.(git|svn|hg|env|nextdeploy-db|ht[a-z]*)(/|$|\\.) {\n        return 404;\n    }\n")
	if hideExtRe := hideExtensionRegex(spec.HidePatterns); hideExtRe != "" {
		fmt.Fprintf(b, "    location ~* %s {\n        return 404;\n    }\n", hideExtRe)
	}
	if sec != nil {
		if sec.BlockXMLRPC {
			b.WriteString("    location = /xmlrpc.php {\n        return 403;\n    }\n")
		}
		if sec.BlockUploadPHP {
			b.WriteString("    location ~* /(uploads|upload|files|media|wp-content/uploads)/.*\\.(php|phtml|phar|php[0-9])$ {\n        return 403;\n    }\n")
		}
	}
	b.WriteString("\n")
}

func hideExtensionRegex(patterns []string) string {
	var exts []string
	for _, raw := range patterns {
		p := strings.TrimSpace(raw)
		if strings.HasPrefix(p, "*.") && len(p) > 2 {
			ext := strings.TrimPrefix(p, "*.")
			if !proxy.HasUnsafeLabelChars(ext) {
				exts = append(exts, escapeRegexMeta(ext))
			}
		}
	}
	if len(exts) == 0 {
		return `\.(sql|db|sqlite|key|sock)$`
	}
	return `\.(` + strings.Join(exts, "|") + `)$`
}
