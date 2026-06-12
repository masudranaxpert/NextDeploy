package volumex

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	pathstd "path"
	"path/filepath"
	"strings"
)

// maxArchiveUncompressedTotal caps the sum of declared uncompressed sizes (tar headers / zip central directory)
// to mitigate zip-bomb-style archives without fully decompressing.
const maxArchiveUncompressedTotal int64 = 10 << 30 // 10 GiB

func normalizeArchivePath(name string) string {
	name = filepath.ToSlash(strings.TrimSpace(name))
	for strings.HasPrefix(name, "./") {
		name = strings.TrimPrefix(name, "./")
	}
	if name == "." {
		return ""
	}
	return name
}

// archive paths are slash-separated and extracted inside Linux containers,
// so validate them with POSIX semantics instead of host-OS filepath rules.
func isLocalArchivePath(name string) bool {
	name = normalizeArchivePath(name)
	if name == "" {
		return true
	}
	if pathstd.IsAbs(name) {
		return false
	}
	clean := pathstd.Clean(name)
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return false
	}
	for _, seg := range strings.Split(clean, "/") {
		if seg == ".." {
			return false
		}
	}
	return true
}

// safeArchiveMemberPath rejects absolute paths and path traversal in tar/zip member names.
func safeArchiveMemberPath(name string) error {
	name = normalizeArchivePath(name)
	if !isLocalArchivePath(name) {
		return fmt.Errorf("unsafe path in archive: %q", name)
	}
	return nil
}

func safeSymlinkTargetInArchive(memberName, target string) error {
	target = normalizeArchivePath(target)
	if target == "" {
		return errors.New("empty symlink target in archive")
	}
	if pathstd.IsAbs(target) {
		return errors.New("unsafe symlink target in archive")
	}
	memberDir := pathstd.Dir(normalizeArchivePath(memberName))
	resolved := pathstd.Clean(pathstd.Join(memberDir, target))
	if pathstd.IsAbs(resolved) {
		return errors.New("unsafe symlink target in archive")
	}
	if resolved == ".." || strings.HasPrefix(resolved, "../") {
		return errors.New("unsafe symlink target in archive")
	}
	for _, seg := range strings.Split(resolved, "/") {
		if seg == ".." {
			return errors.New("unsafe symlink target in archive")
		}
	}
	return nil
}

// ValidateTarGzPaths reads the gzip-tar and rejects unsafe member names (path traversal, absolute paths, unsafe symlinks).
func ValidateTarGzPaths(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	var total int64
	var entries int
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			if entries == 0 {
				return errors.New("backup archive is empty (no entries)")
			}
			return nil
		}
		if err != nil {
			// May include tar.ErrInsecurePath depending on GODEBUG; safeArchiveMemberPath remains primary defense.
			return err
		}
		entries++
		if hdr.Size < 0 {
			return fmt.Errorf("invalid tar header size for %q", hdr.Name)
		}
		if hdr.Size > maxArchiveUncompressedTotal {
			return fmt.Errorf("uncompressed size exceeds limit")
		}
		if total > maxArchiveUncompressedTotal-hdr.Size {
			return fmt.Errorf("uncompressed total exceeds limit")
		}
		total += hdr.Size

		if err := safeArchiveMemberPath(hdr.Name); err != nil {
			return err
		}
		switch hdr.Typeflag {
		case tar.TypeSymlink, tar.TypeLink:
			if err := safeSymlinkTargetInArchive(hdr.Name, hdr.Linkname); err != nil {
				return err
			}
		}
	}
}

// ValidateZipPaths rejects zip slip and absolute paths in zip entries.
func ValidateZipPaths(path string) error {
	r, err := zip.OpenReader(path)
	if err != nil {
		return err
	}
	defer r.Close()
	if len(r.File) == 0 {
		return errors.New("backup archive is empty (no entries)")
	}
	var total uint64
	for _, f := range r.File {
		if err := safeArchiveMemberPath(f.Name); err != nil {
			return err
		}
		sz := f.UncompressedSize64
		if sz > uint64(maxArchiveUncompressedTotal) {
			return fmt.Errorf("uncompressed size exceeds limit")
		}
		if total > uint64(maxArchiveUncompressedTotal)-sz {
			return fmt.Errorf("uncompressed total exceeds limit")
		}
		total += sz

		mode := f.FileInfo().Mode()
		if mode&os.ModeSymlink == 0 {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		b, err := io.ReadAll(io.LimitReader(rc, 8192))
		_ = rc.Close()
		if err != nil {
			return err
		}
		t := strings.TrimSpace(string(b))
		if t == "" {
			return errors.New("empty symlink target in zip")
		}
		if err := safeSymlinkTargetInArchive(f.Name, t); err != nil {
			return err
		}
	}
	return nil
}
