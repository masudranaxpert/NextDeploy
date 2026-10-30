package phphosting

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// SiteSettings are the per-site PHP knobs shown on the Settings tab.
type SiteSettings struct {
	MemoryLimit       string `json:"memory_limit"`       // e.g. 256M
	MaxExecutionTime  int    `json:"max_execution_time"` // seconds
	MaxInputTime      int    `json:"max_input_time"`     // seconds
	MaxInputVars      int    `json:"max_input_vars"`
	PostMaxSize       string `json:"post_max_size"`       // e.g. 64M
	UploadMaxFilesize string `json:"upload_max_filesize"` // e.g. 64M
	MaxChildren       int    `json:"max_children"`
	ProcessManager    string `json:"process_manager"`    // dynamic (default) or ondemand
	Additional        string `json:"additional"`         // extra php.ini directives, one per line
}

var (
	MemoryLimitChoices = []string{"64M", "128M", "256M", "512M", "768M", "1G", "2G", "3G", "4G", "5G"}
	SizeChoices        = []string{"2M", "4M", "8M", "16M", "32M", "64M", "128M", "256M", "512M", "768M", "1G", "2G", "3G", "4G", "5G"}
	TimeChoices        = []int{15, 30, 60, 120, 180, 300, 600, 900, 1800, 3600}
	InputVarChoices    = []int{1000, 2000, 5000, 10000, 20000, 50000, 100000}
	ProcessManagerChoices = []string{"dynamic", "ondemand"}
)

func DefaultSettings() SiteSettings {
	return SiteSettings{
		MemoryLimit:       "256M",
		MaxExecutionTime:  30,
		MaxInputTime:      60,
		MaxInputVars:      1000,
		PostMaxSize:       "64M",
		UploadMaxFilesize: "64M",
		MaxChildren:       10,
		ProcessManager:    "dynamic",
	}
}

// ParseSettings decodes stored settings, filling gaps with defaults.
func ParseSettings(raw string) SiteSettings {
	s := DefaultSettings()
	if strings.TrimSpace(raw) != "" {
		_ = json.Unmarshal([]byte(raw), &s)
	}
	return s.withDefaults()
}

func (s SiteSettings) withDefaults() SiteSettings {
	d := DefaultSettings()
	if s.MemoryLimit == "" {
		s.MemoryLimit = d.MemoryLimit
	}
	if s.MaxExecutionTime <= 0 {
		s.MaxExecutionTime = d.MaxExecutionTime
	}
	if s.MaxInputTime <= 0 {
		s.MaxInputTime = d.MaxInputTime
	}
	if s.MaxInputVars <= 0 {
		s.MaxInputVars = d.MaxInputVars
	}
	if s.PostMaxSize == "" {
		s.PostMaxSize = d.PostMaxSize
	}
	if s.UploadMaxFilesize == "" {
		s.UploadMaxFilesize = d.UploadMaxFilesize
	}
	if s.MaxChildren <= 0 {
		s.MaxChildren = d.MaxChildren
	}
	if s.ProcessManager != "ondemand" && s.ProcessManager != "dynamic" {
		s.ProcessManager = d.ProcessManager
	}
	return s
}

func (s SiteSettings) JSON() string {
	b, _ := json.Marshal(s)
	return string(b)
}

// SizeToMB converts values like 256M / 1G into megabytes.
func SizeToMB(v string) int {
	v = strings.ToUpper(strings.TrimSpace(v))
	if v == "" {
		return 0
	}
	mult := 1
	switch {
	case strings.HasSuffix(v, "G"):
		mult = 1024
		v = strings.TrimSuffix(v, "G")
	case strings.HasSuffix(v, "M"):
		v = strings.TrimSuffix(v, "M")
	default:
		return 0
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return 0
	}
	return n * mult
}

func containsString(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

func containsInt(list []int, v int) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

// Ceilings are the hard upper bounds a site's PHP settings may not exceed: the smaller of the
// owner's quota and what the server really has. 0 means "unknown/unbounded".
type Ceilings struct {
	MemoryMB  int
	StorageMB int
}

// MaxChildrenFor is the largest sensible worker count for a memory ceiling (about 32 MB each).
func (c Ceilings) MaxChildrenFor() int {
	if c.MemoryMB <= 0 {
		return 100
	}
	n := c.MemoryMB / 32
	if n < 1 {
		n = 1
	}
	if n > 100 {
		n = 100
	}
	return n
}

// MemoryChoices returns the memory_limit values that fit under the ceiling.
func (c Ceilings) MemoryChoices() []string {
	out := []string{}
	for _, v := range MemoryLimitChoices {
		if c.MemoryMB <= 0 || SizeToMB(v) <= c.MemoryMB {
			out = append(out, v)
		}
	}
	if len(out) == 0 {
		out = append(out, MemoryLimitChoices[0])
	}
	return out
}

// SizeCapMB is the largest upload/post size offered: it must fit the storage the owner has and
// must not be bigger than the memory they can use (PHP parses form data in memory).
func (c Ceilings) SizeCapMB() int {
	cap := c.StorageMB
	if c.MemoryMB > 0 && (cap <= 0 || c.MemoryMB < cap) {
		cap = c.MemoryMB
	}
	return cap
}

// SizeChoices returns the upload/post size values that fit under SizeCapMB.
func (c Ceilings) SizeChoices() []string {
	out := []string{}
	cap := c.SizeCapMB()
	for _, v := range SizeChoices {
		if cap <= 0 || SizeToMB(v) <= cap {
			out = append(out, v)
		}
	}
	if len(out) == 0 {
		out = append(out, SizeChoices[0])
	}
	return out
}

// Validate checks every field against the allowed choices and the ceilings.
func (s SiteSettings) Validate(c Ceilings) error {
	if !containsString(MemoryLimitChoices, s.MemoryLimit) {
		return fmt.Errorf("invalid memory_limit %q", s.MemoryLimit)
	}
	if c.MemoryMB > 0 && SizeToMB(s.MemoryLimit) > c.MemoryMB {
		return fmt.Errorf("memory_limit %s is more than you can use (%d MB available)", s.MemoryLimit, c.MemoryMB)
	}
	if !containsInt(TimeChoices, s.MaxExecutionTime) {
		return fmt.Errorf("invalid max_execution_time %d", s.MaxExecutionTime)
	}
	if !containsInt(TimeChoices, s.MaxInputTime) {
		return fmt.Errorf("invalid max_input_time %d", s.MaxInputTime)
	}
	if !containsInt(InputVarChoices, s.MaxInputVars) {
		return fmt.Errorf("invalid max_input_vars %d", s.MaxInputVars)
	}
	for name, v := range map[string]string{"post_max_size": s.PostMaxSize, "upload_max_filesize": s.UploadMaxFilesize} {
		if !containsString(SizeChoices, v) {
			return fmt.Errorf("invalid %s %q", name, v)
		}
		if cap := c.SizeCapMB(); cap > 0 && SizeToMB(v) > cap {
			return fmt.Errorf("%s %s is more than your storage or memory allows (%d MB)", name, v, cap)
		}
	}
	if SizeToMB(s.PostMaxSize) < SizeToMB(s.UploadMaxFilesize) {
		return fmt.Errorf("post_max_size must be at least as large as upload_max_filesize")
	}
	if max := c.MaxChildrenFor(); s.MaxChildren < 1 || s.MaxChildren > max {
		return fmt.Errorf("max_children must be between 1 and %d for this memory limit", max)
	}
	if s.ProcessManager != "" && s.ProcessManager != "dynamic" && s.ProcessManager != "ondemand" {
		return fmt.Errorf("invalid process_manager %q (must be dynamic or ondemand)", s.ProcessManager)
	}
	if _, err := ParseAdditional(s.Additional); err != nil {
		return err
	}
	return nil
}

var directiveKey = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.]{0,63}$`)

// Directives that would let a site escape its sandbox or break the pool; they are
// always set by the platform and cannot be overridden through "additional".
var blockedDirectives = map[string]bool{
	"open_basedir": true, "disable_functions": true, "disable_classes": true,
	"upload_tmp_dir": true, "sys_temp_dir": true, "session.save_path": true,
	"extension": true, "zend_extension": true, "extension_dir": true,
	"auto_prepend_file": true, "auto_append_file": true, "user_ini.filename": true,
	"error_log": true, "doc_root": true, "user_dir": true, "enable_dl": true,
	"sendmail_path": true, "mail.log": true, "curl.cainfo": true, "openssl.cafile": true,
	"memory_limit": true, "max_execution_time": true, "max_input_time": true,
	"max_input_vars": true, "post_max_size": true, "upload_max_filesize": true,
}

type Directive struct{ Key, Value string }

// ParseAdditional parses "key = value" lines (comments with ; or #).
func ParseAdditional(text string) ([]Directive, error) {
	var out []Directive
	for i, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, ";") || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf("line %d: expected key = value", i+1)
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		if !directiveKey.MatchString(k) {
			return nil, fmt.Errorf("line %d: invalid directive name %q", i+1, k)
		}
		if blockedDirectives[strings.ToLower(k)] {
			return nil, fmt.Errorf("line %d: %s is managed by the platform", i+1, k)
		}
		if strings.ContainsAny(v, "$\n\r\x00") || strings.Contains(v, " ;") {
			return nil, fmt.Errorf("line %d: unsupported character in value", i+1)
		}
		out = append(out, Directive{Key: k, Value: v})
	}
	if len(out) > 50 {
		return nil, fmt.Errorf("too many additional directives (max 50)")
	}
	return out, nil
}
