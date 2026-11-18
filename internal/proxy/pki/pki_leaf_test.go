package pki

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"
)

func TestEnsureSiteCertAndChainVerification(t *testing.T) {
	mgr, caDir, sitesDir := newTestManager(t)
	t0 := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	mgr.now = func() time.Time { return t0 }

	hosts := []string{"WWW.Example.COM.", "*.example.com", "example.com", "10.0.0.5", "[2001:db8::1]", "10.0.0.5"}
	paths, err := mgr.EnsureSiteCert("app:app1:dom:1", hosts, t0)
	if err != nil {
		t.Fatalf("EnsureSiteCert: %v", err)
	}
	if !paths.Renewed {
		t.Fatalf("expected Renewed=true on initial issuance")
	}
	if filepath.Dir(paths.CertPath) != sitesDir || filepath.Dir(paths.KeyPath) != sitesDir {
		t.Fatalf("leaf files must live in sitesDir (%s), got cert=%s key=%s", sitesDir, paths.CertPath, paths.KeyPath)
	}
	if _, err := os.Stat(filepath.Join(caDir, CAKeyFilename)); err != nil {
		t.Fatalf("expected ca.key in caDir: %v", err)
	}

	certBytes, err := os.ReadFile(paths.CertPath)
	if err != nil {
		t.Fatalf("read leaf cert: %v", err)
	}
	keyBytes, err := os.ReadFile(paths.KeyPath)
	if err != nil {
		t.Fatalf("read leaf key: %v", err)
	}
	cert, err := parseCertPEM(certBytes)
	if err != nil {
		t.Fatalf("parse leaf cert: %v", err)
	}
	key, err := parseECKeyPEM(keyBytes)
	if err != nil {
		t.Fatalf("parse leaf key: %v", err)
	}
	if key.Curve != elliptic.P256() || !keysMatch(key, cert.PublicKey) {
		t.Fatalf("leaf key is not matching ECDSA P-256 key")
	}
	if cert.IsCA || !slices.Equal(cert.ExtKeyUsage, []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}) {
		t.Fatalf("unexpected leaf constraints: IsCA=%v ExtKeyUsage=%v", cert.IsCA, cert.ExtKeyUsage)
	}
	if got := cert.NotAfter.Sub(t0); got != LeafLifetime {
		t.Fatalf("leaf lifetime = %v, want %v", got, LeafLifetime)
	}

	wantDNS := []string{"*.example.com", "example.com", "www.example.com"}
	if !slices.Equal(cert.DNSNames, wantDNS) {
		t.Fatalf("DNSNames = %v, want %v", cert.DNSNames, wantDNS)
	}
	var gotIPs []string
	for _, ip := range cert.IPAddresses {
		gotIPs = append(gotIPs, ip.String())
	}
	slices.Sort(gotIPs)
	if !slices.Equal(gotIPs, []string{"10.0.0.5", "2001:db8::1"}) {
		t.Fatalf("IPAddresses = %v, want [10.0.0.5 2001:db8::1]", gotIPs)
	}

	caPEM, err := mgr.OriginCACertPEM()
	if err != nil {
		t.Fatalf("OriginCACertPEM: %v", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM([]byte(caPEM)) {
		t.Fatalf("failed to parse OriginCACertPEM")
	}
	for _, target := range []string{"example.com", "www.example.com", "api.example.com", "10.0.0.5", "2001:db8::1"} {
		if _, err := cert.Verify(x509.VerifyOptions{
			DNSName:     target,
			Roots:       pool,
			CurrentTime: t0.Add(time.Hour),
			KeyUsages:   []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		}); err != nil {
			t.Fatalf("chain verify for %q failed: %v", target, err)
		}
	}
}

func TestSafeSiteStemAndInputValidation(t *testing.T) {
	s1, err := SafeSiteStem("app:1")
	if err != nil {
		t.Fatalf("SafeSiteStem(app:1): %v", err)
	}
	s2, err := SafeSiteStem("app_1")
	if err != nil {
		t.Fatalf("SafeSiteStem(app_1): %v", err)
	}
	if s1 == s2 {
		t.Fatalf("SafeSiteStem collision between app:1 and app_1: %q", s1)
	}
	if nul, err := SafeSiteStem("nul"); err != nil || nul == "nul" {
		t.Fatalf("SafeSiteStem(nul) = (%q, %v), want suffixed safe stem", nul, err)
	}

	mgr, _, _ := newTestManager(t)
	now := time.Now()
	for _, badID := range []string{"", "   ", ".", "..", "../etc", "a/b", `a\b`, "bad\x00id"} {
		if _, err := mgr.EnsureSiteCert(badID, []string{"ok.example.com"}, now); !errors.Is(err, ErrInvalidSiteID) {
			t.Fatalf("siteID %q err = %v, want ErrInvalidSiteID", badID, err)
		}
	}
	if _, err := mgr.EnsureSiteCert("panel", nil, now); !errors.Is(err, ErrNoHosts) {
		t.Fatalf("empty hosts err = %v, want ErrNoHosts", err)
	}
	for _, badHost := range []string{"", "http://example.com", "example.com:80", "*", "*.*.example.com", "bad host", "foo..bar"} {
		if _, err := mgr.EnsureSiteCert("panel", []string{badHost}, now); !errors.Is(err, ErrInvalidHost) {
			t.Fatalf("host %q err = %v, want ErrInvalidHost", badHost, err)
		}
	}
}

func TestEnsureSiteCertReuseAndRenewal(t *testing.T) {
	mgr, _, _ := newTestManager(t)
	t0 := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	mgr.now = func() time.Time { return t0 }

	hosts := []string{"app.example.com", "www.app.example.com", "192.0.2.10"}
	first, err := mgr.EnsureSiteCert("php:blog", hosts, t0)
	if err != nil {
		t.Fatalf("initial EnsureSiteCert: %v", err)
	}
	origCertBytes, _ := os.ReadFile(first.CertPath)
	origCert, _ := parseCertPEM(origCertBytes)

	// Reordered hosts & > 30 days remaining must reuse existing cert without re-issue.
	reused, err := mgr.EnsureSiteCert("php:blog", []string{"192.0.2.10", "WWW.APP.EXAMPLE.COM", "app.example.com"}, t0.Add(LeafLifetime-RenewBefore-time.Second))
	if err != nil {
		t.Fatalf("reuse EnsureSiteCert: %v", err)
	}
	if reused.Renewed {
		t.Fatalf("expected Renewed=false when >30d remaining and SANs match")
	}
	afterReuseBytes, _ := os.ReadFile(first.CertPath)
	if !bytes.Equal(origCertBytes, afterReuseBytes) {
		t.Fatalf("cert file mutated despite Renewed=false")
	}

	// Exactly 30 days left (<= RenewBefore) triggers automatic renewal.
	tRenew := t0.Add(LeafLifetime - RenewBefore)
	renewed, err := mgr.EnsureSiteCert("php:blog", hosts, tRenew)
	if err != nil {
		t.Fatalf("boundary renewal EnsureSiteCert: %v", err)
	}
	if !renewed.Renewed {
		t.Fatalf("expected Renewed=true at exact RenewBefore threshold")
	}
	renewedCertBytes, _ := os.ReadFile(renewed.CertPath)
	renewedCert, _ := parseCertPEM(renewedCertBytes)
	if renewedCert.SerialNumber.Cmp(origCert.SerialNumber) == 0 {
		t.Fatalf("expected new serial number on renewal")
	}

	// Changing SAN list triggers renewal even with plenty of validity left.
	sanChanged, err := mgr.EnsureSiteCert("php:blog", []string{"app.example.com"}, tRenew.Add(time.Hour))
	if err != nil {
		t.Fatalf("SAN change EnsureSiteCert: %v", err)
	}
	if !sanChanged.Renewed {
		t.Fatalf("expected Renewed=true when SAN list changes")
	}

	// Corrupt cert or leaf signed by an unknown CA triggers automatic re-issuance.
	_ = os.WriteFile(sanChanged.CertPath, []byte("not-a-cert"), CertMode)
	repaired, err := mgr.EnsureSiteCert("php:blog", []string{"app.example.com"}, tRenew.Add(2*time.Hour))
	if err != nil || !repaired.Renewed {
		t.Fatalf("corrupt cert repair = (%+v, %v), want Renewed=true", repaired, err)
	}

	foreignKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	foreignDER, _ := x509.CreateCertificate(rand.Reader, &x509.Certificate{
		SerialNumber: big.NewInt(99),
		Subject:      pkix.Name{CommonName: "app.example.com"},
		NotBefore:    tRenew,
		NotAfter:     tRenew.Add(LeafLifetime),
		DNSNames:     []string{"app.example.com"},
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}, &x509.Certificate{SerialNumber: big.NewInt(98)}, &foreignKey.PublicKey, foreignKey)
	foreignKeyDER, _ := x509.MarshalECPrivateKey(foreignKey)
	_ = os.WriteFile(repaired.KeyPath, encodePEM("EC PRIVATE KEY", foreignKeyDER), PrivateKeyMode)
	_ = os.WriteFile(repaired.CertPath, encodePEM("CERTIFICATE", foreignDER), CertMode)
	resigned, err := mgr.EnsureSiteCert("php:blog", []string{"app.example.com"}, tRenew.Add(3*time.Hour))
	if err != nil || !resigned.Renewed {
		t.Fatalf("foreign-CA cert replacement = (%+v, %v), want Renewed=true", resigned, err)
	}
}

func TestEnsureSiteCertConcurrentAndPruneOrphans(t *testing.T) {
	mgr, _, sitesDir := newTestManager(t)
	now := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	mgr.now = func() time.Time { return now }

	const workers = 12
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			id := fmt.Sprintf("app:race:%d", idx%2)
			hosts := []string{fmt.Sprintf("s%d.example.com", idx%2)}
			if _, err := mgr.EnsureSiteCert(id, hosts, now); err != nil {
				errs <- err
				return
			}
			if _, err := mgr.OriginCACertPEM(); err != nil {
				errs <- err
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent PKI error: %v", err)
	}

	pPanel, err := mgr.EnsureSiteCert("panel", []string{"panel.example.com"}, now)
	if err != nil {
		t.Fatalf("panel cert: %v", err)
	}
	pKeep, err := mgr.EnsureSiteCert("app:1:dom:2", []string{"keep.example.com"}, now)
	if err != nil {
		t.Fatalf("keep cert: %v", err)
	}
	pDrop, err := mgr.EnsureSiteCert("php:deleted", []string{"drop.example.com"}, now)
	if err != nil {
		t.Fatalf("drop cert: %v", err)
	}
	marker := filepath.Join(sitesDir, "README.txt")
	if err := os.WriteFile(marker, []byte("keep"), 0o644); err != nil {
		t.Fatalf("write marker: %v", err)
	}

	if err := mgr.PruneOrphans(map[string]bool{
		"panel":       true,
		"app:1:dom:2": true,
		"php:deleted": false,
	}); err != nil {
		t.Fatalf("PruneOrphans: %v", err)
	}

	for _, exist := range []string{pPanel.CertPath, pPanel.KeyPath, pKeep.CertPath, pKeep.KeyPath, marker, mgr.CACertPath(), mgr.CAKeyPath()} {
		if _, err := os.Stat(exist); err != nil {
			t.Fatalf("expected %s to remain after prune: %v", exist, err)
		}
	}
	for _, gone := range []string{pDrop.CertPath, pDrop.KeyPath} {
		if _, err := os.Stat(gone); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("expected orphan %s to be removed, stat err = %v", gone, err)
		}
	}
}
