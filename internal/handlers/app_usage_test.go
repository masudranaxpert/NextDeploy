package handlers

import "testing"

func TestParsePHPPoolProcOutput(t *testing.T) {
	raw := `
site-20000 40852 536
site-20000 41256 524
site-20002 10224 0
site-20005 168436 7069
site-20005 172100 4004
master 39000 100
`
	got := parsePHPPoolProcOutput(raw)
	if len(got) != 3 {
		t.Fatalf("expected 3 pools, got %d (%+v)", len(got), got)
	}
	if got["site-20000"].rssKB != 82108 || got["site-20000"].jiffies != 1060 {
		t.Errorf("site-20000 = %+v", got["site-20000"])
	}
	if got["site-20002"].rssKB != 10224 || got["site-20002"].jiffies != 0 {
		t.Errorf("site-20002 = %+v", got["site-20002"])
	}
	if got["site-20005"].rssKB != 340536 || got["site-20005"].jiffies != 11073 {
		t.Errorf("site-20005 = %+v", got["site-20005"])
	}
}
