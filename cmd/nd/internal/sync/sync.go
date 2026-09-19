// Package sync implements local ↔ server workspace diff and tar.gz packing.
package sync

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	ignore "github.com/sabhiram/go-gitignore"
)

// FileEntry represents a local or remote file snapshot.
type FileEntry struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

// manifestResponse matches the server's ManifestResult.
type manifestResponse struct {
	Files []struct {
		Path   string `json:"path"`
		IsDir  bool   `json:"is_dir"`
		SHA256 string `json:"sha256"`
	} `json:"files"`
}

// DefaultIgnore lists directories to skip during local scan.
var DefaultIgnore = map[string]bool{
	"node_modules": true,
	".git":         true,
	".nd":          true,
	".next":        true,
	"dist":         true,
	"build":        true,
	"__pycache__":  true,
	".venv":        true,
	"vendor":       true,
}

// loadGitIgnore compiles rules from .gitignore and .ndignore in localDir.
func loadGitIgnore(localDir string) *ignore.GitIgnore {
	var lines []string
	for _, fname := range []string{".gitignore", ".ndignore"} {
		path := filepath.Join(localDir, fname)
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			lines = append(lines, line)
		}
		_ = f.Close()
	}
	// Always ignore VCS, local nd files, and sensitive env files
	lines = append(lines, ".git", ".nd", ".env", ".env.*")
	return ignore.CompileIgnoreLines(lines...)
}

// hashesMatch safely compares two hashes which may be 16-character short SHA or full 64-char SHA256.
func hashesMatch(h1, h2 string) bool {
	h1 = strings.ToLower(strings.TrimSpace(h1))
	h2 = strings.ToLower(strings.TrimSpace(h2))
	if h1 == "" || h2 == "" {
		return false
	}
	if len(h1) < len(h2) {
		return strings.HasPrefix(h2, h1)
	}
	return strings.HasPrefix(h1, h2)
}

// HashCacheEntry stores timestamp and size alongside sha256 to avoid redundant disk reads.
type HashCacheEntry struct {
	MTime int64  `json:"mtime"`
	Size  int64  `json:"size"`
	SHA   string `json:"sha"`
}

func loadHashCache(dir string) map[string]HashCacheEntry {
	cacheFile := filepath.Join(dir, ".nd", "hash_cache.json")
	b, err := os.ReadFile(cacheFile)
	if err != nil {
		return make(map[string]HashCacheEntry)
	}
	var cache map[string]HashCacheEntry
	if err := json.Unmarshal(b, &cache); err != nil {
		return make(map[string]HashCacheEntry)
	}
	return cache
}

func saveHashCache(dir string, cache map[string]HashCacheEntry) {
	ndDir := filepath.Join(dir, ".nd")
	if err := os.MkdirAll(ndDir, 0755); err != nil {
		return
	}
	cacheFile := filepath.Join(ndDir, "hash_cache.json")
	b, err := json.Marshal(cache)
	if err != nil {
		return
	}
	_ = os.WriteFile(cacheFile, b, 0644)
}

// LocalHashes walks localDir and returns path→sha256 for all non-ignored files.
// It utilizes .nd/hash_cache.json to bypass reading unchanged files.
func LocalHashes(localDir string) (map[string]string, error) {
	fi, err := os.Stat(localDir)
	if err != nil {
		return nil, err
	}
	if !fi.IsDir() {
		h, err := fileHash(localDir)
		if err != nil {
			return nil, err
		}
		return map[string]string{filepath.Base(localDir): h}, nil
	}

	cache := loadHashCache(localDir)
	updatedCache := make(map[string]HashCacheEntry, len(cache))
	ignorer := loadGitIgnore(localDir)
	out := make(map[string]string)

	err = filepath.WalkDir(localDir, func(p string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return nil
		}
		name := d.Name()
		rel, err := filepath.Rel(localDir, p)
		if err != nil || rel == "." {
			return nil
		}
		relSlash := filepath.ToSlash(rel)
		isDir := d.IsDir()

		if isDir {
			if DefaultIgnore[name] || ignorer.MatchesPath(relSlash) || ignorer.MatchesPath(relSlash+"/") {
				return filepath.SkipDir
			}
			return nil
		}

		// Never sync .env files to prevent server data loss
		if name == ".env" || strings.HasPrefix(name, ".env.") {
			return nil
		}

		if ignorer.MatchesPath(relSlash) {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return nil
		}

		mtime := info.ModTime().UnixNano()
		size := info.Size()

		var h string
		if cached, ok := cache[relSlash]; ok && cached.MTime == mtime && cached.Size == size && cached.SHA != "" {
			h = cached.SHA
		} else {
			h, err = fileHash(p)
			if err != nil {
				return nil
			}
		}

		updatedCache[relSlash] = HashCacheEntry{
			MTime: mtime,
			Size:  size,
			SHA:   h,
		}
		out[relSlash] = h
		return nil
	})

	if err == nil && len(out) > 0 {
		saveHashCache(localDir, updatedCache)
	}
	return out, err
}

// Diff compares local hashes against server manifest JSON.
// Returns files to upload and files to delete on the server.
func Diff(serverManifestJSON []byte, local map[string]string) (toUpload []string, toDelete []string, err error) {
	var res manifestResponse
	if err = json.Unmarshal(serverManifestJSON, &res); err != nil {
		return
	}
	remote := make(map[string]string, len(res.Files))
	for _, f := range res.Files {
		if !f.IsDir {
			remote[f.Path] = f.SHA256
		}
	}

	for path, lhash := range local {
		rhash, exists := remote[path]
		if !exists || !hashesMatch(rhash, lhash) {
			toUpload = append(toUpload, path)
		}
	}
	for path := range remote {
		base := filepath.Base(path)
		if base == ".env" || strings.HasPrefix(base, ".env.") {
			continue
		}
		if _, exists := local[path]; !exists {
			toDelete = append(toDelete, path)
		}
	}
	return
}

// PackTarGz creates an in-memory tar.gz of selected files from localDir.
// Returns a reader — the caller is responsible for draining it (pipes to HTTP body).
func PackTarGz(localDir string, files []string) (io.Reader, int64, error) {
	pr, pw := io.Pipe()

	go func() {
		gz := gzip.NewWriter(pw)
		tw := tar.NewWriter(gz)
		var totalBytes int64

		for _, relPath := range files {
			absPath := filepath.Join(localDir, filepath.FromSlash(relPath))
			fi, err := os.Stat(absPath)
			if err != nil {
				continue
			}
			hdr := &tar.Header{
				Name:    relPath,
				Mode:    0644,
				Size:    fi.Size(),
				ModTime: fi.ModTime(),
				Typeflag: tar.TypeReg,
			}
			if err := tw.WriteHeader(hdr); err != nil {
				_ = pw.CloseWithError(fmt.Errorf("tar header: %w", err))
				return
			}
			f, err := os.Open(absPath)
			if err != nil {
				_ = pw.CloseWithError(fmt.Errorf("open %s: %w", relPath, err))
				return
			}
			n, err := io.Copy(tw, f)
			f.Close()
			if err != nil {
				_ = pw.CloseWithError(fmt.Errorf("copy %s: %w", relPath, err))
				return
			}
			totalBytes += n
		}

		_ = tw.Close()
		_ = gz.Close()
		_ = pw.Close()
	}()

	return pr, 0, nil // size unknown (streaming); totalBytes tracked in goroutine
}

func fileHash(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
