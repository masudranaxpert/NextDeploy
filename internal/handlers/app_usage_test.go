package handlers

import (
	"testing"
	"time"
)

func TestParsePHPPoolProcOutput(t *testing.T) {
	raw := `
site-20000 32000 536
site-20000 33000 524
site-20002 32900 0
site-20005 105000 7069
site-20005 105000 4004
master 8100 100
`
	got := parsePHPPoolProcOutput(raw)
	if len(got) != 3 {
		t.Fatalf("expected 3 pools, got %d (%+v)", len(got), got)
	}
	if got["site-20000"].anonKB != 65000 || got["site-20000"].jiffies != 1060 {
		t.Errorf("site-20000 = %+v", got["site-20000"])
	}
	if got["site-20002"].anonKB != 32900 || got["site-20002"].jiffies != 0 {
		t.Errorf("site-20002 = %+v", got["site-20002"])
	}
	if got["site-20005"].anonKB != 210000 || got["site-20005"].jiffies != 11073 {
		t.Errorf("site-20005 = %+v", got["site-20005"])
	}
}

func TestAttributeContainerUsageToPools(t *testing.T) {
	t0 := time.Now()
	t1 := t0.Add(10 * time.Second)
	prev := map[string]poolTickSnapshot{
		"nd-php83-u1\x00site-20005": {jiffies: 1000, sampledAt: t0},
		"nd-php83-u1\x00site-20002": {jiffies: 0, sampledAt: t0},
	}
	parsed := map[string]poolProcSample{
		"site-20005": {anonKB: 210000, jiffies: 1150}, // +150 ticks in 10s = 15%
		"site-20002": {anonKB: 32900, jiffies: 0},     // 0 ticks = idle
	}
	got := attributeContainerUsageToPools(150, 16.0, parsed, prev, "nd-php83-u1", t1)
	sumMem := got["site-20005"].MemoryMB + got["site-20002"].MemoryMB
	if sumMem < 148 || sumMem > 152 {
		t.Fatalf("expected cgroup-bounded memory ~150MB, got sum=%d (%+v)", sumMem, got)
	}
	if got["site-20005"].MemoryMB <= got["site-20002"].MemoryMB {
		t.Errorf("busy pool should have more RAM than idle pool: %+v", got)
	}
	if got["site-20005"].CPUPercent != 15 || got["site-20002"].CPUPercent != 0 {
		t.Errorf("unexpected CPU attribution: %+v", got)
	}
}

func TestPHPPoolPerPIDWorkerRestartCPU(t *testing.T) {
	t0 := time.Now()
	t1 := t0.Add(10 * time.Second)
	raw0 := "site-20005 101 40000 500\nsite-20005 102 40000 500\n"
	p0 := parsePHPPoolProcOutput(raw0)
	prev := map[string]poolTickSnapshot{}
	attributeContainerUsageToPools(80, 20, p0, prev, "nd-php83-u1", t0)

	// Worker 102 exits and is replaced by brand-new worker 103 with 100 jiffies;
	// aggregate total drops from 1000 -> 650, yet per-PID tracking still measures +150 ticks = 15% CPU.
	raw1 := "site-20005 101 40000 550\nsite-20005 103 40000 100\n"
	p1 := parsePHPPoolProcOutput(raw1)
	got := attributeContainerUsageToPools(80, 20, p1, prev, "nd-php83-u1", t1)
	if got["site-20005"].CPUPercent != 15 {
		t.Fatalf("expected 15%% CPU across worker restart, got %+v", got["site-20005"])
	}
}
