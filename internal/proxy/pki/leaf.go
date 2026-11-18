package pki

import (
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// CertPaths holds the filesystem paths and renewal metadata for a site's leaf
// certificate and private key inside SitesDir.
type CertPaths struct {
	SiteID   string    // Logical site identifier passed to EnsureSiteCert.
	CertPath string    // Path to the PEM-encoded X.509 leaf certificate (.crt).
	KeyPath  string    // Path to the PEM-encoded ECDSA private key (.key).
	NotAfter time.Time // Certificate expiration timestamp in UTC.
	Renewed  bool      // True when EnsureSiteCert newly issued or renewed the certificate.
}

// SafeSiteStem converts an arbitrary site ID (such as "app:1:dom:2") into a
// deterministic, collision-free, cross-platform filename stem safe on POSIX and
// Windows filesystems.
func SafeSiteStem(siteID string) (string, error) {
	if strings.TrimSpace(siteID) == "" || siteID == "." || siteID == ".." || strings.Contains(siteID, "..") {
		return "", fmt.Errorf("%w: %q", ErrInvalidSiteID, siteID)
	}
	for i := 0; i < len(siteID); i++ {
		b := siteID[i]
		if b < 0x20 || b == 0x7f || b == '/' || b == '\\' {
			return "", fmt.Errorf("%w: unsafe character in %q", ErrInvalidSiteID, siteID)
		}
	}
	var b strings.Builder
	modified := false
	for _, r := range siteID {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
			modified = true
		}
	}
	stem := strings.Trim(b.String(), "_")
	if stem == "" {
		stem = "site"
		modified = true
	}
	if len(stem) > 64 {
		stem = strings.TrimRight(stem[:64], "_")
		modified = true
	}
	if modified || isWindowsReservedStem(stem) {
		sum := sha256.Sum256([]byte(siteID))
		return stem + "_h" + hex.EncodeToString(sum[:4]), nil
	}
	return stem, nil
}

// SiteCertPaths returns the expected leaf certificate and private key paths for
// siteID without creating or inspecting files on disk.
func (m *Manager) SiteCertPaths(siteID string) (CertPaths, error) {
	stem, err := SafeSiteStem(siteID)
	if err != nil {
		return CertPaths{}, err
	}
	return CertPaths{
		SiteID:   siteID,
		CertPath: filepath.Join(m.sitesDir, stem+".crt"),
		KeyPath:  filepath.Join(m.sitesDir, stem+".key"),
	}, nil
}

// EnsureSiteCert ensures a valid ECDSA P-256 leaf TLS certificate and private
// key signed by the NextDeploy CA exist for siteID covering hosts. It renews
// when missing, invalid, mismatched against hosts, or with <= RenewBefore left.
func (m *Manager) EnsureSiteCert(siteID string, hosts []string, now time.Time) (CertPaths, error) {
	paths, err := m.SiteCertPaths(siteID)
	if err != nil {
		return CertPaths{}, err
	}
	wantDNS, wantIPs, err := normalizeHosts(hosts)
	if err != nil {
		return CertPaths{}, err
	}
	if now.IsZero() {
		now = m.currentTime()
	}
	now = now.UTC()

	m.pruneMu.RLock()
	defer m.pruneMu.RUnlock()

	lock := m.siteLock(paths.CertPath)
	lock.Lock()
	defer lock.Unlock()

	if err := ensureDirMode(m.sitesDir, SitesDirMode, false); err != nil {
		return CertPaths{}, err
	}
	caCert, caKey, _, err := m.ensureCALocked()
	if err != nil {
		return CertPaths{}, err
	}

	if existing, ok := validExistingLeaf(paths.CertPath, paths.KeyPath, caCert, wantDNS, wantIPs, now); ok {
		if err := ensureFileMode(paths.KeyPath, PrivateKeyMode); err != nil {
			return CertPaths{}, err
		}
		if err := ensureFileMode(paths.CertPath, CertMode); err != nil {
			return CertPaths{}, err
		}
		paths.NotAfter = existing.NotAfter
		paths.Renewed = false
		return paths, nil
	}

	issued, err := issueLeafCert(paths.CertPath, paths.KeyPath, caCert, caKey, wantDNS, wantIPs, now)
	if err != nil {
		return CertPaths{}, err
	}
	paths.NotAfter = issued.NotAfter
	paths.Renewed = true
	return paths, nil
}

// PruneOrphans deletes .crt and .key files in SitesDir whose siteID is absent or
// false in activeSiteIDs while leaving active site certificates untouched.
func (m *Manager) PruneOrphans(activeSiteIDs map[string]bool) error {
	keep := make(map[string]bool, len(activeSiteIDs))
	for id, active := range activeSiteIDs {
		if !active {
			continue
		}
		stem, err := SafeSiteStem(id)
		if err != nil {
			return fmt.Errorf("prune active site %q: %w", id, err)
		}
		keep[stem] = true
	}

	m.pruneMu.Lock()
	defer m.pruneMu.Unlock()

	entries, err := os.ReadDir(m.sitesDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read sites dir %s: %w", m.sitesDir, err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasPrefix(name, ".tmp-") {
			continue
		}
		ext := filepath.Ext(name)
		if ext != ".crt" && ext != ".key" {
			continue
		}
		stem := strings.TrimSuffix(name, ext)
		if !keep[stem] {
			if err := os.Remove(filepath.Join(m.sitesDir, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("remove orphan %s: %w", name, err)
			}
		}
	}
	return nil
}

func (m *Manager) siteLock(key string) *sync.Mutex {
	m.siteLocksMu.Lock()
	defer m.siteLocksMu.Unlock()
	mu, ok := m.siteLocks[key]
	if !ok {
		mu = &sync.Mutex{}
		m.siteLocks[key] = mu
	}
	return mu
}

func validExistingLeaf(certPath, keyPath string, caCert *x509.Certificate, wantDNS []string, wantIPs []net.IP, now time.Time) (*x509.Certificate, bool) {
	certBytes, err := os.ReadFile(certPath)
	if err != nil {
		return nil, false
	}
	keyBytes, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, false
	}
	cert, err := parseCertPEM(certBytes)
	if err != nil {
		return nil, false
	}
	key, err := parseECKeyPEM(keyBytes)
	if err != nil {
		return nil, false
	}
	if !keysMatch(key, cert.PublicKey) || now.Before(cert.NotBefore) || cert.NotAfter.Sub(now) <= RenewBefore {
		return nil, false
	}
	pool := x509.NewCertPool()
	pool.AddCert(caCert)
	if _, err := cert.Verify(x509.VerifyOptions{
		Roots:       pool,
		CurrentTime: now,
		KeyUsages:   []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}); err != nil {
		return nil, false
	}
	if !sameSANs(cert, wantDNS, wantIPs) {
		return nil, false
	}
	return cert, true
}

func issueLeafCert(certPath, keyPath string, caCert *x509.Certificate, caKey *ecdsa.PrivateKey, wantDNS []string, wantIPs []net.IP, now time.Time) (*x509.Certificate, error) {
	key, serial, ski, err := generateKeyMaterial()
	if err != nil {
		return nil, err
	}
	cn := ""
	if len(wantDNS) > 0 {
		cn = wantDNS[0]
	} else if len(wantIPs) > 0 {
		cn = wantIPs[0].String()
	}
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: cn, Organization: []string{"NextDeploy"}},
		NotBefore:             now.Add(-5 * time.Minute),
		NotAfter:              now.Add(LeafLifetime),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  false,
		DNSNames:              wantDNS,
		IPAddresses:           wantIPs,
		SubjectKeyId:          ski,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, caCert, &key.PublicKey, caKey)
	if err != nil {
		return nil, fmt.Errorf("sign leaf certificate: %w", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, fmt.Errorf("parse leaf certificate: %w", err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("marshal leaf key: %w", err)
	}
	if err := atomicWriteFile(keyPath, encodePEM("EC PRIVATE KEY", keyDER), PrivateKeyMode); err != nil {
		return nil, fmt.Errorf("write leaf key: %w", err)
	}
	if err := atomicWriteFile(certPath, encodePEM("CERTIFICATE", der), CertMode); err != nil {
		return nil, fmt.Errorf("write leaf cert: %w", err)
	}
	return cert, nil
}
