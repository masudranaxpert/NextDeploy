package handlers

import (
	"net/http/httptest"
	"path/filepath"
	"testing"

	"panel/internal/db"

	"github.com/gofiber/fiber/v2"
)

// Calls the real access middleware as five different users against one app and checks who gets in.
func TestAppAccessMiddlewareByRole(t *testing.T) {
	ctx := httptest.NewRequest("GET", "/", nil).Context()
	store, err := db.Open(filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	admin, _ := store.CreateUser(ctx, "admin", "x", db.RoleAdmin)
	owner, _ := store.CreateUser(ctx, "owner", "x", db.RoleUser)
	viewer, _ := store.CreateUser(ctx, "viewer", "x", db.RoleUser)
	dev, _ := store.CreateUser(ctx, "dev", "x", db.RoleUser)
	stranger, _ := store.CreateUser(ctx, "stranger", "x", db.RoleUser)
	if err := store.CreateApp(ctx, "app1", "app1", owner); err != nil {
		t.Fatal(err)
	}
	if err := store.AddCollaborator(ctx, "app1", viewer, db.CollabRoleViewer); err != nil {
		t.Fatal(err)
	}
	if err := store.AddCollaborator(ctx, "app1", dev, db.CollabRoleDeveloper); err != nil {
		t.Fatal(err)
	}

	p := &Panel{DB: store}
	var as db.User
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error { c.Locals(contextUserKey, as); return c.Next() })
	app.Use("/apps/:id", p.AppAccessMiddleware)
	app.All("/apps/:id/*", func(c *fiber.Ctx) error { return c.SendStatus(200) })

	users := map[string]db.User{
		"admin":    {ID: admin, Role: db.RoleAdmin},
		"owner":    {ID: owner, Role: db.RoleUser},
		"viewer":   {ID: viewer, Role: db.RoleUser},
		"dev":      {ID: dev, Role: db.RoleUser},
		"stranger": {ID: stranger, Role: db.RoleUser},
	}
	cases := []struct {
		user, method, path string
		want               int
	}{
		{"stranger", "GET", "/apps/app1/x", 403},
		{"viewer", "GET", "/apps/app1/x", 200},
		{"viewer", "GET", "/apps/app1/files/download-zip", 403}, // reading files can expose .env
		{"viewer", "GET", "/apps/app1/git/blob", 403},
		{"viewer", "POST", "/apps/app1/env", 403},
		{"viewer", "POST", "/apps/app1/php/pma/open", 403},
		{"dev", "GET", "/apps/app1/files/download-zip", 200},
		{"dev", "POST", "/apps/app1/env", 200},
		{"dev", "POST", "/apps/app1/delete", 403}, // deleting the app is for the owner or an admin
		{"owner", "POST", "/apps/app1/env", 200},
		{"owner", "POST", "/apps/app1/delete", 200},
		{"admin", "GET", "/apps/app1/files/download-zip", 200},
	}
	for _, tc := range cases {
		as = users[tc.user]
		resp, err := app.Test(httptest.NewRequest(tc.method, tc.path, nil))
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != tc.want {
			t.Errorf("%s %s %s = %d, want %d", tc.user, tc.method, tc.path, resp.StatusCode, tc.want)
		}
	}
}
