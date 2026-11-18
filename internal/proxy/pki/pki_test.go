package pki

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func newTestManager(t *testing.T) (*Manager, string, string) {
	t.Helper()
	root := t.TempDir()
	caDir := filepath.Join(root, "ca")
	sitesDir := filepath.Join(root, "sites")
	mgr, err := NewManager(caDir, sitesDir)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	return mgr, caDir, sitesDir
}

func TestManagerDirIsolation(t *testing.T) {
	root := t.TempDir()
	badCases := []struct {
		name  string
		ca    string
		sites string
	}{
		{"empty caDir", "", filepath.Join(root, "sites")},
		{"empty sitesDir", filepath.Join(root, "ca"), ""},
		{"same dir", filepath.Join(root, "pki"), filepath.Join(root, "pki")},
		{"ca inside sites", filepath.Join(root, "pki", "ca"), filepath.Join(root, "pki")},
		{"sites inside ca", filepath.Join(root, "pki"), filepath.Join(root, "pki", "sites")},
	}
	for _, tc := range badCases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewManager(tc.ca, tc.sites); !errors.Is(err, ErrInvalidDir) {
				t.Fatalf("NewManager(%q, %q) err = %v, want ErrInvalidDir", tc.ca, tc.sites, err)
			}
		})
	}

	mgr, caDir, sitesDir := newTestManager(t)
	if err := mgr.EnsureCA(); err != nil {
		t.Fatalf("EnsureCA: %v", err)
	}
	if _, err := os.Stat(filepath.Join(caDir, CAKeyFilename)); err != nil {
		t.Fatalf("expected ca.key in caDir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(sitesDir, CAKeyFilename)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("ca.key must not exist in sitesDir, stat err = %v", err)
	}
}

func TestEnsureCAAndPersistence(t *testing.T) {
	mgr, caDir, sitesDir := newTestManager(t)
	t0 := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	mgr.now = func() time.Time { return t0 }

	if err := mgr.EnsureCA(); err != nil {
		t.Fatalf("EnsureCA: %v", err)
	}
	pem1, err := mgr.OriginCACertPEM()
	if err != nil {
		t.Fatalf("OriginCACertPEM: %v", err)
	}
	if !strings.HasPrefix(pem1, "-----BEGIN CERTIFICATE-----\n") || !strings.HasSuffix(pem1, "\n") || strings.HasSuffix(pem1, "\n\n") {
		t.Fatalf("unexpected OriginCACertPEM format: %q", pem1)
	}

	caCert, err := parseCertPEM([]byte(pem1))
	if err != nil {
		t.Fatalf("parseCertPEM: %v", err)
	}
	if !caCert.IsCA || !caCert.BasicConstraintsValid {
		t.Fatalf("expected IsCA=true and BasicConstraintsValid=true, got %+v", caCert)
	}
	wantUsage := x509.KeyUsageCertSign | x509.KeyUsageCRLSign
	if caCert.KeyUsage != wantUsage {
		t.Fatalf("KeyUsage = %v, want %v", caCert.KeyUsage, wantUsage)
	}
	if got := caCert.NotAfter.Sub(t0); got != CALifetime {
		t.Fatalf("CA lifetime = %v, want %v", got, CALifetime)
	}
	keyBytes, err := os.ReadFile(mgr.CAKeyPath())
	if err != nil {
		t.Fatalf("read ca.key: %v", err)
	}
	caKey, err := parseECKeyPEM(keyBytes)
	if err != nil {
		t.Fatalf("parse ca.key: %v", err)
	}
	if caKey.Curve != elliptic.P256() {
		t.Fatalf("CA curve = %v, want P-256", caKey.Curve)
	}

	// Reload from disk using a fresh Manager instance; CA must persist unchanged.
	reloaded, err := NewManager(caDir, sitesDir)
	if err != nil {
		t.Fatalf("NewManager reload: %v", err)
	}
	reloaded.now = func() time.Time { return t0.Add(24 * time.Hour) }
	if err := reloaded.EnsureCA(); err != nil {
		t.Fatalf("reloaded EnsureCA: %v", err)
	}
	pem2, err := reloaded.OriginCACertPEM()
	if err != nil {
		t.Fatalf("reloaded OriginCACertPEM: %v", err)
	}
	if pem2 != pem1 {
		t.Fatalf("reloaded CA PEM changed across reload")
	}
	caCert2, _ := parseCertPEM([]byte(pem2))
	if caCert2.SerialNumber.Cmp(caCert.SerialNumber) != 0 {
		t.Fatalf("reloaded CA serial %v != original %v", caCert2.SerialNumber, caCert.SerialNumber)
	}
}

func TestEnsureCARejectsCorruptOrPartialState(t *testing.T) {
	t.Run("missing key only", func(t *testing.T) {
		mgr, _, _ := newTestManager(t)
		if err := mgr.EnsureCA(); err != nil {
			t.Fatalf("EnsureCA: %v", err)
		}
		if err := os.Remove(mgr.CAKeyPath()); err != nil {
			t.Fatalf("remove ca.key: %v", err)
		}
		mgr2, _ := NewManager(mgr.CADir(), mgr.SitesDir())
		if err := mgr2.EnsureCA(); !errors.Is(err, ErrInvalidCA) {
			t.Fatalf("expected ErrInvalidCA, got %v", err)
		}
	})

	t.Run("corrupt cert", func(t *testing.T) {
		mgr, _, _ := newTestManager(t)
		if err := mgr.EnsureCA(); err != nil {
			t.Fatalf("EnsureCA: %v", err)
		}
		if err := os.WriteFile(mgr.CACertPath(), []byte("garbage"), CertMode); err != nil {
			t.Fatalf("corrupt ca.crt: %v", err)
		}
		mgr2, _ := NewManager(mgr.CADir(), mgr.SitesDir())
		if err := mgr2.EnsureCA(); !errors.Is(err, ErrInvalidCA) {
			t.Fatalf("expected ErrInvalidCA, got %v", err)
		}
	})

	t.Run("mismatched keypair", func(t *testing.T) {
		mgr, _, _ := newTestManager(t)
		if err := mgr.EnsureCA(); err != nil {
			t.Fatalf("EnsureCA: %v", err)
		}
		otherKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		otherDER, _ := x509.MarshalECPrivateKey(otherKey)
		if err := os.WriteFile(mgr.CAKeyPath(), encodePEM("EC PRIVATE KEY", otherDER), PrivateKeyMode); err != nil {
			t.Fatalf("write mismatched key: %v", err)
		}
		mgr2, _ := NewManager(mgr.CADir(), mgr.SitesDir())
		if err := mgr2.EnsureCA(); !errors.Is(err, ErrInvalidCA) {
			t.Fatalf("expected ErrInvalidCA, got %v", err)
		}
	})

	t.Run("non-CA certificate", func(t *testing.T) {
		mgr, _, _ := newTestManager(t)
		if err := ensureDirMode(mgr.CADir(), CADirMode, true); err != nil {
			t.Fatal(err)
		}
		key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		tmpl := &x509.Certificate{
			SerialNumber: big.NewInt(7),
			Subject:      pkix.Name{CommonName: "not-a-ca"},
			NotBefore:    time.Now().Add(-time.Hour),
			NotAfter:     time.Now().Add(time.Hour),
			IsCA:         false,
		}
		der, _ := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
		keyDER, _ := x509.MarshalECPrivateKey(key)
		_ = os.WriteFile(mgr.CAKeyPath(), encodePEM("EC PRIVATE KEY", keyDER), PrivateKeyMode)
		_ = os.WriteFile(mgr.CACertPath(), encodePEM("CERTIFICATE", der), CertMode)
		if err := mgr.EnsureCA(); !errors.Is(err, ErrInvalidCA) {
			t.Fatalf("expected ErrInvalidCA, got %v", err)
		}
	})
}

func TestPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits are not enforced on Windows")
	}
	mgr, caDir, _ := newTestManager(t)
	if err := os.MkdirAll(caDir, 0o755); err != nil {
		t.Fatalf("precreate caDir: %v", err)
	}
	t0 := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	if err := mgr.EnsureCA(); err != nil {
		t.Fatalf("EnsureCA: %v", err)
	}
	paths, err := mgr.EnsureSiteCert("app:perms:1", []string{"perms.example.com"}, t0)
	if err != nil {
		t.Fatalf("EnsureSiteCert: %v", err)
	}

	checkPerm := func(path string, want os.FileMode) {
		t.Helper()
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat %s: %v", path, err)
		}
		if got := info.Mode().Perm(); got != want {
			t.Fatalf("%s perm = %04o, want %04o", path, got, want)
		}
	}
	checkPerm(caDir, CADirMode)
	checkPerm(mgr.CAKeyPath(), PrivateKeyMode)
	checkPerm(mgr.CACertPath(), CertMode)
	checkPerm(paths.KeyPath, PrivateKeyMode)
	checkPerm(paths.CertPath, CertMode)

	// Ensure overly loose existing permissions are tightened back down.
	_ = os.Chmod(mgr.CAKeyPath(), 0o644)
	_ = os.Chmod(paths.KeyPath, 0o644)
	reloaded, err := NewManager(mgr.CADir(), mgr.SitesDir())
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	if err := reloaded.EnsureCA(); err != nil {
		t.Fatalf("reloaded EnsureCA: %v", err)
	}
	if _, err := reloaded.EnsureSiteCert("app:perms:1", []string{"perms.example.com"}, t0); err != nil {
		t.Fatalf("reloaded EnsureSiteCert: %v", err)
	}
	checkPerm(mgr.CAKeyPath(), PrivateKeyMode)
	checkPerm(paths.KeyPath, PrivateKeyMode)
}
