package handlers

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"panel/internal/caddy"
	"panel/internal/db"
	"panel/internal/phphosting"

	"github.com/gofiber/fiber/v2"
)

const caddyOptionsKey = "global_options"

// caddyGlobalOptions loads the saved server-wide Caddy options (defaults when none were saved).
func (p *Panel) caddyGlobalOptions(ctx context.Context) caddy.GlobalOptions {
	raw := p.DB.GetCaddyConfig(ctx, caddyOptionsKey)
	var o caddy.GlobalOptions
	if strings.TrimSpace(raw) == "" || json.Unmarshal([]byte(raw), &o) != nil {
		return caddy.OptionProfiles["default"]
	}
	if n, err := o.Normalize(); err == nil {
		return n
	}
	return caddy.OptionProfiles["default"]
}

// GET /caddy/options
func (p *Panel) CaddyOptionsGet(c *fiber.Ctx) error {
	o := p.caddyGlobalOptions(c.UserContext())
	if o.TrustedProxies == nil {
		o.TrustedProxies = []string{}
	}
	return c.JSON(fiber.Map{
		"ok":                true,
		"options":           o,
		"profiles":          caddy.OptionProfiles,
		"cloudflare_ranges": caddy.CloudflareRanges,
		"labels":            o.Labels(),
		"client_ip":         c.IP(),
	})
}

// POST /caddy/options {options..., "apply": true}
func (p *Panel) CaddyOptionsSave(c *fiber.Ctx) error {
	var body struct {
		caddy.GlobalOptions
		Apply bool `json:"apply"`
	}
	if err := c.BodyParser(&body); err != nil {
		return phpJSONErr(c, 400, "invalid body")
	}
	o, err := body.GlobalOptions.Normalize()
	if err != nil {
		return phpJSONErr(c, 400, err.Error())
	}
	raw, _ := json.Marshal(o)
	ctx := c.UserContext()
	if err := p.DB.SetCaddyConfig(ctx, caddyOptionsKey, string(raw)); err != nil {
		return phpJSONErr(c, 500, err.Error())
	}
	if err := p.syncRootStackCompose(ctx); err != nil {
		return phpJSONErr(c, 500, "saved, but the stack file could not be updated: "+err.Error())
	}
	if body.Apply {
		p.applyRootStackComposeBackground(p.nextDeployComposePath(), "caddy")
	}
	p.RecordAuditLog(c, "caddy_options", "system", "caddy", "Updated Caddy server options")
	return c.JSON(fiber.Map{"ok": true, "applying": body.Apply, "options": o})
}

// ---- proxy routes (reverse proxy and redirects, admin only) ----

func proxyRouteContainer(id int64) string { return "nd-route-" + strconv.FormatInt(id, 10) }

func (p *Panel) syncProxyRoute(ctx context.Context, r db.ProxyRoute) error {
	name := proxyRouteContainer(r.ID)
	if !r.Enabled {
		p.PHP.RemoveHolder(ctx, name)
		return nil
	}
	return p.PHP.PublishHolder(ctx, name, caddy.RouteLabels(r), map[string]string{"nd.proxy": "route", "nd.proxy.id": strconv.FormatInt(r.ID, 10)})
}

// ReconcileProxyRoutes republishes every enabled route (used at start-up).
func (p *Panel) ReconcileProxyRoutes(ctx context.Context) {
	routes, err := p.DB.ListProxyRoutes(ctx)
	if err != nil {
		return
	}
	for _, r := range routes {
		_ = p.syncProxyRoute(ctx, r)
	}
}

// GET /caddy/routes
func (p *Panel) CaddyRoutesList(c *fiber.Ctx) error {
	routes, err := p.DB.ListProxyRoutes(c.UserContext())
	if err != nil {
		return phpJSONErr(c, 500, err.Error())
	}
	return c.JSON(fiber.Map{"ok": true, "routes": routes})
}

// POST /caddy/routes (create) and /caddy/routes/:id (update)
func (p *Panel) CaddyRouteSave(c *fiber.Ctx) error {
	ctx := c.UserContext()
	var r db.ProxyRoute
	if err := c.BodyParser(&r); err != nil {
		return phpJSONErr(c, 400, "invalid body")
	}
	r.ID = 0
	if idStr := c.Params("id"); idStr != "" {
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			return phpJSONErr(c, 400, "invalid id")
		}
		if _, err := p.DB.GetProxyRoute(ctx, id); err != nil {
			return phpJSONErr(c, 404, "route not found")
		}
		r.ID = id
	}
	if err := caddy.ValidateRoute(&r, phphosting.ValidHost); err != nil {
		return phpJSONErr(c, 400, err.Error())
	}
	if who, used := p.DB.DomainInUse(ctx, r.Domain, r.ID); used {
		return phpJSONErr(c, 409, r.Domain+" is already used by "+who)
	}
	id, err := p.DB.SaveProxyRoute(ctx, r)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return phpJSONErr(c, 409, r.Domain+" already has a route")
		}
		return phpJSONErr(c, 500, err.Error())
	}
	r.ID = id
	saved, _ := p.DB.GetProxyRoute(ctx, id)
	if err := p.syncProxyRoute(ctx, saved); err != nil {
		return phpJSONErr(c, 500, "saved, but the web server could not be updated: "+err.Error())
	}
	p.RecordAuditLog(c, "caddy_route", "system", saved.Domain, "Saved "+saved.Kind+" route to "+saved.Target)
	return c.JSON(fiber.Map{"ok": true, "route": saved})
}

// POST /caddy/routes/:id/toggle {"enabled": bool}
func (p *Panel) CaddyRouteToggle(c *fiber.Ctx) error {
	ctx := c.UserContext()
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return phpJSONErr(c, 400, "invalid id")
	}
	r, err := p.DB.GetProxyRoute(ctx, id)
	if err != nil {
		return phpJSONErr(c, 404, "route not found")
	}
	var body struct {
		Enabled bool `json:"enabled"`
	}
	if err := c.BodyParser(&body); err != nil {
		return phpJSONErr(c, 400, "invalid body")
	}
	r.Enabled = body.Enabled
	if _, err := p.DB.SaveProxyRoute(ctx, r); err != nil {
		return phpJSONErr(c, 500, err.Error())
	}
	if err := p.syncProxyRoute(ctx, r); err != nil {
		return phpJSONErr(c, 500, err.Error())
	}
	return c.JSON(fiber.Map{"ok": true, "route": r})
}

// POST /caddy/routes/:id/delete
func (p *Panel) CaddyRouteDelete(c *fiber.Ctx) error {
	ctx := c.UserContext()
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return phpJSONErr(c, 400, "invalid id")
	}
	r, err := p.DB.GetProxyRoute(ctx, id)
	if err != nil {
		return phpJSONErr(c, 404, "route not found")
	}
	p.PHP.RemoveHolder(ctx, proxyRouteContainer(id))
	if err := p.DB.DeleteProxyRoute(ctx, id); err != nil {
		return phpJSONErr(c, 500, err.Error())
	}
	p.RecordAuditLog(c, "caddy_route", "system", r.Domain, "Deleted route")
	return c.JSON(fiber.Map{"ok": true})
}
