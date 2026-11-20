package dockerapi

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
)

// Docker sends an "oom" event when the kernel kills a process of a container for exceeding the
// container's memory limit. Often only a child process dies (a headless browser, a worker) and
// the container keeps running, so its status alone never shows the problem. The panel listens
// for these events once and keeps a small per-container tally in memory.

// OOMInfo is how often a container ran out of memory since the panel started.
type OOMInfo struct {
	Count int       `json:"count"`
	Last  time.Time `json:"last"`
}

var (
	oomMu    sync.Mutex
	oomByID  = map[string]OOMInfo{}
	oomStart sync.Once
)

// StartOOMWatcher subscribes to Docker's container "oom" events. Safe to call more than once;
// it reconnects after Docker restarts.
func StartOOMWatcher() {
	oomStart.Do(func() {
		go func() {
			for {
				watchOOMOnce()
				time.Sleep(10 * time.Second)
			}
		}()
	})
}

func watchOOMOnce() {
	cli, err := apiClient()
	if err != nil {
		return
	}
	f := filters.NewArgs()
	f.Add("type", "container")
	f.Add("event", "oom")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	msgs, errs := cli.Events(ctx, types.EventsOptions{Filters: f})
	for {
		select {
		case m, ok := <-msgs:
			if !ok {
				return
			}
			recordOOM(m.Actor.ID, time.Unix(0, m.TimeNano))
		case err, ok := <-errs:
			if ok && err != nil {
				log.Printf("docker oom watch: %v", err)
			}
			return
		}
	}
}

func recordOOM(id string, at time.Time) {
	if id == "" {
		return
	}
	if at.IsZero() || at.Unix() <= 0 {
		at = time.Now()
	}
	oomMu.Lock()
	defer oomMu.Unlock()
	info := oomByID[id]
	info.Count++
	if at.After(info.Last) {
		info.Last = at
	}
	oomByID[id] = info
	// Containers come and go with every deploy; keep the map from growing without bound.
	if len(oomByID) > 2000 {
		cutoff := time.Now().Add(-7 * 24 * time.Hour)
		for k, v := range oomByID {
			if v.Last.Before(cutoff) {
				delete(oomByID, k)
			}
		}
	}
}

// ContainerOOM returns the out-of-memory tally of a container (zero when none).
func ContainerOOM(id string) OOMInfo {
	oomMu.Lock()
	defer oomMu.Unlock()
	return oomByID[id]
}

// ContainerExists reports whether a container with this name exists. err is set only when the
// Docker API itself could not be asked (so "missing" is never confused with "Docker is down").
func ContainerExists(ctx context.Context, name string) (bool, error) {
	cli, err := apiClient()
	if err != nil {
		return false, err
	}
	if _, err := cli.ContainerInspect(ctx, name); err != nil {
		if client.IsErrNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}
