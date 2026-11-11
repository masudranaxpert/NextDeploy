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
	if o.ShieldProxies == nil {
		o.ShieldProxies = []string{}
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
	ctx := c.UserContext()
	existing := p.caddyGlobalOptions(ctx)
	body.GlobalOptions.ShieldProxies = existing.ShieldProxies
	body.GlobalOptions.DirectCloudflare = existing.DirectCloudflare
	o, err := body.GlobalOptions.Normalize()
	if err != nil {
		return phpJSONErr(c, 400, err.Error())
	}
	raw, _ := json.Marshal(o)
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
	if p.PHP == nil {
		return nil
	}
	name := proxyRouteContainer(r.ID)
	if !r.Enabled {
		p.PHP.RemoveHolder(ctx, name)
		return nil
	}
	return p.PHP.PublishHolder(ctx, name, caddy.RouteLabels(r), map[string]string{"nd.proxy": "route", "nd.proxy.id": strconv.FormatInt(r.ID, 10)})
}

// ReconcileProxyRoutes republishes every enabled route (used at start-up).
func (p *Panel) ReconcileProxyRoutes(ctx context.Context) {
	if p.reconcileProxyRoutesFn != nil {
		p.reconcileProxyRoutesFn(ctx)
		return
	}
	routes, err := p.DB.ListProxyRoutes(ctx, nil)
	if err != nil {
		return
	}
	for _, r := range routes {
		_ = p.syncProxyRoute(ctx, r)
	}
}

// ReconcileAllAppSlotCompose reconciles compose slot overrides for all active apps on startup.
func (p *Panel) ReconcileAllAppSlotCompose(ctx context.Context) {
	apps, err := p.DB.ListApps(ctx)
	if err != nil {
		return
	}
	for _, a := range apps {
		if p.PHP != nil && p.DB.IsPHPSite(ctx, a.ID) {
			continue
		}
		slot := strings.TrimSpace(a.ActiveSlot)
		if slot == "" {
			slot = "blue"
		}
		_, _ = p.SyncAppSlotComposeCtx(ctx, a.ID, slot, false)
		if strings.TrimSpace(a.PinnedSlot) != "" && strings.TrimSpace(a.StandbySlot) != "" {
			_, _ = p.SyncAppSlotComposeCtx(ctx, a.ID, strings.TrimSpace(a.StandbySlot), true)
		}
	}
}

// GET /caddy/routes
func (p *Panel) CaddyRoutesList(c *fiber.Ctx) error {
	ctx := c.UserContext()
	u, _ := currentUser(c)
	var filterUserID *int64
	if u.Role != db.RoleAdmin {
		filterUserID = &u.ID
	}
	routes, err := p.DB.ListProxyRoutes(ctx, filterUserID)
	if err != nil {
		return phpJSONErr(c, 500, err.Error())
	}
	p.attachProxyRouteEdgeStatuses(ctx, routes)
	return c.JSON(fiber.Map{"ok": true, "routes": routes})
}

// POST /caddy/routes (create) and /caddy/routes/:id (update)
func (p *Panel) CaddyRouteSave(c *fiber.Ctx) error {
	ctx := c.UserContext()
	u, _ := currentUser(c)
	isAdmin := u.Role == db.RoleAdmin

	var r db.ProxyRoute
	if err := c.BodyParser(&r); err != nil {
		return phpJSONErr(c, 400, "invalid body")
	}
	var alt struct {
		Upstream  string `json:"upstream"`
		RouteType string `json:"route_type"`
	}
	_ = c.BodyParser(&alt)
	if r.Target == "" && alt.Upstream != "" {
		r.Target = strings.TrimSpace(alt.Upstream)
	}
	if r.Kind == "" && alt.RouteType != "" {
		r.Kind = strings.TrimSpace(alt.RouteType)
	}
	if r.Kind == "reverse_proxy" {
		r.Kind = "proxy"
	}
	if r.Kind == "" {
		r.Kind = "proxy"
	}
	r.ID = 0
	var existing db.ProxyRoute
	hasExisting := false
	if idStr := c.Params("id"); idStr != "" {
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			return phpJSONErr(c, 400, "invalid id")
		}
		ex, err := p.DB.GetProxyRoute(ctx, id)
		if err != nil {
			return phpJSONErr(c, 404, "route not found")
		}
		existing = ex
		hasExisting = true
		r.ID = id
	}

	// Ownership check for existing route
	if hasExisting {
		if !isAdmin && existing.UserID != u.ID {
			return phpJSONErr(c, 403, "you do not have permission to modify this route")
		}
		r.UserID = existing.UserID // preserve original owner
	} else {
		r.UserID = u.ID
	}

	// Shield VPS Enforcement for regular users
	if !isAdmin {
		dbUser, err := p.DB.GetUserByID(ctx, u.ID)
		if err == nil && dbUser.DefaultShieldID != nil && *dbUser.DefaultShieldID > 0 {
			r.ShieldNodeID = dbUser.DefaultShieldID
			r.EnableHTTPS = true
		}
	}

	if r.ShieldNodeID != nil && *r.ShieldNodeID <= 0 {
		r.ShieldNodeID = nil
	}
	if r.ShieldNodeID != nil {
		if _, err := p.DB.GetShieldNode(ctx, *r.ShieldNodeID); err != nil {
			return phpJSONErr(c, 400, "shield node not found")
		}
		r.EnableHTTPS = true
	}
	if r.ShieldNodeID != nil && (!hasExisting || existing.ShieldNodeID == nil || existing.PendingOriginApply) {
		r.PendingOriginApply = true
	} else {
		r.PendingOriginApply = false
	}

	// Validate route with SSRF protection for non-admin users
	if err := caddy.ValidateRouteForUser(&r, phphosting.ValidHost, isAdmin); err != nil {
		return phpJSONErr(c, 400, err.Error())
	}

	// Prevent non-admin users from creating routes on panel_domain or its subdomains
	panelDomain := strings.ToLower(strings.TrimSpace(p.DB.GetSetting(ctx, "panel_domain")))
	if panelDomain != "" && (r.Domain == panelDomain || strings.HasSuffix(r.Domain, "."+panelDomain)) {
		if !isAdmin {
			return phpJSONErr(c, 403, "cannot create routes under the control panel domain ("+panelDomain+")")
		}
	}

	// Domain collision check
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
	if saved.PendingOriginApply {
		_ = p.DB.ClearProxyRoutePendingOriginApply(ctx, saved.ID)
		saved.PendingOriginApply = false
	}
	p.syncShieldProxiesToCaddy(ctx)
	p.RecordAuditLog(c, "caddy_route", u.Username, saved.Domain, "Saved "+saved.Kind+" route to "+saved.Target)
	return c.JSON(fiber.Map{"ok": true, "route": saved})
}

// POST /caddy/routes/:id/toggle {"enabled": bool}
func (p *Panel) CaddyRouteToggle(c *fiber.Ctx) error {
	ctx := c.UserContext()
	u, _ := currentUser(c)
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return phpJSONErr(c, 400, "invalid id")
	}
	r, err := p.DB.GetProxyRoute(ctx, id)
	if err != nil {
		return phpJSONErr(c, 404, "route not found")
	}
	if u.Role != db.RoleAdmin && r.UserID != u.ID {
		return phpJSONErr(c, 403, "you do not have permission to modify this route")
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
	p.syncShieldProxiesToCaddy(ctx)
	return c.JSON(fiber.Map{"ok": true, "route": r})
}

// POST /caddy/routes/:id/delete
func (p *Panel) CaddyRouteDelete(c *fiber.Ctx) error {
	ctx := c.UserContext()
	u, _ := currentUser(c)
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return phpJSONErr(c, 400, "invalid id")
	}
	r, err := p.DB.GetProxyRoute(ctx, id)
	if err != nil {
		return phpJSONErr(c, 404, "route not found")
	}
	if u.Role != db.RoleAdmin && r.UserID != u.ID {
		return phpJSONErr(c, 403, "you do not have permission to delete this route")
	}
	p.PHP.RemoveHolder(ctx, proxyRouteContainer(id))
	if err := p.DB.DeleteProxyRoute(ctx, id); err != nil {
		return phpJSONErr(c, 500, err.Error())
	}
	p.syncShieldProxiesToCaddy(ctx)
	p.RecordAuditLog(c, "caddy_route", u.Username, r.Domain, "Deleted route")
	return c.JSON(fiber.Map{"ok": true})
}
