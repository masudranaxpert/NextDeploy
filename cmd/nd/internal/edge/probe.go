package edge

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	certIssuingTimeout    = 15 * time.Minute
	certRenewWarnWindow   = 14 * 24 * time.Hour
	defaultTLSProbeTarget = "127.0.0.1:443"
)

// CertProbe verifies locally served TLS certificates via 127.0.0.1:443 SNI
// and inspects Caddy's journald logs when issuance stalls > 15m (§3.7).
type CertProbe struct {
	TargetAddr string
	RootCAs    *x509.CertPool
	Systemd    SystemdRunner
	DialTimout time.Duration
	CertsDir   string
}

// NewCertProbe builds a CertProbe using system root CAs by default.
func NewCertProbe(certsDir string, systemd SystemdRunner) *CertProbe {
	return &CertProbe{
		TargetAddr: defaultTLSProbeTarget,
		Systemd:    systemd,
		DialTimout: 4 * time.Second,
		CertsDir:   certsDir,
	}
}

// ProbeResult is the local TLS verification outcome for one host.
type ProbeResult struct {
	Valid       bool
	NotAfter    *time.Time
	Issuer      string
	RenewWarn   bool
	ErrDetail   string
}

// ProbeHost dials 127.0.0.1:443 with SNI=host and manually verifies the leaf
// certificate against RootCAs + hostname (§3.7).
func (p *CertProbe) ProbeHost(ctx context.Context, host string, now time.Time) ProbeResult {
	addr := p.TargetAddr
	if addr == "" {
		addr = defaultTLSProbeTarget
	}
	timeout := p.DialTimout
	if timeout <= 0 {
		timeout = 4 * time.Second
	}

	d := &net.Dialer{Timeout: timeout}
	rawConn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return ProbeResult{ErrDetail: fmt.Sprintf("dial %s: %v", addr, err)}
	}

	tlsConn := tls.Client(rawConn, &tls.Config{
		ServerName:         host,
		InsecureSkipVerify: true, // Leaf verified manually below per §3.7.
	})
	_ = tlsConn.SetDeadline(time.Now().Add(timeout))
	if err := tlsConn.HandshakeContext(ctx); err != nil {
		_ = tlsConn.Close()
		return ProbeResult{ErrDetail: fmt.Sprintf("tls handshake: %v", err)}
	}
	state := tlsConn.ConnectionState()
	_ = tlsConn.Close()

	if len(state.PeerCertificates) == 0 {
		return ProbeResult{ErrDetail: "no peer certificate presented"}
	}

	leaf := state.PeerCertificates[0]
	intermediates := x509.NewCertPool()
	for _, cert := range state.PeerCertificates[1:] {
		intermediates.AddCert(cert)
	}

	roots := p.RootCAs
	if roots == nil {
		roots, _ = x509.SystemCertPool()
	}

	verifyOpts := x509.VerifyOptions{
		DNSName:       host,
		Roots:         roots,
		Intermediates: intermediates,
		CurrentTime:   now,
	}
	if _, err := leaf.Verify(verifyOpts); err != nil {
		return ProbeResult{ErrDetail: fmt.Sprintf("cert verify: %v", err)}
	}

	notAfter := leaf.NotAfter.UTC()
	issuer := leaf.Issuer.CommonName
	if len(leaf.Issuer.Organization) > 0 && issuer == "" {
		issuer = leaf.Issuer.Organization[0]
	}
	if issuer == "" {
		issuer = leaf.Issuer.String()
	}

	renewWarn := leaf.NotAfter.Sub(now) < certRenewWarnWindow
	return ProbeResult{
		Valid:     true,
		NotAfter:  &notAfter,
		Issuer:    issuer,
		RenewWarn: renewWarn,
	}
}

// UpdateHostStatusFromProbe updates a single host's state (active / issuing /
// cert_error / dns_drift / cert_renew_overdue) using ProbeHost + >15m journalctl
// classification (§3.7).
func (p *CertProbe) UpdateHostStatusFromProbe(ctx context.Context, st *PanelState, host string, hasDNSDrift bool, driftDetail string, caMissing bool, now time.Time) HostStatus {
	res := p.ProbeHost(ctx, host, now)
	if res.Valid {
		delete(st.HostFirstIssuing, host)
		var details []string
		if res.RenewWarn {
			details = append(details, WarningCertRenewOverdue)
		}
		if caMissing {
			details = append(details, WarningOriginCAMissing)
		}
		state := StateActive
		if hasDNSDrift {
			state = StateDNSDrift
			if driftDetail != "" {
				details = append(details, driftDetail)
			}
		}
		hs := HostStatus{
			Host:         host,
			State:        state,
			Detail:       strings.Join(details, "; "),
			CertNotAfter: res.NotAfter,
			Issuer:       res.Issuer,
		}
		st.HostStates[host] = hs
		return hs
	}

	first, exists := st.HostFirstIssuing[host]
	if !exists || first.IsZero() {
		first = now
		st.HostFirstIssuing[host] = first
	}

	if now.Sub(first) > certIssuingTimeout {
		detail := p.classifyCertErrorFromJournal(ctx, host)
		if detail == "" {
			detail = res.ErrDetail
		}
		if caMissing {
			detail = appendWarning(detail, WarningOriginCAMissing)
		}
		hs := HostStatus{
			Host:   host,
			State:  StateCertError,
			Detail: detail,
		}
		st.HostStates[host] = hs
		return hs
	}

	detail := res.ErrDetail
	if hasDNSDrift {
		detail = appendWarning(detail, driftDetail)
	}
	if caMissing {
		detail = appendWarning(detail, WarningOriginCAMissing)
	}
	hs := HostStatus{
		Host:   host,
		State:  StateIssuing,
		Detail: detail,
	}
	st.HostStates[host] = hs
	return hs
}

// HasValidStoredCert checks Caddy's cert storage directory for an existing valid
// non-expired certificate for host (so admission throttle exempts renewals/re-adds, §3.4).
func (p *CertProbe) HasValidStoredCert(host string, now time.Time) bool {
	if p == nil || p.CertsDir == "" {
		return false
	}
	certsRoot := filepath.Join(p.CertsDir, "certificates")
	found := false
	_ = filepath.WalkDir(certsRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || found {
			return nil
		}
		if d.Name() != host+".crt" {
			return nil
		}
		raw, rErr := os.ReadFile(path)
		if rErr != nil {
			return nil
		}
		block, _ := pem.Decode(raw)
		if block == nil {
			return nil
		}
		cert, cErr := x509.ParseCertificate(block.Bytes)
		if cErr != nil {
			return nil
		}
		if now.After(cert.NotBefore) && now.Before(cert.NotAfter) {
			found = true
		}
		return nil
	})
	return found
}

func (p *CertProbe) classifyCertErrorFromJournal(ctx context.Context, host string) string {
	if p.Systemd == nil {
		return ""
	}
	raw, err := p.Systemd.JournalCaddyErrors(ctx, "-60min")
	if err != nil || len(raw) == 0 {
		return ""
	}
	return ClassifyJournalEntries(raw, host)
}

// ClassifyJournalEntries parses `journalctl -u caddy -o json` lines where
// logger starts with "tls." and identifier == host, returning a classified
// ACME error summary (§3.7).
func ClassifyJournalEntries(journalJSONLines []byte, host string) string {
	type journalLine struct {
		Message string `json:"MESSAGE"`
	}
	type caddyLog struct {
		Logger     string `json:"logger"`
		Identifier string `json:"identifier"`
		Msg        string `json:"msg"`
		Error      string `json:"error"`
	}

	var latest string
	sc := bufio.NewScanner(bytes.NewReader(journalJSONLines))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var jl journalLine
		if err := json.Unmarshal([]byte(line), &jl); err == nil && jl.Message != "" {
			line = jl.Message
		}
		var cl caddyLog
		if err := json.Unmarshal([]byte(line), &cl); err != nil {
			continue
		}
		if !strings.HasPrefix(cl.Logger, "tls.") {
			continue
		}
		if !strings.EqualFold(cl.Identifier, host) {
			continue
		}
		combined := strings.TrimSpace(cl.Error + " " + cl.Msg)
		if combined != "" {
			latest = combined
		}
	}
	if latest == "" {
		return ""
	}
	lower := strings.ToLower(latest)
	switch {
	case strings.Contains(lower, "unauthorized") || strings.Contains(lower, "403"):
		return "unauthorized/403 (wrong IP or proxy in path): " + truncateStr(latest, 200)
	case strings.Contains(lower, "ratelimited") || strings.Contains(lower, "rate limit") || strings.Contains(lower, "too many certificates"):
		return "rateLimited: " + truncateStr(latest, 200)
	case strings.Contains(lower, "caa"):
		return "caa: " + truncateStr(latest, 200)
	case strings.Contains(lower, "nxdomain") || strings.Contains(lower, "dns"):
		return "dns/NXDOMAIN: " + truncateStr(latest, 200)
	case strings.Contains(lower, "timeout") || strings.Contains(lower, "deadline exceeded"):
		return "timeout: " + truncateStr(latest, 200)
	case strings.Contains(lower, "connection") || strings.Contains(lower, "refused"):
		return "connection: " + truncateStr(latest, 200)
	default:
		return truncateStr(latest, 240)
	}
}
