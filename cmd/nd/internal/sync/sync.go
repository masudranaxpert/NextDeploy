// Package sync implements local ↔ server workspace diff and tar.gz packing.
package sync

import (
	"archive/tar"
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
	".next":        true,
	"dist":         true,
	"build":        true,
	"__pycache__":  true,
	".venv":        true,
	"vendor":       true,
}

// LocalHashes walks localDir and returns path→sha256 for all files.
func LocalHashes(localDir string) (map[string]string, error) {
	out := make(map[string]string)
	err := filepath.WalkDir(localDir, func(p string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return nil
		}
		name := d.Name()
		if d.IsDir() {
			if DefaultIgnore[name] || strings.HasPrefix(name, ".") {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(localDir, p)
		if err != nil {
			return nil
		}
		h, err := fileHash(p)
		if err != nil {
			return nil
		}
		out[filepath.ToSlash(rel)] = h
		return nil
	})
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
		if rhash, exists := remote[path]; !exists || rhash[:16] != lhash[:16] {
			// ponytail: using 16-char prefix match (same as server short_hash default)
			toUpload = append(toUpload, path)
		}
	}
	for path := range remote {
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
