package pki

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
)

func isSameOrSubdir(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	return err == nil && (rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))))
}

func ensureDirMode(dir string, mode os.FileMode, enforce bool) error {
	if err := os.MkdirAll(dir, mode); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	if enforce && runtime.GOOS != "windows" {
		if err := os.Chmod(dir, mode); err != nil {
			return fmt.Errorf("chmod %s: %w", dir, err)
		}
	}
	return nil
}

func ensureFileMode(path string, mode os.FileMode) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	if err := os.Chmod(path, mode); err != nil {
		return fmt.Errorf("chmod %s: %w", path, err)
	}
	return nil
}

func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tmp-pki-*")
	if err != nil {
		return err
	}
	tmpPath, cleanup := tmp.Name(), true
	defer func() {
		if cleanup {
			_ = tmp.Close()
			_ = os.Remove(tmpPath)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		return err
	}
	if runtime.GOOS != "windows" {
		if err := tmp.Chmod(perm); err != nil {
			return err
		}
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	cleanup = false
	if runtime.GOOS != "windows" {
		if d, err := os.Open(dir); err == nil {
			_ = d.Sync()
			_ = d.Close()
		}
	}
	return nil
}

func generateKeyMaterial() (*ecdsa.PrivateKey, *big.Int, []byte, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("generate ECDSA P-256 key: %w", err)
	}
	n, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 127))
	if err != nil {
		return nil, nil, nil, fmt.Errorf("generate serial: %w", err)
	}
	spki, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("marshal public key: %w", err)
	}
	sum := sha256.Sum256(spki)
	return key, n.Add(n, big.NewInt(1)), sum[:20], nil
}

func encodePEM(blockType string, der []byte) []byte {
	var buf bytes.Buffer
	_ = pem.Encode(&buf, &pem.Block{Type: blockType, Bytes: der})
	return buf.Bytes()
}

func parseCertPEM(raw []byte) (*x509.Certificate, error) {
	block, _ := pem.Decode(raw)
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, errors.New("missing CERTIFICATE PEM block")
	}
	return x509.ParseCertificate(block.Bytes)
}

func parseECKeyPEM(raw []byte) (*ecdsa.PrivateKey, error) {
	block, _ := pem.Decode(raw)
	if block == nil {
		return nil, errors.New("missing private key PEM block")
	}
	if key, err := x509.ParseECPrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	ecKey, ok := parsed.(*ecdsa.PrivateKey)
	if !ok {
		return nil, errors.New("private key is not ECDSA")
	}
	return ecKey, nil
}

func keysMatch(priv *ecdsa.PrivateKey, pub any) bool {
	ecPub, ok := pub.(*ecdsa.PublicKey)
	return ok && priv.PublicKey.Equal(ecPub)
}

func normalizeHosts(hosts []string) ([]string, []net.IP, error) {
	if len(hosts) == 0 {
		return nil, nil, ErrNoHosts
	}
	dnsSeen := make(map[string]bool)
	ipSeen := make(map[string]bool)
	var dnsNames []string
	var ipAddrs []net.IP

	for _, raw := range hosts {
		h := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(raw)), ".")
		if h == "" {
			return nil, nil, fmt.Errorf("%w: empty host", ErrInvalidHost)
		}
		if ip := net.ParseIP(strings.TrimSuffix(strings.TrimPrefix(h, "["), "]")); ip != nil {
			if ip4 := ip.To4(); ip4 != nil {
				ip = ip4
			}
			key := ip.String()
			if !ipSeen[key] {
				ipSeen[key] = true
				ipAddrs = append(ipAddrs, ip)
			}
			continue
		}
		if !isValidDNSName(h) {
			return nil, nil, fmt.Errorf("%w: %q", ErrInvalidHost, raw)
		}
		if !dnsSeen[h] {
			dnsSeen[h] = true
			dnsNames = append(dnsNames, h)
		}
	}
	if len(dnsNames) == 0 && len(ipAddrs) == 0 {
		return nil, nil, ErrNoHosts
	}
	slices.Sort(dnsNames)
	slices.SortFunc(ipAddrs, func(a, b net.IP) int { return strings.Compare(a.String(), b.String()) })
	return dnsNames, ipAddrs, nil
}

func isValidDNSName(h string) bool {
	if len(h) == 0 || len(h) > 253 || strings.ContainsAny(h, " \t\r\n/\\:@[]") || strings.Contains(h, "..") {
		return false
	}
	labels := strings.Split(h, ".")
	for i, lbl := range labels {
		if i == 0 && lbl == "*" {
			if len(labels) < 2 {
				return false
			}
			continue
		}
		if len(lbl) == 0 || len(lbl) > 63 || lbl[0] == '-' || lbl[len(lbl)-1] == '-' {
			return false
		}
		for j := 0; j < len(lbl); j++ {
			c := lbl[j]
			if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-') {
				return false
			}
		}
	}
	return true
}

func sameSANs(cert *x509.Certificate, wantDNS []string, wantIPs []net.IP) bool {
	gotDNS := make([]string, len(cert.DNSNames))
	for i, d := range cert.DNSNames {
		gotDNS[i] = strings.ToLower(d)
	}
	slices.Sort(gotDNS)
	if !slices.Equal(gotDNS, wantDNS) {
		return false
	}
	gotIPs := make([]string, len(cert.IPAddresses))
	for i, ip := range cert.IPAddresses {
		if ip4 := ip.To4(); ip4 != nil {
			ip = ip4
		}
		gotIPs[i] = ip.String()
	}
	slices.Sort(gotIPs)
	wantIPStrs := make([]string, len(wantIPs))
	for i, ip := range wantIPs {
		wantIPStrs[i] = ip.String()
	}
	return slices.Equal(gotIPs, wantIPStrs)
}

func isWindowsReservedStem(stem string) bool {
	switch strings.ToUpper(stem) {
	case "CON", "PRN", "AUX", "NUL",
		"COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
		"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
		return true
	default:
		return false
	}
}
