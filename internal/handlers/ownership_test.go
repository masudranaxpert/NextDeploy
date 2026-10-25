package handlers

import (
	"context"
	"path/filepath"
	"testing"

	"panel/internal/db"
	"panel/internal/dockerapi"
	"panel/internal/workspace"
)

// Each container, volume and image is attributed to the user who owns its project; anything
// no project claims belongs to System.
func TestBuildResourceOwners(t *testing.T) {
	dir := t.TempDir()
	store, err := db.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	p := &Panel{DB: store, Store: workspace.NewStore(dir), WorkspacesRoot: dir}
	ctx := context.Background()
	admin, _ := store.CreateUser(ctx, "masud", "h", db.RoleAdmin)
	user, _ := store.CreateUser(ctx, "rafi", "h", db.RoleUser)
	_ = store.CreateApp(ctx, "shop-1", "shop", user)
	_ = store.CreateApp(ctx, "blog-1", "blog", admin)
	users, _ := store.ListUsers(ctx)
	apps, _ := store.ListApps(ctx)

	containers := []dockerapi.ContainerRow{
		{ID: "c1", Name: "/shop-1-web-1", ComposeProject: "shop-1", Image: "shop-1-web:latest"},
		{ID: "c2", Name: "/blog-1-web-1", ComposeProject: "blog-1", Image: "blog-1-web:latest"},
		{ID: "c3", Name: "/caddy", Image: "lucaslorentz/caddy-docker-proxy:ci-alpine"},
	}
	images := []dockerapi.ImageRow{
		{ID: "i1", Tags: "shop-1-web:latest", RepoTags: []string{"shop-1-web:latest"}},
		{ID: "i2", Tags: "lucaslorentz/caddy-docker-proxy:ci-alpine", RepoTags: []string{"lucaslorentz/caddy-docker-proxy:ci-alpine"}},
	}
	got := p.buildResourceOwners(ctx, apps, users, containers, images,
		map[string][]string{"shop-1": {"shop-1_data"}}, []string{"shop-1_data", "caddy_data"})

	want := map[string]string{
		"container c1": "rafi", "container c2": "masud", "container c3": "System",
		"image i1": "rafi", "image i2": "System",
		"volume shop-1_data": "rafi", "volume caddy_data": "System",
	}
	have := map[string]string{
		"container c1": got.Containers["c1"].Name, "container c2": got.Containers["c2"].Name, "container c3": got.Containers["c3"].Name,
		"image i1": got.Images["i1"].Name, "image i2": got.Images["i2"].Name,
		"volume shop-1_data": got.Volumes["shop-1_data"].Name, "volume caddy_data": got.Volumes["caddy_data"].Name,
	}
	for k, w := range want {
		if have[k] != w {
			t.Errorf("%s: owner %q, want %q", k, have[k], w)
		}
	}
	if got.Containers["c2"].Role != db.RoleAdmin || got.Containers["c1"].Role != db.RoleUser || !got.Containers["c3"].System {
		t.Errorf("roles wrong: %+v %+v %+v", got.Containers["c1"], got.Containers["c2"], got.Containers["c3"])
	}
}
