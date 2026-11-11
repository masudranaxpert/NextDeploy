//go:build integration

package edgeintegration

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

const (
	composePanelAURL   = "http://127.0.0.1:18081"
	composePanelBURL   = "http://127.0.0.1:18082"
	composePebbleMgmt  = "https://127.0.0.1:15000"
	composeChallAPI    = "http://127.0.0.1:18055"
	composeShieldHTTPS = "127.0.0.1:10443"
	composeOriginACtrl = "http://127.0.0.1:12021"

	panelAID = "11111111-1111-4111-8111-111111111111"
	panelBID = "22222222-2222-4222-8222-222222222222"
)

type composeRoute struct {
	ID      string              `json:"id"`
	Kind    string              `json:"kind"`
	Hosts   []string            `json:"hosts"`
	Options composeRouteOptions `json:"options"`
}

type composeRouteOptions struct {
	Websocket  bool `json:"websocket"`
	MaxBodyMB  int  `json:"max_body_mb"`
	UpstreamH2 bool `json:"upstream_h2"`
}

type fakePanelControlState struct {
	PanelID     string         `json:"panel_id"`
	PanelName   string         `json:"panel_name"`
	ETag        string         `json:"etag"`
	Routes      []composeRoute `json:"routes"`
	StatusCount int            `json:"status_count"`
	LastStatus  *struct {
		AppliedETag string `json:"applied_etag"`
		Hosts       []struct {
			Host   string `json:"host"`
			State  string `json:"state"`
			Detail string `json:"detail"`
		} `json:"hosts"`
		Conflicts []struct {
			Host       string `json:"host"`
			OwnerPanel string `json:"owner_panel"`
		} `json:"conflicts"`
	} `json:"last_status"`
}

func TestDockerComposePebbleSuite(t *testing.T) {
	if os.Getenv("ND_EDGE_COMPOSE_TEST") != "1" {
		t.Skip("set ND_EDGE_COMPOSE_TEST=1 with docker compose stack running to execute Pebble integration suite")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	// Fetch Pebble root CA so our test HTTP/WS clients verify Shield certs issued by Pebble.
	pebblePool := fetchPebbleRootPool(t, ctx)
	shieldClient := newShieldHTTPSClient(pebblePool)

	// Register DNS A records in pebble-challtestsrv for all test hosts.
	allHosts := []string{
		"a1.panel1.demo.net",
		"b1.panel2.demo.net",
		"shared.demo.net",
		"ws.panel1.demo.net",
	}
	for i := 1; i <= 8; i++ {
		allHosts = append(allHosts, fmt.Sprintf("mass-%d.panel1.demo.net", i))
	}
	for _, h := range allHosts {
		addFakeDNSA(t, h, "172.30.0.10")
	}

	// 1. Initial cert issue for both panels.
	putPanelRoutes(t, composePanelAURL, []composeRoute{
		{ID: "dom_1", Kind: "app_domain", Hosts: []string{"a1.panel1.demo.net"}, Options: composeRouteOptions{Websocket: true}},
		{ID: "dom_2", Kind: "app_domain", Hosts: []string{"shared.demo.net"}, Options: composeRouteOptions{Websocket: true}},
		{ID: "dom_ws", Kind: "app_domain", Hosts: []string{"ws.panel1.demo.net"}, Options: composeRouteOptions{Websocket: true}},
	})
	putPanelRoutes(t, composePanelBURL, []composeRoute{
		{ID: "dom_1", Kind: "app_domain", Hosts: []string{"b1.panel2.demo.net"}, Options: composeRouteOptions{Websocket: true}},
	})

	waitForOriginEcho(t, ctx, shieldClient, "a1.panel1.demo.net", "origin-a")
	waitForOriginEcho(t, ctx, shieldClient, "shared.demo.net", "origin-a")
	waitForOriginEcho(t, ctx, shieldClient, "ws.panel1.demo.net", "origin-a")
	waitForOriginEcho(t, ctx, shieldClient, "b1.panel2.demo.net", "origin-b")

	// 2. WebSocket surviving Caddy reload.
	wsConn := dialShieldWebSocket(t, pebblePool, "ws.panel1.demo.net", "/ws")
	defer wsConn.Close()
	assertWebSocketEcho(t, wsConn, "before-reload", "origin-a: before-reload")

	// Trigger a route change on Panel B -> Shield Caddy reloads while WS is open.
	putPanelRoutes(t, composePanelBURL, []composeRoute{
		{ID: "dom_1", Kind: "app_domain", Hosts: []string{"b1.panel2.demo.net"}, Options: composeRouteOptions{Websocket: true}},
		{ID: "dom_conflict", Kind: "app_domain", Hosts: []string{"shared.demo.net"}, Options: composeRouteOptions{Websocket: true}},
	})

	// 3. Cross-panel conflict: Panel B claims shared.demo.net -> conflict reported, Panel A still serves it.
	waitForHostState(t, ctx, composePanelBURL, "shared.demo.net", "conflict")
	assertOriginEcho(t, shieldClient, "shared.demo.net", "origin-a")
	assertWebSocketEcho(t, wsConn, "after-reload", "origin-a: after-reload")
	_ = wsConn.Close()

	// 4. Domain move A -> B: remove shared.demo.net from Panel A.
	putPanelRoutes(t, composePanelAURL, []composeRoute{
		{ID: "dom_1", Kind: "app_domain", Hosts: []string{"a1.panel1.demo.net"}, Options: composeRouteOptions{Websocket: true}},
		{ID: "dom_ws", Kind: "app_domain", Hosts: []string{"ws.panel1.demo.net"}, Options: composeRouteOptions{Websocket: true}},
	})
	waitForOriginEcho(t, ctx, shieldClient, "shared.demo.net", "origin-b")

	// 5. Panel removal without dropping Panel 1 requests.
	var probeFailures atomic.Int64
	probeCtx, stopProbe := context.WithCancel(ctx)
	probeDone := make(chan struct{})
	go func() {
		defer close(probeDone)
		ticker := time.NewTicker(150 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-probeCtx.Done():
				return
			case <-ticker.C:
				if err := checkOriginEcho(shieldClient, "a1.panel1.demo.net", "origin-a"); err != nil {
					probeFailures.Add(1)
				}
			}
		}
	}()

	runCompose(t, "exec", "-T", "nd-edge", "nd", "edge", "remove-panel", "panel-b")
	time.Sleep(3 * time.Second)
	stopProbe()
	<-probeDone
	if fails := probeFailures.Load(); fails > 0 {
		t.Fatalf("expected 0 failed requests to Panel A during Panel B removal, got %d", fails)
	}
	if err := checkOriginEcho(shieldClient, "b1.panel2.demo.net", "origin-b"); err == nil {
		t.Fatalf("expected b1.panel2.demo.net to stop serving after panel-b removal")
	}

	// 6. Panel offline, Caddy restart, nd-edge restart, and container reboot.
	runCompose(t, "stop", "panel-a")
	assertOriginEcho(t, shieldClient, "a1.panel1.demo.net", "origin-a")

	runCompose(t, "restart", "caddy")
	waitForOriginEcho(t, ctx, shieldClient, "a1.panel1.demo.net", "origin-a")

	runCompose(t, "restart", "nd-edge")
	assertOriginEcho(t, shieldClient, "a1.panel1.demo.net", "origin-a")

	// Container "reboot" (restart both Shield containers while panel-a is still stopped).
	runCompose(t, "restart", "caddy", "nd-edge")
	waitForOriginEcho(t, ctx, shieldClient, "a1.panel1.demo.net", "origin-a")

	// Bring panel-a back up.
	runCompose(t, "start", "panel-a")

	// 7. Corrupt state files -> quarantined and recovered without dropping served routes.
	runCompose(t, "exec", "-T", "nd-edge", "sh", "-c",
		fmt.Sprintf(`echo "{corrupt" > /var/lib/nd-edge/state/%s.json && echo "{corrupt" > /var/lib/nd-edge/claims.json`, panelAID))
	runCompose(t, "restart", "nd-edge")
	waitForOriginEcho(t, ctx, shieldClient, "a1.panel1.demo.net", "origin-a")

	// 8. Mass-deletion guard: seed 8 domains on Panel A, wait active, then remove 6.
	massRoutes := []composeRoute{
		{ID: "dom_1", Kind: "app_domain", Hosts: []string{"a1.panel1.demo.net"}, Options: composeRouteOptions{Websocket: true}},
	}
	for i := 1; i <= 8; i++ {
		massRoutes = append(massRoutes, composeRoute{
			ID:      fmt.Sprintf("mass_%d", i),
			Kind:    "app_domain",
			Hosts:   []string{fmt.Sprintf("mass-%d.panel1.demo.net", i)},
			Options: composeRouteOptions{Websocket: true},
		})
	}
	putPanelRoutes(t, composePanelAURL, massRoutes)
	waitForOriginEcho(t, ctx, shieldClient, "mass-1.panel1.demo.net", "origin-a")
	waitForOriginEcho(t, ctx, shieldClient, "mass-8.panel1.demo.net", "origin-a")

	// Remove 6 (>50% and >5 hosts) -> guard must hold them!
	putPanelRoutes(t, composePanelAURL, massRoutes[:3])
	waitForHostState(t, ctx, composePanelAURL, "mass-8.panel1.demo.net", "held_mass_delete")
	assertOriginEcho(t, shieldClient, "mass-8.panel1.demo.net", "origin-a")

	// Force-sync bypasses mass-deletion guard so cleanup completes cleanly.
	runCompose(t, "exec", "-T", "nd-edge", "nd", "edge", "sync", "--force")

	// 9. Origin CA rotation: wipe origin-a-caddy local PKI, restart origin-a-caddy,
	// push new root CA to panel-a, verify Shield continues proxying HTTPS cleanly.
	runCompose(t, "exec", "-T", "origin-a-caddy", "rm", "-rf", "/data/caddy/pki/authorities/local")
	runCompose(t, "restart", "origin-a-caddy")
	newCAPEM := waitForOriginCaddyRootCA(t, ctx, composeOriginACtrl)
	putPanelOriginCA(t, composePanelAURL, newCAPEM)
	time.Sleep(6 * time.Second)
	waitForOriginEcho(t, ctx, shieldClient, "a1.panel1.demo.net", "origin-a")
}

func runCompose(t *testing.T, args ...string) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	composeFile := filepath.Join(wd, "docker-compose.yml")
	fullArgs := append([]string{"compose", "-f", composeFile}, args...)
	cmd := exec.Command("docker", fullArgs...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("docker %s failed: %v\n%s", strings.Join(fullArgs, " "), err, string(out))
	}
	return string(out)
}

func fetchPebbleRootPool(t *testing.T, ctx context.Context) *x509.CertPool {
	t.Helper()
	insecure := &http.Client{
		Timeout:   5 * time.Second,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}},
	}
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) && ctx.Err() == nil {
		resp, err := insecure.Get(composePebbleMgmt + "/roots/0")
		if err == nil && resp.StatusCode == 200 {
			pemBytes, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			pool := x509.NewCertPool()
			if pool.AppendCertsFromPEM(pemBytes) {
				return pool
			}
		}
		time.Sleep(time.Second)
	}
	t.Fatalf("failed to fetch Pebble root CA from %s/roots/0", composePebbleMgmt)
	return nil
}

func newShieldHTTPSClient(roots *x509.CertPool) *http.Client {
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	return &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{RootCAs: roots},
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				_, port, _ := net.SplitHostPort(addr)
				if port == "443" || port == "" {
					return dialer.DialContext(ctx, network, composeShieldHTTPS)
				}
				return dialer.DialContext(ctx, network, addr)
			},
		},
	}
}

func addFakeDNSA(t *testing.T, host, ip string) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"host": host, "addresses": []string{ip}})
	resp, err := http.Post(composeChallAPI+"/add-a", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("pebble-challtestsrv add-a %s: %v", host, err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode >= 300 {
		t.Fatalf("pebble-challtestsrv add-a %s returned %d", host, resp.StatusCode)
	}
}

func putPanelRoutes(t *testing.T, baseURL string, routes []composeRoute) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"routes": routes})
	req, _ := http.NewRequest(http.MethodPut, baseURL+"/_control/routes", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT %s/_control/routes: %v", baseURL, err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode >= 300 {
		t.Fatalf("PUT %s/_control/routes returned %d", baseURL, resp.StatusCode)
	}
}

func putPanelOriginCA(t *testing.T, baseURL, pemData string) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"ca_pem": pemData, "ca_status": "ok"})
	req, _ := http.NewRequest(http.MethodPut, baseURL+"/_control/origin", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT %s/_control/origin: %v", baseURL, err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode >= 300 {
		t.Fatalf("PUT %s/_control/origin returned %d", baseURL, resp.StatusCode)
	}
}

func waitForOriginCaddyRootCA(t *testing.T, ctx context.Context, adminURL string) string {
	t.Helper()
	deadline := time.Now().Add(45 * time.Second)
	for time.Now().Before(deadline) && ctx.Err() == nil {
		resp, err := http.Get(adminURL + "/pki/ca/local")
		if err == nil && resp.StatusCode == 200 {
			var info struct {
				RootCertificate string `json:"root_certificate"`
			}
			_ = json.NewDecoder(resp.Body).Decode(&info)
			_ = resp.Body.Close()
			if strings.Contains(info.RootCertificate, "-----BEGIN CERTIFICATE-----") {
				return info.RootCertificate
			}
		}
		time.Sleep(time.Second)
	}
	t.Fatalf("timed out waiting for rotated origin CA from %s/pki/ca/local", adminURL)
	return ""
}

func checkOriginEcho(client *http.Client, host, wantOrigin string) error {
	resp, err := client.Get("https://" + host + "/echo")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("status %d: %s", resp.StatusCode, string(b))
	}
	var payload struct {
		Origin string `json:"origin"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return err
	}
	if payload.Origin != wantOrigin {
		return fmt.Errorf("origin=%q want=%q", payload.Origin, wantOrigin)
	}
	return nil
}

func assertOriginEcho(t *testing.T, client *http.Client, host, wantOrigin string) {
	t.Helper()
	if err := checkOriginEcho(client, host, wantOrigin); err != nil {
		t.Fatalf("origin echo check for %s failed: %v", host, err)
	}
}

func waitForOriginEcho(t *testing.T, ctx context.Context, client *http.Client, host, wantOrigin string) {
	t.Helper()
	deadline := time.Now().Add(90 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) && ctx.Err() == nil {
		if err := checkOriginEcho(client, host, wantOrigin); err == nil {
			return
		} else {
			lastErr = err
		}
		time.Sleep(1500 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s -> %s: %v", host, wantOrigin, lastErr)
}

func waitForHostState(t *testing.T, ctx context.Context, panelURL, host, wantState string) {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) && ctx.Err() == nil {
		resp, err := http.Get(panelURL + "/_control/state")
		if err == nil && resp.StatusCode == 200 {
			var st fakePanelControlState
			_ = json.NewDecoder(resp.Body).Decode(&st)
			_ = resp.Body.Close()
			if st.LastStatus != nil {
				for _, h := range st.LastStatus.Hosts {
					if h.Host == host && h.State == wantState {
						return
					}
				}
			}
		}
		time.Sleep(time.Second)
	}
	t.Fatalf("timed out waiting for host %s state=%q on %s", host, wantState, panelURL)
}

func dialShieldWebSocket(t *testing.T, roots *x509.CertPool, host, path string) *websocket.Conn {
	t.Helper()
	d := websocket.Dialer{
		TLSClientConfig: &tls.Config{RootCAs: roots, ServerName: host},
		NetDialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, network, composeShieldHTTPS)
		},
	}
	conn, resp, err := d.Dial("wss://"+host+path, nil)
	if err != nil {
		t.Fatalf("WebSocket dial wss://%s%s failed: %v", host, path, err)
	}
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	return conn
}

func assertWebSocketEcho(t *testing.T, conn *websocket.Conn, msg, want string) {
	t.Helper()
	if err := conn.WriteMessage(websocket.TextMessage, []byte(msg)); err != nil {
		t.Fatalf("ws write %q: %v", msg, err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, got, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("ws read %q: %v", msg, err)
	}
	if string(got) != want {
		t.Fatalf("ws echo mismatch: got %q want %q", string(got), want)
	}
}
