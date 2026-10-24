//go:build e2e

package handlers

// Backup/restore round trip against a live PHP hosting environment (running MariaDB and a
// PHP runtime). It is excluded from normal builds; run it inside a container that mounts the
// panel's data dir and the Docker socket:
//
//	go test -c -tags e2e -o php-e2e.test ./internal/handlers
//	ND_E2E_APP=<php app id> ND_E2E_DATA=/data ./php-e2e.test -test.run PHPBackupRestore -test.v

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"panel/internal/backup"
	"panel/internal/db"
	"panel/internal/phphosting"
	"panel/internal/workspace"
)

func TestPHPBackupRestoreRoundTrip(t *testing.T) {
	data := os.Getenv("ND_E2E_DATA")
	appID := os.Getenv("ND_E2E_APP")
	if data == "" || appID == "" {
		t.Skip("set ND_E2E_DATA and ND_E2E_APP")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	store, err := db.Open(filepath.Join(data, "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	root := filepath.Join(data, "workspaces")
	p := &Panel{DB: store, Store: workspace.NewStore(root), WorkspacesRoot: root}
	p.PHP = phphosting.NewManager(store, root)

	site, err := store.GetPHPSite(ctx, appID)
	if err != nil {
		t.Fatal(err)
	}
	if st := p.PHP.MySQLStatus(ctx); !st.Ready {
		t.Fatalf("database server not ready: %+v", st)
	}

	// 1. a database with data, plus files including a Composer vendor dir
	dbName := "u1_bkp" + strings.ToLower(phphosting.RandomPassword(4))
	dbName = strings.NewReplacer("2", "a", "3", "b", "4", "c", "5", "d", "6", "e", "7", "f", "8", "g", "9", "h").Replace(dbName)
	if err := p.PHP.CreateDatabase(ctx, dbName, dbName, phphosting.RandomPassword(20)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreatePHPDatabase(ctx, db.PHPDatabase{AppID: appID, DBName: dbName, DBUser: dbName}); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = p.PHP.DropDatabase(context.Background(), dbName, dbName)
		dbs, _ := store.ListPHPDatabases(context.Background(), appID)
		for _, d := range dbs {
			if d.DBName == dbName {
				_ = store.DeletePHPDatabase(context.Background(), d.ID)
			}
		}
	}()
	if err := p.PHP.ImportDatabase(ctx, dbName, strings.NewReader("CREATE TABLE t (n INT); INSERT INTO t VALUES (1),(2),(3);"), false); err != nil {
		t.Fatal(err)
	}
	ws := p.PHP.SitePath(appID)
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.MkdirAll(filepath.Join(ws, "htdocs/vendor/lib"), 0o750))
	must(os.WriteFile(filepath.Join(ws, "htdocs/vendor/lib/a.php"), []byte("<?php // vendor"), 0o640))
	must(os.WriteFile(filepath.Join(ws, "htdocs/keep.txt"), []byte("precious"), 0o640))
	defer os.RemoveAll(filepath.Join(ws, "htdocs/vendor"))
	defer os.Remove(filepath.Join(ws, "htdocs/keep.txt"))

	// 2. backup (dump into the workspace, archive, dump removed again)
	bctx, cleanup, err := p.phpBackupPrepare(ctx, appID, func(s string) { t.Log(s) })
	must(err)
	dumpPath := filepath.Join(ws, phpDumpDir, dbName+".sql.gz")
	if _, err := os.Stat(dumpPath); err != nil {
		t.Fatalf("dump missing before archiving: %v", err)
	}
	tarPath, err := backup.BackupFullApp(bctx, "e2e", ws)
	cleanup()
	must(err)
	defer os.Remove(tarPath)
	if _, err := os.Stat(filepath.Join(ws, phpDumpDir)); !os.IsNotExist(err) {
		t.Fatal("dump directory must be removed after the backup")
	}
	listing, err := exec.Command("tar", "tzf", tarPath).CombinedOutput()
	must(err)
	for _, want := range []string{"htdocs/vendor/lib/a.php", "htdocs/keep.txt", phpDumpDir + "/" + dbName + ".sql.gz"} {
		if !strings.Contains(string(listing), want) {
			t.Fatalf("archive misses %s:\n%s", want, listing)
		}
	}

	// 3. destroy data and files
	must(p.PHP.ImportDatabase(ctx, dbName, strings.NewReader("DROP TABLE t;"), false))
	must(os.RemoveAll(filepath.Join(ws, "htdocs/vendor")))
	must(os.Remove(filepath.Join(ws, "htdocs/keep.txt")))

	// 4. restore
	must(backup.RestoreWorkspaceOnly(ctx, ws, tarPath))
	if msg := p.phpAfterRestore(ctx, appID); msg != "" {
		t.Fatalf("restore: %s", msg)
	}
	if b, err := os.ReadFile(filepath.Join(ws, "htdocs/keep.txt")); err != nil || string(b) != "precious" {
		t.Fatalf("file not restored: %v %q", err, b)
	}
	if _, err := os.Stat(filepath.Join(ws, "htdocs/vendor/lib/a.php")); err != nil {
		t.Fatalf("vendor not restored: %v", err)
	}
	if _, err := os.Stat(filepath.Join(ws, phpDumpDir)); !os.IsNotExist(err) {
		t.Fatal("dump directory must be removed after the restore")
	}
	out, err := exec.Command("docker", "exec", "-e", "MYSQL_PWD="+store.GetSetting(ctx, "php_mysql_root_password"),
		phphosting.MySQLContainer, "mariadb", "-uroot", "-N", "-e", "SELECT COUNT(*) FROM "+dbName+".t").CombinedOutput()
	must(err)
	if strings.TrimSpace(string(out)) != "3" {
		t.Fatalf("database rows not restored: %q", out)
	}
	// ownership handed back to the site user
	st, err := os.Stat(filepath.Join(ws, "htdocs/keep.txt"))
	must(err)
	if !phpOwnedBy(st, site.UID) {
		t.Fatalf("restored file is not owned by the site uid %d", site.UID)
	}
}
