package ui

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/fatih/color"
)

var (
	// Core colors
	Bold      = color.New(color.Bold).SprintFunc()
	Dim       = color.New(color.Faint).SprintFunc()
	Cyan      = color.New(color.FgCyan).SprintFunc()
	HiCyan    = color.New(color.FgHiCyan, color.Bold).SprintFunc()
	Green     = color.New(color.FgGreen).SprintFunc()
	HiGreen   = color.New(color.FgHiGreen, color.Bold).SprintFunc()
	Yellow    = color.New(color.FgYellow).SprintFunc()
	HiYellow  = color.New(color.FgHiYellow, color.Bold).SprintFunc()
	Red       = color.New(color.FgRed).SprintFunc()
)

// Success prints a green checkmark followed by the message.
func Success(format string, a ...interface{}) {
	fmt.Printf("%s %s\n", Green("✓"), fmt.Sprintf(format, a...))
}

// Error prints a red cross followed by the error message.
func Error(format string, a ...interface{}) {
	fmt.Fprintf(os.Stderr, "%s %s\n", Red("✗"), fmt.Sprintf(format, a...))
}

// Warn prints a yellow exclamation mark followed by the warning.
func Warn(format string, a ...interface{}) {
	fmt.Printf("%s %s\n", Yellow("!"), fmt.Sprintf(format, a...))
}

// Step prints a cyan arrow followed by the step action.
func Step(format string, a ...interface{}) {
	fmt.Printf("%s %s\n", Cyan("→"), fmt.Sprintf(format, a...))
}

// KeyValue prints an aligned label and value.
func KeyValue(label, value string) {
	fmt.Printf("%-14s %s\n", Dim(label+":"), value)
}

// StatePill formats container and app states with matching colored glyphs.
func StatePill(state string) string {
	lower := strings.ToLower(strings.TrimSpace(state))
	switch {
	case strings.Contains(lower, "running"):
		return Green("● ") + "running"
	case strings.Contains(lower, "active"):
		return Green("● ") + "active"
	case strings.Contains(lower, "exited") || strings.Contains(lower, "stopped"):
		return Dim("○ ") + Dim("stopped")
	case strings.Contains(lower, "restarting"):
		return Yellow("↻ ") + Yellow("restarting")
	case strings.Contains(lower, "unhealthy") || strings.Contains(lower, "failed"):
		return Red("✗ ") + Red("unhealthy")
	default:
		return state
	}
}

// HealthBadge formats healthy/unhealthy status with distinct color.
func HealthBadge(healthy bool) string {
	if healthy {
		return Green("healthy")
	}
	return Red("unhealthy")
}

var ansiCSIRe = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)

func visibleWidth(s string) int {
	plain := ansiCSIRe.ReplaceAllString(s, "")
	return utf8.RuneCountInString(plain)
}

// PrintTable formats tabular data with ANSI-aware dynamic column alignment and styled headers.
func PrintTable(w io.Writer, headers []string, rows [][]string) {
	if w == nil {
		w = os.Stdout
	}
	cols := len(headers)
	for _, r := range rows {
		if len(r) > cols {
			cols = len(r)
		}
	}
	if cols == 0 {
		return
	}

	widths := make([]int, cols)
	styledHeaders := make([]string, len(headers))
	for i, h := range headers {
		up := strings.ToUpper(h)
		styledHeaders[i] = Bold(up)
		if l := utf8.RuneCountInString(up); l > widths[i] {
			widths[i] = l
		}
	}
	for _, r := range rows {
		for i, cell := range r {
			if vw := visibleWidth(cell); vw > widths[i] {
				widths[i] = vw
			}
		}
	}

	var buf bytes.Buffer
	writeRow := func(cells []string) {
		for i := 0; i < cols; i++ {
			cell := ""
			if i < len(cells) {
				cell = cells[i]
			}
			buf.WriteString(cell)
			if i < cols-1 {
				pad := widths[i] - visibleWidth(cell) + 3
				if pad < 1 {
					pad = 1
				}
				buf.WriteString(strings.Repeat(" ", pad))
			}
		}
		buf.WriteByte('\n')
	}

	writeRow(styledHeaders)
	seps := make([]string, cols)
	for i := 0; i < cols; i++ {
		seps[i] = Dim(strings.Repeat("─", widths[i]))
	}
	writeRow(seps)
	for _, r := range rows {
		writeRow(r)
	}
	fmt.Fprint(w, buf.String())
}

// EdgeStatusBadge formats proxy, TLS, DNS, origin, or manual-domain states with semantic colors.
func EdgeStatusBadge(state string) string {
	low := strings.ToLower(strings.TrimSpace(state))
	switch {
	case low == "ok" || low == "up" || low == "active" || low == "enabled" || low == "synced" || strings.HasPrefix(low, "valid"):
		return Green("● " + state)
	case low == "ok_proxied":
		return Cyan("● " + state)
	case low == "issuing" || low == "pending" || strings.HasPrefix(low, "pending_") || low == "dns_drift" || strings.HasPrefix(low, "expiring") || low == "warning":
		return Yellow("◐ " + state)
	case low == "disabled":
		return Dim("○ " + state)
	case low == "unknown" || low == "none" || low == "-":
		return Dim(state)
	case low == "conflict" || low == "cert_error" || low == "error" || low == "nxdomain" || low == "no_records" || low == "elsewhere" || low == "refused" || low == "timeout" || low == "tls_error" || low == "missing" || low == "acme_failing" || low == "held_mass_delete":
		return Red("✗ " + state)
	default:
		return state
	}
}
