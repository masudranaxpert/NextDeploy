package handlers

import (
	"context"
	"strconv"
	"sync"

	"panel/internal/db"
	"panel/internal/dockerapi"
	"panel/internal/volumex"
)

// StorageUsage is what one user's projects take on the server, split by where it lives.
type StorageUsage struct {
	Files     int64 `json:"files"`     // project workspaces: code, uploads, PHP site files
	Databases int64 `json:"databases"` // PHP-site MySQL/MariaDB databases
	Volumes   int64 `json:"volumes"`   // Docker volumes of compose projects
}

func (s StorageUsage) Total() int64 { return s.Files + s.Databases + s.Volumes }

// ownedApps are the projects a user created. Projects shared with them belong to someone else's
// quota, so they are not counted here.
func (p *Panel) ownedApps(ctx context.Context, userID int64) []db.App {
	apps, err := p.DB.ListAppsForUser(ctx, userID)
	if err != nil {
		return nil
	}
	out := apps[:0:0]
	for _, a := range apps {
		if a.OwnerID == userID {
			out = append(out, a)
		}
	}
	return out
}

// UserStorage adds up the files, databases and Docker volumes of the projects a user owns.
// fresh re-reads the workspaces from disk (used before accepting a write); otherwise recent
// results are reused. Databases and volumes are always cached for a short time.
func (p *Panel) UserStorage(ctx context.Context, userID int64, fresh bool) StorageUsage {
	var out StorageUsage
	apps := p.ownedApps(ctx, userID)
	var dbNames []string
	var volumeNames []string
	var all, allProjects []string
	var matcher *volumex.Matcher
	for _, app := range apps {
		if fresh {
			out.Files += dirSizeBytes(p.Store.Path(app.ID))
		} else {
			out.Files += p.AppStorageBytes(app.ID)
		}
		if p.PHP != nil && p.DB.IsPHPSite(ctx, app.ID) {
			if dbs, err := p.DB.ListPHPDatabases(ctx, app.ID); err == nil {
				for _, d := range dbs {
					dbNames = append(dbNames, d.DBName)
				}
			}
			continue
		}
		if matcher == nil {
			all = bootVolumes.get(bootTTL, func() []string { v, _ := volumex.List(ctx); return v })
			allProjects = p.AllPanelComposeProjects(ctx)
			matcher = volumex.SharedMatcher(ctx)
		}
		volumeNames = append(volumeNames, p.appVolumeNames(ctx, app, all, allProjects, matcher)...)
	}
	if p.PHP != nil && len(dbNames) > 0 {
		out.Databases = p.PHP.DatabaseBytes(ctx, dbNames)
	}
	if len(volumeNames) > 0 {
		sizes := dockerapi.VolumeSizes(ctx)
		seen := map[string]bool{}
		for _, v := range volumeNames {
			if !seen[v] {
				seen[v] = true
				out.Volumes += sizes[v]
			}
		}
	}
	return out
}

// UserStorageBytes is the total of UserStorage, from cached numbers.
func (p *Panel) UserStorageBytes(ctx context.Context, userID int64) int64 {
	return p.UserStorage(ctx, userID, false).Total()
}

// userUsageRow is one user's live use of the server, for the admin's Team & Quotas page.
type userUsageRow struct {
	Storage  StorageUsage `json:"storage"`
	Total    int64        `json:"storage_bytes"`
	MemoryMB int64        `json:"memory_mb"`
	CPUCores float64      `json:"cpu_cores"`
}

// allUserUsage measures every user's storage, memory and CPU at the same time (each number is
// cached for a short while, so reloading the page is cheap).
func (p *Panel) allUserUsage(ctx context.Context, users []db.User) map[string]userUsageRow {
	out := make(map[string]userUsageRow, len(users))
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, u := range users {
		wg.Add(1)
		go func(u db.User) {
			defer wg.Done()
			st := p.UserStorage(ctx, u.ID, false)
			mem, cpu := p.userLiveUsage(ctx, u)
			mu.Lock()
			out[strconv.FormatInt(u.ID, 10)] = userUsageRow{Storage: st, Total: st.Total(), MemoryMB: mem, CPUCores: cpu}
			mu.Unlock()
		}(u)
	}
	wg.Wait()
	return out
}
