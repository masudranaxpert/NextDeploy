package edge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestPollerStateMachineAndAuthEAPI(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 11, 11, 4, 0, 0, 0, time.UTC)

	var mode atomic.Int32
	var seenAuthHeader atomic.Value
	var seenIfNoneMatch atomic.Value

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenAuthHeader.Store(r.Header.Get("Authorization"))
		seenIfNoneMatch.Store(r.Header.Get("If-None-Match"))

		switch mode.Load() {
		case 0: // 200 valid snapshot
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(RoutesResponse{
				APIVersion: 1,
				PanelID:    "panel-1",
				ETag:       "etag-1",
				Origin:     OriginInfo{IPs: []string{"204.136.10.51"}, Port: 443, CAStatus: CAStatusMissing},
				Routes:     []EdgeRoute{{ID: "r1", Kind: KindAppDomain, Hosts: []string{"app.example.com"}}},
				Node:       NodeInfo{ID: 1, IP: "107.189.6.222"},
			})
		case 1: // 304 Not Modified (E-API-12)
			w.WriteHeader(http.StatusNotModified)
		case 2: // 401 auth failed (E-API-2)
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
		case 3: // 404 panel outdated (E-API-3)
			w.WriteHeader(http.StatusNotFound)
		case 4: // 410 Gone (E-API-4)
			w.WriteHeader(http.StatusGone)
			_, _ = w.Write([]byte(`{"reason":"node_deleted"}`))
		case 5: // 500 error (E-API-5)
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("internal error"))
		case 6: // Malformed JSON (E-API-6)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{bad json`))
		case 7: // Wrong api_version (E-API-6)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(RoutesResponse{
				APIVersion: 99,
				PanelID:    "panel-1",
				ETag:       "etag-99",
			})
		case 8: // WAF HTML challenge (E-API-10)
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`<!DOCTYPE html><html><title>Just a moment...</title><body>cf-browser-verification</body></html>`))
		}
	}))
	defer srv.Close()

	// Compute SHA-256 pin of test TLS server certificate (E-API-11).
	leaf := srv.Certificate()
	sum := sha256.Sum256(leaf.Raw)
	pin := hex.EncodeToString(sum[:])

	pc := PanelConfig{
		PanelID:         "panel-1",
		PanelName:       "vps-1",
		URL:             srv.URL,
		Token:           "shld_secret_token_value",
		PollIntervalSec: 30,
		PanelCASHA256:   pin,
	}
	client := NewPanelClient(pc, nil, nil)
	st := NewPanelState("panel-1", "vps-1")

	// 1. Initial 200 OK
	out := PollPanelOnce(ctx, client, st, "", false, now)
	if out.Err != nil || !out.SnapshotUpdated || st.LastGoodETag != "etag-1" {
		t.Fatalf("expected initial 200 update, got %+v st=%+v", out, st)
	}
	if auth, _ := seenAuthHeader.Load().(string); auth != "Bearer shld_secret_token_value" {
		t.Fatalf("unexpected Authorization header: %q", auth)
	}

	// 2. 304 Not Modified (E-API-12)
	mode.Store(1)
	out = PollPanelOnce(ctx, client, st, "", false, now.Add(30*time.Second))
	if out.Err != nil || out.SnapshotUpdated || st.SyncState != SyncStateOK {
		t.Fatalf("expected 304 OK, got %+v state=%s", out, st.SyncState)
	}
	if inm, _ := seenIfNoneMatch.Load().(string); inm != "etag-1" {
		t.Fatalf("expected If-None-Match=etag-1, got %q", inm)
	}

	// 3. 401 keeps LastGood and sets auth_failed (E-API-2)
	mode.Store(2)
	out = PollPanelOnce(ctx, client, st, "", false, now.Add(time.Minute))
	if out.Err == nil || st.SyncState != SyncStateAuthFailed || st.LastGood == nil {
		t.Fatalf("expected auth_failed keeping LastGood, got out=%+v state=%s", out, st.SyncState)
	}

	// 4. 404 keeps LastGood and sets panel_outdated (E-API-3)
	mode.Store(3)
	out = PollPanelOnce(ctx, client, st, "", false, now.Add(2*time.Minute))
	if out.Err == nil || st.SyncState != SyncStatePanelOutdate || st.LastGood == nil {
		t.Fatalf("expected panel_outdated keeping LastGood, got out=%+v state=%s", out, st.SyncState)
	}

	// 5. 5xx keeps LastGood and sets stale (E-API-5)
	mode.Store(5)
	out = PollPanelOnce(ctx, client, st, "", false, now.Add(3*time.Minute))
	if out.Err == nil || st.SyncState != SyncStateStale || st.LastGood == nil {
		t.Fatalf("expected stale keeping LastGood on 500, got out=%+v state=%s", out, st.SyncState)
	}

	// 6. Malformed JSON & wrong api_version reject snapshot, keep LastGood (E-API-6)
	mode.Store(6)
	out = PollPanelOnce(ctx, client, st, "", false, now.Add(4*time.Minute))
	if out.Err == nil || st.LastGoodETag != "etag-1" {
		t.Fatalf("expected malformed JSON rejection keeping etag-1, got %+v", out)
	}
	mode.Store(7)
	out = PollPanelOnce(ctx, client, st, "", false, now.Add(5*time.Minute))
	if out.Err == nil || st.LastGoodETag != "etag-1" {
		t.Fatalf("expected api_version=99 rejection keeping etag-1, got %+v", out)
	}

	// 7. WAF HTML challenge detected (E-API-10)
	mode.Store(8)
	out = PollPanelOnce(ctx, client, st, "", false, now.Add(6*time.Minute))
	if out.Err == nil || st.SyncState != SyncStateBlockedWAF || st.LastGood == nil {
		t.Fatalf("expected panel_blocked_by_waf, got out=%+v state=%s", out, st.SyncState)
	}

	// 8. 410 requires 3 consecutive 410s spanning >= 10 min before release (E-API-4)
	mode.Store(4)
	t410 := now.Add(10 * time.Minute)
	out = PollPanelOnce(ctx, client, st, "", false, t410)
	if out.PanelReleased || st.LastGood == nil {
		t.Fatalf("should not release on 1st 410")
	}
	out = PollPanelOnce(ctx, client, st, "", false, t410.Add(5*time.Minute))
	if out.PanelReleased || st.LastGood == nil {
		t.Fatalf("should not release on 2nd 410 at +5m")
	}
	out = PollPanelOnce(ctx, client, st, "", false, t410.Add(9*time.Minute))
	if out.PanelReleased || st.LastGood == nil {
		t.Fatalf("should not release before 10m elapsed even on 3rd 410")
	}
	out = PollPanelOnce(ctx, client, st, "", false, t410.Add(10*time.Minute))
	if !out.PanelReleased || st.SyncState != SyncStateDeleted || st.LastGood != nil {
		t.Fatalf("expected panel release after 3x 410 >=10m, got out=%+v st=%+v", out, st)
	}

	// 9. Bad TLS pin rejected (E-API-11)
	badPinPC := pc
	badPinPC.PanelCASHA256 = strings.Repeat("00", 32)
	badClient := NewPanelClient(badPinPC, nil, nil)
	if _, _, err := badClient.FetchRoutes(ctx, ""); err == nil {
		t.Fatalf("expected mismatched CA pin to fail TLS verification")
	}
}

func TestMassDeletionGuard(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 11, 11, 4, 0, 0, 0, time.UTC)

	makeRoutes := func(n int) []EdgeRoute {
		var routes []EdgeRoute
		for i := 0; i < n; i++ {
			routes = append(routes, EdgeRoute{
				ID:    fmt.Sprintf("r_%d", i),
				Kind:  KindAppDomain,
				Hosts: []string{fmt.Sprintf("h%d.example.com", i)},
			})
		}
		return routes
	}

	var curResp atomic.Value
	curResp.Store(RoutesResponse{
		APIVersion: 1,
		PanelID:    "panel-1",
		ETag:       "etag-10",
		Origin:     OriginInfo{IPs: []string{"204.136.10.51"}, Port: 443, CAStatus: CAStatusMissing},
		Routes:     makeRoutes(10),
	})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := curResp.Load().(RoutesResponse)
		if r.Header.Get("If-None-Match") == resp.ETag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	client := NewPanelClient(PanelConfig{
		PanelID:      "panel-1",
		PanelName:    "vps-1",
		URL:          srv.URL,
		Token:        "tok",
		InsecureHTTP: true,
	}, nil, nil)
	st := NewPanelState("panel-1", "vps-1")

	// Initial load of 10 hosts.
	if out := PollPanelOnce(ctx, client, st, "", false, now); !out.SnapshotUpdated {
		t.Fatalf("expected initial update, got %+v", out)
	}

	// Mass delete: 10 -> 2 hosts (80% & >5 removed) held; a 304 revert clears Pending*.
	curResp.Store(RoutesResponse{
		APIVersion: 1,
		PanelID:    "panel-1",
		ETag:       "etag-2",
		Origin:     OriginInfo{IPs: []string{"204.136.10.51"}, Port: 443, CAStatus: CAStatusMissing},
		Routes:     makeRoutes(2),
	})
	out := PollPanelOnce(ctx, client, st, "", false, now.Add(15*time.Second))
	if !out.HeldMassDelete || st.PendingCount != 1 {
		t.Fatalf("expected transient poll 1 held, got %+v", out)
	}
	// Panel reverts to etag-10 -> server returns 304 -> clears pending candidate.
	curResp.Store(RoutesResponse{
		APIVersion: 1,
		PanelID:    "panel-1",
		ETag:       "etag-10",
		Origin:     OriginInfo{IPs: []string{"204.136.10.51"}, Port: 443, CAStatus: CAStatusMissing},
		Routes:     makeRoutes(10),
	})
	out = PollPanelOnce(ctx, client, st, "", false, now.Add(20*time.Second))
	if out.Err != nil || st.PendingSnapshot != nil || st.PendingCount != 0 || st.PendingETag != "" {
		t.Fatalf("expected 304 to clear held mass-delete state, got out=%+v pending=%d/%q", out, st.PendingCount, st.PendingETag)
	}

	// Real mass delete: 10 -> 2 hosts (80% & >5 removed) held for 3 polls >= 3m.
	curResp.Store(RoutesResponse{
		APIVersion: 1,
		PanelID:    "panel-1",
		ETag:       "etag-2",
		Origin:     OriginInfo{IPs: []string{"204.136.10.51"}, Port: 443, CAStatus: CAStatusMissing},
		Routes:     makeRoutes(2),
	})

	out = PollPanelOnce(ctx, client, st, "", false, now.Add(30*time.Second))
	if !out.HeldMassDelete || out.SnapshotUpdated || st.LastGoodETag != "etag-10" {
		t.Fatalf("poll 1 should hold mass delete, got %+v", out)
	}
	out = PollPanelOnce(ctx, client, st, "", false, now.Add(2*time.Minute))
	if !out.HeldMassDelete || out.SnapshotUpdated || st.LastGoodETag != "etag-10" {
		t.Fatalf("poll 2 should hold mass delete, got %+v", out)
	}
	// Poll 3 at +3m30s (>= 3 min since first seen at +30s) -> releases!
	out = PollPanelOnce(ctx, client, st, "", false, now.Add(3*time.Minute+30*time.Second))
	if out.HeldMassDelete || !out.SnapshotUpdated || st.LastGoodETag != "etag-2" {
		t.Fatalf("poll 3 >= 3m should apply mass delete, got %+v etag=%s", out, st.LastGoodETag)
	}

	// Empty routes (all removed) held unless force=true (E-API-14).
	curResp.Store(RoutesResponse{
		APIVersion: 1,
		PanelID:    "panel-1",
		ETag:       "etag-0",
		Origin:     OriginInfo{IPs: []string{"204.136.10.51"}, Port: 443, CAStatus: CAStatusMissing},
		Routes:     nil,
	})
	out = PollPanelOnce(ctx, client, st, "", false, now.Add(4*time.Minute))
	if !out.HeldMassDelete || st.LastGoodETag != "etag-2" {
		t.Fatalf("empty routes should be held by mass-delete guard, got %+v", out)
	}
	// Force bypasses guard.
	out = PollPanelOnce(ctx, client, st, "", true, now.Add(4*time.Minute+5*time.Second))
	if !out.SnapshotUpdated || st.LastGoodETag != "etag-0" {
		t.Fatalf("force=true should bypass mass-delete guard, got %+v", out)
	}

	// Additions are never held.
	curResp.Store(RoutesResponse{
		APIVersion: 1,
		PanelID:    "panel-1",
		ETag:       "etag-12",
		Origin:     OriginInfo{IPs: []string{"204.136.10.51"}, Port: 443, CAStatus: CAStatusMissing},
		Routes:     makeRoutes(12),
	})
	out = PollPanelOnce(ctx, client, st, "", false, now.Add(5*time.Minute))
	if out.HeldMassDelete || !out.SnapshotUpdated {
		t.Fatalf("additions must never be held, got %+v", out)
	}
}

func TestCFIPStoreRefresh(t *testing.T) {
	v4Srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("173.245.48.0/20\n104.16.0.0/13\n"))
	}))
	defer v4Srv.Close()
	v6Srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("2400:cb00::/32\n"))
	}))
	defer v6Srv.Close()

	paths := NewPaths(t.TempDir())
	store := NewCFIPStore()
	if err := store.Refresh(context.Background(), v4Srv.Client(), paths.CFIPsFile, v4Srv.URL, v6Srv.URL); err != nil {
		t.Fatalf("Refresh failed: %v", err)
	}
	if len(store.Ranges()) != 3 {
		t.Fatalf("expected 3 refreshed ranges, got %v", store.Ranges())
	}
	reloaded := LoadOrInitCFIPStore(paths.CFIPsFile)
	if len(reloaded.Ranges()) != 3 {
		t.Fatalf("expected reloaded cache to have 3 ranges, got %v", reloaded.Ranges())
	}
}

func TestTokenRedactionAndHTTPSRequirement(t *testing.T) {
	pc := PanelConfig{PanelID: "p1", Token: "shld_super_secret", URL: "http://insecure.example.com"}
	red := pc.Redacted()
	if red.Token == pc.Token || strings.Contains(red.Token, "shld_") {
		t.Fatalf("token not redacted: %q", red.Token)
	}
	c := NewPanelClient(pc, nil, nil)
	_, _, err := c.FetchRoutes(context.Background(), "")
	if err == nil || !strings.Contains(err.Error(), "https://") {
		t.Fatalf("expected https:// enforcement error, got %v", err)
	}
	_ = errors.New("unused")
}

func TestNextPollDelayBackoff(t *testing.T) {
	checkBounds := func(name string, got, base time.Duration) {
		t.Helper()
		min := time.Duration(float64(base) * 0.79)
		max := time.Duration(float64(base) * 1.21)
		if got < min || got > max {
			t.Fatalf("%s: got %s, expected within ±20%% of %s (%s..%s)", name, got, base, min, max)
		}
	}

	for i := 0; i < 10; i++ {
		checkBounds("normal_30s", NextPollDelay(30, 0, SyncStateOK), 30*time.Second)
		checkBounds("auth_failed_5m", NextPollDelay(30, 1, SyncStateAuthFailed), 5*time.Minute)
		checkBounds("err_1_30s", NextPollDelay(30, 1, SyncStateStale), 30*time.Second)
		checkBounds("err_2_60s", NextPollDelay(30, 2, SyncStateStale), 60*time.Second)
		checkBounds("err_3_120s", NextPollDelay(30, 3, SyncStateStale), 120*time.Second)
		checkBounds("err_4_240s", NextPollDelay(30, 4, SyncStateStale), 240*time.Second)
		checkBounds("err_5_cap_5m", NextPollDelay(30, 5, SyncStateStale), 5*time.Minute)
		checkBounds("err_10_cap_5m", NextPollDelay(30, 10, SyncStateStale), 5*time.Minute)
	}
}
