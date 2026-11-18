package ferron

import (
	"fmt"
	"regexp"
	"strings"

	"panel/internal/proxy"
)

func (d *Driver) prepareRoutingRules(slug, siteID string, rules []proxy.RoutingRule) ([]phpRedirMatcher, []string, []string, error) {
	var redirs []phpRedirMatcher
	var rewrites []string
	var ignored []string

	for idx, r := range rules {
		if !r.Enabled {
			continue
		}
		if r.RequireTargetFile {
			if d.strictUnsupported {
				return nil, nil, nil, fmt.Errorf("ferron: site %q routing rule %q uses RequireTargetFile which Ferron does not support", siteID, r.ID)
			}
			ignored = append(ignored, fmt.Sprintf("IGNORED rule %q (%s): RequireTargetFile=true is unsupported by Ferron", r.ID, r.Kind))
			continue
		}
		patRegex, err := routingRulePathRegex(r.MatchType, r.Pattern)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("ferron: site %q routing rule %q: %w", siteID, r.ID, err)
		}

		switch strings.ToLower(strings.TrimSpace(r.Kind)) {
		case "block":
			code := r.Status
			if code != 403 && code != 404 && code != 410 {
				code = 403
			}
			redirs = append(redirs, phpRedirMatcher{
				matcher: fmt.Sprintf("nd_%s_b%d", slug, idx),
				regex:   patRegex,
				code:    code,
			})
		case "redir":
			code := r.Status
			if code < 300 || code > 399 {
				code = 301
			}
			target := translateCaptureRefs(strings.TrimSpace(r.Target))
			if r.PreserveQuery {
				sep := "?"
				if strings.Contains(target, "?") {
					sep = "&"
				}
				redirs = append(redirs,
					phpRedirMatcher{
						matcher: fmt.Sprintf("nd_%s_r%d_q", slug, idx),
						regex:   patRegex,
						target:  target + sep + "{{request.uri.query}}",
						code:    code,
					},
					phpRedirMatcher{
						matcher: fmt.Sprintf("nd_%s_r%d_noq", slug, idx),
						regex:   patRegex,
						target:  target,
						code:    code,
					},
				)
			} else {
				redirs = append(redirs, phpRedirMatcher{
					matcher: fmt.Sprintf("nd_%s_r%d", slug, idx),
					regex:   patRegex,
					target:  target,
					code:    code,
				})
			}
		case "rewrite", "":
			rewRe, repl, err := buildFerronRewriteRule(r)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("ferron: site %q routing rule %q: %w", siteID, r.ID, err)
			}
			var rw strings.Builder
			fmt.Fprintf(&rw, "        rewrite %s %s {\n", rawString(rewRe), quoteString(repl))
			if r.UnlessFile {
				rw.WriteString("            file false\n")
				rw.WriteString("            directory false\n")
			}
			rw.WriteString("            last\n")
			rw.WriteString("        }\n")
			rewrites = append(rewrites, rw.String())
		default:
			return nil, nil, nil, fmt.Errorf("ferron: unsupported routing rule kind %q", r.Kind)
		}
	}
	return redirs, rewrites, ignored, nil
}

func routingRulePathRegex(matchType, pattern string) (string, error) {
	pat := strings.TrimSpace(pattern)
	if pat == "" {
		return "", fmt.Errorf("empty pattern")
	}
	switch strings.ToLower(strings.TrimSpace(matchType)) {
	case "exact", "":
		return "^" + regexp.QuoteMeta(pat) + "$", nil
	case "prefix":
		return "^" + regexp.QuoteMeta(pat), nil
	case "regex":
		if _, err := regexp.Compile(pat); err != nil {
			return "", fmt.Errorf("invalid regex %q: %w", pat, err)
		}
		return pat, nil
	default:
		return "", fmt.Errorf("unknown match type %q", matchType)
	}
}

func buildFerronRewriteRule(r proxy.RoutingRule) (string, string, error) {
	pat := strings.TrimSpace(r.Pattern)
	target := strings.TrimSpace(r.Target)
	switch strings.ToLower(strings.TrimSpace(r.MatchType)) {
	case "exact", "":
		rewRe := "^" + regexp.QuoteMeta(pat) + `(?:\?.*)?$`
		if !r.PreserveQuery {
			return rewRe, target, nil
		}
		if strings.Contains(target, "?") {
			return "^" + regexp.QuoteMeta(pat) + `(?:\?(.*))?$`, target + "&$1", nil
		}
		return "^" + regexp.QuoteMeta(pat) + `(\?.*)?$`, target + "$1", nil
	case "prefix":
		if r.PreserveQuery {
			return "^" + regexp.QuoteMeta(pat) + `(.*)$`, target + "$1", nil
		}
		return "^" + regexp.QuoteMeta(pat) + `[^?]*(\?.*)?$`, target, nil
	case "regex":
		if _, err := regexp.Compile(pat); err != nil {
			return "", "", fmt.Errorf("invalid regex %q: %w", pat, err)
		}
		return buildApacheStyleRegexRewrite(pat, target, r.PreserveQuery)
	default:
		return "", "", fmt.Errorf("unknown match type %q", r.MatchType)
	}
}

func translateCaptureRefs(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '$' && i+1 < len(s) && s[i+1] >= '0' && s[i+1] <= '9' {
			b.WriteString("${")
			b.WriteByte(s[i+1])
			b.WriteByte('}')
			i++
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func resolvePHPFallbackRewrite(fallback, entry string, isPHP bool) string {
	switch strings.ToLower(strings.TrimSpace(fallback)) {
	case "strict_404":
		return ""
	case "custom":
		target := strings.TrimSpace(entry)
		if target == "" {
			if isPHP {
				target = "/index.php"
			} else {
				return ""
			}
		} else if !strings.HasPrefix(target, "/") {
			target = "/" + target
		}
		return "        rewrite r\"^[^?]+\" " + quoteString(target) + " {\n            file false\n            directory false\n            last\n        }\n"
	default:
		if !isPHP {
			return ""
		}
		return "        rewrite r\"^[^?]+\" \"/index.php\" {\n            file false\n            directory false\n            last\n        }\n"
	}
}

// buildApacheStyleRegexRewrite emulates an Apache RewriteRule on Ferron. Apache matches the
// pattern against the path only and replaces the whole path; Ferron's `rewrite` matches the
// full URI including "?query" and replaces only the matched part. So the user pattern is made
// path-only (it can never consume "?"), widened to the whole path, and the query string is
// captured in an extra trailing group whose index follows the user's own groups.
func buildApacheStyleRegexRewrite(pat, target string, qsa bool) (string, string, error) {
	body := pat
	anchoredStart := strings.HasPrefix(body, "^")
	body = strings.TrimPrefix(body, "^")
	anchoredEnd := strings.HasSuffix(body, "$") && !strings.HasSuffix(body, `\$`)
	body = strings.TrimSuffix(body, "$")
	if anchoredEnd && strings.HasSuffix(pat, `\$`) {
		anchoredEnd = false
	}
	core := pathOnlyRegex(body)
	re, err := regexp.Compile(core)
	if err != nil {
		return "", "", fmt.Errorf("invalid regex %q: %w", pat, err)
	}
	q := re.NumSubexp() + 1
	full := "^"
	if !anchoredStart {
		full += `[^?]*?`
	}
	full += "(?:" + core + ")"
	if !anchoredEnd {
		full += `[^?]*`
	}
	hasQuery := strings.Contains(target, "?")
	switch {
	case qsa && hasQuery:
		return full + `(?:\?(.*))?$`, target + fmt.Sprintf("&${%d}", q), nil
	case !qsa && hasQuery:
		// Apache drops the original query string when the substitution defines its own.
		return full + `(?:\?.*)?$`, target, nil
	default:
		// No query in the substitution: Apache keeps the original one (with or without QSA).
		return full + `(\?.*)?$`, target + fmt.Sprintf("${%d}", q), nil
	}
}

// pathOnlyRegex rewrites a regex so that none of its wildcards can match "?": "." becomes
// "[^?]", negated classes "[^…]" also exclude "?", and \S \W \D outside classes exclude it too.
func pathOnlyRegex(re string) string {
	var b strings.Builder
	inClass := false
	for i := 0; i < len(re); i++ {
		c := re[i]
		switch {
		case c == '\\' && i+1 < len(re):
			n := re[i+1]
			if !inClass && (n == 'S' || n == 'W' || n == 'D') {
				b.WriteString(`[^\` + strings.ToLower(string(n)) + `?]`)
			} else {
				b.WriteByte(c)
				b.WriteByte(n)
			}
			i++
		case inClass:
			if c == ']' {
				inClass = false
			}
			b.WriteByte(c)
		case c == '[':
			inClass = true
			b.WriteByte(c)
			if i+1 < len(re) && re[i+1] == '^' {
				b.WriteString(`^?`)
				i++
			}
			// A leading "]" is a literal member of the class.
			if i+1 < len(re) && re[i+1] == ']' {
				b.WriteByte(']')
				i++
			}
		case c == '.':
			b.WriteString(`[^?]`)
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}
