# MCP Reference: `php_routing` & `php_power`

## 1. Normal PHP vs Framework Routing Behavior

- **Default (`fallback_mode: ""` / `"default"`)**: Caddy runs `php_fastcgi` with standard `try_files {path} {path}/index.php index.php` plus `<1ms` `@wp_cache` static disk caching. Works out of the box for WordPress, Laravel, CodeIgniter, Slim, FastRoute, and normal multi-file PHP (`/about.php`).
- **Hide `.php` extensions**: Use a `rewrite` rule with `unless_exists: true` and `preserve_query: true` (`^/([^.]+)$` → `/$1.php`).

## 2. `php_routing` Payload Reference

### Inspect current routing
```json
{ "tool": "php_routing", "arguments": { "app_id": "my-site", "action": "get" } }
```

### Dry-run scan `htdocs/` (including subfolders)
```json
{ "tool": "php_routing", "arguments": { "app_id": "my-site", "action": "scan_htaccess", "apply": false } }
```

### Scan & apply converted `.htaccess` rules
```json
{
  "tool": "php_routing",
  "arguments": {
    "app_id": "my-site",
    "action": "scan_htaccess",
    "apply": true,
    "mode": "append"
  }
}
```

### Convert raw `.htaccess` snippet (dry-run preview)
```json
{
  "tool": "php_routing",
  "arguments": {
    "app_id": "my-site",
    "action": "convert_htaccess",
    "file_path": ".htaccess",
    "content": "RewriteEngine On\nRewriteCond %{REQUEST_FILENAME} !-f\nRewriteRule ^([^.]+)$ $1.php [NC,QSA,L]"
  }
}
```

### Save custom ordered rules & fallback mode (`action: "set"`)
```json
{
  "tool": "php_routing",
  "arguments": {
    "app_id": "my-site",
    "action": "set",
    "fallback_mode": "strict_404",
    "rules": [
      {
        "id": "ext-php",
        "kind": "rewrite",
        "match_type": "regex",
        "pattern": "^/([^.]+)$",
        "target": "/$1.php",
        "enabled": true,
        "unless_exists": true,
        "preserve_query": true,
        "note": "Clean extensionless PHP URLs"
      },
      {
        "id": "old-docs",
        "kind": "redir",
        "match_type": "prefix",
        "pattern": "/old-docs",
        "target": "/docs",
        "status": 301,
        "enabled": true
      }
    ]
  }
}
```

## 3. `php_power` Payload Reference

```json
{ "tool": "php_power", "arguments": { "app_id": "my-site", "action": "stop" } }
{ "tool": "php_power", "arguments": { "app_id": "my-site", "action": "start" } }
{ "tool": "php_power", "arguments": { "app_id": "my-site", "action": "restart" } }
```

*(Standard `stop` and `restart` tools also automatically detect PHP/static sites and delegate cleanly.)*

## 4. RBAC & Security Guarantees

- **Viewer role**: Allowed on `action: "get"`, `action: "convert_htaccess"`, and `action: "scan_htaccess"` (`apply: false`).
- **Developer / Admin role**: Required for `action: "set"`, `action: "scan_htaccess"` (`apply: true`), and `php_power`.
- **Injection defense**: All patterns are compiled with Go RE2 (`regexp.Compile`), targets disallow Caddy label meta-chars (`\r`, `\n`, `"`, `'`, `{`, `}`, `;`, `#`, spaces) and dotfile/traversal targets (`/.env`, `..`). All mutations create audit log entries.
