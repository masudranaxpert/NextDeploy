package htaccess

import (
	"fmt"
	"path"
	"regexp"
	"strconv"
	"strings"

	"panel/internal/caddy"
)

var (
	pcreLookaroundRe  = regexp.MustCompile(`\(\?[=!<]|\\[1-9]`)
	simpleLiteralRe   = regexp.MustCompile(`^\^?/[A-Za-z0-9._/-]+\$?$`)
	wpFrontTargetRe   = regexp.MustCompile(`^/?(index\.php)?$`)
	redirectCodeRe    = regexp.MustCompile(`^30[1278]$`)
	filesBlockStartRe = regexp.MustCompile(`(?i)^<\s*(filesmatch|files)\s+(~\s+)?(.+?)\s*>$`)
	ifModuleStartRe   = regexp.MustCompile(`(?i)^<\s*ifmodule\s+(.+?)\s*>$`)
	catchAllPhpPatRe  = regexp.MustCompile(`^((?:\(\?i\))?\^.*/)\(\.(?:\*|\+)\)(?:/\?)?\$$`)
)

type rewriteCond struct {
	test    string
	pattern string
	isOr    bool
}

type blockFrame struct {
	kind       string // "ifmodule" | "files"
	inactive   bool
	filePat    string
	isRegex    bool
	denyInside bool
}

// Parse converts a single .htaccess file into Caddy routing rules.
// fileRelPath is the path relative to document root (e.g. ".htaccess" or "api/.htaccess").
func (c *Converter) Parse(content, fileRelPath string) Result {
	res := Result{
		Rules:        make([]caddy.RoutingRule, 0),
		FilesScanned: 1,
		Diagnostics:  make([]Diagnostic, 0),
	}
	if fileRelPath == "" {
		fileRelPath = ".htaccess"
	}
	res.Files = []string{fileRelPath}
	basePrefix := subfolderPrefix(fileRelPath)
	rewriteBase := basePrefix

	lines := joinLineContinuations(content)
	var conds []rewriteCond
	var stack []blockFrame

	inInactiveBlock := func() bool {
		for _, f := range stack {
			if f.inactive {
				return true
			}
		}
		return false
	}

	addDiag := func(lineNo int, sev Severity, dir, msg string) {
		res.Diagnostics = append(res.Diagnostics, Diagnostic{
			File:      fileRelPath,
			Line:      lineNo,
			Severity:  sev,
			Directive: dir,
			Message:   msg,
		})
	}

	addRule := func(lineNo int, dir string, rule caddy.RoutingRule) {
		if len(res.Rules) >= c.maxRules {
			addDiag(lineNo, SeverityWarning, dir, fmt.Sprintf("skipped rule because max rules limit (%d) was reached", c.maxRules))
			return
		}
		probe := caddy.SiteRouting{Rules: []caddy.RoutingRule{rule}}
		if err := caddy.ValidateRouting(&probe, false); err != nil {
			addDiag(lineNo, SeverityWarning, dir, fmt.Sprintf("could not convert cleanly (%v)", err))
			return
		}
		res.Rules = append(res.Rules, probe.Rules[0])
		addDiag(lineNo, SeverityConverted, dir, fmt.Sprintf("converted to %s (%s %s)", probe.Rules[0].Kind, probe.Rules[0].MatchType, probe.Rules[0].Pattern))
	}

	for _, entry := range lines {
		lineNo := entry.lineNo
		raw := strings.TrimSpace(entry.text)
		if raw == "" || strings.HasPrefix(raw, "#") {
			continue
		}

		low := strings.ToLower(raw)
		if strings.HasPrefix(low, "</") {
			if len(stack) > 0 {
				top := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				if top.kind == "files" && top.denyInside && !inInactiveBlock() {
					pat := top.filePat
					mt := "regex"
					if !top.isRegex {
						pat = globToRegex(pat)
					}
					pat = scopeFileRegex(basePrefix, pat)
					addRule(lineNo, "<Files>", caddy.RoutingRule{
						ID:        fmt.Sprintf("ht-%d", len(res.Rules)+1),
						Kind:      "block",
						MatchType: mt,
						Pattern:   pat,
						Status:    403,
						Enabled:   true,
						Note:      fmt.Sprintf("Imported from %s", fileRelPath),
					})
				}
			}
			conds = nil
			continue
		}

		if m := ifModuleStartRe.FindStringSubmatch(raw); m != nil {
			mod := strings.TrimSpace(m[1])
			stack = append(stack, blockFrame{
				kind:     "ifmodule",
				inactive: strings.HasPrefix(mod, "!"),
			})
			continue
		}

		if m := filesBlockStartRe.FindStringSubmatch(raw); m != nil {
			isRe := strings.EqualFold(m[1], "filesmatch") || strings.TrimSpace(m[2]) == "~"
			pat := unquoteApacheToken(strings.TrimSpace(m[3]))
			stack = append(stack, blockFrame{
				kind:    "files",
				filePat: pat,
				isRegex: isRe,
			})
			continue
		}

		if strings.HasPrefix(raw, "<") {
			// Skip unsupported block containers like <Limit>, <IfDefine>, <Directory> without crashing.
			stack = append(stack, blockFrame{kind: "other", inactive: true})
			addDiag(lineNo, SeverityWarning, firstWord(raw), "container directive skipped; configure equivalent behavior in NextDeploy Security/Settings")
			continue
		}

		if inInactiveBlock() {
			continue
		}

		args := splitApacheArgs(raw)
		if len(args) == 0 {
			continue
		}
		dir := strings.ToLower(args[0])

		// Track Deny / Require all denied inside <Files> / <FilesMatch>
		if len(stack) > 0 && stack[len(stack)-1].kind == "files" {
			if (dir == "deny" && len(args) >= 3 && strings.EqualFold(args[1], "from") && strings.EqualFold(args[2], "all")) ||
				(dir == "require" && len(args) >= 3 && strings.EqualFold(args[1], "all") && strings.EqualFold(args[2], "denied")) ||
				(dir == "order" && strings.Contains(low, "deny")) {
				stack[len(stack)-1].denyInside = true
				continue
			}
		}

		switch dir {
		case "rewriteengine", "rewriteoptions":
			conds = nil
			continue

		case "rewritebase":
			if len(args) >= 2 {
				rewriteBase = normalizeBasePrefix(args[1])
			}
			conds = nil

		case "rewritecond":
			if len(args) >= 3 {
				isOr := false
				if len(args) >= 4 && strings.Contains(strings.ToUpper(args[3]), "OR") {
					isOr = true
				}
				conds = append(conds, rewriteCond{
					test:    args[1],
					pattern: args[2],
					isOr:    isOr,
				})
			}

		case "rewriterule":
			rule, note, sev := c.convertRewriteRule(args, conds, rewriteBase, basePrefix, fileRelPath)
			conds = nil
			if rule != nil {
				addRule(lineNo, "RewriteRule", *rule)
				if note != "" {
					addDiag(lineNo, SeverityWarning, "RewriteRule", note)
				}
			} else if note != "" {
				addDiag(lineNo, sev, "RewriteRule", note)
			}

		case "redirect", "redirectpermanent", "redirecttemp":
			conds = nil
			rule, note := convertRedirectDirective(dir, args, basePrefix, fileRelPath)
			if rule != nil {
				addRule(lineNo, args[0], *rule)
			} else if note != "" {
				addDiag(lineNo, SeverityWarning, args[0], note)
			}

		case "redirectmatch":
			conds = nil
			rule, note := convertRedirectMatchDirective(args, basePrefix, fileRelPath)
			if rule != nil {
				addRule(lineNo, args[0], *rule)
			} else if note != "" {
				addDiag(lineNo, SeverityWarning, args[0], note)
			}

		case "directoryindex":
			conds = nil
			if len(args) >= 2 && basePrefix == "" {
				entryFile := strings.TrimPrefix(strings.TrimSpace(args[1]), "/")
				if entryFile != "" && entryFile != "index.php" && entryFile != "index.html" && !strings.Contains(entryFile, "/") {
					res.FallbackMode = caddy.FallbackCustom
					res.FallbackEntry = entryFile
					addDiag(lineNo, SeverityConverted, "DirectoryIndex", fmt.Sprintf("set custom fallback entry to %q", entryFile))
				}
			}

		case "errordocument":
			conds = nil
			if len(args) >= 3 {
				addDiag(lineNo, SeverityInfo, "ErrorDocument", fmt.Sprintf("ErrorDocument %s %s noted; for non-front-controller 404s use Fallback Mode = strict_404", args[1], args[2]))
			}

		case "php_value", "php_flag", "php_admin_value", "php_admin_flag":
			conds = nil
			addDiag(lineNo, SeverityWarning, args[0], "ignored: configure PHP limits and ini directives in the NextDeploy PHP Settings tab")

		case "header", "expiresactive", "expiresbytype", "addoutputfilterbytype", "options", "deny", "allow", "require", "authtype", "authname", "authuserfile":
			conds = nil
			addDiag(lineNo, SeverityWarning, args[0], "Apache server/security directive ignored; configure equivalents in NextDeploy Site Security or Caddy")

		default:
			conds = nil
		}
	}

	res.finalizeSummary()
	return res
}

func (c *Converter) convertRewriteRule(
	args []string,
	conds []rewriteCond,
	rewriteBase string,
	basePrefix string,
	fileRelPath string,
) (*caddy.RoutingRule, string, Severity) {
	if len(args) < 3 {
		return nil, "RewriteRule requires pattern and substitution", SeverityWarning
	}
	rawPat := args[1]
	rawSub := args[2]
	flags := ""
	if len(args) >= 4 {
		flags = args[3]
	}

	// Skip HTTPS enforcement rules (Caddy handles TLS/HTTPS automatically).
	if isHTTPSRedirectCond(conds) {
		return nil, "skipped HTTPS redirect rule (NextDeploy Caddy manages HTTPS automatically)", SeverityInfo
	}

	// Skip standard WordPress/Laravel front-controller rules (php_fastcgi handles index.php fallback).
	if isStandardFrontControllerRule(rawPat, rawSub, conds) {
		return nil, "skipped standard index.php front-controller rule (handled automatically by NextDeploy php_fastcgi)", SeverityInfo
	}

	if pcreLookaroundRe.MatchString(rawPat) {
		return nil, fmt.Sprintf("pattern %q uses PCRE lookaround or backreference unsupported by Go RE2; rewrite manually if needed", rawPat), SeverityWarning
	}

	upperFlags := parseRewriteFlags(flags)
	if upperFlags["P"] {
		return nil, "proxy flag [P] is not supported in per-site rules; use Caddy Routes & Redirects", SeverityWarning
	}

	// Build anchored Go regex / exact path scoped to directory prefix.
	effectiveBase := basePrefix
	if rewriteBase != "" {
		effectiveBase = rewriteBase
	}
	pattern, matchType := buildScopedPattern(effectiveBase, rawPat, upperFlags["NC"])

	unlessFile, requireTargetPHP, condNotes := analyzeRewriteConds(conds)
	warnNote := ""
	if len(condNotes) > 0 {
		warnNote = fmt.Sprintf("imported without Apache condition (%s); verify if rule should only match conditionally", strings.Join(condNotes, "; "))
	}

	// Handle block flags [F], [G], or [R=403|404|410]
	if upperFlags["F"] {
		return &caddy.RoutingRule{
			Kind:      "block",
			MatchType: matchType,
			Pattern:   pattern,
			Status:    403,
			Enabled:   true,
			Note:      "Imported from " + fileRelPath,
		}, warnNote, SeverityConverted
	}
	if upperFlags["G"] {
		return &caddy.RoutingRule{
			Kind:      "block",
			MatchType: matchType,
			Pattern:   pattern,
			Status:    410,
			Enabled:   true,
			Note:      "Imported from " + fileRelPath,
		}, warnNote, SeverityConverted
	}
	if code, hasR := parseRedirectFlag(flags); hasR {
		if code == 403 || code == 404 || code == 410 {
			return &caddy.RoutingRule{
				Kind:      "block",
				MatchType: matchType,
				Pattern:   pattern,
				Status:    code,
				Enabled:   true,
				Note:      "Imported from " + fileRelPath,
			}, warnNote, SeverityConverted
		}
		if rawSub == "-" {
			return nil, "redirect rule with '-' target cannot be converted", SeverityWarning
		}
		target := scopeTarget(effectiveBase, rawSub, true)
		return &caddy.RoutingRule{
			Kind:          "redir",
			MatchType:     matchType,
			Pattern:       pattern,
			Target:        target,
			Status:        code,
			Enabled:       true,
			PreserveQuery: upperFlags["QSA"],
			Note:          "Imported from " + fileRelPath,
		}, warnNote, SeverityConverted
	}

	if rawSub == "-" {
		return nil, "pass-through RewriteRule '-' skipped", SeverityInfo
	}

	target := scopeTarget(effectiveBase, rawSub, false)
	if upperFlags["QSD"] && !strings.Contains(target, "?") {
		target += "?"
	}
	if matchType == "regex" {
		tightPat, tightened := tightenExtensionlessPhpPattern(pattern, target)
		if tightened {
			pattern = tightPat
			unlessFile = true
		}
	}
	return &caddy.RoutingRule{
		Kind:              "rewrite",
		MatchType:         matchType,
		Pattern:           pattern,
		Target:            target,
		Enabled:           true,
		UnlessFile:        unlessFile,
		RequireTargetFile: requireTargetPHP,
		PreserveQuery:     upperFlags["QSA"],
		Note:              "Imported from " + fileRelPath,
	}, warnNote, SeverityConverted
}

func analyzeRewriteConds(conds []rewriteCond) (unlessFile, requireTargetPHP bool, unhandled []string) {
	for _, cd := range conds {
		upTest := strings.ToUpper(strings.TrimSpace(cd.test))
		lowPat := strings.ToLower(strings.TrimSpace(cd.pattern))
		switch {
		case upTest == "%{REQUEST_FILENAME}" && (lowPat == "!-f" || lowPat == "!-d" || lowPat == "!-l" || lowPat == "!-s"):
			unlessFile = true
		case (upTest == "%{REQUEST_FILENAME}.PHP" || upTest == "%{REQUEST_FILENAME}\\.PHP") && (lowPat == "-f" || lowPat == "-s"):
			requireTargetPHP = true
			unlessFile = true
		case (upTest == "%{REQUEST_URI}" || upTest == "%{THE_REQUEST}" || upTest == "%{REQUEST_FILENAME}") &&
			(strings.Contains(lowPat, "!.php") || strings.Contains(lowPat, "!\\.php")):
			unlessFile = true
		case strings.Contains(upTest, "REQUEST_FILENAME") && (lowPat == "!-f" || lowPat == "!-d" || lowPat == "!-l"):
			unlessFile = true
		default:
			unhandled = append(unhandled, fmt.Sprintf("RewriteCond %s %s", cd.test, cd.pattern))
		}
	}
	return unlessFile, requireTargetPHP, unhandled
}

// tightenExtensionlessPhpPattern narrows catch-all ^/(.+)$ or ^/(.*)$ -> /$1.php rules
// so requests with file extensions (such as /api_r2.php or .css/.js) or bare "/" are not rewritten to /*.php.
func tightenExtensionlessPhpPattern(pattern, target string) (string, bool) {
	targetPath := strings.ToLower(strings.SplitN(target, "?", 2)[0])
	if !strings.HasSuffix(targetPath, "$1.php") {
		return pattern, false
	}
	m := catchAllPhpPatRe.FindStringSubmatch(pattern)
	if m == nil {
		return pattern, false
	}
	return m[1] + "([^.]+)$", true
}

func convertRedirectDirective(dir string, args []string, basePrefix, fileRelPath string) (*caddy.RoutingRule, string) {
	status := 302
	idx := 1
	switch dir {
	case "redirectpermanent":
		status = 301
	case "redirecttemp":
		status = 302
	case "redirect":
		if len(args) >= 2 {
			tok := strings.ToLower(args[1])
			switch tok {
			case "permanent":
				status = 301
				idx = 2
			case "temp":
				status = 302
				idx = 2
			case "seeother":
				status = 303
				idx = 2
			case "gone":
				if len(args) >= 3 {
					pat := scopeLiteralPath(basePrefix, args[2])
					return &caddy.RoutingRule{
						Kind:      "block",
						MatchType: "exact",
						Pattern:   pat,
						Status:    410,
						Enabled:   true,
						Note:      "Imported from " + fileRelPath,
					}, ""
				}
			default:
				if redirectCodeRe.MatchString(tok) {
					status, _ = strconv.Atoi(tok)
					idx = 2
				}
			}
		}
	}
	if len(args) <= idx+1 {
		return nil, "Redirect directive missing source or destination"
	}
	src := scopeLiteralPath(basePrefix, args[idx])
	dst := scopeTarget(basePrefix, args[idx+1], true)
	if status != 301 && status != 302 && status != 307 && status != 308 {
		status = 302
	}
	return &caddy.RoutingRule{
		Kind:      "redir",
		MatchType: "prefix",
		Pattern:   src,
		Target:    dst,
		Status:    status,
		Enabled:   true,
		Note:      "Imported from " + fileRelPath,
	}, ""
}

func convertRedirectMatchDirective(args []string, basePrefix, fileRelPath string) (*caddy.RoutingRule, string) {
	status := 302
	idx := 1
	if len(args) >= 2 {
		tok := strings.ToLower(args[1])
		switch tok {
		case "permanent":
			status = 301
			idx = 2
		case "temp":
			status = 302
			idx = 2
		default:
			if redirectCodeRe.MatchString(tok) {
				status, _ = strconv.Atoi(tok)
				idx = 2
			}
		}
	}
	if len(args) <= idx+1 {
		return nil, "RedirectMatch missing pattern or destination"
	}
	pat, _ := buildScopedPattern(basePrefix, args[idx], false)
	dst := scopeTarget(basePrefix, args[idx+1], true)
	return &caddy.RoutingRule{
		Kind:      "redir",
		MatchType: "regex",
		Pattern:   pat,
		Target:    dst,
		Status:    status,
		Enabled:   true,
		Note:      "Imported from " + fileRelPath,
	}, ""
}

func buildScopedPattern(basePrefix, rawPat string, nocase bool) (string, string) {
	pat := strings.TrimSpace(rawPat)
	if simpleLiteralRe.MatchString(pat) && !nocase && strings.HasPrefix(strings.TrimPrefix(pat, "^"), "/") {
		lit := strings.TrimSuffix(strings.TrimPrefix(pat, "^"), "$")
		return scopeLiteralPath(basePrefix, lit), "exact"
	}

	anchoredStart := strings.HasPrefix(pat, "^")
	inner := strings.TrimPrefix(pat, "^")
	inner = strings.TrimPrefix(inner, "/")

	prefixRegex := ""
	if basePrefix != "" {
		prefixRegex = regexp.QuoteMeta(strings.TrimSuffix(basePrefix, "/")) + "/"
	} else {
		prefixRegex = "/"
	}

	var b strings.Builder
	if nocase {
		b.WriteString("(?i)")
	}
	if anchoredStart {
		b.WriteByte('^')
		b.WriteString(prefixRegex)
		b.WriteString(inner)
	} else {
		if basePrefix != "" {
			b.WriteByte('^')
			b.WriteString(prefixRegex)
			b.WriteString(".*")
		}
		b.WriteString(inner)
	}
	return b.String(), "regex"
}

func scopeFileRegex(basePrefix, fileRegex string) string {
	if basePrefix == "" {
		return fileRegex
	}
	clean := strings.TrimPrefix(fileRegex, "^")
	clean = strings.TrimPrefix(clean, "/")
	return "^" + regexp.QuoteMeta(strings.TrimSuffix(basePrefix, "/")) + "/(.*/)?" + clean
}

func globToRegex(glob string) string {
	var b strings.Builder
	b.WriteByte('^')
	for _, r := range glob {
		switch r {
		case '*':
			b.WriteString("[^/]*")
		case '?':
			b.WriteString("[^/]")
		default:
			b.WriteString(regexp.QuoteMeta(string(r)))
		}
	}
	b.WriteByte('$')
	return b.String()
}

func scopeTarget(basePrefix, rawTarget string, allowExternal bool) string {
	t := strings.TrimSpace(rawTarget)
	low := strings.ToLower(t)
	if allowExternal && (strings.HasPrefix(low, "http://") || strings.HasPrefix(low, "https://")) {
		return t
	}
	if strings.HasPrefix(t, "/") {
		return t
	}
	if basePrefix == "" {
		return "/" + t
	}
	return strings.TrimSuffix(basePrefix, "/") + "/" + t
}

func scopeLiteralPath(basePrefix, p string) string {
	p = strings.TrimSpace(p)
	if strings.HasPrefix(p, "/") {
		return p
	}
	if basePrefix == "" {
		return "/" + p
	}
	return strings.TrimSuffix(basePrefix, "/") + "/" + p
}

func subfolderPrefix(relPath string) string {
	dir := path.Dir(strings.ReplaceAll(relPath, "\\", "/"))
	dir = strings.TrimPrefix(path.Clean("/"+dir), "/")
	if dir == "" || dir == "." {
		return ""
	}
	return "/" + dir
}

func normalizeBasePrefix(b string) string {
	b = strings.TrimSpace(b)
	if b == "" || b == "/" {
		return ""
	}
	return "/" + strings.Trim(b, "/")
}

func isHTTPSRedirectCond(conds []rewriteCond) bool {
	for _, c := range conds {
		if strings.EqualFold(c.test, "%{HTTPS}") && (strings.EqualFold(c.pattern, "off") || c.pattern == "!=on") {
			return true
		}
	}
	return false
}

func isStandardFrontControllerRule(pat, sub string, conds []rewriteCond) bool {
	if sub == "-" && (pat == `^index\.php$` || pat == `index\.php` || pat == "^index.php$") {
		return true
	}
	hasMissingFileGuard := false
	for _, c := range conds {
		if c.test == "%{REQUEST_FILENAME}" && (c.pattern == "!-f" || c.pattern == "!-d") {
			hasMissingFileGuard = true
		}
	}
	if hasMissingFileGuard && wpFrontTargetRe.MatchString(strings.TrimSpace(sub)) {
		if pat == "." || pat == "^" || pat == ".*" || pat == "^.*$" || pat == "^(.*)$" {
			return true
		}
	}
	return false
}

func parseRewriteFlags(raw string) map[string]bool {
	out := map[string]bool{}
	trimmed := strings.Trim(strings.TrimSpace(raw), "[]")
	if trimmed == "" {
		return out
	}
	for _, part := range strings.Split(trimmed, ",") {
		part = strings.TrimSpace(part)
		key := strings.ToUpper(strings.SplitN(part, "=", 2)[0])
		if key != "" {
			out[key] = true
		}
	}
	return out
}

func parseRedirectFlag(raw string) (int, bool) {
	trimmed := strings.Trim(strings.TrimSpace(raw), "[]")
	if trimmed == "" {
		return 0, false
	}
	for _, part := range strings.Split(trimmed, ",") {
		part = strings.TrimSpace(part)
		up := strings.ToUpper(part)
		if up == "R" {
			return 302, true
		}
		if strings.HasPrefix(up, "R=") {
			val := strings.TrimPrefix(up, "R=")
			if val == "PERMANENT" {
				return 301, true
			}
			if val == "TEMP" {
				return 302, true
			}
			if n, err := strconv.Atoi(val); err == nil {
				return n, true
			}
		}
	}
	return 0, false
}

type numberedLine struct {
	lineNo int
	text   string
}

func joinLineContinuations(content string) []numberedLine {
	rawLines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	out := make([]numberedLine, 0, len(rawLines))
	var acc strings.Builder
	startNo := 0
	for idx, l := range rawLines {
		if startNo == 0 {
			startNo = idx + 1
		}
		if strings.HasSuffix(l, `\`) && !strings.HasSuffix(l, `\\`) {
			acc.WriteString(strings.TrimSuffix(l, `\`))
			acc.WriteByte(' ')
			continue
		}
		if acc.Len() > 0 {
			acc.WriteString(l)
			out = append(out, numberedLine{lineNo: startNo, text: acc.String()})
			acc.Reset()
		} else {
			out = append(out, numberedLine{lineNo: idx + 1, text: l})
		}
		startNo = 0
	}
	if acc.Len() > 0 {
		out = append(out, numberedLine{lineNo: startNo, text: acc.String()})
	}
	return out
}

func splitApacheArgs(line string) []string {
	var args []string
	var cur strings.Builder
	inQuote := rune(0)
	escaped := false

	for _, r := range line {
		if escaped {
			cur.WriteRune(r)
			escaped = false
			continue
		}
		if r == '\\' && inQuote != '\'' {
			// Preserve backslashes inside regexes when unquoted unless escaping a space or quote
			cur.WriteRune(r)
			continue
		}
		if inQuote != 0 {
			if r == inQuote {
				inQuote = 0
			} else {
				cur.WriteRune(r)
			}
			continue
		}
		if r == '"' || r == '\'' {
			inQuote = r
			continue
		}
		if r == ' ' || r == '\t' {
			if cur.Len() > 0 {
				args = append(args, cur.String())
				cur.Reset()
			}
			continue
		}
		cur.WriteRune(r)
	}
	if cur.Len() > 0 {
		args = append(args, cur.String())
	}
	return args
}

func unquoteApacheToken(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && ((s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'')) {
		return s[1 : len(s)-1]
	}
	return s
}

func firstWord(s string) string {
	if i := strings.IndexAny(s, " \t>"); i > 0 {
		return s[:i]
	}
	return s
}
