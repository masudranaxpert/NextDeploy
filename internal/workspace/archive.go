package workspace

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"panel/internal/caddy"
	"panel/internal/sandbox"
)

const maxArchiveUncompressedBytes = 2 << 30 // 2 GB limit to prevent decompression bombs

// ExtractArchive safely extracts a tar.gz, tar, or zip archive into the workspace.
// Guards against path traversal, symlink attacks, and decompression bombs.
func (s *Store) ExtractArchive(wsID string, r io.Reader, filename string) (int, int64, error) {
	base := filepath.Clean(s.Path(wsID))
	if err := os.MkdirAll(base, 0750); err != nil {
		return 0, 0, err
	}

	// Buffer initial bytes to sniff format if filename is generic or ambiguous
	headerBuf := make([]byte, 512)
	n, err := io.ReadFull(r, headerBuf)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return 0, 0, err
	}
	headerBuf = headerBuf[:n]

	multiReader := io.MultiReader(bytes.NewReader(headerBuf), r)

	lowerName := strings.ToLower(filename)
	isZip := strings.HasSuffix(lowerName, ".zip") || (len(headerBuf) >= 4 && headerBuf[0] == 'P' && headerBuf[1] == 'K' && headerBuf[2] == 0x03 && headerBuf[3] == 0x04)

	if isZip {
		return s.extractZipStream(wsID, base, multiReader)
	}

	return s.extractTarStream(wsID, base, multiReader, headerBuf)
}

// extractTarStream unpacks gzip-compressed or uncompressed tar archives safely.
func (s *Store) extractTarStream(wsID, base string, r io.Reader, headerBuf []byte) (int, int64, error) {
	var tarReader *tar.Reader
	isGzip := len(headerBuf) >= 2 && headerBuf[0] == 0x1f && headerBuf[1] == 0x8b

	if isGzip {
		gzr, err := gzip.NewReader(r)
		if err != nil {
			return 0, 0, fmt.Errorf("invalid gzip archive: %w", err)
		}
		defer gzr.Close()
		tarReader = tar.NewReader(gzr)
	} else {
		tarReader = tar.NewReader(r)
	}

	var extractedFiles int
	var totalBytes int64

	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return extractedFiles, totalBytes, fmt.Errorf("tar reading error: %w", err)
		}

		rawName := strings.TrimSpace(header.Name)
		if rawName == "" {
			continue
		}

		// Security: reject absolute paths and paths containing '..'
		if filepath.IsAbs(rawName) || strings.HasPrefix(rawName, "/") || strings.HasPrefix(rawName, "\\") || strings.Contains(rawName, "..") {
			return extractedFiles, totalBytes, fmt.Errorf("security violation: illegal path in archive: %s", rawName)
		}

		cleanRel := filepath.ToSlash(filepath.Clean(rawName))
		if cleanRel == "." || strings.HasPrefix(cleanRel, "../") {
			continue
		}

		baseName := filepath.Base(cleanRel)
		if baseName == ".panel-meta" || cleanRel == ".panel-meta" || strings.HasPrefix(cleanRel, ".panel-meta/") ||
			baseName == caddy.GeneratedCompose || cleanRel == ReservedDir || strings.HasPrefix(cleanRel, ReservedDir+"/") {
			continue
		}

		dest := filepath.Join(base, filepath.FromSlash(cleanRel))
		relToBase, err := filepath.Rel(base, filepath.Clean(dest))
		if err != nil || relToBase == ".." || strings.HasPrefix(relToBase, ".."+string(os.PathSeparator)) {
			return extractedFiles, totalBytes, fmt.Errorf("security violation: path escapes workspace: %s", rawName)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(dest, 0750); err != nil {
				return extractedFiles, totalBytes, err
			}

		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(dest), 0750); err != nil {
				return extractedFiles, totalBytes, err
			}

			if header.Size > 0 && totalBytes+header.Size > maxArchiveUncompressedBytes {
				return extractedFiles, totalBytes, fmt.Errorf("archive exceeds maximum uncompressed size limit (%d bytes)", maxArchiveUncompressedBytes)
			}

			// Read file content with bounded size
			limitReader := io.LimitReader(tarReader, maxArchiveUncompressedBytes-totalBytes)
			var buf bytes.Buffer
			copied, err := io.Copy(&buf, limitReader)
			if err != nil {
				return extractedFiles, totalBytes, err
			}
			totalBytes += copied

			// Compose security inspection
			if baseName == "docker-compose.yml" || baseName == "docker-compose.yaml" || baseName == "compose.yml" || baseName == "compose.yaml" {
				if err := sandbox.CheckComposeSecurity(buf.Bytes()); err != nil {
					return extractedFiles, totalBytes, fmt.Errorf("insecure compose file in archive (%s): %w", rawName, err)
				}
			}

			mode := os.FileMode(0640)
			if header.Mode&0111 != 0 {
				mode = 0750
			}

			if err := os.WriteFile(dest, buf.Bytes(), mode); err != nil {
				return extractedFiles, totalBytes, err
			}
			extractedFiles++

		default:
			// Safely ignore symlinks, hardlinks, FIFOs, devices to prevent filesystem escapes
			continue
		}
	}

	return extractedFiles, totalBytes, nil
}

// extractZipStream writes reader to a temporary file to allow random access for zip reading.
func (s *Store) extractZipStream(wsID, base string, r io.Reader) (int, int64, error) {
	tmpFile, err := os.CreateTemp("", "nd-zip-upload-*")
	if err != nil {
		return 0, 0, err
	}
	defer func() {
		tmpFile.Close()
		_ = os.Remove(tmpFile.Name())
	}()

	copied, err := io.Copy(tmpFile, r)
	if err != nil {
		return 0, 0, err
	}

	zr, err := zip.NewReader(tmpFile, copied)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid zip archive: %w", err)
	}

	var extractedFiles int
	var totalBytes int64

	for _, f := range zr.File {
		rawName := strings.TrimSpace(f.Name)
		if rawName == "" {
			continue
		}

		if filepath.IsAbs(rawName) || strings.HasPrefix(rawName, "/") || strings.HasPrefix(rawName, "\\") || strings.Contains(rawName, "..") {
			return extractedFiles, totalBytes, fmt.Errorf("security violation: illegal path in archive: %s", rawName)
		}

		cleanRel := filepath.ToSlash(filepath.Clean(rawName))
		if cleanRel == "." || strings.HasPrefix(cleanRel, "../") {
			continue
		}

		baseName := filepath.Base(cleanRel)
		if baseName == ".panel-meta" || cleanRel == ".panel-meta" || strings.HasPrefix(cleanRel, ".panel-meta/") ||
			baseName == caddy.GeneratedCompose || cleanRel == ReservedDir || strings.HasPrefix(cleanRel, ReservedDir+"/") {
			continue
		}

		dest := filepath.Join(base, filepath.FromSlash(cleanRel))
		relToBase, err := filepath.Rel(base, filepath.Clean(dest))
		if err != nil || relToBase == ".." || strings.HasPrefix(relToBase, ".."+string(os.PathSeparator)) {
			return extractedFiles, totalBytes, fmt.Errorf("security violation: path escapes workspace: %s", rawName)
		}

		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(dest, 0750); err != nil {
				return extractedFiles, totalBytes, err
			}
			continue
		}

		if totalBytes+int64(f.UncompressedSize64) > maxArchiveUncompressedBytes {
			return extractedFiles, totalBytes, errors.New("archive uncompressed size exceeds maximum limit")
		}

		if err := os.MkdirAll(filepath.Dir(dest), 0750); err != nil {
			return extractedFiles, totalBytes, err
		}

		rc, err := f.Open()
		if err != nil {
			return extractedFiles, totalBytes, err
		}

		var buf bytes.Buffer
		n, err := io.Copy(&buf, rc)
		rc.Close()
		if err != nil {
			return extractedFiles, totalBytes, err
		}
		totalBytes += n

		if baseName == "docker-compose.yml" || baseName == "docker-compose.yaml" || baseName == "compose.yml" || baseName == "compose.yaml" {
			if err := sandbox.CheckComposeSecurity(buf.Bytes()); err != nil {
				return extractedFiles, totalBytes, fmt.Errorf("insecure compose file in archive (%s): %w", rawName, err)
			}
		}

		mode := f.Mode().Perm()
		if mode == 0 {
			mode = 0640
		}
		if err := os.WriteFile(dest, buf.Bytes(), mode); err != nil {
			return extractedFiles, totalBytes, err
		}
		extractedFiles++
	}

	return extractedFiles, totalBytes, nil
}
