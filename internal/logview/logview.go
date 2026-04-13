package logview

import (
	"html"
	htmpl "html/template"
	"regexp"
	"strconv"
	"strings"
)

// LogLevel represents a severity tier for CSS class assignment.
type LogLevel int

const (
	LvDefault LogLevel = iota
	LvFatal
	LvError
	LvWarn
	LvInfo
	LvDebug
	LvSuccess
)

func (l LogLevel) String() string {
	switch l {
	case LvFatal:
		return "log-lvl-fatal"
	case LvError:
		return "log-lvl-error"
	case LvWarn:
		return "log-lvl-warn"
	case LvSuccess:
		return "log-lvl-success"
	case LvInfo:
		return "log-lvl-info"
	case LvDebug:
		return "log-lvl-debug"
	default:
		return "log-lvl-default"
	}
}

var (
	// ansiSeq strips ANSI escape sequences from raw docker logs (when apps emit real ones).
	ansiSeq = regexp.MustCompile(`\x1b\[[0-9;]*m`)
	// ansiOSC strips hyperlink / title OSC sequences (e.g. OSC 8) that would otherwise show as garbage.
	ansiOSC = regexp.MustCompile(`\x1b\][^\x07]{0,4096}\x07|\x1b\][^\x1b\\]{0,4096}\x1b\\`)

	// ansiSeqParse parses individual ANSI SGR sequences to extract codes.
	ansiSeqParse = regexp.MustCompile(`\x1b\[([0-9;]*)m`)

	reBracketScrape  = regexp.MustCompile(`(?i)\[Scrape\]`)
	reBracketBrowser = regexp.MustCompile(`(?i)\[Browser\]`)

	// Bracket-level patterns that apps print instead of real ANSI codes:
	//   [DEBUG] [INFO] [WARNING] [ERROR] etc.
	//   [0] [8] [9] [10] ... numeric single-digit level tags
	bracketLevel = regexp.MustCompile(`^\[(\d+|DEBUG|DBG|INFO|INFORMATION|INF|WARN|WARNING|ERR|ERROR|FATAL|PANIC|SUCCESS|OK)\](\s|$)`)

	// httpStatusCode extracts a 3-digit HTTP status code from common log formats:
	//   "GET /path HTTP/1.1" 200 …  (nginx/apache combined log, quoted method)
	//   GET /path 200 12ms          (plain text)
	//   HTTP/1.1 404 Not Found
	httpStatusCode = regexp.MustCompile(`(?:"\s+|HTTP/[\d.]+\s+|\s)([1-9]\d{2})\b`)

	// tsPattern matches Docker/nginx/common timestamp prefixes.
	tsPattern = regexp.MustCompile(`^(\[\d{2}/[A-Za-z]{3}/\d{4}:\d{2}:\d{2}:\d{2}(?: [+\-]\d{4})?\]|\[\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}:\d{2}(?:[^\]]*)?\]|\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+\-]\d{2}:\d{2})?)\s*`)
)

// ansiToHTML converts ANSI SGR escape sequences to HTML <span> elements with CSS classes.
// Preserves colors for terminal output rendering.
func ansiToHTML(raw string) string {
	var out strings.Builder
	var spanDepth int
	last := 0
	for _, m := range ansiSeqParse.FindAllStringSubmatchIndex(raw, -1) {
		// Write text before the match (escaped)
		out.WriteString(html.EscapeString(raw[last:m[0]]))
		// Parse the codes
		codes := raw[m[2]:m[3]]
		if codes == "0" || codes == "" {
			// Reset — close all open spans
			for i := 0; i < spanDepth; i++ {
				out.WriteString(`</span>`)
			}
			spanDepth = 0
		} else if cls := ansiCodesToClass(codes); cls != "" {
			out.WriteString(`<span class="`)
			out.WriteString(cls)
			out.WriteString(`">`)
			spanDepth++
		}
		last = m[1]
	}
	out.WriteString(html.EscapeString(raw[last:]))
	// Close any remaining open spans
	for i := 0; i < spanDepth; i++ {
		out.WriteString(`</span>`)
	}
	return out.String()
}

// ansiCodesToClass maps SGR codes to a CSS class name.
func ansiCodesToClass(codes string) string {
	if codes == "" || codes == "0" {
		return ""
	}
	parts := strings.Split(codes, ";")
	var classes []string
	for _, p := range parts {
		switch p {
		case "1":
			classes = append(classes, "ansi-bold")
		case "2":
			classes = append(classes, "ansi-dim")
		case "3":
			classes = append(classes, "ansi-italic")
		case "4":
			classes = append(classes, "ansi-underline")
		case "30":
			classes = append(classes, "ansi-black")
		case "31":
			classes = append(classes, "ansi-red")
		case "32":
			classes = append(classes, "ansi-green")
		case "33":
			classes = append(classes, "ansi-yellow")
		case "34":
			classes = append(classes, "ansi-blue")
		case "35":
			classes = append(classes, "ansi-magenta")
		case "36":
			classes = append(classes, "ansi-cyan")
		case "37":
			classes = append(classes, "ansi-white")
		case "90":
			classes = append(classes, "ansi-bright-black")
		case "91":
			classes = append(classes, "ansi-bright-red")
		case "92":
			classes = append(classes, "ansi-bright-green")
		case "93":
			classes = append(classes, "ansi-bright-yellow")
		case "94":
			classes = append(classes, "ansi-bright-blue")
		case "95":
			classes = append(classes, "ansi-bright-magenta")
		case "96":
			classes = append(classes, "ansi-bright-cyan")
		case "97":
			classes = append(classes, "ansi-bright-white")
		case "40":
			classes = append(classes, "ansi-bg-black")
		case "41":
			classes = append(classes, "ansi-bg-red")
		case "42":
			classes = append(classes, "ansi-bg-green")
		case "43":
			classes = append(classes, "ansi-bg-yellow")
		case "44":
			classes = append(classes, "ansi-bg-blue")
		case "45":
			classes = append(classes, "ansi-bg-magenta")
		case "46":
			classes = append(classes, "ansi-bg-cyan")
		case "47":
			classes = append(classes, "ansi-bg-white")
		}
	}
	if len(classes) == 0 {
		return ""
	}
	return strings.Join(classes, " ")
}

// wrapLogKeywordHints wraps common bracket-tagged prefixes for CSS coloring (s is HTML-escaped).
func wrapLogKeywordHints(s string) string {
	if s == "" {
		return s
	}
	s = reBracketScrape.ReplaceAllString(s, `<span class="log-kw-scrape">$0</span>`)
	s = reBracketBrowser.ReplaceAllString(s, `<span class="log-kw-browser">$0</span>`)
	return s
}

// FormatDockerLog turns raw docker log text into colored HTML divs.
// Returns htmpl.HTML so Go templates render it as raw HTML (no extra escaping).
func FormatDockerLog(raw string) htmpl.HTML {
	raw = ansiSeq.ReplaceAllString(raw, "")
	raw = ansiOSC.ReplaceAllString(raw, "")
	raw = strings.ReplaceAll(raw, "\r\n", "\n")

	var out strings.Builder
	lines := strings.Split(raw, "\n")
	if len(lines) == 1 && lines[0] == "" {
		return htmpl.HTML(`<div class="log-line log-lvl-default">(empty)</div>`)
	}

	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			out.WriteString(`<div class="log-line-spacer"></div>`)
			continue
		}
		level := classify(line)
		esc := html.EscapeString(line)
		esc = wrapLogKeywordHints(esc)
		out.WriteString(`<div class="log-line `)
		out.WriteString(level.String())
		out.WriteString(`">`)
		out.WriteString(levelBadge(level))
		out.WriteString(wrapTimestamp(esc))
		out.WriteString(`</div>`)
	}
	return htmpl.HTML(out.String())
}

// FormatTerminalOutput converts ANSI terminal output to HTML with color spans.
// Used for docker exec results where ANSI colors should be preserved.
func FormatTerminalOutput(raw string) htmpl.HTML {
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	lines := strings.Split(raw, "\n")
	var out strings.Builder
	for _, line := range lines {
		out.WriteString(`<div class="log-line log-lvl-default">`)
		out.WriteString(ansiToHTML(line))
		out.WriteString(`</div>`)
	}
	return htmpl.HTML(out.String())
}

// wrapTimestamp wraps a detected timestamp in a span for toggle visibility.
func wrapTimestamp(esc string) string {
	if m := tsPattern.FindStringIndex(esc); m != nil {
		return `<span class="log-ts">` + esc[m[0]:m[1]] + `</span>` + esc[m[1]:]
	}
	return esc
}

func levelBadge(l LogLevel) string {
	switch l {
	case LvFatal:
		return `<span class="log-badge log-badge-fatal">FATAL</span> `
	case LvError:
		return `<span class="log-badge log-badge-error">ERROR</span> `
	case LvWarn:
		return `<span class="log-badge log-badge-warn">WARN</span> `
	case LvSuccess:
		return `<span class="log-badge log-badge-success">OK</span> `
	case LvInfo:
		return `<span class="log-badge log-badge-info">INFO</span> `
	case LvDebug:
		return `<span class="log-badge log-badge-debug">DBG</span> `
	default:
		return ""
	}
}

// classify returns the LogLevel for a single log line by checking
// multiple patterns in order of specificity.
func classify(line string) LogLevel {
	low := strings.ToLower(line)

	// HTTP status code detection — parse the actual integer to avoid false positives
	// like "version 4", "port 4000", etc.
	if m := httpStatusCode.FindStringSubmatch(line); m != nil {
		if code, err := strconv.Atoi(m[1]); err == nil {
			switch {
			case code >= 500:
				return LvError
			case code >= 400:
				return LvWarn
			case code >= 200 && code < 400:
				return LvSuccess
			}
		}
	}

	// Bracketed level prefix
	if m := bracketLevel.FindStringSubmatch(line); m != nil {
		switch strings.ToUpper(m[1]) {
		case "FATAL", "PANIC":
			return LvFatal
		case "ERROR", "ERR":
			return LvError
		case "WARN", "WARNING":
			return LvWarn
		case "OK", "SUCCESS":
			return LvSuccess
		case "INFO", "INFORMATION", "INF":
			return LvInfo
		case "DEBUG", "DBG":
			return LvDebug
		}
	}

	// Numeric single-digit level tags that uvicorn emits:
	// 0=DEBUG, 1=INFO, 2=WARNING, 3=ERROR, 4=CRITICAL,
	// 5=WARNING, 6=INFO (gunicorn), 7=DEBUG, 8=INFO (uvicorn), 9=NOTICE
	if len(line) >= 3 && line[0] == '[' {
		end := strings.IndexByte(line[1:], ']')
		if end > 0 {
			switch line[1 : end+1] {
			case "0", "1":
				return LvDebug
			case "2":
				return LvInfo
			case "3":
				return LvWarn
			case "4", "5", "7":
				return LvError
			case "6", "8", "9":
				return LvInfo
			}
		}
	}

	// Keyword matching
	if strings.Contains(low, "fatal") || strings.Contains(low, "panic") {
		return LvFatal
	}
	// Shell/OCI failures often start with "exec "; require a following space-boundary
	// so substrings inside words (e.g. "executing") are not matched.
	if strings.HasPrefix(low, "exec ") ||
		strings.Contains(low, " exec ") ||
		strings.Contains(low, "no such file or directory") ||
		strings.Contains(low, "error") ||
		strings.Contains(low, "err:") ||
		strings.Contains(low, " failed") ||
		strings.Contains(low, "exception") ||
		strings.Contains(low, "traceback") ||
		strings.Contains(low, "denied") ||
		strings.Contains(low, "econnrefused") ||
		strings.Contains(low, "errno ") {
		return LvError
	}
	if strings.Contains(low, "warn") || strings.Contains(low, "deprecated") {
		return LvWarn
	}
	if strings.Contains(low, " listening ") ||
		strings.Contains(low, "listening on") ||
		strings.Contains(low, "started server") ||
		strings.Contains(low, " ready ") ||
		strings.Contains(low, "booting worker") ||
		strings.Contains(low, "application startup complete") ||
		strings.Contains(low, "spawned") {
		return LvSuccess
	}
	if strings.Contains(low, "debug") {
		return LvDebug
	}
	if strings.Contains(low, "info") || strings.Contains(low, "notice") {
		return LvInfo
	}
	return LvDefault
}
