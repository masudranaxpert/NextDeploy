package htaccess

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConverterParse(t *testing.T) {
	conv, err := New()
	if err != nil {
		t.Fatal(err)
	}

	t.Run("wordpress front controller skipped", func(t *testing.T) {
		in := `
# BEGIN WordPress
<IfModule mod_rewrite.c>
RewriteEngine On
RewriteBase /
RewriteRule ^index\.php$ - [L]
RewriteCond %{REQUEST_FILENAME} !-f
RewriteCond %{REQUEST_FILENAME} !-d
RewriteRule . /index.php [L]
</IfModule>
# END WordPress
`
		got := conv.Parse(in, ".htaccess")
		if len(got.Rules) != 0 {
			t.Fatalf("expected 0 rules for standard WP front controller, got %+v", got.Rules)
		}
		if len(got.Diagnostics) == 0 {
			t.Fatal("expected info diagnostics explaining skipped rules")
		}
	})

	t.Run("custom php routes and redirects", func(t *testing.T) {
		in := `
RewriteEngine On
RewriteRule ^profile/([-\w]+)/([-\w]+)$ /profile.php?id=$1&action=$2 [L,QSA]
RewriteRule ^organisations/?$ /organisation-list.php [L,NC]
RewriteRule ^secret/.*$ - [F,L]
Redirect 301 /old-blog /news
RedirectMatch 302 ^/docs/(.*)$ https://docs.example.com/$1
`
		got := conv.Parse(in, ".htaccess")
		if len(got.Rules) != 5 {
			t.Fatalf("expected 5 rules, got %d (%+v)", len(got.Rules), got.Rules)
		}
		if got.Rules[0].Kind != "rewrite" || got.Rules[0].Target != "/profile.php?id=$1&action=$2" {
			t.Errorf("rule 0 = %+v", got.Rules[0])
		}
		if got.Rules[1].Kind != "rewrite" || !strings.HasPrefix(got.Rules[1].Pattern, "(?i)") {
			t.Errorf("rule 1 should carry (?i) for [NC]: %+v", got.Rules[1])
		}
		if got.Rules[2].Kind != "block" || got.Rules[2].Status != 403 {
			t.Errorf("rule 2 should be 403 block: %+v", got.Rules[2])
		}
		if got.Rules[3].Kind != "redir" || got.Rules[3].Status != 301 || got.Rules[3].Pattern != "/old-blog/*" {
			t.Errorf("rule 3 = %+v", got.Rules[3])
		}
		if got.Rules[4].Kind != "redir" || got.Rules[4].Status != 302 || got.Rules[4].Target != "https://docs.example.com/$1" {
			t.Errorf("rule 4 = %+v", got.Rules[4])
		}
	})

	t.Run("subfolder htaccess scopes patterns and relative targets", func(t *testing.T) {
		in := `
RewriteEngine On
RewriteRule ^v1/users/([0-9]+)$ users.php?id=$1 [L]
`
		got := conv.Parse(in, "api/.htaccess")
		if len(got.Rules) != 1 {
			t.Fatalf("expected 1 rule, got %d", len(got.Rules))
		}
		if got.Rules[0].Pattern != `^/api/v1/users/([0-9]+)$` {
			t.Errorf("pattern = %q", got.Rules[0].Pattern)
		}
		if got.Rules[0].Target != "/api/users.php?id=$1" {
			t.Errorf("target = %q", got.Rules[0].Target)
		}
	})

	t.Run("filesmatch deny converted to block", func(t *testing.T) {
		in := `
<FilesMatch "\.(sql|bak|log)$">
  Order allow,deny
  Deny from all
</FilesMatch>
`
		got := conv.Parse(in, ".htaccess")
		if len(got.Rules) != 1 || got.Rules[0].Kind != "block" || got.Rules[0].Status != 403 {
			t.Fatalf("expected 403 block rule, got %+v", got.Rules)
		}
	})

	t.Run("pcre lookaround emits warning instead of invalid regex", func(t *testing.T) {
		in := `RewriteRule ^\.well-known/(?!acme-challenge) /index.php [L]`
		got := conv.Parse(in, ".htaccess")
		if len(got.Rules) != 0 {
			t.Fatalf("expected unsupported lookaround to be skipped, got %+v", got.Rules)
		}
		hasWarn := false
		for _, d := range got.Diagnostics {
			if d.Severity == SeverityWarning {
				hasWarn = true
			}
		}
		if !hasWarn {
			t.Fatal("expected warning diagnostic for PCRE lookaround")
		}
	})

	t.Run("directoryindex sets custom fallback", func(t *testing.T) {
		got := conv.Parse("DirectoryIndex app.php index.php", ".htaccess")
		if got.FallbackMode != "custom" || got.FallbackEntry != "app.php" {
			t.Fatalf("got fallback=%q entry=%q", got.FallbackMode, got.FallbackEntry)
		}
	})

	t.Run("extensionless php with not-file condition, QSA, and Files tilde regex", func(t *testing.T) {
		in := `
<Files ~ "\.(db|sqlite)$">
  Deny from all
</Files>
RewriteEngine On
RewriteCond %{REQUEST_FILENAME} !-f
RewriteRule ^([^.]+)$ $1.php [NC,L,QSA]
RewriteRule ^go/(.+)$ /redir.php?to=$0 [QSA,L]
`
		got := conv.Parse(in, ".htaccess")
		if len(got.Rules) != 3 {
			t.Fatalf("expected 3 rules, got %d (%+v)", len(got.Rules), got.Rules)
		}
		if got.Rules[0].Kind != "block" || got.Rules[0].Pattern != `\.(db|sqlite)$` {
			t.Errorf("Files ~ regex block mismatch: %+v", got.Rules[0])
		}
		if !got.Rules[1].UnlessFile || !got.Rules[1].PreserveQuery {
			t.Errorf("expected UnlessFile=true and PreserveQuery=true: %+v", got.Rules[1])
		}
		if got.Rules[2].Target != "/redir.php?to=$0" || !got.Rules[2].PreserveQuery {
			t.Errorf("expected $0 capture and PreserveQuery=true: %+v", got.Rules[2])
		}
	})

	t.Run("catch-all extensionless php and REQUEST_FILENAME.php -f never rewrites .php endpoints", func(t *testing.T) {
		in := `
RewriteEngine On
RewriteCond %{REQUEST_FILENAME} !-d
RewriteCond %{REQUEST_FILENAME}\.php -f
RewriteRule ^(.+)$ $1.php [L,QSA]

RewriteCond %{REQUEST_URI} !\.php$
RewriteRule ^(.*)$ $1.php [L]
`
		got := conv.Parse(in, ".htaccess")
		if len(got.Rules) != 2 {
			t.Fatalf("expected 2 rules, got %d (%+v)", len(got.Rules), got.Rules)
		}
		if got.Rules[0].Pattern != `^/([^.]+)$` || !got.Rules[0].UnlessFile || !got.Rules[0].RequireTargetFile || !got.Rules[0].PreserveQuery {
			t.Errorf("rule 0 mismatch: %+v", got.Rules[0])
		}
		if got.Rules[1].Pattern != `^/([^.]+)$` || !got.Rules[1].UnlessFile {
			t.Errorf("rule 1 mismatch: %+v", got.Rules[1])
		}
		for _, d := range got.Diagnostics {
			if d.Severity == SeverityWarning {
				t.Errorf("unexpected warning for recognized .php -f / !.php condition: %+v", d)
			}
		}
	})
}

func TestConverterScanDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".htaccess"), []byte("RewriteRule ^home$ /landing.php [L]\n"), 0644); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(dir, "api")
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, ".htaccess"), []byte("RewriteRule ^status$ status.php [L]\n"), 0644); err != nil {
		t.Fatal(err)
	}
	// Ignored vendor folder
	vendor := filepath.Join(dir, "vendor", "pkg")
	if err := os.MkdirAll(vendor, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vendor, ".htaccess"), []byte("RewriteRule ^ignore$ /bad.php [L]\n"), 0644); err != nil {
		t.Fatal(err)
	}

	conv, err := New()
	if err != nil {
		t.Fatal(err)
	}
	res, err := conv.ScanDir(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if res.FilesScanned != 2 {
		t.Fatalf("FilesScanned = %d, want 2 (files=%v)", res.FilesScanned, res.Files)
	}
	if len(res.Rules) != 2 {
		t.Fatalf("Rules = %d, want 2 (%+v)", len(res.Rules), res.Rules)
	}
	if res.Rules[0].Target != "/landing.php" || res.Rules[1].Target != "/api/status.php" {
		t.Errorf("unexpected targets: %+v", res.Rules)
	}
}
