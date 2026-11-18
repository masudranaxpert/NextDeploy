package angie

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"panel/internal/proxy"
)

func (d *Driver) relativeToDataDir(stagedDir string) (string, bool) {
	if d == nil || strings.TrimSpace(d.dataDir) == "" {
		return "", false
	}
	absBase, err := filepath.Abs(d.dataDir)
	if err != nil {
		return "", false
	}
	absStaged, err := filepath.Abs(stagedDir)
	if err != nil {
		return "", false
	}
	rel, err := filepath.Rel(absBase, absStaged)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", false
	}
	return rel, true
}

func activeDirMatches(linkPath string, want proxy.Files) bool {
	targetBase, err := proxy.ReadDirLink(linkPath)
	if err != nil || targetBase == "" {
		return false
	}
	activeDir := filepath.Join(filepath.Dir(linkPath), targetBase)
	for rel, wantContent := range want {
		cleanRel := filepath.FromSlash(strings.TrimSpace(rel))
		gotBytes, rerr := os.ReadFile(filepath.Join(activeDir, cleanRel))
		if rerr != nil || string(gotBytes) != wantContent {
			return false
		}
	}
	var diskFiles int
	_ = filepath.WalkDir(activeDir, func(_ string, de os.DirEntry, werr error) error {
		if werr != nil || de.IsDir() {
			return nil
		}
		diskFiles++
		return nil
	})
	return diskFiles == len(want)
}

func filesDigest(files proxy.Files) string {
	keys := make([]string, 0, len(files))
	for k := range files {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	h := sha256.New()
	for _, k := range keys {
		_, _ = io.WriteString(h, k)
		_, _ = h.Write([]byte{0})
		_, _ = io.WriteString(h, files[k])
		_, _ = h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))[:12]
}
