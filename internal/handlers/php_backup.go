package handlers

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"panel/internal/backup"
	"panel/internal/phphosting"
)

// phpDumpDir holds database dumps inside a site workspace so the regular app backup
// (a tarball of the workspace) carries them. Visible "database" directory for users.
const phpDumpDir = "database"
const legacyPhpDumpDir = ".nextdeploy-db"

// phpBackupPrepare dumps the site's databases into the workspace and returns a context that
// keeps vendor/ directories in the archive. For non-PHP apps it is a no-op.
func (p *Panel) phpBackupPrepare(ctx context.Context, appID string, logf func(string)) (context.Context, func(), error) {
	noop := func() {}
	if p.PHP == nil || !p.DB.IsPHPSite(ctx, appID) {
		return ctx, noop, nil
	}
	ctx = backup.WithKeepVendor(ctx)
	dbs, err := p.DB.ListPHPDatabases(ctx, appID)
	if err != nil {
		return ctx, noop, err
	}
	if len(dbs) == 0 {
		return ctx, noop, nil
	}
	if !p.PHP.MySQLStatus(ctx).Ready {
		return ctx, noop, fmt.Errorf("the database server is not running, so the site's %d database(s) cannot be included. Start it in PHP Hosting → Instance and retry", len(dbs))
	}
	dir := filepath.Join(p.PHP.SitePath(appID), phpDumpDir)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return ctx, noop, err
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	for _, d := range dbs {
		logf("dump database " + d.DBName)
		if err := dumpDatabaseToFile(ctx, p.PHP, d.DBName, filepath.Join(dir, d.DBName+".sql.gz")); err != nil {
			cleanup()
			return ctx, noop, fmt.Errorf("dump %s: %w", d.DBName, err)
		}
	}
	return ctx, cleanup, nil
}

func dumpDatabaseToFile(ctx context.Context, m *phphosting.Manager, dbName, dest string) error {
	f, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	// ExportDatabase already gzips its output
	return m.ExportDatabase(ctx, dbName, f)
}

// phpAfterRestore runs after a workspace archive was extracted: it hands the files back to
// the site user, loads database dumps that came with the archive, and re-applies the runtime.
// It returns an error message ("" on success), matching runRestoreJob's convention.
func (p *Panel) phpAfterRestore(ctx context.Context, appID string) string {
	if p.PHP == nil {
		return ""
	}
	site, err := p.DB.GetPHPSite(ctx, appID)
	if err != nil {
		return ""
	}
	dumpDir := filepath.Join(p.PHP.SitePath(appID), phpDumpDir)
	legacyDir := filepath.Join(p.PHP.SitePath(appID), legacyPhpDumpDir)
	defer os.RemoveAll(dumpDir)
	defer os.RemoveAll(legacyDir)

	if err := p.PHP.PrepareSiteDirs(site); err != nil {
		return err.Error()
	}
	p.PHP.FixOwnership(site)

	dbs, _ := p.DB.ListPHPDatabases(ctx, appID)
	var skipped []string
	for _, d := range dbs {
		dump := filepath.Join(dumpDir, d.DBName+".sql.gz")
		if _, statErr := os.Stat(dump); statErr != nil {
			dump = filepath.Join(legacyDir, d.DBName+".sql.gz")
		}
		f, err := os.Open(dump)
		if err != nil {
			continue
		}
		if !p.PHP.MySQLStatus(ctx).Ready {
			f.Close()
			skipped = append(skipped, d.DBName)
			continue
		}
		// the dump is already gzip data, ImportDatabase unpacks it
		err = p.PHP.ImportDatabase(ctx, d.DBName, f, true)
		f.Close()
		if err != nil {
			log.Printf("restore: import %s: %v", d.DBName, err)
			return fmt.Sprintf("files restored, but importing database %s failed: %v", d.DBName, err)
		}
	}
	if len(skipped) > 0 {
		return "files restored, but the database server is not running so these databases were not restored: " + strings.Join(skipped, ", ")
	}
	if err := p.PHP.SyncSite(ctx, appID); err != nil {
		return "files restored, but the site could not be restarted: " + err.Error()
	}
	return ""
}
