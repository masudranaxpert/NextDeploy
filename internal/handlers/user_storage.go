package handlers

// Per-user workspace storage accounting and quota enforcement.
// Usage = total on-disk size of every app workspace the user owns
// (uploaded files, git checkouts, generated configs).

import (
	"context"
	"fmt"
	"io/fs"
	"path/filepath"
	"time"

	"panel/internal/db"
)

const appStorageCacheTTL = 5 * time.Minute

type appStorageEntry struct {
	size int64
	at   time.Time
}

func dirSizeBytes(root string) int64 {
	var total int64
	_ = filepath.WalkDir(root, func(_ string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if info, ierr := d.Info(); ierr == nil {
			total += info.Size()
		}
		return nil
	})
	return total
}

// AppStorageBytes returns the on-disk size of an app workspace (cached with a short TTL).
func (p *Panel) AppStorageBytes(appID string) int64 {
	if v, ok := p.appStorage.Load(appID); ok {
		if e, ok2 := v.(appStorageEntry); ok2 && time.Since(e.at) < appStorageCacheTTL {
			return e.size
		}
	}
	size := dirSizeBytes(p.Store.Path(appID))
	p.appStorage.Store(appID, appStorageEntry{size: size, at: time.Now()})
	return size
}

// InvalidateAppStorageCache drops the cached size after writes (upload, extract, delete).
func (p *Panel) InvalidateAppStorageCache(appID string) {
	p.appStorage.Delete(appID)
}

// UserStorageBytes returns the total workspace bytes used by all apps owned by the user.
func (p *Panel) UserStorageBytes(ctx context.Context, userID int64) int64 {
	apps, err := p.DB.ListAppsForUser(ctx, userID)
	if err != nil {
		return 0
	}
	var total int64
	for _, app := range apps {
		total += p.AppStorageBytes(app.ID)
	}
	return total
}

func (p *Panel) userStorageBytesUncached(ctx context.Context, userID int64) int64 {
	apps, err := p.DB.ListAppsForUser(ctx, userID)
	if err != nil {
		return 0
	}
	var total int64
	for _, app := range apps {
		total += dirSizeBytes(p.Store.Path(app.ID))
	}
	return total
}

// CheckStorageQuota returns an error when writing incomingBytes more into the
// app would push its owner past their storage limit. Admins are unlimited.
func (p *Panel) CheckStorageQuota(ctx context.Context, appID string, incomingBytes int64) error {
	app, err := p.DB.GetApp(ctx, appID)
	if err != nil {
		return nil
	}
	owner, err := p.DB.GetUserByID(ctx, app.OwnerID)
	if err != nil || owner.Role == db.RoleAdmin || owner.MaxStorageMB <= 0 {
		return nil
	}
	maxBytes := int64(owner.MaxStorageMB) * 1024 * 1024
	used := p.userStorageBytesUncached(ctx, owner.ID)
	if used+incomingBytes > maxBytes {
		return fmt.Errorf("storage limit exceeded: using %s of %s (upload needs %s more)",
			humanStorage(used), humanStorage(maxBytes), humanStorage(incomingBytes))
	}
	return nil
}

func humanStorage(b int64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.1fGB", float64(b)/float64(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.1fMB", float64(b)/float64(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.1fKB", float64(b)/float64(1<<10))
	default:
		return fmt.Sprintf("%dB", b)
	}
}

// HumanStorage is the exported variant used by handlers/templates.
func HumanStorage(b int64) string { return humanStorage(b) }
