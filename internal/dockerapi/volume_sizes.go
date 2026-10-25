package dockerapi

import (
	"context"
	"sync"
	"time"

	"github.com/docker/docker/api/types"
)

const volumeSizesTTL = 5 * time.Minute

var (
	volSizesMu         sync.Mutex
	volSizesData       map[string]int64
	volSizesAt         time.Time
	volSizesRefreshing bool
)

// VolumeSizes returns the bytes used by each Docker volume. Docker computes this by walking
// every volume, which can take seconds, so the answer is cached; a stale answer is returned
// at once while a fresh one is gathered in the background.
func VolumeSizes(ctx context.Context) map[string]int64 {
	volSizesMu.Lock()
	data, at, busy := volSizesData, volSizesAt, volSizesRefreshing
	if data != nil && time.Since(at) < volumeSizesTTL {
		volSizesMu.Unlock()
		return data
	}
	if data != nil && !busy {
		volSizesRefreshing = true
		volSizesMu.Unlock()
		go func() {
			bg, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			fresh := fetchVolumeSizes(bg)
			volSizesMu.Lock()
			if fresh != nil {
				volSizesData, volSizesAt = fresh, time.Now()
			}
			volSizesRefreshing = false
			volSizesMu.Unlock()
		}()
		return data
	}
	volSizesMu.Unlock()
	if data != nil { // a refresh is already running
		return data
	}
	fresh := fetchVolumeSizes(ctx)
	volSizesMu.Lock()
	if fresh != nil {
		volSizesData, volSizesAt = fresh, time.Now()
	}
	volSizesMu.Unlock()
	return fresh
}

func fetchVolumeSizes(ctx context.Context) map[string]int64 {
	cli, err := apiClient()
	if err != nil {
		return nil
	}
	du, err := cli.DiskUsage(ctx, types.DiskUsageOptions{})
	if err != nil {
		return nil
	}
	out := make(map[string]int64, len(du.Volumes))
	for _, v := range du.Volumes {
		if v != nil && v.UsageData != nil && v.UsageData.Size > 0 {
			out[v.Name] = v.UsageData.Size
		}
	}
	return out
}
