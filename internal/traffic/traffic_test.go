package traffic

import (
	"testing"
	"time"
)

func at(min int64) time.Time { return time.Unix(min*60+5, 0) }

func TestDeltasPerMinuteAndWindow(t *testing.T) {
	s := NewStore()
	s.Observe(at(100), "angie", map[string]Counters{"a": {Requests: 1000, S2xx: 900, S5xx: 100}}, true, "")
	s.Observe(at(101), "angie", map[string]Counters{"a": {Requests: 1010, S2xx: 908, S5xx: 102}}, true, "")
	s.Observe(at(102), "angie", map[string]Counters{"a": {Requests: 1040, S2xx: 938, S5xx: 102}}, true, "")

	snap := s.Summary(at(103), 30, nil)
	if len(snap.Sites) != 1 {
		t.Fatalf("sites = %d", len(snap.Sites))
	}
	a := snap.Sites[0]
	if a.Total.Requests != 40 || a.Total.S5xx != 2 || a.LastMin != 30 {
		t.Fatalf("site a = %+v", a)
	}
	if snap.Total.Requests != 40 || snap.PerMin[len(snap.PerMin)-1] != 30 {
		t.Fatalf("totals = %+v %v", snap.Total, snap.PerMin)
	}
	if snap.Coverage != 2 {
		t.Fatalf("coverage = %d, want 2", snap.Coverage)
	}
}

func TestProxyRestartCountsFromZero(t *testing.T) {
	s := NewStore()
	s.Observe(at(10), "angie", map[string]Counters{"a": {Requests: 500}}, true, "")
	s.Observe(at(11), "angie", map[string]Counters{"a": {Requests: 7}}, true, "")
	if got := s.Summary(at(12), 30, nil).Sites[0].Total.Requests; got != 7 {
		t.Fatalf("after restart got %d, want 7", got)
	}
}

func TestEngineChangeResetsBaseline(t *testing.T) {
	s := NewStore()
	s.Observe(at(10), "caddy", map[string]Counters{"a": {Requests: 900}}, true, "")
	s.Observe(at(11), "angie", map[string]Counters{"a": {Requests: 5}}, true, "")
	if snap := s.Summary(at(12), 30, nil); snap.Total.Requests != 0 || snap.Source != "angie" {
		t.Fatalf("engine switch must start a fresh baseline: %+v", snap)
	}
}

func TestOldMinutesFallOutAndFilter(t *testing.T) {
	s := NewStore()
	s.Observe(at(0), "angie", map[string]Counters{"a": {}, "b": {}}, true, "")
	s.Observe(at(1), "angie", map[string]Counters{"a": {Requests: 50}, "b": {Requests: 5}}, true, "")
	// 40 minutes later the 30-minute window no longer covers minute 1.
	s.Observe(at(41), "angie", map[string]Counters{"a": {Requests: 60}, "b": {Requests: 5}}, true, "")
	snap := s.Summary(at(42), 30, func(k string) bool { return k == "a" })
	if len(snap.Sites) != 1 || snap.Sites[0].Total.Requests != 10 {
		t.Fatalf("snap = %+v", snap.Sites)
	}
}

func TestForgetsVanishedSites(t *testing.T) {
	s := NewStore()
	s.Observe(at(0), "angie", map[string]Counters{"gone": {Requests: 1}}, true, "")
	s.Observe(at(Window+2), "angie", map[string]Counters{"a": {Requests: 1}}, true, "")
	if _, ok := s.sites["gone"]; ok {
		t.Fatal("a site missing for longer than the window must be dropped")
	}
}
