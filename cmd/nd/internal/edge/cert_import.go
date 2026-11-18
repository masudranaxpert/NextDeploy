package edge

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// DefaultManualCertRenewWindow is the remaining lifetime threshold below which
// an imported manual certificate is considered near-expiry so ACME takes over.
const DefaultManualCertRenewWindow = 30 * 24 * time.Hour

// DiscoveredCert represents a valid certificate+key pair found in proxy storage.
type DiscoveredCert struct {
	Host      string
	CertPEM   []byte
	KeyPEM    []byte
	NotBefore time.Time
	NotAfter  time.Time
	Issuer    string
	Leaf      *x509.Certificate
}

// ImportedCert records a certificate pair written to the target engine's manual
// certificate directory (/etc/<engine>/nd-edge/certs/).
type ImportedCert struct {
	Host      string    `json:"host"`
	CertPath  string    `json:"cert_path"`
	KeyPath   string    `json:"key_path"`
	NotBefore time.Time `json:"not_before"`
	NotAfter  time.Time `json:"not_after"`
	Issuer    string    `json:"issuer"`
}

// DiscoverCaddyHostCerts scans searchDirs (such as Caddy's certificate store
// and /etc/caddy) for valid, non-expired <host>.crt + <host>.key pairs.
func DiscoverCaddyHostCerts(searchDirs []string, now time.Time) ([]DiscoveredCert, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	byHost := make(map[string]DiscoveredCert)
	seenDirs := make(map[string]struct{}, len(searchDirs))

	for _, rawDir := range searchDirs {
		dir := strings.TrimSpace(rawDir)
		if dir == "" {
			continue
		}
		cleanDir := filepath.Clean(dir)
		if _, seen := seenDirs[cleanDir]; seen {
			continue
		}
		seenDirs[cleanDir] = struct{}{}

		info, err := os.Stat(cleanDir)
		if err != nil || !info.IsDir() {
			continue
		}

		walkErr := filepath.WalkDir(cleanDir, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			name := d.Name()
			if !strings.HasSuffix(strings.ToLower(name), ".crt") {
				return nil
			}
			stem := strings.TrimSuffix(name, filepath.Ext(name))
			keyPath := filepath.Join(filepath.Dir(path), stem+".key")
			if _, statErr := os.Stat(keyPath); statErr != nil {
				return nil
			}

			certPEM, err := os.ReadFile(path)
			if err != nil {
				return nil
			}
			keyPEM, err := os.ReadFile(keyPath)
			if err != nil {
				return nil
			}
			leaf, err := parseAndVerifyKeyPair(certPEM, keyPEM, now)
			if err != nil {
				return nil
			}

			hosts := matchedHostsForCert(stem, leaf)
			for _, host := range hosts {
				existing, ok := byHost[host]
				if !ok || leaf.NotAfter.After(existing.NotAfter) {
					byHost[host] = DiscoveredCert{
						Host:      host,
						CertPEM:   certPEM,
						KeyPEM:    keyPEM,
						NotBefore: leaf.NotBefore.UTC(),
						NotAfter:  leaf.NotAfter.UTC(),
						Issuer:    leafIssuerName(leaf),
						Leaf:      leaf,
					}
				}
			}
			return nil
		})
		if walkErr != nil {
			return nil, walkErr
		}
	}

	out := make([]DiscoveredCert, 0, len(byHost))
	for _, dc := range byHost {
		out = append(out, dc)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Host < out[j].Host })
	return out, nil
}

// ImportCaddyCertsToDir discovers valid Caddy certificates from caddyCertsDir
// and caddyEtcDir and writes <host>.crt (0644) and <host>.key (0600) into
// targetCertsDir.
func ImportCaddyCertsToDir(caddyCertsDir, caddyEtcDir, targetCertsDir string, now time.Time) ([]ImportedCert, error) {
	if strings.TrimSpace(targetCertsDir) == "" {
		return nil, errors.New("target certificate directory is required")
	}
	var search []string
	if caddyCertsDir != "" {
		search = append(search, filepath.Join(caddyCertsDir, "certificates"), caddyCertsDir)
	}
	if caddyEtcDir != "" {
		search = append(search, caddyEtcDir)
	}

	discovered, err := DiscoverCaddyHostCerts(search, now)
	if err != nil {
		return nil, fmt.Errorf("discover caddy certs: %w", err)
	}
	if len(discovered) == 0 {
		return nil, nil
	}

	if err := os.MkdirAll(targetCertsDir, 0o750); err != nil {
		return nil, fmt.Errorf("mkdir %s: %w", targetCertsDir, err)
	}

	imported := make([]ImportedCert, 0, len(discovered))
	for _, dc := range discovered {
		if _, verr := NormalizeAndValidateHost(dc.Host, ""); verr != nil {
			continue
		}
		certPath := filepath.Join(targetCertsDir, dc.Host+".crt")
		keyPath := filepath.Join(targetCertsDir, dc.Host+".key")
		if err := WriteFileAtomic(certPath, dc.CertPEM, 0o644); err != nil {
			return nil, fmt.Errorf("write imported cert %s: %w", certPath, err)
		}
		if err := WriteFileAtomic(keyPath, dc.KeyPEM, 0o600); err != nil {
			return nil, fmt.Errorf("write imported key %s: %w", keyPath, err)
		}
		imported = append(imported, ImportedCert{
			Host:      dc.Host,
			CertPath:  certPath,
			KeyPath:   keyPath,
			NotBefore: dc.NotBefore,
			NotAfter:  dc.NotAfter,
			Issuer:    dc.Issuer,
		})
	}
	return imported, nil
}

// LoadUsableManualCert checks whether targetCertsDir/<host>.{crt,key} exists,
// matches host, forms a valid X.509 key pair, and has more than renewWindow
// remaining before expiry.
func LoadUsableManualCert(certsDir, host string, now time.Time, renewWindow time.Duration) (certPath, keyPath string, leaf *x509.Certificate, ok bool) {
	if strings.TrimSpace(certsDir) == "" {
		return "", "", nil, false
	}
	normHost, verr := NormalizeAndValidateHost(host, "")
	if verr != nil {
		return "", "", nil, false
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if renewWindow < 0 {
		renewWindow = 0
	}

	certPath = filepath.Join(certsDir, normHost+".crt")
	keyPath = filepath.Join(certsDir, normHost+".key")

	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		return "", "", nil, false
	}
	keyPEM, err := os.ReadFile(keyPath)
	if err != nil {
		return "", "", nil, false
	}
	parsed, err := parseAndVerifyKeyPair(certPEM, keyPEM, now)
	if err != nil {
		return "", "", nil, false
	}
	if parsed.VerifyHostname(normHost) != nil {
		return "", "", nil, false
	}
	if parsed.NotAfter.Sub(now) <= renewWindow {
		return "", "", nil, false
	}
	return certPath, keyPath, parsed, true
}

func parseAndVerifyKeyPair(certPEM, keyPEM []byte, now time.Time) (*x509.Certificate, error) {
	if _, err := tls.X509KeyPair(certPEM, keyPEM); err != nil {
		return nil, err
	}
	block, _ := pem.Decode(certPEM)
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, errors.New("missing CERTIFICATE PEM block")
	}
	leaf, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, err
	}
	if now.Before(leaf.NotBefore) || !now.Before(leaf.NotAfter) {
		return nil, errors.New("certificate is expired or not yet valid")
	}
	return leaf, nil
}

func matchedHostsForCert(stem string, leaf *x509.Certificate) []string {
	if norm, verr := NormalizeAndValidateHost(stem, ""); verr == nil {
		if leaf.VerifyHostname(norm) == nil {
			return []string{norm}
		}
	}
	var out []string
	seen := make(map[string]struct{})
	for _, dnsName := range leaf.DNSNames {
		if norm, verr := NormalizeAndValidateHost(dnsName, ""); verr == nil {
			if _, ok := seen[norm]; !ok {
				seen[norm] = struct{}{}
				out = append(out, norm)
			}
		}
	}
	sort.Strings(out)
	return out
}

func leafIssuerName(leaf *x509.Certificate) string {
	if leaf == nil {
		return ""
	}
	if leaf.Issuer.CommonName != "" {
		return leaf.Issuer.CommonName
	}
	if len(leaf.Issuer.Organization) > 0 {
		return leaf.Issuer.Organization[0]
	}
	return leaf.Issuer.String()
}
