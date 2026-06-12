package migrate

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"panel/internal/db"
)

var stagingDirPatterns = []string{"export-*", "import-*"}
var stagingFilePatterns = []string{"panel-migrate-*" + BundleExtension}

func ProtectedStagingPaths(database *db.Store) map[string]struct{} {
	protected := map[string]struct{}{}
	if database == nil {
		return protected
	}
	running, _ := database.ListRunningMigrateExports(context.Background())
	for _, row := range running {
		protected[ExportWorkDir(row.ID)] = struct{}{}
		if p := strings.TrimSpace(row.BundlePath); p != "" {
			protected[p] = struct{}{}
		}
	}
	active, _ := database.ListActiveMigrateBundleExports()
	for _, row := range active {
		if p := strings.TrimSpace(row.BundlePath); p != "" {
			protected[p] = struct{}{}
		}
	}
	return protected
}

func ScanOrphans(database *db.Store) (paths []string, totalBytes int64) {
	root := StagingRoot()
	if root == "" {
		return
	}
	protected := ProtectedStagingPaths(database)
	cutoff := time.Now().Add(-OrphanAge)
	seen := map[string]struct{}{}
	add := func(p string) {
		if _, ok := seen[p]; ok {
			return
		}
		if _, ok := protected[p]; ok {
			return
		}
		seen[p] = struct{}{}
		st, err := os.Stat(p)
		if err != nil {
			return
		}
		paths = append(paths, p)
		if st.IsDir() {
			totalBytes += dirSizeBytes(p)
		} else {
			totalBytes += st.Size()
		}
	}
	for _, pat := range stagingDirPatterns {
		matches, _ := filepath.Glob(filepath.Join(root, pat))
		for _, m := range matches {
			st, err := os.Stat(m)
			if err != nil || !st.IsDir() || st.ModTime().After(cutoff) {
				continue
			}
			add(m)
		}
	}
	for _, pat := range stagingFilePatterns {
		matches, _ := filepath.Glob(filepath.Join(root, pat))
		for _, m := range matches {
			st, err := os.Stat(m)
			if err != nil || st.IsDir() || st.ModTime().After(cutoff) {
				continue
			}
			add(m)
		}
	}
	return
}

func CleanOrphans(database *db.Store) (removed int, freed int64) {
	paths, _ := ScanOrphans(database)
	for _, p := range paths {
		st, err := os.Stat(p)
		if err != nil {
			continue
		}
		var size int64
		if st.IsDir() {
			size = dirSizeBytes(p)
			if err := os.RemoveAll(p); err == nil {
				removed++
				freed += size
			}
		} else {
			size = st.Size()
			if err := os.Remove(p); err == nil {
				removed++
				freed += size
			}
		}
	}
	return
}

func RemoveExportArtifacts(row db.MigrateExport) {
	if p := strings.TrimSpace(row.BundlePath); p != "" {
		_ = os.Remove(p)
	}
	if p := strings.TrimSpace(row.WorkDir); p != "" {
		_ = os.RemoveAll(p)
	}
	_ = os.RemoveAll(ExportWorkDir(row.ID))
}

func StartupSweep(database *db.Store) {
	if database != nil {
		_ = database.FailStaleMigrateExports()
	}
	CleanOrphans(database)
}

func SweepExpiredExports(database *db.Store) {
	if database == nil {
		return
	}
	rows, _ := database.ListExpiredMigrateExports()
	for _, row := range rows {
		RemoveExportArtifacts(row)
		_ = database.MarkMigrateExportExpired(row.ID)
	}
}
