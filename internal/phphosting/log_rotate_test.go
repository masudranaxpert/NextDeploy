package phphosting

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRotateLogsIn(t *testing.T) {
	dir := t.TempDir()
	big := filepath.Join(dir, "php-error.log")
	_ = os.WriteFile(big, make([]byte, 2048), 0o644)
	_ = os.WriteFile(big+".1", []byte("old"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "small.log"), []byte("x"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "notes.txt"), make([]byte, 4096), 0o644)

	if n := rotateLogsIn(dir, 1024); n != 1 {
		t.Fatalf("rotated %d, want 1", n)
	}
	if _, err := os.Stat(big); !os.IsNotExist(err) {
		t.Fatal("big log must be moved away")
	}
	if fi, _ := os.Stat(big + ".1"); fi == nil || fi.Size() != 2048 {
		t.Fatal(".1 must hold the rotated log")
	}
	if _, err := os.Stat(filepath.Join(dir, "small.log")); err != nil {
		t.Fatal("small log must stay")
	}
}
