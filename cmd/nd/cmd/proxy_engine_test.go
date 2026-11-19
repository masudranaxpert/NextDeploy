package cmd

import "testing"

func TestParseEngineTarget(t *testing.T) {
	if e, f, err := parseEngineTarget([]string{"Angie", "--yes"}); err != nil || e != "angie" || !f["--yes"] {
		t.Fatalf("got %q %v %v", e, f, err)
	}
	for _, bad := range [][]string{{}, {"nginx"}, {"angie", "caddy"}, {"angie", "--force"}} {
		if _, _, err := parseEngineTarget(bad); err == nil {
			t.Errorf("%v: expected an error", bad)
		}
	}
}
