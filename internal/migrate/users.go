package migrate

import (
	"time"

	"panel/internal/db"
)

func userToSnapshot(u db.User) UserSnapshot {
	return UserSnapshot{
		ID:                    u.ID,
		Username:              u.Username,
		PasswordHash:          u.PasswordHash,
		Role:                  u.Role,
		CreatedAt:             u.CreatedAt.UTC().Format(time.RFC3339),
		MaxApps:               u.MaxApps,
		MaxMemoryMB:           u.MaxMemoryMB,
		MaxCPUs:               u.MaxCPUs,
		MaxStorageMB:          u.MaxStorageMB,
		Status:                u.Status,
		AllowDomainFileServer: u.AllowDomainFileServer,
	}
}

func snapshotToUser(s UserSnapshot) db.User {
	created, _ := time.Parse(time.RFC3339, s.CreatedAt)
	return db.User{
		ID:                    s.ID,
		Username:              s.Username,
		PasswordHash:          s.PasswordHash,
		Role:                  s.Role,
		CreatedAt:             created,
		MaxApps:               s.MaxApps,
		MaxMemoryMB:           s.MaxMemoryMB,
		MaxCPUs:               s.MaxCPUs,
		MaxStorageMB:          s.MaxStorageMB,
		Status:                s.Status,
		AllowDomainFileServer: s.AllowDomainFileServer,
	}
}

func snapshotHasUsers(snap PanelSnapshot) bool {
	return len(snap.Users) > 0
}
