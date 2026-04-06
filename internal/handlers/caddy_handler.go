package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"panel/internal/caddy"
	"panel/internal/db"
	"panel/internal/dockerapi"
	"panel/internal/dockerx"

	"github.com/gofiber/fiber/v2"
)

func sanitizeDomainRecord(d *db.AppDomain) {
	if d == nil {
		return
	}
	d.Domain = caddy.CleanQuotedValue(d.Domain)
	d.Service = caddy.CleanQuotedValue(d.Service)
}

func (p *Panel) syncAppCaddyOverride(c *fiber.Ctx, appID string) error {
	app, err := p.DB.GetApp(c.UserContext(), appID)
	if err != nil {
		return err
	}
	domains, err := p.DB.ListAppDomains(c.UserContext(), appID)
	if err != nil {
		return err
	}
	ctx := c.UserContext()
	overridePath := p.composeOverridePath(ctx, appID)
	basePath := p.composeFilePath(ctx, app, appID)
	base, err := os.ReadFile(basePath)
	if err != nil {
		if os.IsNotExist(err) {
			if err := os.Remove(overridePath); err != nil && !os.IsNotExist(err) {
				return err
			}
			return nil
		}
		return err
	}
	content, err := caddy.GenerateMergedCompose(base, p.composeProjectName(app, appID), domains)
	if err != nil {
		return fmt.Errorf("generate merged compose: %w", err)
	}
	return os.WriteFile(overridePath, content, 0640)
}

// syncAndApplyBackground writes the Caddy override then runs `docker compose up -d`
// in a background goroutine so domain add/edit/delete handlers return immediately.
func (p *Panel) syncAndApplyBackground(c *fiber.Ctx, appID string) error {
	if err := p.syncAppCaddyOverride(c, appID); err != nil {
		return err
	}
	app, err := p.DB.GetApp(c.UserContext(), appID)
	if err != nil {
		return err
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		project := p.activeComposeProjectName(ctx, app, appID)
		dir := p.appSourcePath(ctx, appID)
		if res := dockerx.ComposeApply(ctx, dir, p.effectiveComposePaths(ctx, app, appID), project, nil, p.composeEnvFiles(ctx, appID)); !res.OK {
			log.Printf("compose apply app=%s project=%s: %s", appID, project, strings.TrimSpace(res.Output))
		}
	}()
	return nil
}

// ── Caddy global page ─────────────────────────────────────────────────────────

func (p *Panel) CaddyPage(c *fiber.Ctx) error {
	return c.Redirect("/nextdeploy", fiber.StatusFound)
}

// POST /caddy/config — save global caddy settings
func (p *Panel) CaddySaveConfig(c *fiber.Ctx) error {
	ctx := c.UserContext()
	fields := map[string]string{
		"admin_api":   strings.TrimSpace(c.FormValue("admin_api")),
		"email":       strings.TrimSpace(c.FormValue("email")),
		"caddy_image": strings.TrimSpace(c.FormValue("caddy_image")),
	}
	for k, v := range fields {
		if err := p.DB.SetCaddyConfig(ctx, k, v); err != nil {
			return c.Status(500).SendString(err.Error())
		}
	}
	if err := p.syncRootStackCompose(ctx); err != nil {
		return c.Status(500).SendString(err.Error())
	}
	return c.Redirect("/nextdeploy")
}

func (p *Panel) CaddyContainerAction(c *fiber.Ctx) error {
	action := strings.TrimSpace(c.FormValue("action"))
	ctx := c.UserContext()
	var err error
	switch action {
	case "start":
		err = dockerapi.StartContainerByName(ctx, caddy.CaddyContainerName)
	case "restart":
		err = dockerapi.RestartContainerByName(ctx, caddy.CaddyContainerName)
	case "stop":
		err = dockerapi.StopContainerByName(ctx, caddy.CaddyContainerName)
	default:
		return c.Status(400).SendString("invalid action")
	}
	if err != nil {
		return c.Status(500).SendString(err.Error())
	}
	return c.Redirect("/nextdeploy")
}

// ── App domain tab ────────────────────────────────────────────────────────────

// GET /apps/:id/domains — render domains tab (used by HTMX partial or full page)
func (p *Panel) AppDomainPartial(c *fiber.Ctx) error {
	id := c.Params("id")
	domains, _ := p.DB.ListAppDomains(c.UserContext(), id)
	for i := range domains {
		sanitizeDomainRecord(&domains[i])
	}
	services := p.loadComposeServices(c, id)
	return c.Render("partials/domain/domain_tab", fiber.Map{
		"ID":       id,
		"Domains":  domains,
		"Services": services,
	})
}

// POST /apps/:id/domains — create domain
func (p *Panel) AppDomainCreate(c *fiber.Ctx) error {
	id := c.Params("id")
	port, _ := strconv.Atoi(c.FormValue("port", "80"))
	if port <= 0 {
		port = 80
	}
	rulesJSON, err := parseDomainRoutesFromForm(c)
	if err != nil {
		return c.Status(400).SendString(err.Error())
	}
	d := db.AppDomain{
		AppID:       id,
		Domain:      caddy.CleanQuotedValue(c.FormValue("domain")),
		Service:     caddy.CleanQuotedValue(c.FormValue("service")),
		Port:        port,
		EnableHTTPS: c.FormValue("enable_https") == "on",
		EnableWWW:   c.FormValue("enable_www") == "on",
		ServeStatic: c.FormValue("serve_static") == "on",
		StaticPath:  strings.TrimSpace(c.FormValue("static_path")),
		ServeMedia:  c.FormValue("serve_media") == "on",
		MediaPath:   strings.TrimSpace(c.FormValue("media_path")),
		RouteRulesJSON: rulesJSON,
	}
	if d.Domain == "" {
		return c.Status(400).SendString("domain required")
	}
	if d.Service == "" {
		return c.Status(400).SendString("service required")
	}
	if _, err := p.DB.CreateAppDomain(c.UserContext(), d); err != nil {
		return c.Status(500).SendString(err.Error())
	}
	if err := p.syncAndApplyBackground(c, id); err != nil {
		return c.Status(500).SendString(err.Error())
	}
	return c.Redirect("/apps/" + id + "?tab=domains&domainSaved=1")
}

// POST /apps/:id/domains/:did/delete — delete domain
func (p *Panel) AppDomainDelete(c *fiber.Ctx) error {
	id := c.Params("id")
	did, _ := strconv.ParseInt(c.Params("did"), 10, 64)
	if err := p.DB.DeleteAppDomain(c.UserContext(), did); err != nil {
		return c.Status(500).SendString(err.Error())
	}
	if err := p.syncAndApplyBackground(c, id); err != nil {
		return c.Status(500).SendString(err.Error())
	}
	return c.Redirect("/apps/" + id + "?tab=domains&domainSaved=1")
}

// POST /apps/:id/domains/:did/edit — update domain
func (p *Panel) AppDomainEdit(c *fiber.Ctx) error {
	id := c.Params("id")
	did, _ := strconv.ParseInt(c.Params("did"), 10, 64)
	port, _ := strconv.Atoi(c.FormValue("port", "80"))
	if port <= 0 {
		port = 80
	}
	rulesJSON, err := parseDomainRoutesFromForm(c)
	if err != nil {
		return c.Status(400).SendString(err.Error())
	}
	d := db.AppDomain{
		ID:          did,
		AppID:       id,
		Domain:      caddy.CleanQuotedValue(c.FormValue("domain")),
		Service:     caddy.CleanQuotedValue(c.FormValue("service")),
		Port:        port,
		EnableHTTPS: c.FormValue("enable_https") == "on",
		EnableWWW:   c.FormValue("enable_www") == "on",
		ServeStatic: c.FormValue("serve_static") == "on",
		StaticPath:  strings.TrimSpace(c.FormValue("static_path")),
		ServeMedia:  c.FormValue("serve_media") == "on",
		MediaPath:   strings.TrimSpace(c.FormValue("media_path")),
		RouteRulesJSON: rulesJSON,
	}
	if d.Domain == "" {
		return c.Status(400).SendString("domain required")
	}
	if d.Service == "" {
		return c.Status(400).SendString("service required")
	}
	if err := p.DB.UpdateAppDomain(c.UserContext(), d); err != nil {
		return c.Status(500).SendString(err.Error())
	}
	if err := p.syncAndApplyBackground(c, id); err != nil {
		return c.Status(500).SendString(err.Error())
	}
	return c.Redirect("/apps/" + id + "?tab=domains&domainSaved=1")
}

// GET /apps/:id/domains/:did/labels — return generated labels as JSON (for preview modal)
func (p *Panel) AppDomainLabels(c *fiber.Ctx) error {
	did, _ := strconv.ParseInt(c.Params("did"), 10, 64)
	d, err := p.DB.GetAppDomain(c.UserContext(), did)
	if err != nil {
		return c.Status(404).SendString("not found")
	}
	sanitizeDomainRecord(&d)
	labels := caddy.GenerateLabels(d)
	yamlStr := caddy.LabelsToYAML(labels)
	return c.JSON(fiber.Map{
		"labels": labels,
		"yaml":   yamlStr,
	})
}

// GET /apps/:id/domains/:did/dns-check — resolve domain DNS and detect Cloudflare
func (p *Panel) AppDomainDNSCheck(c *fiber.Ctx) error {
	did, _ := strconv.ParseInt(c.Params("did"), 10, 64)
	d, err := p.DB.GetAppDomain(c.UserContext(), did)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"error": "domain not found"})
	}
	sanitizeDomainRecord(&d)
	domain := strings.TrimSpace(d.Domain)
	if domain == "" {
		return c.JSON(fiber.Map{"ok": false, "error": "empty domain"})
	}

	addrs, lookupErr := net.LookupHost(domain)
	if lookupErr != nil {
		return c.JSON(fiber.Map{
			"ok":          false,
			"domain":      domain,
			"error":       lookupErr.Error(),
			"cloudflare":  false,
			"ips":         []string{},
		})
	}

	// Cloudflare IP ranges (IPv4 + IPv6) — from https://www.cloudflare.com/ips/
	cfRanges := []string{
		"173.245.48.0/20", "103.21.244.0/22", "103.22.200.0/22", "103.31.4.0/22",
		"141.101.64.0/18", "108.162.192.0/18", "190.93.240.0/20", "188.114.96.0/20",
		"197.234.240.0/22", "198.41.128.0/17", "162.158.0.0/15", "104.16.0.0/13",
		"104.24.0.0/14", "172.64.0.0/13", "131.0.72.0/22",
		"2400:cb00::/32", "2606:4700::/32", "2803:f800::/32", "2405:b500::/32",
		"2405:8100::/32", "2a06:98c0::/29", "2c0f:f248::/32",
	}

	isBehindCF := false
	for _, ip := range addrs {
		parsed := net.ParseIP(ip)
		if parsed == nil {
			continue
		}
		for _, cidr := range cfRanges {
			_, network, err := net.ParseCIDR(cidr)
			if err != nil {
				continue
			}
			if network.Contains(parsed) {
				isBehindCF = true
				break
			}
		}
		if isBehindCF {
			break
		}
	}

	return c.JSON(fiber.Map{
		"ok":         true,
		"domain":     domain,
		"ips":        addrs,
		"cloudflare": isBehindCF,
		"internalTLS": caddy.ShouldUseInternalTLS(domain),
	})
}

// GET /caddy/logs — return caddy container logs as JSON lines
func (p *Panel) CaddyLogs(c *fiber.Ctx) error {
	tail, _ := strconv.Atoi(c.Query("tail", "300"))
	if tail <= 0 || tail > 2000 {
		tail = 300
	}
	res := dockerx.DockerLogs(c.UserContext(), caddy.CaddyContainerName, tail)
	lines := splitLogLines(res.Output)
	return c.JSON(fiber.Map{"ok": res.OK, "lines": lines})
}

func splitLogLines(s string) []string {
	if strings.TrimSpace(s) == "" {
		return []string{}
	}
	raw := strings.Split(s, "\n")
	out := make([]string, 0, len(raw))
	for _, l := range raw {
		l = strings.TrimRight(l, "\r")
		if l != "" {
			out = append(out, l)
		}
	}
	return out
}

// ── helpers ───────────────────────────────────────────────────────────────────

// loadComposeServices parses the compose file to get service names.
func (p *Panel) loadComposeServices(c *fiber.Ctx, appID string) []string {
	app, err := p.DB.GetApp(c.UserContext(), appID)
	if err != nil {
		return nil
	}
	cfPath := p.composeFilePath(c.UserContext(), app, appID)
	data, err := os.ReadFile(cfPath)
	if err != nil {
		return nil
	}
	return parseComposeServiceNames(data)
}

// parseComposeServiceNames extracts service names from a docker-compose YAML
// using a simple regex — avoids adding a yaml dependency.
func parseComposeServiceNames(data []byte) []string {
	// Find the services: block and extract top-level keys under it.
	// This is a best-effort parser for well-formatted compose files.
	lines := strings.Split(string(data), "\n")
	inServices := false
	serviceRe := regexp.MustCompile(`^  ([a-zA-Z0-9_\-]+)\s*:`)
	var names []string
	seen := map[string]bool{}
	for _, line := range lines {
		if strings.TrimSpace(line) == "services:" {
			inServices = true
			continue
		}
		if inServices {
			// A top-level key (no leading spaces) ends the services block
			if len(line) > 0 && line[0] != ' ' && line[0] != '\t' && line[0] != '#' {
				inServices = false
				continue
			}
			if m := serviceRe.FindStringSubmatch(line); len(m) == 2 {
				name := m[1]
				if !seen[name] {
					seen[name] = true
					names = append(names, name)
				}
			}
		}
	}
	sort.Strings(names)
	return names
}

func parseDomainRoutesFromForm(c *fiber.Ctx) (string, error) {
	paths := c.Request().PostArgs().PeekMulti("route_path")
	roots := c.Request().PostArgs().PeekMulti("route_root")
	priorities := c.Request().PostArgs().PeekMulti("route_priority")

	var routes []db.AppDomainRoute
	for i := 0; i < len(paths); i++ {
		path := strings.TrimSpace(string(paths[i]))
		var root string
		if i < len(roots) {
			root = strings.TrimSpace(string(roots[i]))
		}
		if path == "" && root == "" {
			continue
		}
		if path == "" || root == "" {
			return "", fiber.NewError(fiber.StatusBadRequest, "every route needs both path and root")
		}
		priority := i + 1
		if i < len(priorities) {
			if p, err := strconv.Atoi(strings.TrimSpace(string(priorities[i]))); err == nil && p > 0 {
				priority = p
			}
		}
		routes = append(routes, db.AppDomainRoute{
			Priority: priority,
			Path:     path,
			Root:     root,
		})
	}
	b, err := json.Marshal(routes)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

