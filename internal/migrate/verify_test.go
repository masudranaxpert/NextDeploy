package migrate

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeBundle(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		_ = tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(body))})
		_, _ = tw.Write([]byte(body))
	}
	_ = tw.Close()
	_ = gz.Close()
	return buf.Bytes()
}

func TestVerifyBundle(t *testing.T) {
	dir := t.TempDir()
	data := writeBundle(t, map[string]string{
		"manifest.json":       "{}",
		"./apps/shop.tar.gz":  strings.Repeat("x", 50000),
		"apps/blog.tar.gz":    "y",
	})
	ok := filepath.Join(dir, "ok.nd-migrate")
	_ = os.WriteFile(ok, data, 0o644)
	ctx := context.Background()

	if err := verifyBundle(ctx, ok, []string{"apps/shop.tar.gz", "apps/blog.tar.gz"}); err != nil {
		t.Fatalf("complete bundle rejected: %v", err)
	}
	if err := verifyBundle(ctx, ok, []string{"apps/missing.tar.gz"}); err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("missing archive not reported: %v", err)
	}
	cut := filepath.Join(dir, "cut.nd-migrate")
	_ = os.WriteFile(cut, data[:len(data)/2], 0o644)
	if err := verifyBundle(ctx, cut, nil); err == nil {
		t.Fatal("a truncated bundle must be rejected")
	}
}
