package handlers

import (
	"context"
	"encoding/json"
	"io"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"panel/internal/db"
	"panel/internal/proxy"
	"panel/internal/traffic"

	"github.com/gofiber/fiber/v2"
)

func TestTrafficMapAndZoneSums(t *testing.T) {
	st := &proxy.State{
		Panel: &proxy.Site{ID: "panel", Hosts: []string{"panel.example.com"}},
		Sites: []proxy.Site{
			{ID: "app_blog_dom_1", AppID: "blog", Hosts: []string{"blog.com", "www.blog.com"}},
			{ID: "app_blog_dom_2", AppID: "blog", Hosts: []string{"blog.net"}},
			{ID: "php_shop_7", AppID: "shop", Hosts: []string{"Shop.com"}},
			{ID: "route_9", RouteID: 9, Hosts: []string{"r.example.com"}},
		},
		Gateways: []proxy.GatewayPort{{Port: 3000, AppID: "blog"}, {Port: 8000}},
	}
	m := buildTrafficMap("angie", st)
	if m.zones["nd_app_blog_dom_1"] != "app:blog" || m.zones["nd_panel"] != keyPanel ||
		m.zones["nd_gw_3000"] != "app:blog" || m.zones["nd_gw_8000"] != "gw:8000" || m.hosts["shop.com"] != "app:shop" {
		t.Fatalf("map = %+v", m)
	}
	totals, unknown := sumByKey(map[string]proxy.ZoneStats{
		"nd_app_blog_dom_1": {Requests: 10, S2xx: 9, S5xx: 1},
		"nd_app_blog_dom_2": {Requests: 5, S2xx: 5},
		"nd_gw_3000":        {Requests: 1, S2xx: 1},
	}, m.zones)
	if unknown || totals["app:blog"].Requests != 16 || totals["app:blog"].S5xx != 1 {
		t.Fatalf("totals = %+v unknown=%v", totals, unknown)
	}
	if _, unknown := sumByKey(map[string]proxy.ZoneStats{"nd_new_site": {Requests: 1}}, m.zones); !unknown {
		t.Fatal("a zone without a site must ask for a fresh map")
	}
}

func TestTrafficSummaryShowsUsersOnlyTheirSites(t *testing.T) {
	store, err := db.Open(filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	adminID, _ := store.CreateUser(ctx, "root", "x", db.RoleAdmin)
	meID, err := store.CreateUser(ctx, "me", "x", "user")
	if err != nil {
		t.Fatal(err)
	}
	otherID, _ := store.CreateUser(ctx, "other", "x", "user")
	if err := store.CreateApp(ctx, "mine", "Mine", meID); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateApp(ctx, "theirs", "Theirs", otherID); err != nil {
		t.Fatal(err)
	}

	old := trafficCol
	trafficCol = &trafficCollector{store: traffic.NewStore()}
	trafficCol.once.Do(func() {}) // no background reader in the test
	defer func() { trafficCol = old }()
	now := time.Now()
	prev := now.Add(-time.Minute)
	trafficCol.store.Observe(prev.Add(-time.Minute), "angie", map[string]traffic.Counters{"app:mine": {}, "app:theirs": {}, keyPanel: {}}, true, "")
	trafficCol.store.Observe(prev, "angie", map[string]traffic.Counters{"app:mine": {Requests: 4}, "app:theirs": {Requests: 9}, keyPanel: {Requests: 2}}, true, "")

	p := &Panel{DB: store}
	run := func(user db.User) []trafficSite {
		app := fiber.New()
		app.Get("/t", func(c *fiber.Ctx) error {
			c.Locals(contextUserKey, user)
			return p.TrafficSummary(c)
		})
		resp, err := app.Test(httptest.NewRequest("GET", "/t?window=15", nil))
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		var out struct {
			Sites []trafficSite `json:"sites"`
		}
		if err := json.Unmarshal(body, &out); err != nil {
			t.Fatalf("%v: %s", err, body)
		}
		return out.Sites
	}

	user := run(db.User{ID: meID, Role: "user"})
	if len(user) != 1 || user[0].AppID != "mine" || user[0].Name != "Mine" {
		t.Fatalf("user sees %+v", user)
	}
	admin := run(db.User{ID: adminID, Role: db.RoleAdmin})
	if len(admin) != 3 || admin[0].AppID != "theirs" {
		t.Fatalf("admin sees %+v", admin)
	}
}
