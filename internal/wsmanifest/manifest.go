package wsmanifest

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// ManifestEntry represents file metadata and optional content hash.
type ManifestEntry struct {
	Path    string `json:"path"`
	IsDir   bool   `json:"is_dir,omitempty"`
	Size    int64  `json:"size"`
	ModTime int64  `json:"mod_time"`
	SHA256  string `json:"sha256,omitempty"`
}

// ManifestResult contains the collected workspace file entries.
type ManifestResult struct {
	AppID        string          `json:"app_id"`
	Root         string          `json:"root"`
	TotalEntries int             `json:"total_entries"`
	Truncated    bool            `json:"truncated"`
	Files        []ManifestEntry `json:"files"`
}

// manifestCacheKey identifies an immutable file snapshot for SHA256 reuse.
type manifestCacheKey struct {
	appID   string
	path    string
	size    int64
	modNano int64
}

var (
	manifestHashMu    sync.RWMutex
	manifestHashCache = make(map[manifestCacheKey]string)
)

// InvalidateManifestCache removes cached hashes for a specific app or file.
func InvalidateManifestCache(appID, relPath string) {
	manifestHashMu.Lock()
	defer manifestHashMu.Unlock()
	for k := range manifestHashCache {
		if k.appID == appID && (relPath == "" || k.path == relPath) {
			delete(manifestHashCache, k)
		}
	}
}

// getCachedOrComputeHash checks in-memory cache or computes SHA256 from disk.
func getCachedOrComputeHash(appID, relPath, fullPath string, size, modNano int64) (string, error) {
	key := manifestCacheKey{
		appID:   appID,
		path:    relPath,
		size:    size,
		modNano: modNano,
	}

	manifestHashMu.RLock()
	if cached, ok := manifestHashCache[key]; ok {
		manifestHashMu.RUnlock()
		return cached, nil
	}
	manifestHashMu.RUnlock()

	f, err := os.Open(fullPath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	hashHex := hex.EncodeToString(h.Sum(nil))

	manifestHashMu.Lock()
	// Bound cache size to prevent runaway memory usage.
	if len(manifestHashCache) > 100000 {
		manifestHashCache = make(map[manifestCacheKey]string)
	}
	manifestHashCache[key] = hashHex
	manifestHashMu.Unlock()

	return hashHex, nil
}

// defaultExcludedDirs are system, vendor, and venv directories skipped during scans.
var defaultExcludedDirs = map[string]bool{
	".git":                             true,
	".nextdeploy":                      true,
	".panel-meta":                      true,
	"node_modules":                     true,
	"vendor":                           true,
	".venv":                            true,
	"venv":                             true,
	"__pycache__":                      true,
	".idea":                            true,
	".vscode":                          true,
	".nextdeploy.generated.compose.yml": true,
}

// defaultLockFiles are dependency lock files excluded unless include_locks is set.
var defaultLockFiles = map[string]bool{
	"package-lock.json": true,
	"yarn.lock":         true,
	"pnpm-lock.yaml":    true,
	"bun.lockb":         true,
	"Cargo.lock":        true,
	"poetry.lock":       true,
	"Pipfile.lock":      true,
	"composer.lock":     true,
	"uv.lock":           true,
}

// defaultExcludedFilePatterns are common noise/build patterns skipped to save tokens.
var defaultExcludedFilePatterns = []string{
	"*.map",
	".DS_Store",
	"Thumbs.db",
}

// loadGitignoreRules parses simple wildcard and prefix ignore rules from .gitignore.
func loadGitignoreRules(wsRoot string) []string {
	giPath := filepath.Join(wsRoot, ".gitignore")
	f, err := os.Open(giPath)
	if err != nil {
		return nil
	}
	defer f.Close()

	var rules []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		rules = append(rules, filepath.ToSlash(line))
	}
	return rules
}

// matchesIgnore reports whether a slash-separated relative path should be excluded.
func matchesIgnore(relPath string, isDir bool, rules []string, customExcludes []string, includeLocks bool) bool {
	base := filepath.Base(relPath)
	if defaultExcludedDirs[base] || defaultExcludedDirs[relPath] {
		return true
	}

	if !isDir {
		if !includeLocks && defaultLockFiles[base] {
			return true
		}
		for _, pattern := range defaultExcludedFilePatterns {
			if matched, _ := filepath.Match(pattern, base); matched {
				return true
			}
		}
	}

	for _, rule := range rules {
		rule = strings.TrimPrefix(rule, "/")
		if isDir && strings.HasSuffix(rule, "/") {
			trimmed := strings.TrimSuffix(rule, "/")
			if relPath == trimmed || strings.HasPrefix(relPath, trimmed+"/") {
				return true
			}
		}
		if relPath == rule || strings.HasPrefix(relPath, rule+"/") {
			return true
		}
		if matched, _ := filepath.Match(rule, base); matched {
			return true
		}
		if matched, _ := filepath.Match(rule, relPath); matched {
			return true
		}
	}

	for _, pattern := range customExcludes {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			continue
		}
		if matched, _ := filepath.Match(pattern, base); matched {
			return true
		}
		if matched, _ := filepath.Match(pattern, relPath); matched {
			return true
		}
		if strings.Contains(relPath, pattern) {
			return true
		}
	}

	return false
}

// BuildWorkspaceManifest traverses the workspace and builds a filtered file manifest.
// If depth > 0, traversal stops at that depth and directories at target depth are included as IsDir entries (like ls).
func BuildWorkspaceManifest(wsRoot, appID, relScope string, depth int, computeHash bool, includeLocks bool, fullHash bool, customExcludes []string, maxEntries int) (ManifestResult, error) {
	if maxEntries <= 0 {
		maxEntries = 5000
	} else if maxEntries > 20000 {
		maxEntries = 20000
	}

	cleanScope := filepath.ToSlash(strings.Trim(relScope, "/"))
	scanRoot := wsRoot
	if cleanScope != "" {
		scanRoot = filepath.Join(wsRoot, filepath.FromSlash(cleanScope))
	}

	res := ManifestResult{
		AppID: appID,
		Root:  cleanScope,
		Files: []ManifestEntry{},
	}

	gitignoreRules := loadGitignoreRules(wsRoot)

	err := filepath.WalkDir(scanRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}

		rel, err := filepath.Rel(wsRoot, path)
		if err != nil || rel == "." {
			return nil
		}
		rel = filepath.ToSlash(rel)

		// Calculate relative depth from scanRoot
		relScan, err := filepath.Rel(scanRoot, path)
		if err != nil || relScan == "." {
			return nil
		}
		relScan = filepath.ToSlash(relScan)
		currentDepth := strings.Count(relScan, "/") + 1

		isDir := d.IsDir()
		if matchesIgnore(rel, isDir, gitignoreRules, customExcludes, includeLocks) {
			if isDir {
				return fs.SkipDir
			}
			return nil
		}

		if depth > 0 && currentDepth > depth {
			if isDir {
				return fs.SkipDir
			}
			return nil
		}

		if isDir {
			if depth > 0 && currentDepth == depth {
				info, err := d.Info()
				var modTime int64
				if err == nil {
					modTime = info.ModTime().Unix()
				}
				res.Files = append(res.Files, ManifestEntry{
					Path:    rel,
					IsDir:   true,
					ModTime: modTime,
				})
				return fs.SkipDir
			}
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return nil
		}

		if len(res.Files) >= maxEntries {
			res.Truncated = true
			return fs.SkipAll
		}

		entry := ManifestEntry{
			Path:    rel,
			Size:    info.Size(),
			ModTime: info.ModTime().Unix(),
		}

		if computeHash {
			h, herr := getCachedOrComputeHash(appID, rel, path, info.Size(), info.ModTime().UnixNano())
			if herr == nil {
				if fullHash || len(h) <= 16 {
					entry.SHA256 = h
				} else {
					entry.SHA256 = h[:16]
				}
			}
		}

		res.Files = append(res.Files, entry)
		return nil
	})

	if err != nil && err != fs.SkipAll {
		return res, err
	}

	sort.Slice(res.Files, func(i, j int) bool {
		return res.Files[i].Path < res.Files[j].Path
	})

	res.TotalEntries = len(res.Files)
	return res, nil
}

// ManifestDiffResult represents the server-side diff against client local files.
type ManifestDiffResult struct {
	AppID       string   `json:"app_id"`
	Diff        bool     `json:"diff"`
	InSyncCount int      `json:"in_sync_count"`
	ToUpload    []string `json:"to_upload"`
	ToDelete    []string `json:"to_delete"`
	TotalRemote int      `json:"total_remote"`
	TotalLocal  int      `json:"total_local"`
}

// hashesMatch safely compares two hashes which may be 16-character short SHA or full 64-char SHA256.
func hashesMatch(h1, h2 string) bool {
	h1 = strings.ToLower(strings.TrimSpace(h1))
	h2 = strings.ToLower(strings.TrimSpace(h2))
	if h1 == "" || h2 == "" {
		return false
	}
	if h1 == h2 {
		return true
	}
	if len(h1) < len(h2) {
		return strings.HasPrefix(h2, h1)
	}
	return strings.HasPrefix(h1, h2)
}

// ComputeManifestDiff calculates which files need uploading or deleting compared to local client files.
func ComputeManifestDiff(res ManifestResult, localFiles map[string]string) ManifestDiffResult {
	remoteMap := make(map[string]ManifestEntry, len(res.Files))
	for _, f := range res.Files {
		if !f.IsDir {
			remoteMap[filepath.ToSlash(f.Path)] = f
		}
	}

	normLocal := make(map[string]string, len(localFiles))
	for p, h := range localFiles {
		clean := filepath.ToSlash(strings.Trim(p, "/"))
		if clean == "" {
			continue
		}
		// If manifest was scoped to a subpath, only compare within that scope.
		if res.Root != "" && !strings.HasPrefix(clean, res.Root+"/") && clean != res.Root {
			continue
		}
		normLocal[clean] = strings.TrimSpace(h)
	}

	inSyncCount := 0
	var toUpload []string
	for p, localHash := range normLocal {
		remote, exists := remoteMap[p]
		if !exists {
			toUpload = append(toUpload, p)
			continue
		}
		if remote.SHA256 != "" && localHash != "" && hashesMatch(remote.SHA256, localHash) {
			inSyncCount++
		} else {
			toUpload = append(toUpload, p)
		}
	}

	var toDelete []string
	for p := range remoteMap {
		if _, exists := normLocal[p]; !exists {
			toDelete = append(toDelete, p)
		}
	}

	sort.Strings(toUpload)
	sort.Strings(toDelete)

	return ManifestDiffResult{
		AppID:       res.AppID,
		Diff:        true,
		InSyncCount: inSyncCount,
		ToUpload:    toUpload,
		ToDelete:    toDelete,
		TotalRemote: len(remoteMap),
		TotalLocal:  len(normLocal),
	}
}

