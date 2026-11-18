package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nd/internal/edge"
)

// Every `nd edge` subcommand must have documented help, and each documented flag must be one
// the command's parser really accepts (so help cannot drift from behaviour).
func TestEdgeCommandDocsCoverEverySubcommand(t *testing.T) {
	var top bytes.Buffer
	printEdgeHelp(&top)
	for _, name := range edgeCommandOrder {
		doc, ok := edgeCommandDocs[name]
		if !ok || doc.Summary == "" || doc.Usage == "" {
			t.Fatalf("no help for %q", name)
		}
		if !strings.Contains(top.String(), name) {
			t.Errorf("nd edge --help does not list %q", name)
		}
		var b bytes.Buffer
		printEdgeCommandHelp(&b, name)
		if !strings.Contains(b.String(), "Usage: nd edge "+name) {
			t.Errorf("help of %q lacks its usage line:\n%s", name, b.String())
		}
		for _, f := range doc.Flags {
			flag := strings.Fields(f[0])[0]
			if !strings.Contains(doc.Usage, flag) {
				t.Errorf("%s: flag %s is documented but missing from the usage line", name, flag)
			}
		}
	}
	if len(edgeCommandDocs) != len(edgeCommandOrder) {
		t.Errorf("%d documented commands, %d listed", len(edgeCommandDocs), len(edgeCommandOrder))
	}
}

func TestEdgeHelpFlagPrintsRichHelp(t *testing.T) {
	var out bytes.Buffer
	ops := &edgeOps{stdout: &out}
	if err := runEdgeInstall(ops, []string{"--help"}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"--proxy NAME", "--token-stdin", "Examples:", "OpenSSL 3.5"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("install --help lacks %q:\n%s", want, out.String())
		}
	}
}

func TestRemoveManagedDirKeepUser(t *testing.T) {
	t.Run("keeps hand-written sites, removes generated files", func(t *testing.T) {
		managed := filepath.Join(t.TempDir(), "nd-edge")
		user := filepath.Join(managed, "user")
		for _, d := range []string{user, filepath.Join(managed, "panels.gen-000001"), filepath.Join(managed, "certs")} {
			if err := os.MkdirAll(d, 0o755); err != nil {
				t.Fatal(err)
			}
		}
		write := func(p, c string) {
			if err := os.WriteFile(p, []byte(c), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		write(filepath.Join(managed, "global.conf"), "x")
		write(filepath.Join(user, edge.PlaceholderAngieFileName), "placeholder")
		write(filepath.Join(user, "manual-sites.conf"), "server {}")

		removeManagedDirKeepUser(managed, user)

		if b, err := os.ReadFile(filepath.Join(user, "manual-sites.conf")); err != nil || string(b) != "server {}" {
			t.Fatalf("manual config lost: %v %q", err, b)
		}
		for _, gone := range []string{"global.conf", "panels.gen-000001", "certs", "user/" + edge.PlaceholderAngieFileName} {
			if _, err := os.Stat(filepath.Join(managed, filepath.FromSlash(gone))); err == nil {
				t.Errorf("%s should have been removed", gone)
			}
		}
	})

	t.Run("removes everything when only the placeholder exists", func(t *testing.T) {
		managed := filepath.Join(t.TempDir(), "nd-edge")
		user := filepath.Join(managed, "user")
		if err := os.MkdirAll(user, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(user, edge.PlaceholderAngieFileName), []byte("p"), 0o644); err != nil {
			t.Fatal(err)
		}
		removeManagedDirKeepUser(managed, user)
		if _, err := os.Stat(managed); err == nil {
			t.Fatal("managed dir should be gone")
		}
	})
}
