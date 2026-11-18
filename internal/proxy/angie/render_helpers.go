package angie

import (
	"sort"
	"strings"

	"panel/internal/proxy"
)

func writePrecompressedDirectives(b *strings.Builder, indent string, pre []string) {
	for _, p := range pre {
		switch strings.ToLower(strings.TrimSpace(p)) {
		case "gzip":
			b.WriteString(indent + "gzip_static on;\n")
		case "zstd":
			b.WriteString(indent + "zstd_static on;\n")
		}
	}
}

func writeStaticHideBlock(b *strings.Builder, indent string, hide []string) {
	if len(hide) == 0 {
		b.WriteString(indent + "include snippets/hide.conf;\n")
		return
	}
	defaultList := []string{".git", ".env*", "*.sql", "*.db", "*.sqlite", "*.key", "*.sock"}
	if equalSortedStrings(hide, defaultList) {
		b.WriteString(indent + "include snippets/hide.conf;\n")
		return
	}
	if regex := buildHideRegex(hide); regex != "" {
		b.WriteString(indent + "location ~ " + regex + " {\n" + indent + "    deny all;\n" + indent + "}\n")
	}
}

func buildHideRegex(patterns []string) string {
	var parts []string
	for _, raw := range patterns {
		p := strings.TrimSpace(raw)
		if p == "" || proxy.HasUnsafeLabelChars(p) {
			continue
		}
		if strings.HasPrefix(p, "*.") {
			parts = append(parts, `\.`+escapeRegexMeta(strings.TrimPrefix(p, "*."))+`$`)
		} else if strings.HasSuffix(p, "*") {
			parts = append(parts, `/`+escapeRegexMeta(strings.TrimSuffix(p, "*")))
		} else {
			parts = append(parts, `/`+escapeRegexMeta(p)+`(?:/|$)`)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return "(" + strings.Join(parts, "|") + ")"
}

func equalSortedStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	ca, cb := append([]string(nil), a...), append([]string(nil), b...)
	sort.Strings(ca)
	sort.Strings(cb)
	for i := range ca {
		if ca[i] != cb[i] {
			return false
		}
	}
	return true
}

func normalizeLocationPrefix(raw string) string {
	s := strings.TrimRight(strings.TrimSuffix(strings.TrimSuffix(strings.TrimSpace(raw), "/*"), "*"), "/")
	if s == "" {
		return "/"
	}
	if !strings.HasPrefix(s, "/") {
		return "/" + s
	}
	return s
}

func sanitizeHeaderValue(v string) string {
	return strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(v, "\r", ""), "\n", ""))
}

func escapeNginxString(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, `\`, `\\`), `"`, `\"`)
}

func escapeRegexMeta(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '.', '+', '*', '?', '(', ')', '|', '[', ']', '{', '}', '^', '$', '\\':
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}
