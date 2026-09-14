package ui

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

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
	HiRed     = color.New(color.FgHiRed, color.Bold).SprintFunc()
	Magenta   = color.New(color.FgMagenta).SprintFunc()
	HiMagenta = color.New(color.FgHiMagenta, color.Bold).SprintFunc()
)

// DisableColor turns off all terminal colors and formatting.
func DisableColor() {
	color.NoColor = true
}

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

// SectionHeader prints a styled block header.
func SectionHeader(title string, badge string) {
	if badge != "" {
		fmt.Printf("\n%s %s\n", HiCyan(title), badge)
	} else {
		fmt.Printf("\n%s\n", HiCyan(title))
	}
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

// PrintTable formats tabular data with proper dynamic tab alignment and styled headers.
func PrintTable(w io.Writer, headers []string, rows [][]string) {
	if w == nil {
		w = os.Stdout
	}
	var buf bytes.Buffer
	tw := tabwriter.NewWriter(&buf, 0, 4, 3, ' ', 0)

	// Format headers with bold uppercase styling
	styledHeaders := make([]string, len(headers))
	for i, h := range headers {
		styledHeaders[i] = Bold(strings.ToUpper(h))
	}
	fmt.Fprintln(tw, strings.Join(styledHeaders, "\t"))

	// Print data rows
	for _, row := range rows {
		fmt.Fprintln(tw, strings.Join(row, "\t"))
	}
	_ = tw.Flush()

	// Print to destination
	fmt.Fprint(w, buf.String())
}
