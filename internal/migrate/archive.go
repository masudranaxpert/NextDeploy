package migrate

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const streamBuf = 64 * 1024

func writeTarGz(ctx context.Context, outPath string, members []string, baseDir string) error {
	out, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer out.Close()
	gz, err := gzip.NewWriterLevel(out, gzip.BestSpeed)
	if err != nil {
		return err
	}
	tw := tar.NewWriter(gz)
	for _, rel := range members {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		rel = filepath.ToSlash(strings.TrimPrefix(rel, string(filepath.Separator)))
		abs := filepath.Join(baseDir, filepath.FromSlash(rel))
		fi, err := os.Stat(abs)
		if err != nil {
			return err
		}
		if fi.IsDir() {
			continue
		}
		hdr := &tar.Header{
			Name:     rel,
			Mode:     0600,
			Size:     fi.Size(),
			ModTime:  fi.ModTime(),
			Typeflag: tar.TypeReg,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		f, err := os.Open(abs)
		if err != nil {
			return err
		}
		buf := make([]byte, streamBuf)
		if _, err := io.CopyBuffer(tw, f, buf); err != nil {
			_ = f.Close()
			return err
		}
		_ = f.Close()
	}
	if err := tw.Close(); err != nil {
		return err
	}
	if err := gz.Close(); err != nil {
		return err
	}
	return out.Close()
}

func packBundle(ctx context.Context, workDir, outPath string, manifest BundleManifest) error {
	payload := []string{snapshotName}
	appsDir := filepath.Join(workDir, appsDirName)
	entries, err := os.ReadDir(appsDir)
	if err != nil {
		return fmt.Errorf("apps dir: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		payload = append(payload, filepath.Join(appsDirName, e.Name()))
	}
	manifest.Checksums = map[string]string{}
	for _, rel := range payload {
		abs := filepath.Join(workDir, filepath.FromSlash(rel))
		sum, err := fileSHA256(abs)
		if err != nil {
			return err
		}
		manifest.Checksums[filepath.ToSlash(rel)] = sum
	}
	mb, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(workDir, manifestName), mb, 0600); err != nil {
		return err
	}
	members := append([]string{manifestName}, payload...)
	return writeTarGz(ctx, outPath, members, workDir)
}

func extractBundle(ctx context.Context, bundlePath, destDir string) error {
	if err := os.MkdirAll(destDir, 0700); err != nil {
		return err
	}
	f, err := os.Open(bundlePath)
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
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		name := filepath.Clean(filepath.FromSlash(hdr.Name))
		if strings.Contains(name, "..") {
			return fmt.Errorf("invalid path in bundle: %s", hdr.Name)
		}
		target := filepath.Join(destDir, name)
		if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
		if err != nil {
			return err
		}
		buf := make([]byte, streamBuf)
		if _, err := io.CopyBuffer(out, tr, buf); err != nil {
			_ = out.Close()
			return err
		}
		_ = out.Close()
	}
}

func readManifestFromDir(dir string) (BundleManifest, error) {
	var m BundleManifest
	b, err := os.ReadFile(filepath.Join(dir, manifestName))
	if err != nil {
		return m, err
	}
	if err := json.Unmarshal(b, &m); err != nil {
		return m, err
	}
	if m.Version != BundleVersion || m.Type != BundleType {
		return m, fmt.Errorf("unsupported bundle version/type")
	}
	return m, nil
}

func readSnapshotFromDir(dir string) (PanelSnapshot, error) {
	var s PanelSnapshot
	b, err := os.ReadFile(filepath.Join(dir, snapshotName))
	if err != nil {
		return s, err
	}
	if err := json.Unmarshal(b, &s); err != nil {
		return s, err
	}
	return s, nil
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	buf := make([]byte, streamBuf)
	if _, err := io.CopyBuffer(h, f, buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func bundleFileName() string {
	return "panel-migrate-" + time.Now().UTC().Format("20060102-150405") + BundleExtension
}
