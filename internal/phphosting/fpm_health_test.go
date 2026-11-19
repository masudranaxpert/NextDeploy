package phphosting

import "testing"

func TestParseFPMSaturation(t *testing.T) {
	logs := `[18-Nov-2026 13:07:43] WARNING: [pool site-20000] seems busy (you may need to increase pm.start_servers), spawning 32 children
[18-Nov-2026 13:07:44] WARNING: [pool site-20000] server reached pm.max_children setting (20), consider raising it
[18-Nov-2026 18:58:01] WARNING: [pool site-20000] server reached pm.max_children setting (20), consider raising it
[18-Nov-2026 18:58:02] WARNING: [pool site-200001] server reached pm.max_children setting (20), consider raising it
[18-Nov-2026 18:58:03] WARNING: [pool site-20004] seems busy (you may need to increase pm.start_servers)`
	reached, busy, last := ParseFPMSaturation(logs, 20000, 20)
	if reached != 2 || busy != 1 || last != "18-Nov-2026 18:58:01" {
		t.Fatalf("reached=%d busy=%d last=%q", reached, busy, last)
	}
	if r, b, _ := ParseFPMSaturation(logs, 20004, 20); r != 0 || b != 1 {
		t.Fatalf("site-20004: reached=%d busy=%d", r, b)
	}
	if r, _, _ := ParseFPMSaturation(logs, 20000, 30); r != 0 {
		t.Fatalf("events logged under the old limit 20 must not count after raising it to 30, got %d", r)
	}
}

func TestRecommendMaxChildren(t *testing.T) {
	cases := []struct {
		name                               string
		current, reached, avg, avail, ceil int
		want                               int
		ok                                 bool
	}{
		{"never saturated keeps value", 20, 0, 78, 4700, 100, 20, true},
		{"204 emwbd: 20 workers, 78 MB, 4.7 GB free", 20, 2, 78, 4700, 100, 30, true},
		{"rounded up to 5", 12, 1, 60, 8000, 100, 20, true},
		{"limited by free RAM", 20, 3, 100, 1000, 100, 25, true},
		{"no RAM to grow", 20, 3, 100, 100, 100, 20, false},
		{"owner ceiling caps it", 20, 3, 50, 9000, 24, 24, true},
		{"unknown worker size uses 64 MB", 10, 1, 0, 2000, 100, 15, true},
	}
	for _, tc := range cases {
		got, ok, _ := RecommendMaxChildren(tc.current, tc.reached, tc.avg, tc.avail, tc.ceil)
		if got != tc.want || ok != tc.ok {
			t.Errorf("%s: got %d,%v want %d,%v", tc.name, got, ok, tc.want, tc.ok)
		}
	}
}
