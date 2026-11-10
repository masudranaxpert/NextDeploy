package caddy

import (
	"fmt"
	"path"
	"regexp"
	"strconv"
	"strings"
)

const (
	FallbackDefault   = "default"
	FallbackStrict404 = "strict_404"
	FallbackCustom    = "custom"

	maxRoutingRules = 100
)

// RoutingRule represents one ordered per-site URL rewrite, redirect, or path-block rule.
type RoutingRule struct {
	ID            string `json:"id"`
	Kind          string `json:"kind"`                     // "rewrite" | "redir" | "block"
	MatchType     string `json:"match_type"`               // "exact" | "prefix" | "regex"
	Pattern       string `json:"pattern"`                  // path or Go RE2 regex
	Target        string `json:"target"`                   // destination path (rewrite) or path/URL (redir)
	Status        int    `json:"status"`                   // 301/302/307/308 (redir) or 403/404/410 (block)
	Enabled           bool   `json:"enabled"`
	UnlessFile        bool   `json:"unless_exists,omitempty"`       // true when Apache RewriteCond !-f / !-d applies
	RequireTargetFile bool   `json:"require_target_file,omitempty"` // true when Apache RewriteCond %{REQUEST_FILENAME}.php -f applies
	PreserveQuery     bool   `json:"preserve_query,omitempty"`      // true for Apache [QSA] when target contains ?
	Note              string `json:"note,omitempty"`
}

// SiteRouting stores a PHP or static site's custom fallback strategy and ordered routing rules.
type SiteRouting struct {
	FallbackMode  string        `json:"fallback_mode,omitempty"`
	FallbackEntry string        `json:"fallback_entry,omitempty"`
	Rules         []RoutingRule `json:"rules,omitempty"`
}

var (
	captureRefRe      = regexp.MustCompile(`\$([0-9])`)
	blockedHiddenSeg  = regexp.MustCompile(`(?i)(^|/)\.(git|svn|hg|env|ht)`)
	allowedBlockCodes = map[int]bool{403: true, 404: true, 410: true}
)

func hasUnsafeLabelChars(s string) bool {
	if strings.ContainsAny(s, "\r\n\t\"'`{};# ") {
		return true
	}
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}

// ValidateRouting normalizes and validates a site's fallback mode and routing rules.
func ValidateRouting(r *SiteRouting, isStatic bool) error {
	if r == nil {
		return nil
	}
	mode := strings.TrimSpace(strings.ToLower(r.FallbackMode))
	switch mode {
	case "", FallbackDefault:
		r.FallbackMode = ""
		r.FallbackEntry = ""
	case FallbackStrict404:
		if isStatic {
			// Static file_server already returns 404 for missing files; normalize cleanly.
			r.FallbackMode = FallbackStrict404
		} else {
			r.FallbackMode = FallbackStrict404
		}
		r.FallbackEntry = ""
	case FallbackCustom:
		entry := strings.TrimSpace(strings.ReplaceAll(r.FallbackEntry, "\\", "/"))
		entry = strings.TrimPrefix(entry, "/")
		if entry == "" || hasUnsafeLabelChars(entry) || strings.Contains(entry, "..") || strings.ContainsAny(entry, "?&") {
			return fmt.Errorf("custom fallback entry must be a safe relative file path such as app.php or index.html")
		}
		cleaned := strings.TrimPrefix(path.Clean("/"+entry), "/")
		if cleaned == "" || cleaned == "." || blockedHiddenSeg.MatchString("/"+cleaned) {
			return fmt.Errorf("invalid fallback entry %q", r.FallbackEntry)
		}
		r.FallbackMode = FallbackCustom
		r.FallbackEntry = cleaned
	default:
		return fmt.Errorf("unsupported fallback mode %q", r.FallbackMode)
	}

	if len(r.Rules) > maxRoutingRules {
		return fmt.Errorf("at most %d routing rules are supported per site", maxRoutingRules)
	}

	for i := range r.Rules {
		rule := &r.Rules[i]
		rule.ID = strings.TrimSpace(rule.ID)
		if rule.ID == "" {
			rule.ID = fmt.Sprintf("rule-%d", i+1)
		}
		rule.Kind = strings.TrimSpace(strings.ToLower(rule.Kind))
		rule.MatchType = strings.TrimSpace(strings.ToLower(rule.MatchType))
		if rule.MatchType == "" {
			rule.MatchType = "exact"
		}
		rule.Pattern = strings.TrimSpace(rule.Pattern)
		rule.Target = strings.TrimSpace(rule.Target)
		rule.Note = strings.TrimSpace(rule.Note)

		if rule.Pattern == "" || hasUnsafeLabelChars(rule.Pattern) {
			return fmt.Errorf("rule %d: pattern cannot be empty or contain spaces, quotes, braces, or control characters", i+1)
		}

		switch rule.MatchType {
		case "exact", "prefix":
			if !strings.HasPrefix(rule.Pattern, "/") {
				rule.Pattern = "/" + rule.Pattern
			}
			if rule.MatchType == "prefix" && rule.Pattern != "/*" {
				base := strings.TrimSuffix(strings.TrimSuffix(rule.Pattern, "/*"), "*")
				if base == "" || base == "/" {
					rule.Pattern = "/*"
				} else {
					rule.Pattern = strings.TrimRight(base, "/") + "/*"
				}
			}
		case "regex":
			if _, err := regexp.Compile(rule.Pattern); err != nil {
				return fmt.Errorf("rule %d: invalid regex %q: %w", i+1, rule.Pattern, err)
			}
		default:
			return fmt.Errorf("rule %d: invalid match_type %q (use exact, prefix, or regex)", i+1, rule.MatchType)
		}

		switch rule.Kind {
		case "rewrite":
			if rule.Target == "" || !strings.HasPrefix(rule.Target, "/") || hasUnsafeLabelChars(rule.Target) || strings.Contains(rule.Target, "..") {
				return fmt.Errorf("rule %d: rewrite target must be a safe internal path starting with /", i+1)
			}
			if blockedHiddenSeg.MatchString(rule.Target) {
				return fmt.Errorf("rule %d: rewrite target cannot point to hidden system files", i+1)
			}
			rule.Status = 0
		case "redir":
			if rule.Target == "" || hasUnsafeLabelChars(rule.Target) {
				return fmt.Errorf("rule %d: redirect target is invalid", i+1)
			}
			low := strings.ToLower(rule.Target)
			if !strings.HasPrefix(rule.Target, "/") && !strings.HasPrefix(low, "http://") && !strings.HasPrefix(low, "https://") {
				return fmt.Errorf("rule %d: redirect target must start with /, http://, or https://", i+1)
			}
			if rule.Status == 0 {
				rule.Status = 301
			}
			if !allowedRedirectCodes[rule.Status] {
				return fmt.Errorf("rule %d: redirect status must be 301, 302, 307, or 308", i+1)
			}
		case "block":
			rule.Target = ""
			if rule.Status == 0 {
				rule.Status = 404
			}
			if !allowedBlockCodes[rule.Status] {
				return fmt.Errorf("rule %d: block status must be 403, 404, or 410", i+1)
			}
		default:
			return fmt.Errorf("rule %d: invalid kind %q (use rewrite, redir, or block)", i+1, rule.Kind)
		}
	}
	return nil
}

func (r SiteRouting) enabledCount() int {
	n := 0
	for _, rule := range r.Rules {
		if rule.Enabled {
			n++
		}
	}
	return n
}

func (r SiteRouting) appendLabels(labels map[string]string, prefix string, _ bool) bool {
	if r.enabledCount() == 0 {
		return false
	}
	idx := 0
	for _, rule := range r.Rules {
		if !rule.Enabled {
			continue
		}
		if rule.Pattern == "" || hasUnsafeLabelChars(rule.Pattern) {
			continue
		}
		// Never use a trailing _<digits> suffix (e.g. @nd_rule_0): caddy-docker-proxy strips
		// _<digits> as an isolation suffix from directive keys, turning @nd_rule_0 into @nd_rule
		// while leaving the @nd_rule_0 reference in route labels intact.
		mName := "@ndrule" + strconv.Itoa(idx)
		regName := "ndr" + strconv.Itoa(idx)
		switch rule.MatchType {
		case "exact":
			labels[prefix+"."+mName+".path"] = rule.Pattern
		case "prefix":
			if rule.Pattern == "/*" {
				labels[prefix+"."+mName+".path"] = "/*"
			} else {
				base := strings.TrimSuffix(rule.Pattern, "/*")
				labels[prefix+"."+mName+".path"] = base + " " + base + "/*"
			}
		case "regex":
			labels[prefix+"."+mName+".path_regexp"] = regName + " " + rule.Pattern
		default:
			continue
		}
		if rule.UnlessFile && rule.Kind == "rewrite" {
			labels[prefix+"."+mName+".not.file"] = "{path} {path}/"
		}
		if rule.RequireTargetFile && rule.Kind == "rewrite" &&
			strings.HasSuffix(strings.ToLower(strings.SplitN(rule.Target, "?", 2)[0]), ".php") {
			labels[prefix+"."+mName+".file.try_files"] = "{path}.php"
		}

		expandTarget := func(t string) string {
			if rule.MatchType == "regex" {
				t = captureRefRe.ReplaceAllString(t, "{re."+regName+".$1}")
			}
			if rule.PreserveQuery && strings.Contains(t, "?") && !strings.Contains(t, "{query}") {
				if strings.HasSuffix(t, "?") || strings.HasSuffix(t, "&") {
					t += "{query}"
				} else {
					t += "&{query}"
				}
			}
			return t
		}

		step := fmt.Sprintf("%s.route.%03d_", prefix, idx)
		switch rule.Kind {
		case "redir":
			status := rule.Status
			if !allowedRedirectCodes[status] {
				status = 301
			}
			labels[step+"redir"] = fmt.Sprintf("%s %s %d", mName, expandTarget(rule.Target), status)
		case "block":
			status := rule.Status
			if !allowedBlockCodes[status] {
				status = 404
			}
			labels[step+"respond"] = fmt.Sprintf("%s %d", mName, status)
		case "rewrite":
			labels[step+"rewrite"] = fmt.Sprintf("%s %s", mName, expandTarget(rule.Target))
		}
		idx++
	}
	return idx > 0
}

// MergeUniqueRoutingRules appends incoming rules while deduplicating by kind, match type, pattern, and target.
func MergeUniqueRoutingRules(existing, incoming []RoutingRule) []RoutingRule {
	out := append([]RoutingRule(nil), existing...)
	seen := make(map[string]bool, len(out))
	for _, r := range out {
		seen[r.Kind+"|"+r.MatchType+"|"+r.Pattern+"|"+r.Target] = true
	}
	for _, r := range incoming {
		k := r.Kind + "|" + r.MatchType + "|" + r.Pattern + "|" + r.Target
		if seen[k] {
			continue
		}
		seen[k] = true
		r.ID = fmt.Sprintf("ht-%d", len(out)+1)
		out = append(out, r)
	}
	return out
}
