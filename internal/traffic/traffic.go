// Package traffic keeps a short, in-memory history of requests per site (project, PHP site,
// the panel itself) so the Traffic page can show what each site served in the last hour.
//
// The reverse proxy already counts requests per site (Angie status zones, Caddy per-host
// metrics). The panel reads those running totals once a minute and stores only the
// difference per minute, so nothing is added to the request path and memory stays small:
// one bucket of five numbers per site per minute, for at most Window minutes.
package traffic

import (
	"sort"
	"sync"
	"time"
)

// Window is how many one-minute buckets are kept per site.
const Window = 60

// Counters are running totals (or per-minute differences) of one site.
type Counters struct {
	Requests uint64 `json:"requests"`
	S2xx     uint64 `json:"s2xx"`
	S3xx     uint64 `json:"s3xx"`
	S4xx     uint64 `json:"s4xx"`
	S5xx     uint64 `json:"s5xx"`
}

func (c *Counters) add(o Counters) {
	c.Requests += o.Requests
	c.S2xx += o.S2xx
	c.S3xx += o.S3xx
	c.S4xx += o.S4xx
	c.S5xx += o.S5xx
}

// sub returns cur-prev. A counter that went down means the proxy restarted (or the engine
// changed) and started from zero, so everything it counted since then is new.
func sub(cur, prev Counters) Counters {
	if cur.Requests < prev.Requests {
		return cur
	}
	d := func(a, b uint64) uint64 {
		if a < b {
			return a
		}
		return a - b
	}
	return Counters{
		Requests: cur.Requests - prev.Requests,
		S2xx:     d(cur.S2xx, prev.S2xx),
		S3xx:     d(cur.S3xx, prev.S3xx),
		S4xx:     d(cur.S4xx, prev.S4xx),
		S5xx:     d(cur.S5xx, prev.S5xx),
	}
}

type series struct {
	buckets  [Window]Counters
	minutes  [Window]int64 // unix minute each bucket belongs to
	last     Counters      // last running total seen
	hasLast  bool
	lastSeen int64
}

// slot is the ring position of a unix minute (never negative).
func slot(minute int64) int {
	i := int(minute % Window)
	if i < 0 {
		i += Window
	}
	return i
}

// Store holds the per-site history. It is safe for concurrent use.
type Store struct {
	mu      sync.Mutex
	sites   map[string]*series
	source  string // engine the running totals came from
	since   time.Time
	lastAt  time.Time
	perSite bool
	note    string
}

// NewStore returns an empty store.
func NewStore() *Store { return &Store{sites: map[string]*series{}} }

// Observe records the running totals read at now. source names where they came from
// (the proxy engine); when it changes, old totals are not comparable, so only a new
// baseline is taken. perSite is false when the engine cannot split requests by site;
// note then says why.
func (s *Store) Observe(now time.Time, source string, totals map[string]Counters, perSite bool, note string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if source != s.source {
		s.sites = map[string]*series{}
		s.source = source
		s.since = now
	}
	if s.since.IsZero() {
		s.since = now
	}
	s.lastAt, s.perSite, s.note = now, perSite, note
	minute := now.Unix() / 60
	for key, cur := range totals {
		se := s.sites[key]
		if se == nil {
			se = &series{}
			s.sites[key] = se
		}
		if se.hasLast {
			delta := sub(cur, se.last)
			i := slot(minute)
			if se.minutes[i] != minute {
				se.buckets[i], se.minutes[i] = Counters{}, minute
			}
			se.buckets[i].add(delta)
		}
		se.last, se.hasLast, se.lastSeen = cur, true, minute
	}
	// Forget sites that disappeared (deleted, renamed) once their history is out of range.
	for key, se := range s.sites {
		if minute-se.lastSeen > Window {
			delete(s.sites, key)
		}
	}
}

// SiteSummary is one site's totals over the asked window.
type SiteSummary struct {
	Key     string   `json:"key"`
	Total   Counters `json:"total"`
	LastMin uint64   `json:"last_minute"` // requests in the last complete minute
	PerMin  []uint64 `json:"per_minute"`  // oldest first, one value per minute of the window
	Active  bool     `json:"active"`      // seen in the latest reading
}

// Snapshot is the view the Traffic page needs.
type Snapshot struct {
	Source   string        `json:"source"`
	PerSite  bool          `json:"per_site"`
	Note     string        `json:"note,omitempty"`
	Since    time.Time     `json:"since"`
	Updated  time.Time     `json:"updated"`
	Minutes  []int64       `json:"minutes"` // unix seconds of each minute in the window, oldest first
	Sites    []SiteSummary `json:"sites"`
	Total    Counters      `json:"total"`
	PerMin   []uint64      `json:"per_minute"`
	Coverage int           `json:"coverage_minutes"` // minutes of the window that have data
}

// Summary returns the last window minutes (1..Window) ending at now, for the keys allowed
// by include (nil means all). Sites are sorted by requests, busiest first.
func (s *Store) Summary(now time.Time, window int, include func(key string) bool) Snapshot {
	if window <= 0 || window > Window {
		window = 30
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cur := now.Unix() / 60
	// The current minute is still filling up; the window ends at the last complete one.
	end := cur - 1
	start := end - int64(window) + 1
	snap := Snapshot{
		Source: s.source, PerSite: s.perSite, Note: s.note, Since: s.since, Updated: s.lastAt,
		Minutes: make([]int64, window), PerMin: make([]uint64, window), Sites: []SiteSummary{},
	}
	for i := 0; i < window; i++ {
		snap.Minutes[i] = (start + int64(i)) * 60
	}
	if !s.since.IsZero() {
		first := s.since.Unix()/60 + 1 // the first minute with a full difference
		from := start
		if first > from {
			from = first
		}
		if end >= from {
			snap.Coverage = int(end - from + 1)
		}
	}
	for key, se := range s.sites {
		if include != nil && !include(key) {
			continue
		}
		sum := SiteSummary{Key: key, PerMin: make([]uint64, window), Active: se.lastSeen >= cur-1}
		for i := 0; i < window; i++ {
			m := start + int64(i)
			b := slot(m)
			if se.minutes[b] != m {
				continue
			}
			c := se.buckets[b]
			sum.PerMin[i] = c.Requests
			sum.Total.add(c)
			snap.PerMin[i] += c.Requests
		}
		sum.LastMin = sum.PerMin[window-1]
		snap.Total.add(sum.Total)
		snap.Sites = append(snap.Sites, sum)
	}
	sort.Slice(snap.Sites, func(i, j int) bool {
		if snap.Sites[i].Total.Requests != snap.Sites[j].Total.Requests {
			return snap.Sites[i].Total.Requests > snap.Sites[j].Total.Requests
		}
		return snap.Sites[i].Key < snap.Sites[j].Key
	})
	return snap
}
