package handlers

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"panel/internal/db"
	"panel/internal/dockerapi"
	"panel/internal/dockerx"
	"panel/internal/phphosting"
)

// appUsage is what one project's containers use right now.
type appUsage struct {
	MemoryMB   int64   `json:"memory_mb"`
	CPUPercent float64 `json:"cpu_percent"` // 100 = one full core
}

type poolProcSample struct {
	anonKB  int64
	jiffies uint64
}

type poolTickSnapshot struct {
	jiffies   uint64
	sampledAt time.Time
}

const (
	usageSampleEvery = 10 * time.Second
	usageWindow      = 6 // samples kept: the last minute
	usageIdleAfter   = 3 * time.Minute

	// Reads private heap (RssAnon, avoiding OPcache/shared-lib multi-counting) and CPU ticks per pool.
	phpPoolProcScript = `for d in /proc/[0-9]*; do cmd=$(tr "\0" " " < "$d/cmdline" 2>/dev/null) || continue; case "$cmd" in "php-fpm: pool "*) pool=${cmd#php-fpm: pool }; pool=${pool%% *}; st=$(cat "$d/stat" 2>/dev/null) || continue; st=${st#*) }; set -- $st; mem=$(awk "/^RssAnon:/{a=\$2} /^VmRSS:/{r=\$2} END{print (a>0?a:r)+0}" "$d/status" 2>/dev/null); echo "$pool ${mem:-0} $((${12:-0}+${13:-0}))";; esac; done`
)

// A single one-second CPU reading of a lightly used site is nearly always 0, so the panel keeps
// the last minute of readings and reports their average. Sampling only runs while the panel is
// being looked at.
var usageSampler struct {
	mu            sync.Mutex
	hist          [][]dockerapi.ContainerUsageRow
	phpPools      map[string]map[string]appUsage
	prevPoolTicks map[string]poolTickSnapshot
	touched       time.Time
	started       bool
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
				samplePHPPools(ctx, rows)
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

func isPHPRuntimeContainer(cleanName string) bool {
	return strings.HasPrefix(cleanName, "nd-php") &&
		!strings.HasPrefix(cleanName, "nd-phpr-") &&
		cleanName != phphosting.MySQLContainer &&
		!strings.HasPrefix(cleanName, "nd-phpmyadmin")
}

func parsePHPPoolProcOutput(raw string) map[string]poolProcSample {
	out := make(map[string]poolProcSample)
	for _, line := range strings.Split(raw, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		pool := fields[0]
		if !strings.HasPrefix(pool, "site-") {
			continue
		}
		anonKB, err1 := strconv.ParseInt(fields[1], 10, 64)
		jiffies, err2 := strconv.ParseUint(fields[2], 10, 64)
		if err1 != nil || err2 != nil {
			continue
		}
		cur := out[pool]
		cur.anonKB += anonKB
		cur.jiffies += jiffies
		out[pool] = cur
	}
	return out
}

// attributeContainerUsageToPools scales per-pool private RssAnon against the container's true
// cgroup MemUsage (preventing OPcache/CoW double-counting across workers) and caps summed CPU%.
func attributeContainerUsageToPools(
	containerMemMB int64,
	containerCPUPct float64,
	parsed map[string]poolProcSample,
	prevTicks map[string]poolTickSnapshot,
	containerName string,
	now time.Time,
) map[string]appUsage {
	var totalAnonKB int64
	rawCPU := make(map[string]float64, len(parsed))
	var totalRawCPU float64

	for pool, s := range parsed {
		totalAnonKB += s.anonKB
		tickKey := containerName + "\x00" + pool
		if prev, ok := prevTicks[tickKey]; ok {
			elapsed := now.Sub(prev.sampledAt).Seconds()
			if elapsed > 0.5 && s.jiffies >= prev.jiffies {
				// Linux USER_HZ is 100 jiffies/s, so deltaJiffies / elapsedSec equals % of 1 core.
				cpu := float64(s.jiffies-prev.jiffies) / elapsed
				rawCPU[pool] = cpu
				totalRawCPU += cpu
			}
		}
		prevTicks[tickKey] = poolTickSnapshot{jiffies: s.jiffies, sampledAt: now}
	}

	out := make(map[string]appUsage, len(parsed))
	for pool, s := range parsed {
		memMB := s.anonKB / 1024
		if containerMemMB > 0 && totalAnonKB > 0 {
			memMB = int64(math.Round(float64(containerMemMB) * (float64(s.anonKB) / float64(totalAnonKB))))
		}
		cpuPct := rawCPU[pool]
		if containerCPUPct > 0 && totalRawCPU > containerCPUPct {
			cpuPct = containerCPUPct * (cpuPct / totalRawCPU)
		}
		out[pool] = appUsage{
			MemoryMB:   memMB,
			CPUPercent: math.Round(cpuPct*100) / 100,
		}
	}
	return out
}

func samplePHPPools(ctx context.Context, rows []dockerapi.ContainerUsageRow) {
	seen := map[string]bool{}
	for _, r := range rows {
		cleanName := strings.TrimPrefix(r.Name, "/")
		if !isPHPRuntimeContainer(cleanName) || seen[cleanName] {
			continue
		}
		seen[cleanName] = true
		res := dockerx.DockerExec(ctx, cleanName, phpPoolProcScript)
		if !res.OK && strings.TrimSpace(res.Output) == "" {
			continue
		}
		parsed := parsePHPPoolProcOutput(res.Output)
		now := time.Now()
		containerMemMB := int64(r.MemUsage / (1024 * 1024))

		usageSampler.mu.Lock()
		if usageSampler.phpPools == nil {
			usageSampler.phpPools = make(map[string]map[string]appUsage)
		}
		if usageSampler.prevPoolTicks == nil {
			usageSampler.prevPoolTicks = make(map[string]poolTickSnapshot)
		}
		usageSampler.phpPools[cleanName] = attributeContainerUsageToPools(
			containerMemMB,
			r.CPUPercent,
			parsed,
			usageSampler.prevPoolTicks,
			cleanName,
			now,
		)
		usageSampler.mu.Unlock()
	}
}

func phpContainerPoolUsage(containerName string) (map[string]appUsage, bool) {
	usageSampler.mu.Lock()
	defer usageSampler.mu.Unlock()
	if usageSampler.phpPools == nil {
		return nil, false
	}
	m, ok := usageSampler.phpPools[containerName]
	return m, ok
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
		samplePHPPools(ctx, rows)
		usageSampler.mu.Lock()
		if len(usageSampler.hist) == 0 && len(rows) > 0 {
			usageSampler.hist = [][]dockerapi.ContainerUsageRow{rows}
		}
		usageSampler.mu.Unlock()
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

// usageForApp adds up the usage of every compose project or PHP container that belongs to the app.
func (p *Panel) usageForApp(ctx context.Context, app db.App, byProject map[string]appUsage) appUsage {
	var out appUsage
	if len(byProject) > 0 {
		for _, proj := range p.composeProjectsForApps(ctx, []db.App{app}) {
			if u, ok := byProject[proj]; ok {
				out.MemoryMB += u.MemoryMB
				out.CPUPercent += u.CPUPercent
			}
		}
	}
	if p.DB.IsPHPSite(ctx, app.ID) {
		site, err := p.DB.GetPHPSite(ctx, app.ID)
		if err == nil {
			if phphosting.SettingsFor(site).Stopped {
				return appUsage{}
			}
			rows := p.usageRows(ctx)
			routeTarget := phphosting.RouteContainer(app.ID)
			runtimeTarget := phphosting.RuntimeContainer(site.OwnerID, site.PHPVersion)

			for _, r := range rows {
				if strings.TrimPrefix(r.Name, "/") == routeTarget {
					out.MemoryMB += int64(r.MemUsage / (1024 * 1024))
					out.CPUPercent += r.CPUPercent
					break
				}
			}

			if site.SiteType != phphosting.SiteTypeStatic {
				poolMap, hasPoolStats := phpContainerPoolUsage(runtimeTarget)
				if hasPoolStats {
					if pu, ok := poolMap[fmt.Sprintf("site-%d", site.UID)]; ok {
						out.MemoryMB += pu.MemoryMB
						out.CPUPercent += pu.CPUPercent
					}
					return out
				}

				activePeers := 1
				if allSites, sErr := p.DB.ListPHPSites(ctx); sErr == nil {
					cnt := 0
					for _, s := range allSites {
						if s.OwnerID == site.OwnerID && s.PHPVersion == site.PHPVersion &&
							s.SiteType != phphosting.SiteTypeStatic && !phphosting.SettingsFor(s).Stopped {
							cnt++
						}
					}
					if cnt > 1 {
						activePeers = cnt
					}
				}
				for _, r := range rows {
					if strings.TrimPrefix(r.Name, "/") == runtimeTarget {
						out.MemoryMB += int64(r.MemUsage/(1024*1024)) / int64(activePeers)
						out.CPUPercent += r.CPUPercent / float64(activePeers)
						break
					}
				}
			}
		}
	}
	return out
}
