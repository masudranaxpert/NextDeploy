# PHP URL Routing, `.htaccess` Conversion & Power Lifecycle (`nd php`)

## 1. How Normal PHP & Frameworks Work on NextDeploy

NextDeploy serves PHP and static sites directly through Caddy + PHP-FPM (`php_fastcgi`):
- **Normal PHP sites (`/about.php`, `/admin/index.php`)**: Served automatically by default.
- **Extensionless `.php` URLs (`/about` → `/about.php`)**: Add a single rewrite rule with `--unless-exists` (`not file`) and `--qsa`:
  ```bash
  nd php rules add my-site rewrite regex '^/([^.]+)$' '/$1.php' --unless-exists --qsa --note 'Hide .php extension'
  ```
- **WordPress / Laravel / Symfony / CodeIgniter / Slim / FastRoute**: Front-controller routing (`index.php`) and `<1ms` WP static cache (`@wp_cache`) work out of the box when **Fallback Mode = `default`**. Standard WordPress/Laravel `.htaccess` front-controller blocks are automatically skipped during `.htaccess` import.

## 2. Power Lifecycle (`stop` / `start` / `restart`)

```bash
# Take a PHP or static site offline (removes Caddy route label + frees PHP-FPM pool)
nd php stop my-site

# Restore live Caddy routing and PHP-FPM pool
nd php start my-site

# Reload PHP-FPM workers and sync Caddy labels
nd php restart my-site
```

Note: `nd stop my-site` and `nd restart my-site` also detect PHP/static sites automatically.

## 3. URL Routing & `.htaccess` Auto-Converter (`nd php rules`)

```bash
# List current fallback mode & active Caddy rules
nd php rules list my-site

# Dry-run scan of htdocs/ (root + subfolders) to preview converted rules and warnings
nd php rules scan my-site

# Scan htdocs/ and persist converted rules into Caddy
nd php rules scan my-site --apply --mode append   # or --mode replace

# Configure missing-file fallback strategy (try_files)
nd php rules fallback my-site default             # index.php front-controller (WP/Laravel default)
nd php rules fallback my-site strict_404          # return 404 when file/dir missing (=404)
nd php rules fallback my-site custom app.php      # custom PHP or SPA entry (e.g. index.html)

# Add ordered rules manually (rewrite | redir | block)
nd php rules add my-site redir prefix /old-docs /docs --status 301
nd php rules add my-site rewrite regex '^/blog/(.*)$' '/post.php?slug=$1' --qsa --unless-exists
nd php rules add my-site block regex '\.(bak|sql|sqlite)$' --status 404

# Batch import / export routing rules across sites
nd php rules import my-site rules.json --mode append
nd php rules import my-site .htaccess --htaccess --dry-run
nd php rules export my-site -o rules.json

# Batch provision sites with custom domains, DBs, and routing rules
nd php import-manifest sites.json

# Create PHP site with custom domain (no .sslip.io fallback) and auto DB
nd php create my-site example.com --no-default-domain --db appdb --version 8.3

# Grant DB privileges & add cron safely
nd php db grant my-site appuser appdb --access all
nd php cron add my-site --schedule "*/5 * * * *" --command "php artisan schedule:run"

# Remove a rule by ID (e.g. ht-1) or 1-based row number
nd php rules rm my-site 1
```

## 4. Supported Apache `.htaccess` Mappings & Gotchas

| Apache Directive / Flag | NextDeploy Caddy Translation | Notes |
| :--- | :--- | :--- |
| `RewriteRule` (`$0`–`$9`) | `route.%03d_rewrite` + `path_regexp` (`{re.ndrN.$1}`) | Subfolder `.htaccess` paths and relative targets are automatically prefixed |
| `[NC]` | Prepends `(?i)` Go RE2 flag | Case-insensitive regex matching |
| `RewriteCond %{REQUEST_FILENAME} !-f` / `!-d` | `unless_exists: true` → `@ndruleN.not.file {path} {path}/` | Prevents rules from breaking existing `.css`/`.js`/images |
| `[QSA]` | `preserve_query: true` → appends `&{query}` | Only needed when target includes `?`; Caddy preserves query strings otherwise |
| `[R=301|302|307|308]` / `Redirect` / `RedirectMatch` | `route.%03d_redir` | External HTTPS enforced automatically by Caddy |
| `[F]` / `[G]` / `<Files ~ ...>` / `<FilesMatch>` deny | `route.%03d_respond` (`403` / `404` / `410`) | Security rules evaluate inside `route` before `@wp_cache` and `php_fastcgi` |
| `DirectoryIndex <file>` | Suggests `fallback_mode: custom` | Non-default entry scripts mapped cleanly |
| `%{HTTP_HOST}`, `RewriteCond` complex expr, PCRE lookarounds `(?!...)` | Warning diagnostic | Re2 rejects lookarounds cleanly; use NextDeploy Domains/Proxy or Security UI |
