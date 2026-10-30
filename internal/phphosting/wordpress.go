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
	primary, allowed := m.ResolveAllowedDomains(ctx, site.AppID, o.URL)
	hostSnippet := BuildDynamicWPHostSnippet(primary, allowed)
	steps := []struct {
		name string
		cmd  string
	}{
		{"Downloading WordPress", fmt.Sprintf("wp core download --path=%s --locale=en_US", shellQuote(htdocs))},
		{"Writing wp-config.php", fmt.Sprintf("wp config create --path=%s --dbname=%s --dbuser=%s --dbpass=%s --dbhost=%s --dbcharset=utf8mb4 --skip-check --extra-php <<'PHP'\ndefine('FS_METHOD', 'direct');\ndefine('WP_AUTO_UPDATE_CORE', 'minor');\nif (isset($_SERVER['HTTP_X_FORWARDED_PROTO']) && strpos($_SERVER['HTTP_X_FORWARDED_PROTO'], 'https') !== false) {\n    $_SERVER['HTTPS'] = 'on';\n}\n%s\nPHP",
			shellQuote(htdocs), shellQuote(o.DBName), shellQuote(o.DBUser), shellQuote(o.DBPass), MySQLContainer, hostSnippet)},
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

var (
	oldDynamicWPHostPattern = regexp.MustCompile(`(?s)if \(!defined\('WP_HOME'\) && isset\(\$_SERVER\['HTTP_HOST'\]\)\) \{.*?define\('WP_SITEURL', \$proto \. \$_SERVER\['HTTP_HOST'\]\);\s*\}`)
	nextDeployHostPattern   = regexp.MustCompile(`(?s)// NextDeploy: Dynamic host validation.*?define\('WP_SITEURL', \$proto \. \$target_host\);\s*\}`)
)

// ResolveAllowedDomains returns the primary domain and all registered domain aliases (including www variants if enabled).
func (m *Manager) ResolveAllowedDomains(ctx context.Context, appID string, fallbackURL ...string) (primary string, allowed []string) {
	seen := map[string]bool{}
	add := func(h string) string {
		h = strings.ToLower(strings.TrimSpace(h))
		h = strings.TrimPrefix(h, "https://")
		h = strings.TrimPrefix(h, "http://")
		if idx := strings.Index(h, "/"); idx != -1 {
			h = h[:idx]
		}
		if idx := strings.Index(h, ":"); idx != -1 {
			h = h[:idx]
		}
		if h != "" && !seen[h] {
			seen[h] = true
			allowed = append(allowed, h)
		}
		return h
	}

	if m.DB != nil {
		doms, err := m.DB.ListAppDomains(ctx, appID)
		if err == nil {
			for _, d := range doms {
				dom := strings.ToLower(strings.TrimSpace(d.Domain))
				if dom == "" {
					continue
				}
				cleanDom := add(dom)
				if d.IsPrimary && primary == "" && cleanDom != "" {
					primary = cleanDom
				}
				if d.EnableWWW && cleanDom != "" {
					if strings.HasPrefix(cleanDom, "www.") {
						add(strings.TrimPrefix(cleanDom, "www."))
					} else {
						add("www." + cleanDom)
					}
				}
			}
		}
	}

	if primary == "" && len(allowed) > 0 {
		primary = allowed[0]
	}

	if primary == "" && len(fallbackURL) > 0 && fallbackURL[0] != "" {
		cleaned := add(fallbackURL[0])
		if cleaned != "" {
			primary = cleaned
		}
	}

	if primary == "" {
		primary = "localhost"
	}

	return primary, allowed
}

// BuildDynamicWPHostSnippet generates PHP code that validates HTTP_HOST against allowed domains,
// falling back safely to the primary domain to prevent Host Header Injection attacks.
func BuildDynamicWPHostSnippet(primary string, allowed []string) string {
	primary = strings.TrimSpace(primary)
	if primary == "" {
		if len(allowed) > 0 {
			primary = allowed[0]
		} else {
			primary = "localhost"
		}
	}
	var b strings.Builder
	b.WriteString("// NextDeploy: Dynamic host validation (protects against Host header injection)\n")
	b.WriteString("$allowed_hosts = array(\n")
	for _, h := range allowed {
		h = strings.TrimSpace(h)
		if h != "" {
			b.WriteString(fmt.Sprintf("    '%s',\n", strings.ReplaceAll(h, "'", "\\'")))
		}
	}
	b.WriteString(");\n")
	b.WriteString(fmt.Sprintf("$primary_host = '%s';\n", strings.ReplaceAll(primary, "'", "\\'")))
	b.WriteString(`$raw_host = isset($_SERVER['HTTP_HOST']) ? strtolower(trim($_SERVER['HTTP_HOST'])) : '';
$raw_host = preg_replace('/:[0-9]+$/', '', $raw_host);
$target_host = in_array($raw_host, $allowed_hosts, true) ? $raw_host : $primary_host;
$proto = (!empty($_SERVER['HTTPS']) && $_SERVER['HTTPS'] !== 'off' || (isset($_SERVER['HTTP_X_FORWARDED_PROTO']) && strpos($_SERVER['HTTP_X_FORWARDED_PROTO'], 'https') !== false)) ? 'https://' : 'http://';
if (!defined('WP_HOME')) {
    define('WP_HOME', $proto . $target_host);
}
if (!defined('WP_SITEURL')) {
    define('WP_SITEURL', $proto . $target_host);
}`)
	return b.String()
}

// EnsureDynamicWPHost ensures wp-config.php contains dynamic WP_HOME / WP_SITEURL definitions
// with strict domain whitelist validation to prevent Host header injection and redirect loops.
func (m *Manager) EnsureDynamicWPHost(appID string) error {
	configPath := filepath.Join(m.SitePath(appID), "htdocs", "wp-config.php")
	data, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}
	content := string(data)
	primary, allowed := m.ResolveAllowedDomains(context.Background(), appID)
	newSnippet := BuildDynamicWPHostSnippet(primary, allowed)

	if nextDeployHostPattern.MatchString(content) {
		newContent := nextDeployHostPattern.ReplaceAllLiteralString(content, newSnippet)
		if newContent != content {
			return os.WriteFile(configPath, []byte(newContent), 0640)
		}
		return nil
	}

	if oldDynamicWPHostPattern.MatchString(content) {
		newContent := oldDynamicWPHostPattern.ReplaceAllLiteralString(content, newSnippet)
		return os.WriteFile(configPath, []byte(newContent), 0640)
	}

	if strings.Contains(content, "WP_HOME") || strings.Contains(content, "WP_SITEURL") {
		return nil
	}

	target := "/* Add any custom values between this line and the \"stop editing\" line. */"
	if idx := strings.Index(content, target); idx != -1 {
		insertPoint := idx + len(target)
		newContent := content[:insertPoint] + "\n\n" + newSnippet + "\n" + content[insertPoint:]
		return os.WriteFile(configPath, []byte(newContent), 0640)
	}

	target = "require_once"
	if idx := strings.Index(content, target); idx != -1 {
		newContent := content[:idx] + newSnippet + "\n\n" + content[idx:]
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
