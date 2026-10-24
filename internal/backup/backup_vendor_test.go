package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"io"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func archiveEntries(t *testing.T, ctx context.Context, src string) []string {
	t.Helper()
	out := filepath.Join(t.TempDir(), "a.tar.gz")
	if err := writeFullAppArchive(ctx, src, out); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(out)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gz)
	var names []string
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, h.Name)
	}
	sort.Strings(names)
	return names
}

func TestPHPBackupKeepsVendorOnlyWhenAsked(t *testing.T) {
	src := t.TempDir()
	for _, p := range []string{"htdocs/index.php", "htdocs/vendor/autoload.php", "htdocs/node_modules/x.js", "tmp/cache.txt", "htdocs/.git/HEAD"} {
		full := filepath.Join(src, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	has := func(names []string, want string) bool {
		for _, n := range names {
			if filepath.ToSlash(n) == want {
				return true
			}
		}
		return false
	}
	def := archiveEntries(t, context.Background(), src)
	if has(def, "htdocs/vendor/autoload.php") {
		t.Fatal("default archives must keep skipping vendor/")
	}
	php := archiveEntries(t, WithKeepVendor(context.Background()), src)
	if !has(php, "htdocs/vendor/autoload.php") {
		t.Fatalf("PHP archives must include vendor/: %v", php)
	}
	for _, skip := range []string{"htdocs/node_modules/x.js", "tmp/cache.txt", "htdocs/.git/HEAD"} {
		if has(php, skip) {
			t.Fatalf("%s must stay excluded", skip)
		}
	}
	if !has(php, "htdocs/index.php") {
		t.Fatal("regular files must be included")
	}
}
