package edge

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

const (
	MaxRoutesBodyBytes = 16 * 1024 * 1024
	MaxErrorBodyBytes  = 64 * 1024
	DefaultHTTPTimeout = 20 * time.Second
)

// PanelAuthError indicates HTTP 401 or 403 from the panel API (non-WAF).
type PanelAuthError struct {
	Status int
	Body   string
}

func (e *PanelAuthError) Error() string {
	return fmt.Sprintf("panel auth failed (HTTP %d)", e.Status)
}

// PanelOutdatedError indicates HTTP 404 on /api/v1/edge/routes (E-API-3).
type PanelOutdatedError struct{}

func (PanelOutdatedError) Error() string { return "panel_outdated (404 on /api/v1/edge/routes)" }

// PanelDeletedError indicates HTTP 410 Gone (E-API-4).
type PanelDeletedError struct {
	Reason string
}

func (e *PanelDeletedError) Error() string {
	if e.Reason != "" {
		return fmt.Sprintf("panel node deleted (410: %s)", e.Reason)
	}
	return "panel node deleted (410 Gone)"
}

// PanelWAFBlockedError indicates a Cloudflare/WAF HTML challenge intercepted the API request (E-API-10).
type PanelWAFBlockedError struct {
	Status int
}

func (e *PanelWAFBlockedError) Error() string {
	return fmt.Sprintf("panel_blocked_by_waf (HTTP %d HTML challenge; allowlist Shield IP)", e.Status)
}

// PanelHTTPStatusError represents an unexpected 5xx or 4xx HTTP status.
type PanelHTTPStatusError struct {
	Status int
	Body   string
}

func (e *PanelHTTPStatusError) Error() string {
	return fmt.Sprintf("panel HTTP %d: %s", e.Status, e.Body)
}

// PanelClient talks to one NextDeploy panel's /api/v1/edge/* endpoints.
type PanelClient struct {
	httpClient *http.Client
	panel      PanelConfig
}

// NewPanelClient builds an HTTP client configured with 20s timeout, optional
// SHA-256 leaf/chain certificate pinning (E-API-11), and custom dialer for
// shielded panel URLs (E-API-9).
func NewPanelClient(panel PanelConfig, shieldIPs []netip.Addr, originIPs []netip.Addr) *PanelClient {
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}

	var parsedHost string
	if u, err := url.Parse(panel.URL); err == nil {
		parsedHost = strings.ToLower(u.Hostname())
	}

	transport := &http.Transport{
		Proxy:               http.ProxyFromEnvironment,
		MaxIdleConns:        8,
		IdleConnTimeout:     60 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			// E-API-9: if panel hostname resolves to one of this Shield's own IPs,
			// dial the panel's origin IP directly while keeping SNI/Host intact.
			host, port, splitErr := net.SplitHostPort(addr)
			if splitErr == nil && parsedHost != "" && strings.EqualFold(host, parsedHost) && len(originIPs) > 0 && len(shieldIPs) > 0 {
				if resolved, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host); err == nil {
					for _, r := range resolved {
						if ipInSet(r, shieldIPs) {
							target := net.JoinHostPort(originIPs[0].Unmap().String(), port)
							return dialer.DialContext(ctx, network, target)
						}
					}
				}
			}
			return dialer.DialContext(ctx, network, addr)
		},
	}

	if pin := strings.ToLower(strings.TrimSpace(panel.PanelCASHA256)); pin != "" {
		pin = strings.TrimPrefix(pin, "sha256:")
		transport.TLSClientConfig = &tls.Config{
			InsecureSkipVerify: true, // Verified explicitly via VerifyPeerCertificate below (E-API-11).
			MinVersion:         tls.VersionTLS12,
			VerifyPeerCertificate: func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
				if len(rawCerts) == 0 {
					return errors.New("panel TLS presented no certificates")
				}
				for _, der := range rawCerts {
					sum := sha256.Sum256(der)
					got := hex.EncodeToString(sum[:])
					if subtle.ConstantTimeCompare([]byte(got), []byte(pin)) == 1 {
						// Verify hostname & validity if not self-signed leaf? Pin match authorizes cert.
						if _, err := x509.ParseCertificate(der); err != nil {
							return fmt.Errorf("invalid pinned certificate: %w", err)
						}
						return nil
					}
				}
				return fmt.Errorf("panel TLS certificate does not match pinned sha256 %s", pin)
			},
		}
	}

	return &PanelClient{
		panel: panel,
		httpClient: &http.Client{
			Timeout:   DefaultHTTPTimeout,
			Transport: transport,
		},
	}
}

// Register calls POST /api/v1/edge/register.
func (c *PanelClient) Register(ctx context.Context, req RegisterRequest) (*RegisterResponse, error) {
	data, status, contentType, err := c.doJSON(ctx, http.MethodPost, "/api/v1/edge/register", req, "")
	if err != nil {
		return nil, err
	}
	if isWAFHTMLChallenge(status, contentType, data) {
		return nil, &PanelWAFBlockedError{Status: status}
	}
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		return nil, &PanelAuthError{Status: status, Body: snippet(data)}
	}
	if status == http.StatusConflict {
		var apiErr APIErrorResponse
		_ = json.Unmarshal(data, &apiErr)
		if apiErr.Error != "" {
			return nil, fmt.Errorf("register conflict (409): %s", apiErr.Error)
		}
		return nil, fmt.Errorf("register conflict (409): %s", snippet(data))
	}
	if status < 200 || status >= 300 {
		return nil, &PanelHTTPStatusError{Status: status, Body: snippet(data)}
	}

	var resp RegisterResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("decode register response: %w", err)
	}
	if err := CheckAPIVersion(resp.APIVersion); err != nil {
		return nil, err
	}
	if err := ValidatePanelID(resp.PanelID); err != nil {
		return nil, err
	}
	return &resp, nil
}

// FetchRoutes calls GET /api/v1/edge/routes with If-None-Match.
// Returns (resp, notModified, error).
func (c *PanelClient) FetchRoutes(ctx context.Context, etag string) (*RoutesResponse, bool, error) {
	data, status, contentType, err := c.doJSON(ctx, http.MethodGet, "/api/v1/edge/routes", nil, etag)
	if err != nil {
		return nil, false, err
	}
	if status == http.StatusNotModified {
		return nil, true, nil
	}
	if isWAFHTMLChallenge(status, contentType, data) {
		return nil, false, &PanelWAFBlockedError{Status: status}
	}
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return nil, false, &PanelAuthError{Status: status, Body: snippet(data)}
	case http.StatusNotFound:
		return nil, false, PanelOutdatedError{}
	case http.StatusGone:
		var apiErr APIErrorResponse
		_ = json.Unmarshal(data, &apiErr)
		return nil, false, &PanelDeletedError{Reason: apiErr.Reason}
	}
	if status < 200 || status >= 300 {
		return nil, false, &PanelHTTPStatusError{Status: status, Body: snippet(data)}
	}

	var resp RoutesResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, false, fmt.Errorf("malformed routes JSON: %w", err)
	}
	if err := CheckAPIVersion(resp.APIVersion); err != nil {
		return nil, false, err
	}
	if err := ValidatePanelID(resp.PanelID); err != nil {
		return nil, false, err
	}
	if resp.Node.Deleted {
		return nil, false, &PanelDeletedError{Reason: "node.deleted=true"}
	}
	return &resp, false, nil
}

// PostStatus calls POST /api/v1/edge/status.
func (c *PanelClient) PostStatus(ctx context.Context, statusDoc StatusRequest) error {
	data, status, contentType, err := c.doJSON(ctx, http.MethodPost, "/api/v1/edge/status", statusDoc, "")
	if err != nil {
		return err
	}
	if isWAFHTMLChallenge(status, contentType, data) {
		return &PanelWAFBlockedError{Status: status}
	}
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		return &PanelAuthError{Status: status, Body: snippet(data)}
	}
	if status < 200 || status >= 300 {
		return &PanelHTTPStatusError{Status: status, Body: snippet(data)}
	}
	return nil
}

// Unregister calls POST /api/v1/edge/unregister (best-effort on panel removal).
func (c *PanelClient) Unregister(ctx context.Context, req UnregisterRequest) error {
	data, status, _, err := c.doJSON(ctx, http.MethodPost, "/api/v1/edge/unregister", req, "")
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return &PanelHTTPStatusError{Status: status, Body: snippet(data)}
	}
	return nil
}

func (c *PanelClient) doJSON(ctx context.Context, method, path string, body any, ifNoneMatch string) ([]byte, int, string, error) {
	base := strings.TrimRight(c.panel.URL, "/")
	if !strings.HasPrefix(base, "https://") && !(c.panel.InsecureHTTP && strings.HasPrefix(base, "http://")) {
		return nil, 0, "", fmt.Errorf("panel URL %q must use https:// (or explicit --insecure-http)", base)
	}

	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, 0, "", err
		}
		bodyReader = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, base+path, bodyReader)
	if err != nil {
		return nil, 0, "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.panel.Token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if ifNoneMatch != "" {
		req.Header.Set("If-None-Match", ifNoneMatch)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, "", err
	}
	defer resp.Body.Close()

	limit := int64(MaxRoutesBodyBytes)
	if resp.StatusCode >= 400 {
		limit = MaxErrorBodyBytes
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, limit))
	if err != nil {
		return nil, resp.StatusCode, resp.Header.Get("Content-Type"), err
	}
	return raw, resp.StatusCode, resp.Header.Get("Content-Type"), nil
}

func isWAFHTMLChallenge(status int, contentType string, body []byte) bool {
	ct := strings.ToLower(contentType)
	trimmed := strings.TrimSpace(string(body))
	lower := strings.ToLower(trimmed)
	isHTML := strings.Contains(ct, "text/html") || strings.HasPrefix(lower, "<!doctype html") || strings.HasPrefix(lower, "<html")
	if !isHTML {
		return false
	}
	if status == http.StatusForbidden || status == http.StatusTooManyRequests || status == http.StatusServiceUnavailable {
		return true
	}
	return strings.Contains(lower, "cf-browser-verification") ||
		strings.Contains(lower, "just a moment...") ||
		strings.Contains(lower, "attention required!") ||
		strings.Contains(lower, "cloudflare")
}

func snippet(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 256 {
		return s[:256] + "..."
	}
	return s
}

// PollOutcome summarizes a single poll cycle for a panel.
type PollOutcome struct {
	SnapshotUpdated bool
	HeldMassDelete  bool
	PanelReleased   bool
	Err             error
}

// PollPanelOnce executes one poll cycle against client and updates st according
// to §3.2 (200/304/401/403/404/410/5xx/WAF) and §3.4 (mass-deletion guard).
func PollPanelOnce(ctx context.Context, client *PanelClient, st *PanelState, shieldHostname string, force bool, now time.Time) PollOutcome {
	st.LastSyncAttemptAt = now

	// Always send LastGoodETag (not PendingETag) so a held mass-deletion snapshot
	// continues returning 200 and increments PendingCount on subsequent polls.
	resp, notModified, err := client.FetchRoutes(ctx, st.LastGoodETag)
	if err != nil {
		var authErr *PanelAuthError
		var wafErr *PanelWAFBlockedError
		var delErr *PanelDeletedError
		switch {
		case errors.As(err, &wafErr):
			st.SyncState = SyncStateBlockedWAF
			st.LastError = wafErr.Error()
			return PollOutcome{Err: err}
		case errors.As(err, &authErr):
			st.SyncState = SyncStateAuthFailed
			st.LastError = authErr.Error()
			return PollOutcome{Err: err}
		case errors.As(err, &PanelOutdatedError{}):
			st.SyncState = SyncStatePanelOutdate
			st.LastError = err.Error()
			return PollOutcome{Err: err}
		case errors.As(err, &delErr):
			if st.Consecutive410Count == 0 {
				st.First410At = now
			}
			st.Consecutive410Count++
			st.LastError = delErr.Error()
			if st.Consecutive410Count >= 3 && now.Sub(st.First410At) >= 10*time.Minute {
				st.SyncState = SyncStateDeleted
				st.LastGood = nil
				st.LastGoodETag = ""
				return PollOutcome{PanelReleased: true, Err: err}
			}
			return PollOutcome{Err: err}
		default:
			if st.LastGood != nil {
				st.SyncState = SyncStateStale
			} else {
				st.SyncState = SyncStateError
			}
			st.LastError = err.Error()
			return PollOutcome{Err: err}
		}
	}

	// Non-410 response clears consecutive 410 tracking.
	st.Consecutive410Count = 0
	st.First410At = time.Time{}

	if notModified {
		st.SyncState = SyncStateOK
		st.LastSyncOKAt = now
		st.LastError = ""
		return PollOutcome{}
	}

	// Mass-deletion guard (§3.4):
	// Hold if removing >50% of previous hosts AND >5 hosts (or all hosts when prev > 0).
	if !force && shouldHoldMassDelete(st.LastGood, resp, shieldHostname) {
		if st.PendingETag == resp.ETag && resp.ETag != "" {
			st.PendingCount++
		} else {
			st.PendingSnapshot = resp
			st.PendingETag = resp.ETag
			st.PendingCount = 1
			st.PendingFirstSeen = now
		}

		// Apply only after same etag seen in 3 consecutive polls spanning >= 3 min.
		if st.PendingCount < 3 || now.Sub(st.PendingFirstSeen) < 3*time.Minute {
			st.SyncState = SyncStateOK
			st.LastSyncOKAt = now
			st.LastError = ""
			return PollOutcome{HeldMassDelete: true}
		}
	}

	// Accept snapshot as new LastGood.
	st.LastGood = resp
	st.LastGoodETag = resp.ETag
	st.LastGoodAt = now
	st.PendingSnapshot = nil
	st.PendingETag = ""
	st.PendingCount = 0
	st.PendingFirstSeen = time.Time{}
	st.SyncState = SyncStateOK
	st.LastSyncOKAt = now
	st.LastError = ""
	return PollOutcome{SnapshotUpdated: true}
}

func shouldHoldMassDelete(prev, next *RoutesResponse, shieldHostname string) bool {
	if prev == nil {
		return false
	}
	prevHosts := collectSnapshotValidHostSet(prev, shieldHostname)
	if len(prevHosts) == 0 {
		return false
	}
	nextHosts := collectSnapshotValidHostSet(next, shieldHostname)
	if len(nextHosts) == 0 {
		return true
	}
	removed := 0
	for h := range prevHosts {
		if _, ok := nextHosts[h]; !ok {
			removed++
		}
	}
	return removed > 5 && float64(removed) > 0.5*float64(len(prevHosts))
}

func collectSnapshotValidHostSet(resp *RoutesResponse, shieldHostname string) map[string]struct{} {
	set := make(map[string]struct{})
	if resp == nil {
		return set
	}
	for _, r := range resp.Routes {
		if ValidateRouteID(r.ID) != nil {
			continue
		}
		for _, raw := range r.Hosts {
			if norm, verr := NormalizeAndValidateHost(raw, shieldHostname); verr == nil {
				set[norm] = struct{}{}
			}
		}
	}
	return set
}

// NextPollDelay computes the next sleep interval with ±20% jitter and
// exponential error backoff (30s -> 5m, or 5m on auth_failed).
func NextPollDelay(baseSec int, consecutiveErrs int, syncState string) time.Duration {
	if baseSec <= 0 {
		baseSec = DefaultPollInterval
	}
	var base time.Duration
	switch {
	case syncState == SyncStateAuthFailed:
		base = 5 * time.Minute
	case consecutiveErrs > 0:
		d := 30 * time.Second
		for i := 1; i < consecutiveErrs && d < 5*time.Minute; i++ {
			d *= 2
		}
		if d > 5*time.Minute {
			d = 5 * time.Minute
		}
		base = d
	default:
		base = time.Duration(baseSec) * time.Second
	}
	// Apply ±20% jitter.
	jitterFactor := 0.8 + rand.Float64()*0.4
	return time.Duration(float64(base) * jitterFactor)
}
