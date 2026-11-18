package angie

import (
	"fmt"
	"regexp"
	"strings"

	"panel/internal/proxy"
)

func writePHPRoutingRules(b *strings.Builder, rules []proxy.RoutingRule) error {
	if len(rules) == 0 {
		return nil
	}
	wrote := false
	for idx, rule := range rules {
		if !rule.Enabled {
			continue
		}
		if rule.Pattern == "" || proxy.HasUnsafeLabelChars(rule.Pattern) {
			return fmt.Errorf("rule %d (%s): invalid pattern %q", idx+1, rule.ID, rule.Pattern)
		}
		switch rule.Kind {
		case "block":
			if err := writeBlockRule(b, rule); err != nil {
				return err
			}
			wrote = true
		case "redir":
			if err := writeRedirRule(b, rule); err != nil {
				return err
			}
			wrote = true
		case "rewrite":
			if err := writeRewriteRule(b, idx, rule); err != nil {
				return err
			}
			wrote = true
		default:
			return fmt.Errorf("rule %d (%s): unsupported kind %q", idx+1, rule.ID, rule.Kind)
		}
	}
	if wrote {
		b.WriteString("\n")
	}
	return nil
}

func writeBlockRule(b *strings.Builder, rule proxy.RoutingRule) error {
	status := rule.Status
	if status != 403 && status != 404 && status != 410 {
		status = 404
	}
	cond, err := buildURICondition(rule.MatchType, rule.Pattern)
	if err != nil {
		return err
	}
	if cond == "" {
		fmt.Fprintf(b, "    return %d;\n", status)
		return nil
	}
	fmt.Fprintf(b, "    if (%s) {\n        return %d;\n    }\n", cond, status)
	return nil
}

func writeRedirRule(b *strings.Builder, rule proxy.RoutingRule) error {
	target := strings.TrimSpace(rule.Target)
	if target == "" || proxy.HasUnsafeLabelChars(target) {
		return fmt.Errorf("rule %q: invalid redirect target %q", rule.ID, rule.Target)
	}
	status := normalizeRedirectCode(rule.Status)

	pat := rule.Pattern
	if rule.MatchType == "regex" {
		if _, err := regexp.Compile(pat); err != nil {
			return fmt.Errorf("rule %q: invalid regex %q: %w", rule.ID, pat, err)
		}
		pat, target = shiftRegexCaptures(pat, target)
	}
	target = appendRedirPreserveQuery(target, rule.PreserveQuery)

	cond, err := buildURICondition(rule.MatchType, pat)
	if err != nil {
		return err
	}
	if cond == "" {
		fmt.Fprintf(b, "    return %d %s;\n", status, target)
		return nil
	}
	fmt.Fprintf(b, "    if (%s) {\n        return %d %s;\n    }\n", cond, status, target)
	return nil
}

func writeRewriteRule(b *strings.Builder, idx int, rule proxy.RoutingRule) error {
	target := strings.TrimSpace(rule.Target)
	if target == "" || !strings.HasPrefix(target, "/") || proxy.HasUnsafeLabelChars(target) || strings.Contains(target, "..") {
		return fmt.Errorf("rule %q: invalid rewrite target %q", rule.ID, rule.Target)
	}

	var regexPat string
	switch rule.MatchType {
	case "exact", "":
		regexPat = "^" + escapeRegexMeta(rule.Pattern) + "$"
	case "prefix":
		regexPat = prefixToRegex(rule.Pattern)
	case "regex":
		if _, err := regexp.Compile(rule.Pattern); err != nil {
			return fmt.Errorf("rule %q: invalid regex %q: %w", rule.ID, rule.Pattern, err)
		}
		regexPat, target = shiftRegexCaptures(rule.Pattern, target)
	default:
		return fmt.Errorf("rule %q: unsupported match_type %q", rule.ID, rule.MatchType)
	}

	// Nginx/Angie rewrite preserves $args automatically even when ? is in target,
	// unless target ends with a trailing '?'.
	if strings.Contains(target, "?") && !rule.PreserveQuery && !strings.HasSuffix(target, "?") {
		target += "?"
	}

	switch {
	case rule.UnlessFile && rule.RequireTargetFile:
		flagVar := fmt.Sprintf("$nd_rw_%d", idx)
		fmt.Fprintf(b, "    set %s \"1\";\n", flagVar)
		fmt.Fprintf(b, "    if (-e $request_filename) { set %s \"0\"; }\n", flagVar)
		fmt.Fprintf(b, "    if (!-f $document_root$uri.php) { set %s \"0\"; }\n", flagVar)
		fmt.Fprintf(b, "    if (%s = \"1\") {\n        rewrite %s %s last;\n    }\n", flagVar, regexPat, target)
	case rule.UnlessFile:
		fmt.Fprintf(b, "    if (!-e $request_filename) {\n        rewrite %s %s last;\n    }\n", regexPat, target)
	case rule.RequireTargetFile:
		fmt.Fprintf(b, "    if (-f $document_root$uri.php) {\n        rewrite %s %s last;\n    }\n", regexPat, target)
	default:
		fmt.Fprintf(b, "    rewrite %s %s last;\n", regexPat, target)
	}
	return nil
}

func buildURICondition(matchType, pattern string) (string, error) {
	switch matchType {
	case "exact", "":
		return fmt.Sprintf(`$uri = "%s"`, escapeNginxString(pattern)), nil
	case "prefix":
		if pattern == "/*" {
			return "", nil
		}
		return "$uri ~ " + prefixToRegex(pattern), nil
	case "regex":
		if _, err := regexp.Compile(pattern); err != nil {
			return "", fmt.Errorf("invalid regex %q: %w", pattern, err)
		}
		return "$uri ~ " + pattern, nil
	default:
		return "", fmt.Errorf("unsupported match_type %q", matchType)
	}
}

func prefixToRegex(pattern string) string {
	if pattern == "/*" {
		return "^/"
	}
	base := strings.TrimSuffix(strings.TrimSuffix(pattern, "/*"), "*")
	if base == "" || base == "/" {
		return "^/"
	}
	return "^" + escapeRegexMeta(base) + "(?:/.*)?$"
}

func appendRedirPreserveQuery(target string, preserveQuery bool) string {
	if !preserveQuery {
		return target
	}
	if strings.Contains(target, "?") {
		if strings.HasSuffix(target, "?") {
			return strings.TrimSuffix(target, "?") + "$is_args$args"
		}
		if strings.HasSuffix(target, "&") {
			return strings.TrimSuffix(target, "&") + "$nd_amp_args"
		}
		return target + "$nd_amp_args"
	}
	return target + "$is_args$args"
}

// shiftRegexCaptures adapts Go RE2 $0 whole-match references for Angie PCRE,
// which only supports $1..$9 capture variables.
func shiftRegexCaptures(pattern, target string) (string, string) {
	if !strings.Contains(target, "$0") {
		return pattern, target
	}
	if strings.Contains(target, "$9") {
		return pattern, strings.ReplaceAll(target, "$0", "$uri")
	}
	var b strings.Builder
	for i := 0; i < len(target); i++ {
		if target[i] == '$' && i+1 < len(target) && target[i+1] >= '0' && target[i+1] <= '8' {
			b.WriteByte('$')
			b.WriteByte(target[i+1] + 1)
			i++
			continue
		}
		b.WriteByte(target[i])
	}
	return "(" + pattern + ")", b.String()
}
