//go:build e2e

package phphosting

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"panel/internal/db"
)

// Needs Docker. A user whose database name contains "_" must not reach other users' databases.
func TestGrantDoesNotReachOtherUsersDatabases(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	store, err := db.Open(filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	os.Setenv("PHP_DB_NETWORK", "nd-grants-db")
	m := NewManager(store, t.TempDir())
	defer func() {
		bg := context.Background()
		_ = m.StopMySQL(bg)
		removeContainer(bg, MySQLContainer)
		docker(bg, "volume", "rm", "-f", MySQLVolume)
		docker(bg, "network", "rm", "nd-grants-db")
	}()
	if err := m.EnableMySQL(ctx, 512); err != nil {
		t.Fatal(err)
	}
	// user 2 owns "u2__x" (a name with two underscores); user 21 owns "u21_x"
	if err := m.CreateDatabase(ctx, "u2__x", "u2__x", "Passw0rd-attacker-1"); err != nil {
		t.Fatal(err)
	}
	if err := m.CreateDatabaseOnly(ctx, "u21_x"); err != nil {
		t.Fatal(err)
	}
	pw, _ := m.rootPassword(ctx)
	_ = pw
	out, err := m.sql(ctx, "SHOW GRANTS FOR 'u2__x'@'%';")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `u2\\_\\_x`) {
		t.Fatalf("grant is not escaped: %s", out)
	}
	// the attacker's own login must see only its own database
	res := docker(ctx, "exec", "-e", "MYSQL_PWD=Passw0rd-attacker-1", MySQLContainer, "mariadb", "-uu2__x", "-N", "-B", "-e", "SHOW DATABASES")
	if !res.OK {
		t.Fatalf("login failed: %s", res.Output)
	}
	if strings.Contains(res.Output, "u21_x") {
		t.Fatalf("the grant leaks another user's database: %s", res.Output)
	}
	if !strings.Contains(res.Output, "u2__x") {
		t.Fatalf("own database is missing: %s", res.Output)
	}
	// legacy wildcard grants are removed by RepairGrants
	if _, err := m.sql(ctx, "GRANT ALL PRIVILEGES ON `u2__x`.* TO 'u2__x'@'%'; FLUSH PRIVILEGES;"); err != nil {
		t.Fatal(err)
	}
	if err := m.GrantAccess(ctx, "u2__x", "u2__x", "all"); err != nil {
		t.Fatal(err)
	}
	res = docker(ctx, "exec", "-e", "MYSQL_PWD=Passw0rd-attacker-1", MySQLContainer, "mariadb", "-uu2__x", "-N", "-B", "-e", "SHOW DATABASES")
	if strings.Contains(res.Output, "u21_x") {
		t.Fatalf("legacy wildcard grant survived the repair: %s", res.Output)
	}
}
