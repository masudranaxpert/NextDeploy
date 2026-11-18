package proxy

import (
	"fmt"
	"path"
	"regexp"
	"strings"
)

// Supported front-controller fallback modes for PHP and static sites.
const (
	FallbackDefault   = "default"
	FallbackStrict404 = "strict_404"
	FallbackCustom    = "custom"

	maxRoutingRules = 100
)

// RoutingRule represents one ordered per-site URL rewrite, redirect, or path-block rule.
type RoutingRule struct {
	ID                string `json:"id"`
	Kind              string `json:"kind"`       // "rewrite" | "redir" | "block"
	MatchType         string `json:"match_type"` // "exact" | "prefix" | "regex"
	Pattern           string `json:"pattern"`    // path or Go RE2 regex
	Target            string `json:"target"`     // destination path (rewrite) or path/URL (redir)
	Status            int    `json:"status"`     // 301/302/307/308 (redir) or 403/404/410 (block)
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
	blockedHiddenSeg  = regexp.MustCompile(`(?i)(^|/)\.(git|svn|hg|env|ht)`)
	allowedBlockCodes = map[int]bool{403: true, 404: true, 410: true}
)

// ValidateRouting normalizes and validates a site's fallback mode and routing rules,
// rejecting unsafe paths, invalid regexes, unsupported statuses, and rule counts above the limit.
func ValidateRouting(r *SiteRouting, isStatic bool) error {
	if r == nil {
		return nil
	}
	_ = isStatic
	mode := strings.TrimSpace(strings.ToLower(r.FallbackMode))
	switch mode {
	case "", FallbackDefault:
		r.FallbackMode = ""
		r.FallbackEntry = ""
	case FallbackStrict404:
		r.FallbackMode = FallbackStrict404
		r.FallbackEntry = ""
	case FallbackCustom:
		entry := strings.TrimSpace(strings.ReplaceAll(r.FallbackEntry, "\\", "/"))
		entry = strings.TrimPrefix(entry, "/")
		if entry == "" || HasUnsafeLabelChars(entry) || strings.Contains(entry, "..") || strings.ContainsAny(entry, "?&") {
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
		if err := validateRoutingRule(&r.Rules[i], i); err != nil {
			return err
		}
	}
	return nil
}

func validateRoutingRule(rule *RoutingRule, idx int) error {
	rule.ID = strings.TrimSpace(rule.ID)
	if rule.ID == "" {
		rule.ID = fmt.Sprintf("rule-%d", idx+1)
	}
	rule.Kind = strings.TrimSpace(strings.ToLower(rule.Kind))
	rule.MatchType = strings.TrimSpace(strings.ToLower(rule.MatchType))
	if rule.MatchType == "" {
		rule.MatchType = "exact"
	}
	rule.Pattern = strings.TrimSpace(rule.Pattern)
	rule.Target = strings.TrimSpace(rule.Target)
	rule.Note = strings.TrimSpace(rule.Note)

	if rule.Pattern == "" || HasUnsafeLabelChars(rule.Pattern) {
		return fmt.Errorf("rule %d: pattern cannot be empty or contain spaces, quotes, braces, or control characters", idx+1)
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
			return fmt.Errorf("rule %d: invalid regex %q: %w", idx+1, rule.Pattern, err)
		}
	default:
		return fmt.Errorf("rule %d: invalid match_type %q (use exact, prefix, or regex)", idx+1, rule.MatchType)
	}

	switch rule.Kind {
	case "rewrite":
		if rule.Target == "" || !strings.HasPrefix(rule.Target, "/") || HasUnsafeLabelChars(rule.Target) || strings.Contains(rule.Target, "..") {
			return fmt.Errorf("rule %d: rewrite target must be a safe internal path starting with /", idx+1)
		}
		if blockedHiddenSeg.MatchString(rule.Target) {
			return fmt.Errorf("rule %d: rewrite target cannot point to hidden system files", idx+1)
		}
		rule.Status = 0
	case "redir":
		if rule.Target == "" || HasUnsafeLabelChars(rule.Target) {
			return fmt.Errorf("rule %d: redirect target is invalid", idx+1)
		}
		low := strings.ToLower(rule.Target)
		if !strings.HasPrefix(rule.Target, "/") && !strings.HasPrefix(low, "http://") && !strings.HasPrefix(low, "https://") {
			return fmt.Errorf("rule %d: redirect target must start with /, http://, or https://", idx+1)
		}
		if rule.Status == 0 {
			rule.Status = 301
		}
		if !allowedRedirectCodes[rule.Status] {
			return fmt.Errorf("rule %d: redirect status must be 301, 302, 307, or 308", idx+1)
		}
	case "block":
		rule.Target = ""
		if rule.Status == 0 {
			rule.Status = 404
		}
		if !allowedBlockCodes[rule.Status] {
			return fmt.Errorf("rule %d: block status must be 403, 404, or 410", idx+1)
		}
	default:
		return fmt.Errorf("rule %d: invalid kind %q (use rewrite, redir, or block)", idx+1, rule.Kind)
	}
	return nil
}

// MergeUniqueRoutingRules appends incoming rules while deduplicating by kind,
// match type, pattern, and target. Newly appended rules receive sequential "ht-N" IDs.
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
