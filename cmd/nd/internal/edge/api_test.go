package edge

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

type edgeContractFixture struct {
	RegisterRequest   RegisterRequest   `json:"register_request"`
	RegisterResponse  RegisterResponse  `json:"register_response"`
	RoutesResponse    RoutesResponse    `json:"routes_response"`
	StatusRequest     StatusRequest     `json:"status_request"`
	UnregisterRequest UnregisterRequest `json:"unregister_request"`
}

func TestEdgeContractGolden(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "edge_contract.json"))
	if err != nil {
		t.Fatalf("read contract fixture: %v", err)
	}

	var doc edgeContractFixture
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&doc); err != nil {
		t.Fatalf("decode contract fixture strictly: %v", err)
	}

	if doc.RegisterRequest.EdgeID == "" || len(doc.RegisterRequest.PublicIPv4) != 1 {
		t.Fatalf("unexpected register_request: %+v", doc.RegisterRequest)
	}
	if doc.RegisterResponse.ShieldNodeID != 3 || doc.RegisterResponse.PollIntervalSec != 30 {
		t.Fatalf("unexpected register_response: %+v", doc.RegisterResponse)
	}
	if err := CheckAPIVersion(doc.RoutesResponse.APIVersion); err != nil {
		t.Fatalf("routes api_version check failed: %v", err)
	}
	if len(doc.RoutesResponse.Routes) != 1 || doc.RoutesResponse.Routes[0].ID != "dom_12" {
		t.Fatalf("unexpected routes: %+v", doc.RoutesResponse.Routes)
	}
	if len(doc.StatusRequest.Hosts) != 1 || !IsValidHostState(doc.StatusRequest.Hosts[0].State) {
		t.Fatalf("unexpected status hosts: %+v", doc.StatusRequest.Hosts)
	}
	if doc.StatusRequest.LastApplyAt == nil || doc.StatusRequest.Hosts[0].CertNotAfter == nil {
		t.Fatalf("expected timestamps in status_request")
	}
	if doc.UnregisterRequest.Reason != "panel_removed" {
		t.Fatalf("unexpected unregister_request: %+v", doc.UnregisterRequest)
	}

	// Round-trip marshal and verify no key drift against original JSON object keys.
	roundTripBytes, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal round-trip: %v", err)
	}

	var origMap, rtMap map[string]any
	if err := json.Unmarshal(raw, &origMap); err != nil {
		t.Fatalf("unmarshal origMap: %v", err)
	}
	if err := json.Unmarshal(roundTripBytes, &rtMap); err != nil {
		t.Fatalf("unmarshal rtMap: %v", err)
	}

	assertSameJSONShape(t, "", origMap, rtMap)
}

func assertSameJSONShape(t *testing.T, prefix string, want, got any) {
	t.Helper()
	switch w := want.(type) {
	case map[string]any:
		g, ok := got.(map[string]any)
		if !ok {
			t.Fatalf("%s: expected object, got %T", prefix, got)
		}
		for k, wv := range w {
			gv, exists := g[k]
			if !exists {
				t.Fatalf("%s.%s: missing in marshaled output", prefix, k)
			}
			assertSameJSONShape(t, prefix+"."+k, wv, gv)
		}
		for k := range g {
			if _, exists := w[k]; !exists {
				t.Fatalf("%s.%s: unexpected extra key in marshaled output", prefix, k)
			}
		}
	case []any:
		g, ok := got.([]any)
		if !ok {
			t.Fatalf("%s: expected array, got %T", prefix, got)
		}
		if len(w) != len(g) {
			t.Fatalf("%s: array len want %d got %d", prefix, len(w), len(g))
		}
		for i := range w {
			assertSameJSONShape(t, prefix+"[]", w[i], g[i])
		}
	}
}
