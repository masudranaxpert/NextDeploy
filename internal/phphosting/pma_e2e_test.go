//go:build e2e

package phphosting

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"panel/internal/db"
)

// Needs Docker. Creates nd-mysql / nd-phpmyadmin, so run it only where those do not exist.
func TestPMASignOnScoping(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()
	dir := t.TempDir()
	store, err := db.Open(filepath.Join(dir, "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	os.Setenv("PHP_DB_NETWORK", "nd-pmatest-db")
	m := NewManager(store, dir)
	defer func() {
		bg := context.Background()
		_ = m.StopPMA(bg)
		_ = m.StopMySQL(bg)
		removeContainer(bg, MySQLContainer)
		docker(bg, "volume", "rm", "-f", MySQLVolume)
		docker(bg, "network", "rm", PMANetwork)
		docker(bg, "network", "rm", "nd-pmatest-db")
	}()
	if err := m.EnableMySQL(ctx, 512); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"u9_alpha", "u9_beta", "u7_other"} {
		if err := m.CreateDatabaseOnly(ctx, n); err != nil {
			t.Fatal(err)
		}
	}
	if err := m.EnablePMA(ctx); err != nil {
		t.Fatal(err)
	}
	var addr string
	for i := 0; i < 60; i++ {
		if a, err := m.PMAAddr(ctx); err == nil {
			addr = a
			resp, err := http.Get("http://" + addr + "/phpmyadmin/")
			if err == nil {
				resp.Body.Close()
				break
			}
		}
		time.Sleep(2 * time.Second)
	}
	fetch := func(s *PMASession, path string) string {
		req, _ := http.NewRequest("GET", "http://"+addr+path, nil)
		req.SetBasicAuth(s.DBUser, s.Password)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return string(b)
	}
	site, err := m.PMAOpen(ctx, 9, "app1", []string{"u9_alpha", "u9_beta"}, false)
	if err != nil {
		t.Fatal(err)
	}
	body := fetch(site, "/phpmyadmin/index.php?route=/server/databases")
	t.Logf("site page length %d", len(body))
	for _, want := range []string{"u9_alpha", "u9_beta"} {
		if !strings.Contains(body, want) {
			t.Errorf("site view is missing %s", want)
		}
	}
	for _, bad := range []string{"u7_other", "mysql"} {
		if strings.Contains(body, ">"+bad+"<") || strings.Contains(body, "db="+bad) {
			t.Errorf("site view leaks %s", bad)
		}
	}
	adm, err := m.PMAOpen(ctx, 1, "", nil, true)
	if err != nil {
		t.Fatal(err)
	}
	abody := fetch(adm, "/phpmyadmin/index.php?route=/server/databases")
	for _, want := range []string{"u9_alpha", "u7_other"} {
		if !strings.Contains(abody, want) {
			t.Errorf("admin view is missing %s", want)
		}
	}
	m.PMAClose(ctx, site.Token)
	if out, _ := m.sql(ctx, "SELECT COUNT(*) FROM mysql.user WHERE user='"+site.DBUser+"'"); strings.TrimSpace(out) != "0" {
		t.Errorf("temporary user was not dropped: %q", out)
	}
}
