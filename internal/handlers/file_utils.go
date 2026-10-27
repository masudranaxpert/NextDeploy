package handlers

import (
	"bytes"
	"encoding/base64"
	"net/http"
	"path/filepath"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/gofiber/fiber/v2"
)

// ResolveSafePathQuery extracts a relative file path from request query.
// It checks "p_b64" first (Base64-encoded to bypass external WAF / Cloudflare 403 blocks
// on sensitive files like wp-config.php or .env) and falls back to "path".
func ResolveSafePathQuery(c *fiber.Ctx) string {
	if b64 := strings.TrimSpace(c.Query("p_b64")); b64 != "" {
		b64Clean := strings.TrimRight(b64, "=")
		if decoded, err := base64.RawURLEncoding.DecodeString(b64Clean); err == nil {
			return strings.TrimSpace(string(decoded))
		}
		if decoded, err := base64.URLEncoding.DecodeString(b64); err == nil {
			return strings.TrimSpace(string(decoded))
		}
		if decoded, err := base64.StdEncoding.DecodeString(b64); err == nil {
			return strings.TrimSpace(string(decoded))
		}
	}
	return strings.TrimSpace(c.Query("path"))
}

func WorkspaceFileContentType(abs string, head []byte) string {
	t := http.DetectContentType(head)
	if t != "application/octet-stream" && t != "" {
		return t
	}
	switch strings.ToLower(filepath.Ext(abs)) {
	case ".html", ".htm":
		return "text/html; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".js", ".mjs":
		return "text/javascript; charset=utf-8"
	case ".json":
		return "application/json; charset=utf-8"
	case ".yml", ".yaml":
		return "text/yaml; charset=utf-8"
	case ".txt", ".conf", ".env", ".md", ".log", ".ini", ".sh":
		return "text/plain; charset=utf-8"
	default:
		return "application/octet-stream"
	}
}

func SafeContentDispositionFilename(rel string) string {
	base := filepath.Base(strings.ReplaceAll(rel, "\\", "/"))
	base = strings.ReplaceAll(base, `"`, "")
	if base == "." || base == "/" || base == "" {
		return "file"
	}
	return base
}

func GitRepoBlobPreviewText(b []byte) (text string, binary bool) {
	if len(b) == 0 {
		return "", false
	}
	if len(b) >= 2 && b[0] == 0xFF && b[1] == 0xFE {
		if len(b) == 2 {
			return "", false
		}
		return decodeUTF16Bytes(b[2:], false), false
	}
	if len(b) >= 2 && b[0] == 0xFE && b[1] == 0xFF {
		if len(b) == 2 {
			return "", false
		}
		return decodeUTF16Bytes(b[2:], true), false
	}
	trim := b
	if len(b) >= 3 && b[0] == 0xEF && b[1] == 0xBB && b[2] == 0xBF {
		trim = b[3:]
	}
	ct := http.DetectContentType(trim)
	if !strings.HasPrefix(ct, "text/") && ct != "application/octet-stream" {
		return "", true
	}
	if bytes.IndexByte(trim, 0) >= 0 {
		return "", true
	}
	if utf8.Valid(trim) {
		return string(trim), false
	}
	if gitRepoLikelyTextDespiteInvalidUTF8(trim) {
		return strings.ToValidUTF8(string(trim), "\uFFFD"), false
	}
	return "", true
}

func gitRepoLikelyTextDespiteInvalidUTF8(b []byte) bool {
	const maxSample = 256 * 1024
	n := len(b)
	if n > maxSample {
		n = maxSample
	}
	if n == 0 {
		return true
	}
	ok := 0
	for i := 0; i < n; i++ {
		c := b[i]
		switch {
		case c == 0x09 || c == 0x0A || c == 0x0D:
			ok++
		case c >= 0x20 && c <= 0x7E:
			ok++
		case c >= 0x80:
			ok++
		default:
		}
	}
	return float64(ok)/float64(n) >= 0.88
}

func decodeUTF16Bytes(b []byte, bigEndian bool) string {
	if len(b) < 2 {
		return ""
	}
	if len(b)%2 == 1 {
		b = b[:len(b)-1]
	}
	n := len(b) / 2
	u := make([]uint16, n)
	for i := 0; i < n; i++ {
		if bigEndian {
			u[i] = uint16(b[2*i])<<8 | uint16(b[2*i+1])
		} else {
			u[i] = uint16(b[2*i]) | uint16(b[2*i+1])<<8
		}
	}
	return string(utf16.Decode(u))
}
