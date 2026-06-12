package migrate

import (
	"context"
	"os"
	"path/filepath"

	"panel/internal/db"
)

func ResetPanelForImport(ctx context.Context, store *db.Store, workspacesRoot string) error {
	if err := store.ClearPanelForMigrateImport(ctx); err != nil {
		return err
	}
	return wipeWorkspaces(workspacesRoot)
}

func wipeWorkspaces(root string) error {
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return os.MkdirAll(root, 0750)
		}
		return err
	}
	for _, e := range entries {
		if err := os.RemoveAll(filepath.Join(root, e.Name())); err != nil {
			return err
		}
	}
	return nil
}
