package htaccess

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"panel/internal/caddy"
)

var skippedScanDirs = map[string]bool{
	".git":         true,
	".svn":         true,
	".hg":          true,
	".well-known":  true,
	"node_modules": true,
	"vendor":       true,
}

// ScanDir walks htdocsRoot recursively, converts every .htaccess found (root and subfolders),
// and merges the resulting Caddy routing rules and diagnostics.
func (c *Converter) ScanDir(ctx context.Context, htdocsRoot string) (Result, error) {
	if strings.TrimSpace(htdocsRoot) == "" {
		return Result{}, ErrInvalidRoot
	}
	absRoot, err := filepath.Abs(htdocsRoot)
	if err != nil {
		return Result{}, fmt.Errorf("%w: %v", ErrInvalidRoot, err)
	}
	realRoot, err := filepath.EvalSymlinks(absRoot)
	if err != nil {
		return Result{}, fmt.Errorf("%w: %v", ErrInvalidRoot, err)
	}
	info, err := os.Stat(realRoot)
	if err != nil || !info.IsDir() {
		return Result{}, ErrInvalidRoot
	}

	var found []string
	err = filepath.WalkDir(realRoot, func(p string, d fs.DirEntry, walkErr error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if walkErr != nil {
			return nil
		}
		rel, rerr := filepath.Rel(realRoot, p)
		if rerr != nil {
			return nil
		}
		relSlash := filepath.ToSlash(rel)

		// Skip symlinks everywhere so directory traversal cannot escape htdocsRoot.
		if d.Type()&os.ModeSymlink != 0 {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if d.IsDir() {
			if relSlash != "." {
				if skippedScanDirs[strings.ToLower(d.Name())] {
					return filepath.SkipDir
				}
				depth := strings.Count(relSlash, "/") + 1
				if depth > c.maxDepth {
					return filepath.SkipDir
				}
			}
			return nil
		}

		if strings.EqualFold(d.Name(), ".htaccess") {
			found = append(found, relSlash)
			if len(found) > c.maxFiles {
				return ErrTooManyFiles
			}
		}
		return nil
	})
	if err != nil {
		return Result{}, err
	}

	sort.Slice(found, func(i, j int) bool {
		// Root .htaccess first, then shallowest to deepest, then alphabetical.
		di, dj := strings.Count(found[i], "/"), strings.Count(found[j], "/")
		if di != dj {
			return di < dj
		}
		return found[i] < found[j]
	})

	merged := Result{
		Rules:       make([]caddy.RoutingRule, 0),
		Files:       make([]string, 0, len(found)),
		Diagnostics: make([]Diagnostic, 0),
	}
	seenRuleKey := map[string]bool{}

	for _, relPath := range found {
		if ctx.Err() != nil {
			return merged, ctx.Err()
		}
		fullPath := filepath.Join(realRoot, filepath.FromSlash(relPath))
		fi, err := os.Lstat(fullPath)
		if err != nil || fi.Mode()&os.ModeSymlink != 0 {
			continue
		}
		if fi.Size() > c.maxFileSize {
			merged.Diagnostics = append(merged.Diagnostics, Diagnostic{
				File:      relPath,
				Severity:  SeverityWarning,
				Directive: ".htaccess",
				Message:   fmt.Sprintf("skipped file (%d bytes exceeds %d byte limit)", fi.Size(), c.maxFileSize),
			})
			continue
		}
		data, err := os.ReadFile(fullPath)
		if err != nil {
			continue
		}
		parsed := c.Parse(string(data), relPath)
		merged.FilesScanned++
		merged.Files = append(merged.Files, relPath)
		merged.Diagnostics = append(merged.Diagnostics, parsed.Diagnostics...)
		if merged.FallbackMode == "" && parsed.FallbackMode != "" {
			merged.FallbackMode = parsed.FallbackMode
			merged.FallbackEntry = parsed.FallbackEntry
		}
		for _, r := range parsed.Rules {
			if len(merged.Rules) >= c.maxRules {
				break
			}
			key := r.Kind + "|" + r.MatchType + "|" + r.Pattern + "|" + r.Target
			if seenRuleKey[key] {
				continue
			}
			seenRuleKey[key] = true
			r.ID = fmt.Sprintf("ht-%d", len(merged.Rules)+1)
			merged.Rules = append(merged.Rules, r)
		}
	}

	merged.finalizeSummary()
	return merged, nil
}
