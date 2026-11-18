package ferron

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"panel/internal/proxy"
)

type preparedPathRoute struct {
	route      proxy.PathRoute
	matcher    string
	regex      string
	hideMatch  string
	hideRegex  string
	staticRoot string
}

func (d *Driver) renderAppProxySite(b *strings.Builder, slug string, s proxy.Site, acmeEmail string) error {
	hostLine, err := formatHostsHeader(s)
	if err != nil {
		return err
	}

	routes := sortedPathRoutes(s.Paths)
	prepared, err := preparePathMatchers(slug, routes)
	if err != nil {
		return err
	}
	for _, p := range prepared {
		if p.hideMatch != "" {
			fmt.Fprintf(b, "match %s {\n", p.hideMatch)
			fmt.Fprintf(b, "    request.uri.path ~ %s\n", rawString(p.regex))
			fmt.Fprintf(b, "    request.uri.path ~ %s\n", rawString(p.hideRegex))
			b.WriteString("}\n\n")
		}
		fmt.Fprintf(b, "match %s {\n", p.matcher)
		fmt.Fprintf(b, "    request.uri.path ~ %s\n", rawString(p.regex))
		for _, prev := range prepared {
			if prev.matcher == p.matcher {
				break
			}
			fmt.Fprintf(b, "    request.uri.path !~ %s\n", rawString(prev.regex))
		}
		b.WriteString("}\n\n")
	}

	fallbackMatcher := ""
	if len(prepared) > 0 {
		fallbackMatcher = fmt.Sprintf("nd_%s_default", slug)
		alts := make([]string, 0, len(prepared))
		for _, p := range prepared {
			alts = append(alts, "(?:"+p.regex+")")
		}
		fmt.Fprintf(b, "match %s {\n", fallbackMatcher)
		fmt.Fprintf(b, "    request.uri.path !~ %s\n", rawString(strings.Join(alts, "|")))
		b.WriteString("}\n\n")
	}

	fmt.Fprintf(b, "%s {\n", hostLine)
	if err := writeTLSBlock(b, s, d, acmeEmail); err != nil {
		return err
	}
	writeHeaderOps(b, "    ", s.Headers)

	for _, p := range prepared {
		if p.hideMatch != "" {
			fmt.Fprintf(b, "    if %s {\n", p.hideMatch)
			b.WriteString("        status 404\n")
			b.WriteString("    }\n")
		}
		fmt.Fprintf(b, "    if %s {\n", p.matcher)
		if p.route.Handler.Kind == proxy.HandlerStaticDir && p.route.Handler.Static != nil {
			writeStaticPathHandler(b, "        ", p)
		} else if p.route.Handler.Upstream != nil {
			if p.route.StripPrefix {
				stripRe := "^" + regexp.QuoteMeta(normalizePrefix(p.route.Prefix)) + "/?"
				fmt.Fprintf(b, "        rewrite %s \"/\" {\n            last\n        }\n", rawString(stripRe))
			}
			if err := writeProxyBlock(b, "        ", s, *p.route.Handler.Upstream); err != nil {
				return err
			}
		}
		b.WriteString("    }\n")
	}

	if len(prepared) > 0 {
		fmt.Fprintf(b, "    if %s {\n", fallbackMatcher)
		if err := writeSiteDefaultHandler(b, "        ", s); err != nil {
			return err
		}
		b.WriteString("    }\n")
	} else {
		if err := writeSiteDefaultHandler(b, "    ", s); err != nil {
			return err
		}
	}

	b.WriteString("}\n")
	return nil
}

func (d *Driver) renderRedirectSite(b *strings.Builder, _ string, s proxy.Site, acmeEmail string) error {
	hostLine, err := formatHostsHeader(s)
	if err != nil {
		return err
	}
	redir := s.Redirect
	if redir == nil && s.Default != nil {
		redir = s.Default.Redirect
	}
	if redir == nil || strings.TrimSpace(redir.Target) == "" {
		return fmt.Errorf("ferron: redirect site %q missing target", s.ID)
	}
	code := redir.Code
	if code < 300 || code > 399 {
		code = 302
	}
	target := strings.TrimSpace(redir.Target)
	location := target
	if redir.KeepPath {
		location = strings.TrimRight(target, "/") + "{{request.uri}}"
	}

	fmt.Fprintf(b, "%s {\n", hostLine)
	if err := writeTLSBlock(b, s, d, acmeEmail); err != nil {
		return err
	}
	writeHeaderOps(b, "    ", s.Headers)
	fmt.Fprintf(b, "    status %d {\n", code)
	fmt.Fprintf(b, "        location %s\n", quoteString(location))
	b.WriteString("    }\n")
	b.WriteString("}\n")
	return nil
}

func writeSiteDefaultHandler(b *strings.Builder, indent string, s proxy.Site) error {
	if s.Default != nil {
		if s.Default.Kind == proxy.HandlerStaticDir && s.Default.Static != nil {
			writeStaticDir(b, indent, s.Default.Static.Root, len(s.Default.Static.Precompressed) > 0)
			return nil
		}
		if s.Default.Kind == proxy.HandlerRedirect && s.Default.Redirect != nil {
			code := s.Default.Redirect.Code
			if code < 300 || code > 399 {
				code = 302
			}
			location := strings.TrimSpace(s.Default.Redirect.Target)
			if s.Default.Redirect.KeepPath {
				location = strings.TrimRight(location, "/") + "{{request.uri}}"
			}
			fmt.Fprintf(b, "%sstatus %d {\n%s    location %s\n%s}\n", indent, code, indent, quoteString(location), indent)
			return nil
		}
	}
	up := resolveSiteUpstream(s)
	if up == nil {
		return fmt.Errorf("ferron: site %q has no upstream", s.ID)
	}
	return writeProxyBlock(b, indent, s, *up)
}

func writeStaticPathHandler(b *strings.Builder, indent string, p preparedPathRoute) {
	pre := p.route.Handler.Static != nil && len(p.route.Handler.Static.Precompressed) > 0
	writeStaticDir(b, indent, p.staticRoot, pre)
	if p.route.StripPrefix {
		stripRe := "^" + regexp.QuoteMeta(normalizePrefix(p.route.Prefix)) + "/?"
		fmt.Fprintf(b, "%srewrite %s \"/\" {\n%s    last\n%s}\n", indent, rawString(stripRe), indent, indent)
	}
}

// defaultStaticHidePatterns mirrors the panel's default hide list for static routes.
var defaultStaticHidePatterns = []string{".git", ".env*", "*.sql", "*.db", "*.sqlite", "*.key", "*.sock"}

func writeStaticDir(b *strings.Builder, indent, rootDir string, precompressed bool) {
	fmt.Fprintf(b, "%sroot %s\n", indent, quoteString(rootDir))
	fmt.Fprintf(b, "%sdisable_symlinks false\n", indent)
	fmt.Fprintf(b, "%scompressed true\n", indent)
	if precompressed {
		fmt.Fprintf(b, "%sprecompressed true\n", indent)
	}
}

func preparePathMatchers(slug string, routes []proxy.PathRoute) ([]preparedPathRoute, error) {
	if len(routes) == 0 {
		return nil, nil
	}
	out := make([]preparedPathRoute, 0, len(routes))
	for i, r := range routes {
		p := preparedPathRoute{
			route:   r,
			matcher: fmt.Sprintf("nd_%s_p%d", slug, i),
			regex:   prefixMatchRegex(r.Prefix),
		}
		if r.Handler.Kind == proxy.HandlerStaticDir && r.Handler.Static != nil {
			p.staticRoot = strings.TrimSpace(r.Handler.Static.Root)
			if p.staticRoot == "" {
				return nil, fmt.Errorf("ferron: static path %q missing root", r.Prefix)
			}
			hide := r.Handler.Static.Hide
			if len(hide) == 0 {
				// Same as Caddy (defaultStaticHide) and Angie (snippets/hide.conf): a static path
				// never serves .env, .git, dumps or keys, even when no patterns were set.
				hide = defaultStaticHidePatterns
			}
			if hideRe := buildHideRegex(hide, false); hideRe != "" {
				p.hideMatch = fmt.Sprintf("nd_%s_p%d_hide", slug, i)
				p.hideRegex = hideRe
			}
		}
		out = append(out, p)
	}
	return out, nil
}

func sortedPathRoutes(in []proxy.PathRoute) []proxy.PathRoute {
	if len(in) == 0 {
		return nil
	}
	out := make([]proxy.PathRoute, len(in))
	copy(out, in)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Priority != out[j].Priority {
			return out[i].Priority < out[j].Priority
		}
		if len(out[i].Prefix) != len(out[j].Prefix) {
			return len(out[i].Prefix) > len(out[j].Prefix)
		}
		if out[i].Prefix != out[j].Prefix {
			return out[i].Prefix < out[j].Prefix
		}
		return out[i].Handler.Kind < out[j].Handler.Kind
	})
	return out
}
