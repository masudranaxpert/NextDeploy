package phphosting

import (
	"context"
	"fmt"
	"net/mail"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"panel/internal/db"
)

// WPOptions are the inputs for a one-click WordPress install.
type WPOptions struct {
	URL        string // public URL (https://domain) used as siteurl/home
	Title      string
	AdminUser  string
	AdminEmail string
	AdminPass  string
	DBName     string
	DBUser     string
	DBPass     string
}

var wpUserRe = regexp.MustCompile(`^[A-Za-z0-9_.@-]{3,60}$`)

func (o WPOptions) Validate() error {
	if !wpUserRe.MatchString(o.AdminUser) {
		return fmt.Errorf("invalid WordPress admin user name")
	}
	if _, err := mail.ParseAddress(o.AdminEmail); err != nil {
		return fmt.Errorf("invalid WordPress admin e-mail")
	}
	if len(o.AdminPass) < 10 {
		return fmt.Errorf("WordPress admin password must be at least 10 characters")
	}
	return nil
}

// shellQuote single-quotes s for sh -c.
func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// InstallWordPress downloads WordPress with wp-cli inside the site's PHP container and
// configures it against the site's database. It reports progress through log.
func (m *Manager) InstallWordPress(ctx context.Context, site db.PHPSite, o WPOptions, log func(string)) error {
	if err := o.Validate(); err != nil {
		return err
	}
	htdocs := DocRoot(site.AppID, "")
	steps := []struct {
		name string
		cmd  string
	}{
		{"Downloading WordPress", fmt.Sprintf("wp core download --path=%s --locale=en_US", shellQuote(htdocs))},
		{"Writing wp-config.php", fmt.Sprintf("wp config create --path=%s --dbname=%s --dbuser=%s --dbpass=%s --dbhost=%s --dbcharset=utf8mb4 --skip-check --extra-php <<'PHP'\ndefine('FS_METHOD', 'direct');\ndefine('WP_AUTO_UPDATE_CORE', 'minor');\nif (isset($_SERVER['HTTP_X_FORWARDED_PROTO']) && strpos($_SERVER['HTTP_X_FORWARDED_PROTO'], 'https') !== false) {\n    $_SERVER['HTTPS'] = 'on';\n}\nif (!defined('WP_HOME') && isset($_SERVER['HTTP_HOST'])) {\n    $proto = (!empty($_SERVER['HTTPS']) && $_SERVER['HTTPS'] !== 'off' || (isset($_SERVER['HTTP_X_FORWARDED_PROTO']) && $_SERVER['HTTP_X_FORWARDED_PROTO'] === 'https')) ? 'https://' : 'http://';\n    define('WP_HOME', $proto . $_SERVER['HTTP_HOST']);\n    define('WP_SITEURL', $proto . $_SERVER['HTTP_HOST']);\n}\nPHP",
			shellQuote(htdocs), shellQuote(o.DBName), shellQuote(o.DBUser), shellQuote(o.DBPass), MySQLContainer)},
		{"Installing WordPress", fmt.Sprintf("wp core install --path=%s --url=%s --title=%s --admin_user=%s --admin_password=%s --admin_email=%s --skip-email",
			shellQuote(htdocs), shellQuote(o.URL), shellQuote(o.Title), shellQuote(o.AdminUser), shellQuote(o.AdminPass), shellQuote(o.AdminEmail))},
	}
	for _, st := range steps {
		log("==> " + st.name)
		cctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
		res := m.RunInSite(cctx, site, "php -d memory_limit=512M /usr/local/bin/wp "+strings.TrimPrefix(st.cmd, "wp "))
		cancel()
		out := res.Output
		// never echo secrets that wp-cli prints back
		for _, secret := range []string{o.DBPass, o.AdminPass} {
			if secret != "" {
				out = strings.ReplaceAll(out, secret, "********")
			}
		}
		if out != "" {
			log(out)
		}
		if !res.OK {
			return fmt.Errorf("%s failed", strings.ToLower(st.name))
		}
	}
	m.FixOwnership(site)
	return nil
}

const dynamicWPHostSnippet = `if (!defined('WP_HOME') && isset($_SERVER['HTTP_HOST'])) {
    $proto = (!empty($_SERVER['HTTPS']) && $_SERVER['HTTPS'] !== 'off' || (isset($_SERVER['HTTP_X_FORWARDED_PROTO']) && $_SERVER['HTTP_X_FORWARDED_PROTO'] === 'https')) ? 'https://' : 'http://';
    define('WP_HOME', $proto . $_SERVER['HTTP_HOST']);
    define('WP_SITEURL', $proto . $_SERVER['HTTP_HOST']);
}`

// EnsureDynamicWPHost ensures wp-config.php contains dynamic WP_HOME / WP_SITEURL definitions
// to prevent redirect loops when accessing via custom domains, aliases, or sslip.io.
func (m *Manager) EnsureDynamicWPHost(appID string) error {
	configPath := filepath.Join(m.SitePath(appID), "htdocs", "wp-config.php")
	data, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}
	content := string(data)
	if strings.Contains(content, "WP_HOME") || strings.Contains(content, "WP_SITEURL") {
		return nil
	}

	target := "/* Add any custom values between this line and the \"stop editing\" line. */"
	if idx := strings.Index(content, target); idx != -1 {
		insertPoint := idx + len(target)
		newContent := content[:insertPoint] + "\n\n" + dynamicWPHostSnippet + "\n" + content[insertPoint:]
		return os.WriteFile(configPath, []byte(newContent), 0640)
	}

	target = "require_once"
	if idx := strings.Index(content, target); idx != -1 {
		newContent := content[:idx] + dynamicWPHostSnippet + "\n\n" + content[idx:]
		return os.WriteFile(configPath, []byte(newContent), 0640)
	}
	return nil
}

// SyncWordPressDomain updates WordPress siteurl and home to the new primary domain,
// flushes rewrites, and ensures dynamic host handling is present in wp-config.php.
func (m *Manager) SyncWordPressDomain(ctx context.Context, site db.PHPSite, domain string) error {
	domain = strings.TrimSpace(domain)
	if domain == "" {
		return nil
	}
	configPath := filepath.Join(m.SitePath(site.AppID), "htdocs", "wp-config.php")
	if _, err := os.Stat(configPath); err != nil {
		return nil
	}

	// 1. Ensure dynamic host logic in wp-config.php
	_ = m.EnsureDynamicWPHost(site.AppID)

	// 2. Update database options using WP-CLI
	htdocs := DocRoot(site.AppID, "")
	targetURL := "https://" + domain
	cmd := fmt.Sprintf("php -d memory_limit=512M /usr/local/bin/wp option update siteurl %s --path=%s --skip-plugins --skip-themes && php -d memory_limit=512M /usr/local/bin/wp option update home %s --path=%s --skip-plugins --skip-themes",
		shellQuote(targetURL), shellQuote(htdocs), shellQuote(targetURL), shellQuote(htdocs))

	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	res := m.RunInSite(cctx, site, cmd)
	if !res.OK {
		return fmt.Errorf("wp option update failed: %s", res.Output)
	}
	return nil
}

// AutoHealExistingWordPressSites runs on startup and heals all existing WordPress installations:
// 1. Injects dynamic WP_HOME/WP_SITEURL into wp-config.php so redirect loops never happen on any domain.
// 2. Synchronizes database siteurl/home to the primary domain configured in NextDeploy.
func (m *Manager) AutoHealExistingWordPressSites(ctx context.Context) {
	if m.DB == nil {
		return
	}
	sites, err := m.DB.ListPHPSites(ctx)
	if err != nil || len(sites) == 0 {
		return
	}
	for _, site := range sites {
		configPath := filepath.Join(m.SitePath(site.AppID), "htdocs", "wp-config.php")
		if _, err := os.Stat(configPath); err != nil {
			continue // Not a WordPress site
		}

		// 1. Ensure dynamic WP_HOME/WP_SITEURL in wp-config.php
		_ = m.EnsureDynamicWPHost(site.AppID)

		// 2. Determine the primary domain dynamically from app_domains
		doms, err := m.DB.ListAppDomains(ctx, site.AppID)
		if err != nil || len(doms) == 0 {
			continue
		}
		var primaryDomain string
		for _, d := range doms {
			if d.IsPrimary {
				primaryDomain = d.Domain
				break
			}
		}
		if primaryDomain == "" {
			primaryDomain = doms[0].Domain
		}

		if primaryDomain != "" {
			_ = m.SyncWordPressDomain(ctx, site, primaryDomain)
		}
	}
}
