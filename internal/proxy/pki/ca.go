// Package pki manages the NextDeploy internal certificate authority (CA) and
// per-site leaf TLS certificates for internal origin HTTPS with non-Caddy
// proxy drivers (such as Angie or Ferron).
//
// Private CA keys live in CADir (0700 dir, 0600 key), isolated from SitesDir
// (0755) so proxy containers mounting SitesDir read-only never see ca.key.
// Leaf keys use 0600 permissions: panel and proxy containers (Angie master /
// user root, Ferron user 0:0) run as UID 0 and read 0600 keys via read-only
// mounts without exposing private keys to unprivileged host users.
package pki

import (
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	// DefaultCADir is the default directory storing the NextDeploy private CA keypair.
	DefaultCADir = "/data/proxy/pki/ca"
	// DefaultSitesDir is the default directory storing per-site leaf certificates.
	DefaultSitesDir = "/data/proxy/pki/sites"
	// CACertFilename is the filename of the PEM-encoded root CA certificate.
	CACertFilename = "ca.crt"
	// CAKeyFilename is the filename of the PEM-encoded root CA private key.
	CAKeyFilename = "ca.key"
	// CALifetime is the validity duration of a generated root CA (10 years).
	CALifetime = 10 * 365 * 24 * time.Hour
	// LeafLifetime is the validity duration of issued site leaf certificates (90 days).
	LeafLifetime = 90 * 24 * time.Hour
	// RenewBefore triggers leaf renewal when remaining validity is <= 30 days.
	RenewBefore = 30 * 24 * time.Hour
	// CADirMode restricts the private CA directory to the owner only.
	CADirMode os.FileMode = 0o700
	// SitesDirMode allows proxy containers to traverse the leaf cert directory.
	SitesDirMode os.FileMode = 0o755
	// PrivateKeyMode restricts private key files to owner read/write only.
	PrivateKeyMode os.FileMode = 0o600
	// CertMode grants read access to public X.509 certificate files.
	CertMode os.FileMode = 0o644
)

var (
	// ErrInvalidDir indicates invalid or overlapping CA and site certificate directories.
	ErrInvalidDir = errors.New("pki: invalid directory configuration")
	// ErrInvalidCA indicates a corrupted, incomplete, or mismatched CA keypair.
	ErrInvalidCA = errors.New("pki: invalid certificate authority")
	// ErrInvalidSiteID indicates an empty or unsafe site identifier.
	ErrInvalidSiteID = errors.New("pki: invalid site identifier")
	// ErrNoHosts indicates no valid SAN hostnames or IP addresses were provided.
	ErrNoHosts = errors.New("pki: at least one host is required")
	// ErrInvalidHost indicates a malformed hostname or IP SAN entry.
	ErrInvalidHost = errors.New("pki: invalid SAN host")
)

// Manager provisions and caches the NextDeploy internal root CA and issues or
// renews per-site leaf TLS certificates. All methods are safe for concurrent use.
type Manager struct {
	caDir       string
	sitesDir    string
	now         func() time.Time
	caMu        sync.RWMutex
	caCert      *x509.Certificate
	caKey       *ecdsa.PrivateKey
	caPEM       string
	pruneMu     sync.RWMutex
	siteLocksMu sync.Mutex
	siteLocks   map[string]*sync.Mutex
}

// NewManager constructs a PKI Manager storing the root CA in caDir and per-site
// leaf certificates in sitesDir. caDir and sitesDir must be non-empty and
// disjoint so mounting sitesDir into a proxy container never exposes ca.key.
func NewManager(caDir, sitesDir string) (*Manager, error) {
	caClean, sitesClean := strings.TrimSpace(caDir), strings.TrimSpace(sitesDir)
	if caClean == "" || sitesClean == "" {
		return nil, fmt.Errorf("%w: caDir and sitesDir are required", ErrInvalidDir)
	}
	caAbs, err := filepath.Abs(filepath.Clean(caClean))
	if err != nil {
		return nil, fmt.Errorf("%w: resolve caDir: %v", ErrInvalidDir, err)
	}
	sitesAbs, err := filepath.Abs(filepath.Clean(sitesClean))
	if err != nil {
		return nil, fmt.Errorf("%w: resolve sitesDir: %v", ErrInvalidDir, err)
	}
	if isSameOrSubdir(caAbs, sitesAbs) || isSameOrSubdir(sitesAbs, caAbs) {
		return nil, fmt.Errorf("%w: caDir (%s) and sitesDir (%s) must be disjoint", ErrInvalidDir, caAbs, sitesAbs)
	}
	return &Manager{caDir: caAbs, sitesDir: sitesAbs, now: time.Now, siteLocks: make(map[string]*sync.Mutex)}, nil
}

// CADir returns the absolute path to the private CA directory.
func (m *Manager) CADir() string { return m.caDir }

// SitesDir returns the absolute path to the leaf certificates directory.
func (m *Manager) SitesDir() string { return m.sitesDir }

// CACertPath returns the full filesystem path to ca.crt inside CADir.
func (m *Manager) CACertPath() string { return filepath.Join(m.caDir, CACertFilename) }

// CAKeyPath returns the full filesystem path to ca.key inside CADir.
func (m *Manager) CAKeyPath() string { return filepath.Join(m.caDir, CAKeyFilename) }

// EnsureCA loads the existing ECDSA P-256 root CA from CADir or creates a new
// 10-year root CA when neither ca.crt nor ca.key exists, enforcing 0700 on
// CADir, 0600 on ca.key, and 0644 on ca.crt on POSIX platforms.
func (m *Manager) EnsureCA() error {
	_, _, _, err := m.ensureCALocked()
	return err
}

// OriginCACertPEM ensures the NextDeploy internal CA exists and returns its
// PEM-encoded public certificate with a single trailing newline for nd-edge.
func (m *Manager) OriginCACertPEM() (string, error) {
	_, _, pemStr, err := m.ensureCALocked()
	return pemStr, err
}

func (m *Manager) ensureCALocked() (*x509.Certificate, *ecdsa.PrivateKey, string, error) {
	m.caMu.Lock()
	defer m.caMu.Unlock()
	if err := ensureDirMode(m.caDir, CADirMode, true); err != nil {
		return nil, nil, "", err
	}
	if m.caCert != nil && m.caKey != nil && m.caPEM != "" {
		if _, err := os.Stat(m.CACertPath()); err == nil {
			if _, err := os.Stat(m.CAKeyPath()); err == nil {
				return m.caCert, m.caKey, m.caPEM, nil
			}
		}
		m.caCert, m.caKey, m.caPEM = nil, nil, ""
	}
	cert, key, pemStr, found, err := m.loadExistingCA()
	if err != nil {
		return nil, nil, "", err
	}
	if !found {
		if cert, key, pemStr, err = m.createCA(); err != nil {
			return nil, nil, "", err
		}
	}
	m.caCert, m.caKey, m.caPEM = cert, key, pemStr
	return cert, key, pemStr, nil
}

func (m *Manager) loadExistingCA() (*x509.Certificate, *ecdsa.PrivateKey, string, bool, error) {
	certBytes, certErr := os.ReadFile(m.CACertPath())
	keyBytes, keyErr := os.ReadFile(m.CAKeyPath())
	if errors.Is(certErr, os.ErrNotExist) && errors.Is(keyErr, os.ErrNotExist) {
		return nil, nil, "", false, nil
	}
	if certErr != nil {
		return nil, nil, "", false, fmt.Errorf("%w: read %s: %v", ErrInvalidCA, m.CACertPath(), certErr)
	}
	if keyErr != nil {
		return nil, nil, "", false, fmt.Errorf("%w: read %s: %v", ErrInvalidCA, m.CAKeyPath(), keyErr)
	}
	cert, err := parseCertPEM(certBytes)
	if err != nil {
		return nil, nil, "", false, fmt.Errorf("%w: parse ca.crt: %v", ErrInvalidCA, err)
	}
	key, err := parseECKeyPEM(keyBytes)
	if err != nil {
		return nil, nil, "", false, fmt.Errorf("%w: parse ca.key: %v", ErrInvalidCA, err)
	}
	if !cert.IsCA || !cert.BasicConstraintsValid || (cert.KeyUsage&x509.KeyUsageCertSign) == 0 {
		return nil, nil, "", false, fmt.Errorf("%w: certificate lacks CA constraints or certSign usage", ErrInvalidCA)
	}
	if !keysMatch(key, cert.PublicKey) {
		return nil, nil, "", false, fmt.Errorf("%w: ca.key does not match ca.crt public key", ErrInvalidCA)
	}
	if m.currentTime().After(cert.NotAfter) {
		return nil, nil, "", false, fmt.Errorf("%w: ca.crt expired at %s", ErrInvalidCA, cert.NotAfter.UTC().Format(time.RFC3339))
	}
	if err := ensureFileMode(m.CAKeyPath(), PrivateKeyMode); err != nil {
		return nil, nil, "", false, err
	}
	if err := ensureFileMode(m.CACertPath(), CertMode); err != nil {
		return nil, nil, "", false, err
	}
	return cert, key, strings.TrimSpace(string(encodePEM("CERTIFICATE", cert.Raw))) + "\n", true, nil
}

func (m *Manager) createCA() (*x509.Certificate, *ecdsa.PrivateKey, string, error) {
	key, serial, ski, err := generateKeyMaterial()
	if err != nil {
		return nil, nil, "", err
	}
	now := m.currentTime().UTC()
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "NextDeploy Internal Origin CA", Organization: []string{"NextDeploy"}},
		NotBefore:             now.Add(-5 * time.Minute),
		NotAfter:              now.Add(CALifetime),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
		SubjectKeyId:          ski,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, nil, "", fmt.Errorf("sign CA certificate: %w", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, nil, "", fmt.Errorf("parse generated CA certificate: %w", err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, nil, "", fmt.Errorf("marshal CA key: %w", err)
	}
	certPEM := encodePEM("CERTIFICATE", der)
	if err := atomicWriteFile(m.CAKeyPath(), encodePEM("EC PRIVATE KEY", keyDER), PrivateKeyMode); err != nil {
		return nil, nil, "", fmt.Errorf("write ca.key: %w", err)
	}
	if err := atomicWriteFile(m.CACertPath(), certPEM, CertMode); err != nil {
		return nil, nil, "", fmt.Errorf("write ca.crt: %w", err)
	}
	return cert, key, strings.TrimSpace(string(certPEM)) + "\n", nil
}

func (m *Manager) currentTime() time.Time {
	if m.now != nil {
		return m.now()
	}
	return time.Now()
}
