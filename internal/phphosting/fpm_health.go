package phphosting

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"panel/internal/db"
)

// FPMHealth tells whether a site's PHP-FPM pool ran out of workers recently and what limit
// would fit, the way cPanel's "max_children reached" notice does, but with a concrete number
// based on the measured memory per worker and the RAM the server really has free.
type FPMHealth struct {
	MaxChildren      int    `json:"max_children"`
	ReachedCount     int    `json:"reached_count"`     // "server reached pm.max_children" in the window
	BusyCount        int    `json:"busy_count"`        // "seems busy" warnings in the window
	LastReachedAt    string `json:"last_reached_at"`   // as logged by php-fpm, e.g. 18-Nov-2026 18:58:01
	WindowHours      int    `json:"window_hours"`      // log window that was examined
	Workers          int    `json:"workers"`           // worker processes alive now
	AvgWorkerMB      int    `json:"avg_worker_mb"`     // measured; 0 when unknown
	AvailableMB      int    `json:"available_mb"`      // host MemAvailable; 0 when unknown
	Recommended      int    `json:"recommended"`       // suggested max_children (== MaxChildren when no change is needed)
	RecommendationOK bool   `json:"recommendation_ok"` // false when RAM does not allow raising it
	Note             string `json:"note,omitempty"`
}

const fpmHealthWindow = 7 * 24 * time.Hour

var (
	fpmHealthMu    sync.Mutex
	fpmHealthCache = map[string]fpmHealthEntry{}
)

var (
	fpmLogMu    sync.Mutex
	fpmLogCache = map[string]fpmLogEntry{}
)

type fpmLogEntry struct {
	at  time.Time
	out string
}

type fpmHealthEntry struct {
	at time.Time
	h  FPMHealth
}

// fpmLogFetcher and fpmWorkerRSS are replaceable in tests.
var (
	fpmLogFetcher = func(ctx context.Context, container string, since time.Duration) string {
		// One shared runtime container serves many sites and its log also carries PHP warnings
		// (catch_workers_output), so read it once per container every 5 minutes and cap the size.
		fpmLogMu.Lock()
		if e, ok := fpmLogCache[container]; ok && time.Since(e.at) < 5*time.Minute {
			fpmLogMu.Unlock()
			return e.out
		}
		fpmLogMu.Unlock()
		out := docker(ctx, "logs", "--since", fmt.Sprintf("%dh", int(since.Hours())), "--tail", "50000", container).Output
		fpmLogMu.Lock()
		fpmLogCache[container] = fpmLogEntry{at: time.Now(), out: out}
		fpmLogMu.Unlock()
		return out
	}
	fpmWorkerRSS = func(ctx context.Context, container string, uid int) []int {
		script := fmt.Sprintf(`for p in $(pgrep -f "pool site-%d$"); do grep VmRSS /proc/$p/status 2>/dev/null; done`, uid)
		out := docker(ctx, "exec", container, "sh", "-c", script).Output
		var kb []int
		for _, line := range strings.Split(out, "\n") {
			f := strings.Fields(line)
			if len(f) >= 2 && f[0] == "VmRSS:" {
				if v, err := strconv.Atoi(f[1]); err == nil && v > 0 {
					kb = append(kb, v)
				}
			}
		}
		return kb
	}
	hostAvailableMB = func() int {
		f, err := os.Open("/proc/meminfo")
		if err != nil {
			return 0
		}
		defer f.Close()
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			fields := strings.Fields(sc.Text())
			if len(fields) >= 2 && fields[0] == "MemAvailable:" {
				v, _ := strconv.Atoi(fields[1])
				return v / 1024
			}
		}
		return 0
	}
)

// FPMHealth returns the pool health of a site; results are cached for two minutes because the
// PHP settings page polls the site detail.
func (m *Manager) FPMHealth(ctx context.Context, site db.PHPSite, s SiteSettings, ceil Ceilings) FPMHealth {
	s = s.withDefaults()
	key := fmt.Sprintf("%s|%d|%d", site.AppID, s.MaxChildren, ceil.MaxChildrenFor())
	fpmHealthMu.Lock()
	if e, ok := fpmHealthCache[key]; ok && time.Since(e.at) < 2*time.Minute {
		fpmHealthMu.Unlock()
		return e.h
	}
	fpmHealthMu.Unlock()

	container := RuntimeContainer(site.OwnerID, site.PHPVersion)
	cctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	reached, busy, last := ParseFPMSaturation(fpmLogFetcher(cctx, container, fpmHealthWindow), site.UID, s.MaxChildren)
	rss := fpmWorkerRSS(cctx, container, site.UID)
	avg := 0
	if len(rss) > 0 {
		sum := 0
		for _, v := range rss {
			sum += v
		}
		avg = (sum/len(rss) + 1023) / 1024
	}
	avail := hostAvailableMB()
	rec, ok, note := RecommendMaxChildren(s.MaxChildren, reached, avg, avail, ceil.MaxChildrenFor())
	h := FPMHealth{
		MaxChildren: s.MaxChildren, ReachedCount: reached, BusyCount: busy, LastReachedAt: last,
		WindowHours: int(fpmHealthWindow.Hours()), Workers: len(rss), AvgWorkerMB: avg, AvailableMB: avail,
		Recommended: rec, RecommendationOK: ok, Note: note,
	}
	fpmHealthMu.Lock()
	fpmHealthCache[key] = fpmHealthEntry{at: time.Now(), h: h}
	fpmHealthMu.Unlock()
	return h
}

var (
	fpmLogTime    = regexp.MustCompile(`^\[([0-9]{2}-[A-Za-z]{3}-[0-9]{4} [0-9:]{8})\]`)
	fpmLogSetting = regexp.MustCompile(`max_children setting \(([0-9]+)\)`)
)

// ParseFPMSaturation counts php-fpm "max_children reached" and "seems busy" warnings of one pool.
// A "reached" line logged under a lower limit than currentMax is ignored: the operator already
// raised the limit since then, so it no longer says anything about the current setting.
func ParseFPMSaturation(logs string, uid, currentMax int) (reached, busy int, last string) {
	pool := fmt.Sprintf("[pool site-%d]", uid)
	for _, line := range strings.Split(logs, "\n") {
		if !strings.Contains(line, pool) {
			continue
		}
		switch {
		case strings.Contains(line, "reached pm.max_children"), strings.Contains(line, "reached max_children"):
			if m := fpmLogSetting.FindStringSubmatch(line); m != nil {
				if v, err := strconv.Atoi(m[1]); err == nil && v < currentMax {
					continue
				}
			}
			reached++
			if m := fpmLogTime.FindStringSubmatch(strings.TrimSpace(line)); m != nil {
				last = m[1]
			}
		case strings.Contains(line, "seems busy"):
			busy++
		}
	}
	return reached, busy, last
}

// RecommendMaxChildren suggests a new pm.max_children for a pool that hit its limit: about
// 1.5x the current value (rounded up to 5), but never more than half of the RAM that is free
// right now allows at the measured memory per worker, nor the owner's ceiling.
func RecommendMaxChildren(current, reached, avgWorkerMB, availableMB, ceiling int) (int, bool, string) {
	if current <= 0 {
		current = 20
	}
	if reached == 0 {
		return current, true, ""
	}
	if ceiling <= 0 || ceiling > 100 {
		ceiling = 100
	}
	perWorker := avgWorkerMB
	if perWorker <= 0 {
		perWorker = 64 // typical WordPress worker when it cannot be measured
	}
	want := (current*3 + 1) / 2
	if want%5 != 0 {
		want += 5 - want%5
	}
	if want > ceiling {
		want = ceiling
	}
	if availableMB > 0 {
		// The extra workers must fit in half of the free RAM, leaving the rest for MySQL,
		// other sites and the page cache.
		extraFit := (availableMB / 2) / perWorker
		if current+extraFit < want {
			want = current + extraFit
		}
	}
	if want <= current {
		return current, false, "Not enough free RAM to add PHP workers safely; enable a page cache or add memory."
	}
	return want, true, ""
}
