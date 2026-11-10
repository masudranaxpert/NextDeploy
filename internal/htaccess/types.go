// Package htaccess converts Apache .htaccess files into safe, structured
// NextDeploy Caddy routing rules and fallback settings.
package htaccess

import (
	"errors"

	"panel/internal/caddy"
)

var (
	ErrInvalidRoot  = errors.New("htaccess: invalid document root")
	ErrFileTooLarge = errors.New("htaccess: file exceeds size limit")
	ErrTooManyFiles = errors.New("htaccess: directory scan exceeded file limit")
)

// Severity classifies converter diagnostics. Zero is reserved as unknown.
type Severity int

const (
	SeverityUnknown Severity = iota
	SeverityInfo
	SeverityConverted
	SeverityWarning
)

// String returns the JSON-friendly lowercase severity name.
func (s Severity) String() string {
	switch s {
	case SeverityInfo:
		return "info"
	case SeverityConverted:
		return "converted"
	case SeverityWarning:
		return "warning"
	default:
		return "unknown"
	}
}

// MarshalText encodes Severity as its readable string name.
func (s Severity) MarshalText() ([]byte, error) {
	return []byte(s.String()), nil
}

// Diagnostic explains what the converter did for an individual .htaccess line.
type Diagnostic struct {
	File      string   `json:"file"`
	Line      int      `json:"line"`
	Severity  Severity `json:"severity"`
	Directive string   `json:"directive"`
	Message   string   `json:"message"`
}

// Summary totals converted rules and per-line diagnostics.
type Summary struct {
	Converted int `json:"converted"`
	Skipped   int `json:"skipped"`
	Warnings  int `json:"warnings"`
}

// Result contains the converted Caddy rules, suggested fallback mode, and diagnostics.
type Result struct {
	Rules         []caddy.RoutingRule `json:"rules"`
	FallbackMode  string              `json:"fallback_mode,omitempty"`
	FallbackEntry string              `json:"fallback_entry,omitempty"`
	FilesScanned  int                 `json:"files_scanned"`
	Files         []string            `json:"files,omitempty"`
	Diagnostics   []Diagnostic        `json:"diagnostics"`
	Summary       Summary             `json:"summary"`
}

func (r *Result) finalizeSummary() {
	r.Summary = Summary{Converted: len(r.Rules)}
	for _, d := range r.Diagnostics {
		switch d.Severity {
		case SeverityInfo:
			r.Summary.Skipped++
		case SeverityWarning:
			r.Summary.Warnings++
		}
	}
}

// Converter transforms Apache .htaccess content or directory trees into Caddy routing rules.
type Converter struct {
	maxFileSize int64
	maxFiles    int
	maxDepth    int
	maxRules    int
}

// Option configures a Converter instance.
type Option func(*Converter) error

// WithMaxFileSize caps individual .htaccess file reads in bytes.
func WithMaxFileSize(n int64) Option {
	return func(c *Converter) error {
		if n <= 0 {
			return errors.New("htaccess: max file size must be positive")
		}
		c.maxFileSize = n
		return nil
	}
}

// WithMaxFiles caps how many .htaccess files ScanDir will inspect.
func WithMaxFiles(n int) Option {
	return func(c *Converter) error {
		if n <= 0 {
			return errors.New("htaccess: max files must be positive")
		}
		c.maxFiles = n
		return nil
	}
}

// WithMaxDepth caps directory recursion depth during ScanDir.
func WithMaxDepth(n int) Option {
	return func(c *Converter) error {
		if n < 0 {
			return errors.New("htaccess: max depth cannot be negative")
		}
		c.maxDepth = n
		return nil
	}
}

// WithMaxRules caps how many routing rules are emitted per conversion.
func WithMaxRules(n int) Option {
	return func(c *Converter) error {
		if n <= 0 {
			return errors.New("htaccess: max rules must be positive")
		}
		c.maxRules = n
		return nil
	}
}

// New constructs a Converter with safe defaults and applies functional options.
func New(opts ...Option) (*Converter, error) {
	c := &Converter{
		maxFileSize: 512 << 10, // 512 KiB
		maxFiles:    100,
		maxDepth:    8,
		maxRules:    100,
	}
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		if err := opt(c); err != nil {
			return nil, err
		}
	}
	return c, nil
}
