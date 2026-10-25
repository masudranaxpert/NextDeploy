package handlers

import (
	"strings"

	"panel/internal/caddy"
	"panel/internal/db"
	"panel/internal/phphosting"

	"github.com/gofiber/fiber/v2"
	"golang.org/x/crypto/bcrypt"
)

type phpSecurityView struct {
	BlockedIPs      []string `json:"blocked_ips"`
	AllowOnly       []string `json:"allow_only"`
	BasicAuthUser   string   `json:"basic_auth_user"`
	HasPassword     bool     `json:"has_password"`
	BlockXMLRPC     bool     `json:"block_xmlrpc"`
	BlockUploadPHP  bool     `json:"block_upload_php"`
	BlockScanners   bool     `json:"block_scanners"`
	HSTS            bool     `json:"hsts"`
	FrameProtection bool     `json:"frame_protection"`
}

func securityView(s caddy.SiteSecurity) phpSecurityView {
	v := phpSecurityView{
		BlockedIPs: s.BlockedIPs, AllowOnly: s.AllowOnly,
		BasicAuthUser: s.BasicAuthUser, HasPassword: s.BasicAuthHash != "",
		BlockXMLRPC: s.BlockXMLRPC, BlockUploadPHP: s.BlockUploadPHP, BlockScanners: s.BlockScanners,
		HSTS: s.HSTS, FrameProtection: s.FrameProtection,
	}
	if v.BlockedIPs == nil {
		v.BlockedIPs = []string{}
	}
	if v.AllowOnly == nil {
		v.AllowOnly = []string{}
	}
	return v
}

// phpSecurityAccess: protection rules decide who can reach the site, so only people who may
// change the site (owner, developer, admin) can read or change them.
func (p *Panel) phpSecuritySite(c *fiber.Ctx) (db.PHPSite, bool) {
	_, site, err := p.phpSiteFor(c)
	if site.AppID == "" {
		_ = err
		return site, false
	}
	u, _ := currentUser(c)
	if allowed, _ := p.CanAccessApp(c.UserContext(), u.ID, u.Role, site.AppID, db.CollabRoleDeveloper); !allowed {
		_ = phpJSONErr(c, 403, "forbidden")
		return site, false
	}
	return site, true
}

// GET /apps/:id/php/security
func (p *Panel) PHPSecurityGet(c *fiber.Ctx) error {
	site, ok := p.phpSecuritySite(c)
	if !ok {
		return nil
	}
	return c.JSON(fiber.Map{
		"ok":        true,
		"security":  securityView(p.PHP.SiteSecurity(c.UserContext(), site.AppID)),
		"client_ip": c.IP(),
	})
}

// POST /apps/:id/php/security
func (p *Panel) PHPSecuritySave(c *fiber.Ctx) error {
	site, ok := p.phpSecuritySite(c)
	if !ok {
		return nil
	}
	var body struct {
		phpSecurityView
		Password     string `json:"basic_auth_password"`
		RemovePasswd bool   `json:"remove_password"`
	}
	if err := c.BodyParser(&body); err != nil {
		return phpJSONErr(c, 400, "invalid body")
	}
	ctx := c.UserContext()
	cur := p.PHP.SiteSecurity(ctx, site.AppID)

	blocked, err := caddy.NormalizeIPList(body.BlockedIPs)
	if err != nil {
		return phpJSONErr(c, 400, "blocked list: "+err.Error())
	}
	allow, err := caddy.NormalizeIPList(body.AllowOnly)
	if err != nil {
		return phpJSONErr(c, 400, "allowed list: "+err.Error())
	}
	next := caddy.SiteSecurity{
		BlockedIPs: blocked, AllowOnly: allow,
		BlockXMLRPC: body.BlockXMLRPC, BlockUploadPHP: body.BlockUploadPHP, BlockScanners: body.BlockScanners,
		HSTS: body.HSTS, FrameProtection: body.FrameProtection,
		BasicAuthUser: cur.BasicAuthUser, BasicAuthHash: cur.BasicAuthHash,
	}

	user := strings.TrimSpace(body.BasicAuthUser)
	switch {
	case body.RemovePasswd || (user == "" && body.Password == ""):
		next.BasicAuthUser, next.BasicAuthHash = "", ""
	case user != "":
		if !phphosting.ValidAuthUser(user) {
			return phpJSONErr(c, 400, "the user name may contain letters, numbers, dot, dash and underscore (max 32)")
		}
		if body.Password != "" {
			if len(body.Password) < 8 || len(body.Password) > 72 {
				return phpJSONErr(c, 400, "the password needs 8 to 72 characters")
			}
			h, err := bcrypt.GenerateFromPassword([]byte(body.Password), 10)
			if err != nil {
				return phpJSONErr(c, 500, err.Error())
			}
			next.BasicAuthHash = string(h)
		}
		if next.BasicAuthHash == "" {
			return phpJSONErr(c, 400, "set a password for the protected site")
		}
		next.BasicAuthUser = user
	}

	if err := p.PHP.SaveSiteSecurity(ctx, site.AppID, next); err != nil {
		return phpJSONErr(c, 500, "saved, but the web server could not be updated: "+err.Error())
	}
	p.RecordAuditLog(c, "php_security", "app", site.AppID, "Updated site protection")
	return c.JSON(fiber.Map{"ok": true, "security": securityView(next)})
}
