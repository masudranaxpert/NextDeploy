// Package quota is the single place where per-user limits are enforced, so every
// entry point (panel UI, MCP, imports, PHP hosting) goes through the same checks.
package quota

import (
	"context"
	"errors"
	"fmt"

	"panel/internal/db"
)

// AppLimit returns the app limit that applies to the user (0 = unlimited).
func AppLimit(u db.User) int {
	if u.Role == db.RoleAdmin || u.MaxApps <= 0 {
		return 0
	}
	return u.MaxApps
}

// LimitError turns the store's limit sentinel into a user-facing error.
func LimitError(u db.User, err error) error {
	if errors.Is(err, db.ErrAppLimitReached) {
		return fmt.Errorf("maximum app limit reached (%d)", u.MaxApps)
	}
	return err
}

// CheckAppSlot is a fast pre-check used before doing expensive work such as
// creating directories. The authoritative check is CreateApp, which is atomic.
func CheckAppSlot(ctx context.Context, store *db.Store, u db.User) error {
	limit := AppLimit(u)
	if limit == 0 {
		return nil
	}
	n, err := store.CountAppsOwnedByUser(ctx, u.ID)
	if err != nil {
		return err
	}
	if n >= limit {
		return LimitError(u, db.ErrAppLimitReached)
	}
	return nil
}

// CreateApp inserts an app for owner, enforcing the app limit atomically.
func CreateApp(ctx context.Context, store *db.Store, owner db.User, id, name string) error {
	return LimitError(owner, store.CreateAppAtomic(ctx, id, name, owner.ID, AppLimit(owner), nil))
}

// CreatePHPSite inserts an app plus its php_sites row, enforcing the app limit atomically.
func CreatePHPSite(ctx context.Context, store *db.Store, owner db.User, id, name string, site *db.PHPSite) error {
	return LimitError(owner, store.CreateAppAtomic(ctx, id, name, owner.ID, AppLimit(owner), site))
}

// CheckDatabaseSlot verifies the owner can create another PHP database.
func CheckDatabaseSlot(ctx context.Context, store *db.Store, owner db.User) error {
	if owner.Role == db.RoleAdmin {
		return nil
	}
	lim := store.GetPHPUserLimits(ctx, owner.ID)
	if lim.MaxDatabases <= 0 {
		return nil
	}
	n, err := store.CountPHPDatabasesForOwner(ctx, owner.ID)
	if err != nil {
		return err
	}
	if n >= lim.MaxDatabases {
		return fmt.Errorf("database limit reached (%d)", lim.MaxDatabases)
	}
	return nil
}

// CheckCronSlot verifies the owner can create another cron job.
func CheckCronSlot(ctx context.Context, store *db.Store, owner db.User) error {
	if owner.Role == db.RoleAdmin {
		return nil
	}
	lim := store.GetPHPUserLimits(ctx, owner.ID)
	if lim.MaxCronJobs <= 0 {
		return nil
	}
	n, err := store.CountPHPCronJobsForOwner(ctx, owner.ID)
	if err != nil {
		return err
	}
	if n >= lim.MaxCronJobs {
		return fmt.Errorf("cron job limit reached (%d)", lim.MaxCronJobs)
	}
	return nil
}
