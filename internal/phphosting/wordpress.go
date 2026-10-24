package phphosting

import (
	"context"
	"fmt"
	"net/mail"
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
		{"Writing wp-config.php", fmt.Sprintf("wp config create --path=%s --dbname=%s --dbuser=%s --dbpass=%s --dbhost=%s --dbcharset=utf8mb4 --skip-check --extra-php <<'PHP'\ndefine('FS_METHOD', 'direct');\ndefine('WP_AUTO_UPDATE_CORE', 'minor');\nPHP",
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
