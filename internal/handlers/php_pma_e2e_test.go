//go:build e2e

package handlers

import (
	"context"
	"io"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"panel/internal/db"
	"panel/internal/phphosting"

	"github.com/gofiber/fiber/v2"
)

// Needs Docker and no existing nd-mysql / nd-phpmyadmin.
func TestPMAProxySingleSignOn(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()
	dir := t.TempDir()
	store, err := db.Open(filepath.Join(dir, "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	os.Setenv("PHP_DB_NETWORK", "nd-pmatest-db")
	m := phphosting.NewManager(store, dir)
	defer func() {
		bg := context.Background()
		_ = m.StopPMA(bg)
		_ = m.StopMySQL(bg)
		_ = dockerRm("docker", "rm", "-f", phphosting.MySQLContainer)
		_ = dockerRm("docker", "volume", "rm", "-f", phphosting.MySQLVolume)
		_ = dockerRm("docker", "network", "rm", phphosting.PMANetwork)
		_ = dockerRm("docker", "network", "rm", "nd-pmatest-db")
	}()
	if err := m.EnableMySQL(ctx, 512); err != nil {
		t.Fatal(err)
	}
	if err := m.CreateDatabaseOnly(ctx, "u9_alpha"); err != nil {
		t.Fatal(err)
	}
	if err := m.EnablePMA(ctx); err != nil {
		t.Fatal(err)
	}
	time.Sleep(8 * time.Second)

	p := &Panel{DB: store, PHP: m}
	sess, err := m.PMAOpen(ctx, 9, "app1", []string{"u9_alpha"}, false)
	if err != nil {
		t.Fatal(err)
	}
	var who int64 = 9
	app := fiber.New()
	app.All("/phpmyadmin/*", func(c *fiber.Ctx) error {
		c.Locals(contextUserKey, db.User{ID: who})
		return p.PMAProxy(c)
	})
	do := func(path string) (int, string) {
		req := httptest.NewRequest("GET", path, nil)
		req.Header.Set("Cookie", pmaCookie+"="+sess.Token+"; "+sessionCookie+"=secret")
		resp, err := app.Test(req, 60000)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b)
	}
	code, body := do("/phpmyadmin/index.php?route=/server/databases")
	if code != 200 || !strings.Contains(body, "u9_alpha") || strings.Contains(body, `name="pma_username"`) {
		t.Fatalf("single sign-on failed: %d %.300s", code, body)
	}
	who = 8
	if code, _ := do("/phpmyadmin/"); code != 403 {
		t.Fatalf("another user got %d, want 403", code)
	}
}

func dockerRm(name string, args ...string) error {
	return exec.Command(name, args...).Run()
}
