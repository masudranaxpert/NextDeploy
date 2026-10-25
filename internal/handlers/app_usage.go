package handlers

import (
	"context"
	"sync"
	"time"

	"panel/internal/db"
	"panel/internal/dockerapi"
)

// appUsage is what one project's containers use right now.
type appUsage struct {
	MemoryMB   int64   `json:"memory_mb"`
	CPUPercent float64 `json:"cpu_percent"` // 100 = one full core
}

const (
	usageSampleEvery = 10 * time.Second
	usageWindow      = 6 // samples kept: the last minute
	usageIdleAfter   = 3 * time.Minute
)

// A single one-second CPU reading of a lightly used site is nearly always 0, so the panel keeps
// the last minute of readings and reports their average. Sampling only runs while the panel is
// being looked at.
var usageSampler struct {
	mu      sync.Mutex
	hist    [][]dockerapi.ContainerUsageRow
	touched time.Time
	started bool
}

func touchUsageSampler() {
	usageSampler.mu.Lock()
	usageSampler.touched = time.Now()
	usageSampler.mu.Unlock()
}

// startUsageSampler begins background sampling once.
func (p *Panel) startUsageSampler() {
	usageSampler.mu.Lock()
	if usageSampler.started {
		usageSampler.mu.Unlock()
		return
	}
	usageSampler.started = true
	usageSampler.mu.Unlock()
	go func() {
		for {
			usageSampler.mu.Lock()
			active := time.Since(usageSampler.touched) < usageIdleAfter
			usageSampler.mu.Unlock()
			if active {
				ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
				rows, _ := dockerapi.ListContainerUsageMatching(ctx, func(string, string) bool { return true })
				cancel()
				usageSampler.mu.Lock()
				usageSampler.hist = append(usageSampler.hist, rows)
				if len(usageSampler.hist) > usageWindow {
					usageSampler.hist = usageSampler.hist[len(usageSampler.hist)-usageWindow:]
				}
				usageSampler.mu.Unlock()
			}
			time.Sleep(usageSampleEvery)
		}
	}()
}

// usageRows is every container's usage with CPU averaged over the recent samples (memory is the
// latest). Before any sample exists it reads Docker once, directly.
func (p *Panel) usageRows(ctx context.Context) []dockerapi.ContainerUsageRow {
	touchUsageSampler()
	p.startUsageSampler()
	usageSampler.mu.Lock()
	hist := append([][]dockerapi.ContainerUsageRow(nil), usageSampler.hist...)
	usageSampler.mu.Unlock()
	if len(hist) == 0 {
		rows, _ := dockerapi.ListContainerUsageMatching(ctx, func(string, string) bool { return true })
		return rows
	}
	latest := hist[len(hist)-1]
	sum, n := map[string]float64{}, map[string]int{}
	for _, rows := range hist {
		for _, r := range rows {
			sum[r.ID] += r.CPUPercent
			n[r.ID]++
		}
	}
	out := make([]dockerapi.ContainerUsageRow, len(latest))
	for i, r := range latest {
		if n[r.ID] > 0 {
			r.CPUPercent = sum[r.ID] / float64(n[r.ID])
		}
		out[i] = r
	}
	return out
}

// projectUsage is the live memory and average CPU of every compose project.
func (p *Panel) projectUsage(ctx context.Context) map[string]appUsage {
	out := make(map[string]appUsage)
	for _, r := range p.usageRows(ctx) {
		if r.ComposeProject == "" {
			continue
		}
		u := out[r.ComposeProject]
		u.MemoryMB += int64(r.MemUsage / (1024 * 1024))
		u.CPUPercent += r.CPUPercent
		out[r.ComposeProject] = u
	}
	return out
}

// WarmUsage measures project usage once at start, so the first page load already has numbers.
func (p *Panel) WarmUsage() { p.usageRows(context.Background()) }

// usageForApp adds up the usage of every compose project that belongs to the app.
func (p *Panel) usageForApp(ctx context.Context, app db.App, byProject map[string]appUsage) appUsage {
	var out appUsage
	if len(byProject) == 0 {
		return out
	}
	for _, proj := range p.composeProjectsForApps(ctx, []db.App{app}) {
		if u, ok := byProject[proj]; ok {
			out.MemoryMB += u.MemoryMB
			out.CPUPercent += u.CPUPercent
		}
	}
	return out
}
