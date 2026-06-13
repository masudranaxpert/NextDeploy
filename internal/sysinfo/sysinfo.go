package sysinfo

import (
	"context"
	"runtime"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/mem"
)

type Snapshot struct {
	GoVersion    string
	NumCPU       int
	MemTotalGB   float64
	MemUsedGB    float64
	MemUsedPct   float64
	DiskTotalGB  float64
	DiskUsedGB   float64
	DiskUsedPct  float64
	CPUPercent   float64
	HostNote     string
}

const diskCacheTTL = 45 * time.Second

var (
	staticOnce sync.Once
	staticBase Snapshot

	diskMu   sync.RWMutex
	diskData Snapshot
	diskAt   time.Time
)

func initStatic() {
	staticBase = Snapshot{
		GoVersion: runtime.Version(),
		NumCPU:    runtime.NumCPU(),
		HostNote:  "Docker Container (" + runtime.GOOS + "/" + runtime.GOARCH + ")",
	}
}

func refreshDisk(ctx context.Context) {
	diskMu.RLock()
	if time.Since(diskAt) < diskCacheTTL {
		diskMu.RUnlock()
		return
	}
	diskMu.RUnlock()

	var d Snapshot
	if parts, err := disk.UsageWithContext(ctx, "/"); err == nil && parts != nil {
		d.DiskTotalGB = float64(parts.Total) / (1024 * 1024 * 1024)
		d.DiskUsedGB = float64(parts.Used) / (1024 * 1024 * 1024)
		d.DiskUsedPct = parts.UsedPercent
	}

	diskMu.Lock()
	diskData = d
	diskAt = time.Now()
	diskMu.Unlock()
}

func Collect(ctx context.Context) Snapshot {
	return CollectFast(ctx)
}

func CollectFast(ctx context.Context) Snapshot {
	staticOnce.Do(initStatic)
	refreshDisk(ctx)

	s := staticBase
	diskMu.RLock()
	s.DiskTotalGB = diskData.DiskTotalGB
	s.DiskUsedGB = diskData.DiskUsedGB
	s.DiskUsedPct = diskData.DiskUsedPct
	diskMu.RUnlock()

	if v, err := mem.VirtualMemoryWithContext(ctx); err == nil && v != nil {
		s.MemTotalGB = float64(v.Total) / (1024 * 1024 * 1024)
		s.MemUsedGB = float64(v.Used) / (1024 * 1024 * 1024)
		if v.Total > 0 {
			s.MemUsedPct = float64(v.Used) * 100 / float64(v.Total)
		}
	}
	if pct, err := cpu.PercentWithContext(ctx, 0, false); err == nil && len(pct) > 0 {
		s.CPUPercent = pct[0]
	}
	return s
}

func InvalidateDiskCache() {
	diskMu.Lock()
	diskAt = time.Time{}
	diskMu.Unlock()
}
