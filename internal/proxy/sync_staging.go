package proxy

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// StageConfig configures atomic generation staging and symlink switching on disk.
type StageConfig struct {
	BaseDir         string // parent directory holding generation dirs and the live symlink
	LinkName        string // live symlink name inside BaseDir (default "sites")
	GenPrefix       string // generation directory prefix (default "<LinkName>.gen-")
	KeepGenerations int    // number of old inactive generations to retain (default 3)
}

// StageStore writes rendered proxy Files into generation directories and swaps a
// relative symlink atomically without overwriting directories in place.
type StageStore struct {
	baseDir   string
	linkName  string
	genPrefix string
	keepGens  int
}

// NewStageStore validates cfg and creates a StageStore rooted at cfg.BaseDir.
func NewStageStore(cfg StageConfig) (*StageStore, error) {
	base := strings.TrimSpace(cfg.BaseDir)
	if base == "" {
		return nil, errors.New("stage base directory is required")
	}
	linkName := strings.TrimSpace(cfg.LinkName)
	if linkName == "" {
		linkName = "sites"
	}
	if err := validateRelativeTarget(linkName); err != nil {
		return nil, fmt.Errorf("invalid link name: %w", err)
	}
	genPrefix := strings.TrimSpace(cfg.GenPrefix)
	if genPrefix == "" {
		genPrefix = linkName + ".gen-"
	}
	keep := cfg.KeepGenerations
	if keep <= 0 {
		keep = 3
	}
	if err := os.MkdirAll(base, 0o755); err != nil {
		return nil, fmt.Errorf("create stage base dir: %w", err)
	}
	return &StageStore{
		baseDir:   base,
		linkName:  linkName,
		genPrefix: genPrefix,
		keepGens:  keep,
	}, nil
}

// LinkPath returns the full path of the live configuration symlink.
func (s *StageStore) LinkPath() string {
	return filepath.Join(s.baseDir, s.linkName)
}

// Stage writes files into a fresh generation directory under BaseDir and returns
// both its full path (for Driver.Validate) and its relative basename (for SwapDirLinkAtomic).
func (s *StageStore) Stage(generation string, files Files) (stagedDir string, targetBase string, err error) {
	short := sanitizeShortGen(generation)
	targetBase = fmt.Sprintf("%s%s-%d", s.genPrefix, short, linkSeq.Add(1))
	stagedDir = filepath.Join(s.baseDir, targetBase)
	if err := os.MkdirAll(stagedDir, 0o755); err != nil {
		return "", "", fmt.Errorf("create staged dir: %w", err)
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(stagedDir)
		}
	}()

	for relPath, content := range files {
		cleanRel, verr := sanitizeRelFilePath(relPath)
		if verr != nil {
			return "", "", verr
		}
		dst := filepath.Join(stagedDir, cleanRel)
		if werr := WriteFileAtomic(dst, []byte(content), 0o644); werr != nil {
			return "", "", werr
		}
	}
	cleanup = false
	return stagedDir, targetBase, nil
}

// Activate swaps the live symlink atomically to targetBase and returns the prior target.
func (s *StageStore) Activate(targetBase string) (prevTarget string, err error) {
	prevTarget, _ = ReadDirLink(s.LinkPath())
	if err := SwapDirLinkAtomic(s.LinkPath(), targetBase); err != nil {
		return "", err
	}
	return prevTarget, nil
}

// Rollback restores the live symlink to prevTarget (or removes it if none existed).
func (s *StageStore) Rollback(prevTarget string) error {
	if prevTarget == "" {
		return RemoveDirLink(s.LinkPath())
	}
	return SwapDirLinkAtomic(s.LinkPath(), prevTarget)
}

// ReadTargetFiles loads the rendered Files stored under a relative generation directory
// inside BaseDir (used for post-restart rollback when in-memory lastFiles is empty).
func (s *StageStore) ReadTargetFiles(targetBase string) (Files, error) {
	if err := validateRelativeTarget(targetBase); err != nil {
		return nil, err
	}
	root := filepath.Join(s.baseDir, targetBase)
	out := make(Files)
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		if d.IsDir() || strings.HasPrefix(d.Name(), ".tmp-") {
			return nil
		}
		rel, rerr := filepath.Rel(root, p)
		if rerr != nil {
			return rerr
		}
		data, rerr := os.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		out[filepath.ToSlash(rel)] = string(data)
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no config files found in generation %q", targetBase)
	}
	return out, nil
}

// RemoveStaged deletes an uncommitted staged directory inside BaseDir.
func (s *StageStore) RemoveStaged(stagedDir string) {
	if stagedDir == "" {
		return
	}
	rel, err := filepath.Rel(s.baseDir, stagedDir)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return
	}
	_ = os.RemoveAll(stagedDir)
}

// Prune removes stale generation directories older than the retained KeepGenerations,
// never deleting the active symlink target or any explicitly protected keepTargets.
func (s *StageStore) Prune(keepTargets ...string) error {
	protected := make(map[string]bool, len(keepTargets)+1)
	if cur, err := ReadDirLink(s.LinkPath()); err == nil && cur != "" {
		protected[cur] = true
	}
	for _, t := range keepTargets {
		if t != "" {
			protected[t] = true
		}
	}

	entries, err := os.ReadDir(s.baseDir)
	if err != nil {
		return err
	}
	type genEntry struct {
		name    string
		modUnix int64
	}
	var candidates []genEntry
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), s.genPrefix) || protected[e.Name()] {
			continue
		}
		info, ierr := e.Info()
		var mod int64
		if ierr == nil {
			mod = info.ModTime().UnixNano()
		}
		candidates = append(candidates, genEntry{name: e.Name(), modUnix: mod})
	}
	if len(candidates) <= s.keepGens {
		return nil
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].modUnix != candidates[j].modUnix {
			return candidates[i].modUnix > candidates[j].modUnix
		}
		return candidates[i].name > candidates[j].name
	})
	for _, old := range candidates[s.keepGens:] {
		_ = os.RemoveAll(filepath.Join(s.baseDir, old.name))
	}
	return nil
}

func sanitizeShortGen(gen string) string {
	var b strings.Builder
	for _, r := range gen {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			b.WriteRune(r)
			if b.Len() == 12 {
				break
			}
		}
	}
	if b.Len() == 0 {
		return "init"
	}
	return b.String()
}

func sanitizeRelFilePath(rel string) (string, error) {
	norm := strings.ReplaceAll(strings.TrimSpace(rel), "\\", "/")
	if norm == "" || strings.HasPrefix(norm, "/") {
		return "", fmt.Errorf("invalid staged file path %q", rel)
	}
	clean := filepath.Clean(filepath.FromSlash(norm))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("unsafe staged file path %q", rel)
	}
	return clean, nil
}
