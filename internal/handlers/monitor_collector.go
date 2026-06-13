package handlers

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"panel/internal/db"
	"panel/internal/dockerapi"
	"panel/internal/sysinfo"
)

const (
	monitorCollectorInterval = 2 * time.Second
	monitorResourceCacheTTL  = 30 * time.Second
	monitorCollectorTimeout  = 8 * time.Second
	monitorAuthRecheck       = 30 * time.Second
)

type monitorPayload struct {
	Sys                    sysinfo.Snapshot              `json:"sys"`
	UsageRows              []dockerapi.ContainerUsageRow `json:"usageRows"`
	DockerError            string                        `json:"dockerError"`
	UpdatedAt              string                        `json:"updatedAt"`
	TotalAllocatedMemoryGB float64                       `json:"totalAllocatedMemoryGB"`
	TotalAllocatedCPUs     float64                       `json:"totalAllocatedCPUs"`
	MemoryAllocatedPct     float64                       `json:"memoryAllocatedPct"`
	CPUAllocatedPct        float64                       `json:"cpuAllocatedPct"`
	LimitedUserCount       int                           `json:"limitedUserCount"`
	UsedMemoryGB           float64                       `json:"usedMemoryGB"`
	UsedMemoryPct          float64                       `json:"usedMemoryPct"`
	UsedCPUs               float64                       `json:"usedCPUs"`
	UsedCPUPct             float64                       `json:"usedCPUPct"`
}

type monitorCache struct {
	mu      sync.RWMutex
	body    []byte
	prev    map[string]dockerapi.ContainerUsageRow
	prevAt  time.Time
	resAt   time.Time
	resCnt  int
	resMem  int64
	resCPUs float64
}

func (p *Panel) monitorCacheBody() []byte {
	p.monitorCache.mu.RLock()
	defer p.monitorCache.mu.RUnlock()
	if len(p.monitorCache.body) == 0 {
		return nil
	}
	out := make([]byte, len(p.monitorCache.body))
	copy(out, p.monitorCache.body)
	return out
}

func (p *Panel) StartMonitorCollector() {
	go func() {
		ticker := time.NewTicker(monitorCollectorInterval)
		defer ticker.Stop()
		p.refreshMonitorSnapshot()
		for range ticker.C {
			p.refreshMonitorSnapshot()
		}
	}()
}

func (p *Panel) refreshMonitorSnapshot() {
	ctx, cancel := context.WithTimeout(context.Background(), monitorCollectorTimeout)
	defer cancel()

	now := time.Now()
	sys := sysinfo.CollectFast(ctx)
	rows, errMsg := dockerapi.ListContainerUsage(ctx)

	p.monitorCache.mu.Lock()
	defer p.monitorCache.mu.Unlock()

	elapsed := now.Sub(p.monitorCache.prevAt).Seconds()
	if elapsed <= 0 {
		elapsed = float64(monitorCollectorInterval.Seconds())
	}
	for i := range rows {
		if old, ok := p.monitorCache.prev[rows[i].ID]; ok {
			if rows[i].NetInput >= old.NetInput {
				rows[i].NetDLRateHuman = dockerapi.HumanBytes(uint64(float64(rows[i].NetInput-old.NetInput)/elapsed)) + "/s"
			} else {
				rows[i].NetDLRateHuman = "0 B/s"
			}
			if rows[i].NetOutput >= old.NetOutput {
				rows[i].NetULRateHuman = dockerapi.HumanBytes(uint64(float64(rows[i].NetOutput-old.NetOutput)/elapsed)) + "/s"
			} else {
				rows[i].NetULRateHuman = "0 B/s"
			}
		} else {
			rows[i].NetDLRateHuman = "—"
			rows[i].NetULRateHuman = "—"
		}
	}
	nextPrev := make(map[string]dockerapi.ContainerUsageRow, len(rows))
	for _, row := range rows {
		nextPrev[row.ID] = row
	}
	p.monitorCache.prev = nextPrev
	p.monitorCache.prevAt = now

	if p.monitorCache.resAt.IsZero() || time.Since(p.monitorCache.resAt) > monitorResourceCacheTTL {
		cnt, memMB, cpus, err := p.DB.GetLimitedUserResourceSummary(ctx)
		if err == nil {
			p.monitorCache.resCnt = cnt
			p.monitorCache.resMem = memMB
			p.monitorCache.resCPUs = cpus
			p.monitorCache.resAt = now
		}
	}

	memTotal := sys.MemTotalGB
	var memPct float64
	if memTotal > 0 {
		memPct = ((float64(p.monitorCache.resMem) / 1024.0) / memTotal) * 100.0
	}
	cpuTotal := float64(sys.NumCPU)
	var cpuPct float64
	if cpuTotal > 0 {
		cpuPct = (p.monitorCache.resCPUs / cpuTotal) * 100.0
	}

	var usedMemBytes uint64
	var usedCPUs float64
	for _, row := range rows {
		usedMemBytes += row.MemUsage
		usedCPUs += row.CPUPercent / 100.0
	}
	usedMemGB := float64(usedMemBytes) / (1024.0 * 1024.0 * 1024.0)
	var usedMemPct float64
	if memTotal > 0 {
		usedMemPct = (usedMemGB / memTotal) * 100.0
	}
	var usedCPUPct float64
	if cpuTotal > 0 {
		usedCPUPct = (usedCPUs / cpuTotal) * 100.0
	}

	payload := monitorPayload{
		Sys:                    sys,
		UsageRows:              rows,
		DockerError:            errMsg,
		UpdatedAt:              now.Format(time.RFC3339),
		TotalAllocatedMemoryGB: float64(p.monitorCache.resMem) / 1024.0,
		TotalAllocatedCPUs:     p.monitorCache.resCPUs,
		MemoryAllocatedPct:     memPct,
		CPUAllocatedPct:        cpuPct,
		LimitedUserCount:       p.monitorCache.resCnt,
		UsedMemoryGB:           usedMemGB,
		UsedMemoryPct:          usedMemPct,
		UsedCPUs:               usedCPUs,
		UsedCPUPct:             usedCPUPct,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return
	}
	p.monitorCache.body = body
}

func (p *Panel) InvalidateMonitorResourceCache() {
	p.monitorCache.mu.Lock()
	p.monitorCache.resAt = time.Time{}
	p.monitorCache.resCnt = 0
	p.monitorCache.resMem = 0
	p.monitorCache.resCPUs = 0
	p.monitorCache.mu.Unlock()
	go p.refreshMonitorSnapshot()
}

func (p *Panel) monitorAuthOK(u db.User) bool {
	dbUser, err := p.DB.GetUserByID(context.Background(), u.ID)
	if err != nil || dbUser.Status == db.UserStatusSuspended || dbUser.Role != db.RoleAdmin {
		return false
	}
	return true
}
