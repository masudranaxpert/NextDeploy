# Temporary CloudPanel Test Bridges — Cleanup Tracker

The following lines were added temporarily to allow live testing of CloudPanel's existing sites on the test VPS without migrating or re-exporting MySQL databases.

Once migration testing is concluded, remove these lines to keep NextDeploy 100% independent.

---

### 1. `docker-compose.yml`
- **Location:** Line 44
- **Action:** Remove the host mysqld volume mount from `panel` service:
```yaml
            - /var/run/mysqld:/var/run/mysqld
```

---

### 2. `internal/phphosting/runtime.go`
- **Location:** Around lines 143-145 in `ensureRuntimeContainer`
- **Action:** Remove host mysqld check and volume mount:
```go
	if _, err := os.Stat("/var/run/mysqld"); err == nil {
		args = append(args, "-v", "/var/run/mysqld:/var/run/mysqld")
	}
```

---

### 3. `internal/phphosting/pool.go`
- **Location:** Around lines 96-97 in `RenderPool`
- **Action:** Remove explicit host socket overrides:
```go
	w("php_admin_value[mysqli.default_socket] = /var/run/mysqld/mysqld.sock")
	w("php_admin_value[pdo_mysql.default_socket] = /var/run/mysqld/mysqld.sock")
```

---

### Note on Permanent Features:
- `internal/caddy/caddy.go` (`IsBehindCloudflare` and automatic `tls internal` selection):
  **PERMANENT CORE FEATURE.** Keep this in NextDeploy permanently so any domain behind Cloudflare proxy works out of the box with zero SSL errors.
