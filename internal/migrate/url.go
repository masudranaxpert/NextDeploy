package migrate

import (
	"fmt"
	"strings"
)

func PublicBaseURL(panelDomain string, enableHTTPS bool, host string, port int) string {
	domain := strings.TrimSpace(panelDomain)
	if domain != "" {
		if enableHTTPS {
			return "https://" + domain
		}
		return "http://" + domain
	}
	host = strings.TrimSpace(host)
	if host == "" {
		host = "127.0.0.1"
	}
	if port <= 0 {
		port = 8080
	}
	return fmt.Sprintf("http://%s:%d", host, port)
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
