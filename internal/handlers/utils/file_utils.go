package utils

import (
	"bytes"
	"net/http"
	"path/filepath"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

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
	if gitRepoBinaryMagic(trim) {
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

func gitRepoBinaryMagic(b []byte) bool {
	if len(b) < 4 {
		return false
	}
	if bytes.HasPrefix(b, []byte("%PDF")) {
		return true
	}
	if len(b) >= 8 && bytes.HasPrefix(b, []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}) {
		return true
	}
	if len(b) >= 3 && b[0] == 0xFF && b[1] == 0xD8 && b[2] == 0xFF {
		return true
	}
	if len(b) >= 6 && (bytes.HasPrefix(b, []byte("GIF87a")) || bytes.HasPrefix(b, []byte("GIF89a"))) {
		return true
	}
	if bytes.HasPrefix(b, []byte("PK\x03\x04")) {
		return true
	}
	if len(b) >= 12 && bytes.HasPrefix(b, []byte{0x00, 0x00, 0x00, 0x18, 0x66, 0x74, 0x79, 0x70}) {
		return true
	}
	return false
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
