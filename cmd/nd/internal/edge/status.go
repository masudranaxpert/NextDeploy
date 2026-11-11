package edge

import (
	"path/filepath"
	"sort"
	"time"
)

// BuildStatusForPanel constructs a StatusRequest strictly scoped to panelID (§3.6).
// It never includes hosts from other panels; conflicts report only OwnerPanel display names.
func BuildStatusForPanel(
	cfg *Config,
	st *PanelState,
	meta *ApplyMeta,
	edgeVersion string,
	caddyVersion string,
	caddyRunning bool,
	caddyErr string,
	uptimeSec int64,
	clockSkewSec int64,
	paths Paths,
) StatusRequest {
	var hosts []HostStatus
	if st != nil {
		hosts = make([]HostStatus, 0, len(st.HostStates))
		for _, hs := range st.HostStates {
			hosts = append(hosts, hs)
		}
		sort.Slice(hosts, func(i, j int) bool { return hosts[i].Host < hosts[j].Host })
	} else {
		hosts = []HostStatus{}
	}

	var conflicts []ConflictStatus
	if st != nil && len(st.Conflicts) > 0 {
		conflicts = make([]ConflictStatus, len(st.Conflicts))
		copy(conflicts, st.Conflicts)
		sort.Slice(conflicts, func(i, j int) bool { return conflicts[i].Host < conflicts[j].Host })
	} else {
		conflicts = []ConflictStatus{}
	}

	var appliedETag, lastApplyErr string
	var lastApplyAt *time.Time
	if st != nil {
		appliedETag = st.LastAppliedETag
	}
	if meta != nil {
		lastApplyAt = meta.LastApplyAt
		lastApplyErr = meta.LastApplyError
	}
	if !caddyRunning && caddyErr != "" {
		if lastApplyErr == "" {
			lastApplyErr = "caddy not running: " + caddyErr
		}
	}

	diskPath := paths.CaddyNDDir
	if diskPath == "" {
		diskPath = string(filepath.Separator)
	}

	return StatusRequest{
		EdgeID:         cfg.EdgeID,
		EdgeVersion:    edgeVersion,
		CaddyVersion:   caddyVersion,
		CaddyRunning:   caddyRunning,
		AppliedETag:    appliedETag,
		LastApplyAt:    lastApplyAt,
		LastApplyError: lastApplyErr,
		UptimeSec:      uptimeSec,
		DiskFreePct:    getDiskFreePct(diskPath),
		MemFreeMB:      getMemFreeMB(),
		ClockSkewSec:   clockSkewSec,
		Hosts:          hosts,
		Conflicts:      conflicts,
	}
}
