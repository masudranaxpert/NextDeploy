package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A link planted inside a workspace must not let the panel write or read outside it.
func TestWorkspaceRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	s := NewStore(root)
	victim := filepath.Join(root, "victim")
	attacker := filepath.Join(root, "attacker")
	for _, d := range []string{victim, attacker} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(victim, filepath.Join(attacker, "l")); err != nil {
		t.Fatal(err)
	}

	if _, err := s.SaveUploadedFile("attacker", "l/evil.php", strings.NewReader("x")); err == nil {
		t.Fatal("upload through a symlink was allowed")
	}
	if _, err := os.Stat(filepath.Join(victim, "evil.php")); err == nil {
		t.Fatal("file was written into the other workspace")
	}
	if _, err := s.SafeFilePath("attacker", "l/new.txt"); err == nil {
		t.Fatal("SafeFilePath accepted a new file below an escaping symlink")
	}
	if _, err := s.SafeFilePath("attacker", "l"); err == nil {
		t.Fatal("SafeFilePath accepted the escaping symlink itself")
	}
	if _, err := s.SaveUploadedFile("attacker", "ok/dir/file.txt", strings.NewReader("x")); err != nil {
		t.Fatalf("normal upload failed: %v", err)
	}
}
