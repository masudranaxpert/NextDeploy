package caddy

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"panel/internal/proxy"
)

const (
	FallbackDefault   = proxy.FallbackDefault
	FallbackStrict404 = proxy.FallbackStrict404
	FallbackCustom    = proxy.FallbackCustom
)

// RoutingRule represents one ordered per-site URL rewrite, redirect, or path-block rule.
type RoutingRule = proxy.RoutingRule

// SiteRouting stores a PHP or static site's custom fallback strategy and ordered routing rules.
type SiteRouting = proxy.SiteRouting

var (
	captureRefRe         = regexp.MustCompile(`\$([0-9])`)
	allowedRedirectCodes = map[int]bool{301: true, 302: true, 307: true, 308: true}
	allowedBlockCodes    = map[int]bool{403: true, 404: true, 410: true}
)

// ValidateRouting normalizes and validates a site's fallback mode and routing rules.
func ValidateRouting(r *SiteRouting, isStatic bool) error {
	return proxy.ValidateRouting(r, isStatic)
}

// MergeUniqueRoutingRules appends incoming rules while deduplicating by kind, match type, pattern, and target.
func MergeUniqueRoutingRules(existing, incoming []RoutingRule) []RoutingRule {
	return proxy.MergeUniqueRoutingRules(existing, incoming)
}

func routingEnabledCount(r SiteRouting) int {
	n := 0
	for _, rule := range r.Rules {
		if rule.Enabled {
			n++
		}
	}
	return n
}

func appendRoutingLabels(r SiteRouting, labels map[string]string, prefix string, _ bool) bool {
	if routingEnabledCount(r) == 0 {
		return false
	}
	idx := 0
	for _, rule := range r.Rules {
		if !rule.Enabled {
			continue
		}
		if rule.Pattern == "" || proxy.HasUnsafeLabelChars(rule.Pattern) {
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
