package handlers

import (
	"context"
	"encoding/base64"
	"net/url"
	"strings"
	"time"

	"panel/internal/db"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/proxy"
)

const pmaCookie = "nd_pma"

// pmaOpenResponse sets the short-lived phpMyAdmin cookie and tells the UI where to go.
func (p *Panel) pmaStart(c *fiber.Ctx, u db.User, appID string, names []string, all bool) error {
	s, err := p.PHP.PMAOpen(c.UserContext(), u.ID, appID, names, all)
	if err != nil {
		return phpJSONErr(c, 409, err.Error())
	}
	c.Cookie(&fiber.Cookie{
		Name: pmaCookie, Value: s.Token, Path: "/phpmyadmin", HTTPOnly: true,
		Secure: c.Protocol() == "https", SameSite: "Lax", Expires: s.Expires,
	})
	return c.JSON(fiber.Map{"ok": true, "url": "/phpmyadmin/"})
}

// POST /apps/:id/php/phpmyadmin/open  — phpMyAdmin limited to this site's databases.
func (p *Panel) PHPPMAOpen(c *fiber.Ctx) error {
	_, site, err := p.phpSiteFor(c)
	if site.AppID == "" {
		return err
	}
	u, _ := currentUser(c)
	if allowed, _ := p.CanAccessApp(c.UserContext(), u.ID, u.Role, site.AppID, db.CollabRoleDeveloper); !allowed {
		return phpJSONErr(c, 403, "forbidden")
	}
	dbs, _ := p.DB.ListPHPDatabases(c.UserContext(), site.AppID)
	names := make([]string, 0, len(dbs))
	for _, d := range dbs {
		names = append(names, d.DBName)
	}
	p.RecordAuditLog(c, "php_phpmyadmin_open", "php", site.AppID, "phpMyAdmin")
	return p.pmaStart(c, u, site.AppID, names, false)
}

// POST /api/php/instance/phpmyadmin/open  — administrator view of every database.
func (p *Panel) PHPPMAOpenAll(c *fiber.Ctx) error {
	u, _ := currentUser(c)
	p.RecordAuditLog(c, "php_phpmyadmin_open", "php", "all", "phpMyAdmin (all databases)")
	return p.pmaStart(c, u, "", nil, true)
}

// POST /api/php/instance/pma {"action":"enable|disable|restart"}
func (p *Panel) PHPPMAAction(c *fiber.Ctx) error {
	var body struct {
		Action string `json:"action"`
	}
	if err := c.BodyParser(&body); err != nil {
		return phpJSONErr(c, 400, "invalid body")
	}
	switch body.Action {
	case "enable", "restart":
		started := phpAdminTasks.start("pma", func() error {
			bg, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
			defer cancel()
			if body.Action == "restart" {
				return p.PHP.RestartPMA(bg)
			}
			return p.PHP.EnablePMA(bg)
		})
		if !started {
			return phpJSONErr(c, 409, "phpMyAdmin is already being started")
		}
		p.RecordAuditLog(c, "php_phpmyadmin", "php", "pma", body.Action)
		return c.Status(202).JSON(fiber.Map{"ok": true, "started": true})
	case "disable":
		if err := p.PHP.StopPMA(c.UserContext()); err != nil {
			return phpJSONErr(c, 500, err.Error())
		}
		p.RecordAuditLog(c, "php_phpmyadmin", "php", "pma", "disable")
		return c.JSON(fiber.Map{"ok": true})
	}
	return phpJSONErr(c, 400, "unknown action")
}

// pmaProxy forwards /phpmyadmin/* to the private phpMyAdmin container. The caller must be logged in to
// NextDeploy (AuthMiddleware ran) and hold a phpMyAdmin cookie that was issued to that same user.
func (p *Panel) PMAProxy(c *fiber.Ctx) error {
	u, ok := currentUser(c)
	if !ok {
		// Session cookie fallback if AuthMiddleware was bypassed
		if sessToken := c.Cookies(sessionCookie); sessToken != "" {
			if userID, expiresAt, err := p.DB.GetSession(c.UserContext(), sessToken); err == nil && time.Now().Before(expiresAt) {
				if user, err := p.DB.GetUserByID(c.UserContext(), userID); err == nil && user.Status != db.UserStatusSuspended {
					u = user
					ok = true
					c.Locals(contextUserKey, user)
				}
			}
		}
	}
	if !ok || p.PHP == nil {
		return c.Status(401).SendString("sign in to NextDeploy first")
	}
	s, ok := p.PHP.PMASessionFor(c.Cookies(pmaCookie))
	if !ok || s.UserID != u.ID {
		return c.Status(403).SendString("This phpMyAdmin session has expired. Open phpMyAdmin again from the site's Databases tab.")
	}
	// Access can be taken away while a login is still valid (collaborator removed, app moved):
	// check it again on every request, not only when the login was created.
	if s.AppID == "" {
		if u.Role != db.RoleAdmin {
			return c.Status(403).SendString("forbidden")
		}
	} else if allowed, _ := p.CanAccessApp(c.UserContext(), u.ID, u.Role, s.AppID, db.CollabRoleDeveloper); !allowed {
		return c.Status(403).SendString("You no longer have access to this site.")
	}
	addr, err := p.PHP.PMAAddr(c.UserContext())
	if err != nil {
		return c.Status(503).SendString("phpMyAdmin is not running")
	}

	req := c.Request()
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(s.DBUser+":"+s.Password)))
	// Never hand the panel's own cookies to a third-party application.
	var keep []string
	for _, part := range strings.Split(string(req.Header.Peek("Cookie")), ";") {
		part = strings.TrimSpace(part)
		if part == "" || strings.HasPrefix(part, sessionCookie+"=") || strings.HasPrefix(part, pmaCookie+"=") {
			continue
		}
		keep = append(keep, part)
	}
	if len(keep) > 0 {
		req.Header.Set("Cookie", strings.Join(keep, "; "))
	} else {
		req.Header.Del("Cookie")
	}

	target := "http://" + addr + c.OriginalURL()
	if err := proxy.DoTimeout(c, target, 15*time.Minute); err != nil {
		return c.Status(502).SendString("phpMyAdmin did not respond")
	}
	res := c.Response()
	// phpMyAdmin answers 401 + WWW-Authenticate on "logout"; a browser login popup must never appear.
	res.Header.Del("WWW-Authenticate")
	if loc := string(res.Header.Peek("Location")); loc != "" {
		if lu, err := url.Parse(loc); err == nil && lu.Host == addr {
			lu.Scheme, lu.Host = "", ""
			res.Header.Set("Location", lu.String())
		}
	}
	return nil
}
