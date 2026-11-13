# PHP, WordPress, MariaDB & Caddy Routing (`nd`)

Reference for PHP/WordPress/static hosting (`nd php`), MariaDB databases (`nd db` / `nd php db`), cron jobs, and converting Apache `.htaccess` rules into Caddy routing (`nd php rules`).

---

## PHP / WordPress Site Management

```bash
nd php list [--json]
nd php create <name> [domain] [--domain <d>] [--no-default-domain] [--php 8.3] [--db]
nd php info [app_id] [--json]
nd php set [app_id] [--php 8.3] [--memory 256M] [--upload 64M] [--extensions "gd,intl,zip"]

nd php stop [app_id]                 # Remove Caddy route and free PHP-FPM pool
nd php start [app_id]                # Restore Caddy route and start site
nd php restart [app_id]              # Restart PHP-FPM runtime container
nd php fix-perms [app_id]            # Repair ownership & POSIX permissions in htdocs/ (755/775/644)
nd php pma [app_id]                  # Generate one-click phpMyAdmin SSO login URL
nd php import-manifest <file.json>   # Batch provision sites, domains, DBs, and routing
```

---

## MariaDB Databases (`nd db` / `nd php db`)

`nd db` is an alias for `nd php db`.

```bash
nd db list [app_id]
nd db create [app_id] <dbname> [--user <username>] [--password <pass>]
nd db grant [app_id] <user_or_id> <db_or_id> [--access all|read]
nd db rm [app_id] <db_id>
nd db export [app_id] <db_id> [dump.sql]
nd db import [app_id] <db_id> <dump.sql>

nd db user list [app_id]
nd db user create [app_id] <username> [password] [--db <db_id>]
nd db user rm [app_id] <user_id>
```

---

## Scheduled Cron Jobs (`nd php cron`)

```bash
nd php cron list [app_id]
nd php cron add [app_id] "*/5 * * * *" "php /var/www/html/wp-cron.php"
nd php cron rm [app_id] <cron_id>
nd php cron run [app_id] <cron_id>   # Trigger immediate execution
```

---

## Caddy URL Routing & `.htaccess` Conversion (`nd php rules`)

NextDeploy serves PHP and static sites behind **Caddy**, which does not read Apache `.htaccess` files at runtime. Use `nd php rules` to scan `.htaccess`, convert rules into Caddy routing rules, and configure fallback behavior.

### Quick Flow

```bash
# 1. Inspect current fallback mode and active rules
nd php rules list [app_id]

# 2. Scan htdocs/ for .htaccess files (dry-run, then apply)
nd php rules scan [app_id]
nd php rules scan [app_id] --apply --mode replace   # or --mode append

# 3. Adjust fallback behavior if needed
nd php rules fallback [app_id] <default|strict_404|custom> [entry_file]
```

### Rule Types, Fallback Modes & Commands

- **Fallback Modes**:
  - `default` — Try static file -> dir -> `/index.php` (WordPress, Laravel, CodeIgniter, Symfony)
  - `strict_404` — Real files only; missing paths return `404`
  - `custom` — Fallback to a custom script like `app.php` or `router.php`
- **Supported `.htaccess` conversions**:
  - Clean URL front-controller rewrites (`RewriteRule . /index.php [L]` -> `rewrite`)
  - Permanent/temporary redirects (`RewriteRule ... [R=301,L]`, `Redirect 301` -> `redir`)
  - Sensitive path blocks (`.env`, `.git`, `xmlrpc.php`, `wp-config.php` -> `block` returning 403/404)
- Unsupported Apache-only directives (`mod_expires`, `mod_deflate`, `php_value`, `Header set`) are skipped cleanly without corrupting Caddyfile generation (`gzip` and PHP INI settings are managed natively via `nd php set`).

```bash
# Add rule manually
nd php rules add [app_id] rewrite exact /api /api/index.php
nd php rules add [app_id] redir prefix /old /new --status 301
nd php rules add [app_id] block regex "^/\.env" --status 404

# Import, export, or remove rules
nd php rules import [app_id] path/to/.htaccess --htaccess --mode append
nd php rules import [app_id] rules.json --mode replace
nd php rules export [app_id] -o rules.json
nd php rules rm [app_id] <rule_id_or_index>
```
