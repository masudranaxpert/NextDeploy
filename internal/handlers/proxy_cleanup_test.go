package handlers

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
	"time"

	"panel/internal/proxy/pki"
)

func TestPruneSwitchBackupsKeepsNewest(t *testing.T) {
	dir := t.TempDir()
	compose := filepath.Join(dir, "docker-compose.yml")
	names := []string{"20261101T000000", "20261102T000000", "20261103T000000", "20261104T000000", "20261105T000000"}
	for _, n := range names {
		if err := os.WriteFile(compose+".proxy-switch-"+n+".bak", []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if got := pruneSwitchBackups(compose, 3); got != 2 {
		t.Fatalf("removed %d, want 2", got)
	}
	left, _ := filepath.Glob(compose + ".proxy-switch-*.bak")
	if len(left) != 3 || filepath.Base(left[0]) != "docker-compose.yml.proxy-switch-20261103T000000.bak" {
		t.Fatalf("left = %v", left)
	}
	if got := pruneSwitchBackups(compose, 3); got != 0 {
		t.Fatalf("second prune removed %d", got)
	}
}

func TestAlignPKIWithCaddyRoot(t *testing.T) {
	base := t.TempDir()
	// A stand-in for Caddy's local root, created by the same PKI code.
	caddyMgr, err := pki.NewManager(filepath.Join(base, "caddy-ca"), filepath.Join(base, "caddy-sites"))
	if err != nil {
		t.Fatal(err)
	}
	if err := caddyMgr.EnsureCA(); err != nil {
		t.Fatal(err)
	}
	rootCrt, _ := os.ReadFile(caddyMgr.CACertPath())
	rootKey, _ := os.ReadFile(caddyMgr.CAKeyPath())

	caDir, sitesDir := filepath.Join(base, "ca"), filepath.Join(base, "sites")
	own, _ := pki.NewManager(caDir, sitesDir)
	if _, err := own.EnsureSiteCert("php_a", []string{"a.example.com"}, time.Time{}); err != nil {
		t.Fatal(err)
	}

	orig := readCaddyLocalRoot
	defer func() { readCaddyLocalRoot = orig }()
	readCaddyLocalRoot = func(context.Context) ([]byte, []byte, bool) { return rootCrt, rootKey, true }

	alignPKIWithCaddyRoot(context.Background(), caDir, sitesDir)
	got, _ := os.ReadFile(filepath.Join(caDir, pki.CACertFilename))
	if string(got) != string(rootCrt) {
		t.Fatal("CA was not replaced by the Caddy root")
	}
	if left, _ := filepath.Glob(filepath.Join(sitesDir, "*.crt")); len(left) != 0 {
		t.Fatalf("old leaves must be removed, left %v", left)
	}

	mgr, _ := pki.NewManager(caDir, sitesDir)
	paths, err := mgr.EnsureSiteCert("php_a", []string{"a.example.com"}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	leafPEM, _ := os.ReadFile(paths.CertPath)
	block, _ := pem.Decode(leafPEM)
	leaf, _ := x509.ParseCertificate(block.Bytes)
	rb, _ := pem.Decode(rootCrt)
	root, _ := x509.ParseCertificate(rb.Bytes)
	pool := x509.NewCertPool()
	pool.AddCert(root)
	if _, err := leaf.Verify(x509.VerifyOptions{Roots: pool, DNSName: "a.example.com"}); err != nil {
		t.Fatalf("leaf does not chain to the Caddy root: %v", err)
	}
}
