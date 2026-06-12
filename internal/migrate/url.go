package migrate

import (
	"fmt"
	"net"
	"strconv"
	"strings"
)

func normalizePanelDomain(domain string) string {
	domain = strings.TrimSpace(domain)
	domain = strings.TrimPrefix(domain, "https://")
	domain = strings.TrimPrefix(domain, "http://")
	return strings.TrimRight(domain, "/")
}

func PublicBaseURL(panelDomain string, enableHTTPS bool, host string, port int) string {
	domain := normalizePanelDomain(panelDomain)
	if domain != "" {
		scheme := "http"
		if enableHTTPS {
			scheme = "https"
		}
		return scheme + "://" + domain
	}
	host = strings.TrimSpace(host)
	if host == "" {
		host = "127.0.0.1"
	}
	if h, pStr, err := net.SplitHostPort(host); err == nil {
		host = h
		if n, err := strconv.Atoi(pStr); err == nil && n > 0 {
			port = n
		}
	}
	if port <= 0 {
		if enableHTTPS {
			port = 443
		} else {
			port = 80
		}
	}
	scheme := "http"
	if enableHTTPS {
		scheme = "https"
	}
	if (scheme == "http" && port == 80) || (scheme == "https" && port == 443) {
		return fmt.Sprintf("%s://%s", scheme, host)
	}
	return fmt.Sprintf("%s://%s:%d", scheme, host, port)
}

func DownloadURL(base, token string) string {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	token = strings.TrimSpace(token)
	if base == "" || token == "" {
		return ""
	}
	return base + "/migrate/download/" + token
}

func FormatBytes(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/float64(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/float64(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/float64(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}
