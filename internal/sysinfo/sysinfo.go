package sysinfo

import (
	"context"
	"runtime"

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
	CPUPercent float64
	HostNote   string
}

func Collect(ctx context.Context) Snapshot {
	s := Snapshot{
		GoVersion: runtime.Version(),
		NumCPU:    runtime.NumCPU(),
		HostNote:  "Docker Container (" + runtime.GOOS + "/" + runtime.GOARCH + ")",
	}
	if v, err := mem.VirtualMemoryWithContext(ctx); err == nil && v != nil {
		s.MemTotalGB = float64(v.Total) / (1024 * 1024 * 1024)
		s.MemUsedGB = float64(v.Used) / (1024 * 1024 * 1024)
		if v.Total > 0 {
			s.MemUsedPct = float64(v.Used) * 100 / float64(v.Total)
		}
	}
	if parts, err := disk.UsageWithContext(ctx, "/"); err == nil && parts != nil {
		s.DiskTotalGB = float64(parts.Total) / (1024 * 1024 * 1024)
		s.DiskUsedGB = float64(parts.Used) / (1024 * 1024 * 1024)
		s.DiskUsedPct = parts.UsedPercent
	}
	if pct, err := cpu.PercentWithContext(ctx, 0, false); err == nil && len(pct) > 0 {
		s.CPUPercent = pct[0]
	}
	return s
}
