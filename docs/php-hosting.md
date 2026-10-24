# PHP Hosting

NextDeploy can host PHP, WordPress and static sites next to your Docker apps, in the style of CloudPanel:
sites with databases, cron jobs, a file manager, a web terminal, domains with automatic HTTPS and backups.
Everything runs in Docker, behind the existing Caddy proxy, and uses NextDeploy's own users, roles and quotas.
There is no separate login or panel.

## For users

1. **PHP Sites → New site.** Only a name is required. Pick *PHP*, *WordPress* or *Static HTML* and a PHP version.
   You get a temporary `*.sslip.io` address right away; add your own domains in the **Domains** tab.
   WordPress is installed automatically; the admin password is shown once when the install finishes.
2. Tabs of a site: **Settings** (PHP version, root directory, `memory_limit`, upload limits, extra `php.ini`
   directives), **Domains**, **Databases**, **File Manager** (opens in `htdocs/`), **Cron Jobs**, **Logs**,
   **Terminal** (a shell as the site's own system user), **Backups** and **Access**.
3. Changing the PHP version or any setting is applied with a graceful PHP-FPM reload and does not interrupt the
   site's files or databases.
4. **Databases** and **database users** are separate, as in CloudPanel: create a database (`u<userid>_<name>`), then
   add users (`u<userid>_<name>`) and tick which databases each may use, read & write or read only. Access can be
   changed later; deleting a database removes every user's privileges on it. Passwords are never stored; reset one
   to see a new password once. Databases can be exported (`.sql.gz`) and imported.
5. PHP settings are bounded by the smaller of your quota and what the server really has: `memory_limit` cannot exceed
   your memory limit (or the server's RAM), upload/post sizes cannot exceed your storage limit, and the worker count is
   capped for your memory. The settings page only offers values that fit.

## For administrators

**PHP Hosting → Instance** (admin only):

- **Enable/disable** the module. Existing sites keep running when it is disabled; creating new ones is blocked.
- **Database server:** one shared MariaDB container (`nd-mysql`, data in the `nd-mysql-data` volume). Set its memory
  limit, start, restart or stop it.
- **PHP versions:** each version is one image (`nextdeploy/php:<version>`) built on the server from an embedded
  Dockerfile, so nothing has to be published to a registry. Install only what you need. An image is shared by every
  site using that version, and a version in use cannot be removed.
- **Services:** status and restart of the managed containers.
- **Limits:** the shortest allowed cron interval, and per-user maximum databases and cron jobs. Site count, memory,
  CPU and storage come from *Team & Quotas*; a PHP site counts as one app.

## How it works

```
Browser ─► Caddy ─ static files ─────────────► data/workspaces/<site>/htdocs   (read-only mount)
              └ *.php ─► unix socket ─► PHP-FPM pool (one per site, own UID) ─► nd-mysql
```

- A PHP site is a normal app (row in `apps`) plus a row in `php_sites`, so owners, collaborators, the file manager,
  storage quota, backups and deletion reuse the existing code. Its workspace holds `htdocs/`, `logs/` and `tmp/`.
- **Runtime:** one PHP-FPM container per *(user, PHP version)* — `nd-php<ver>-u<userid>` — with one FPM pool per
  site. Pools run as the site's own UID (starting at 20000), are confined with `open_basedir`, and listen on unix
  sockets that only Caddy can reach (FastCGI is never exposed on a TCP port). The container joins the user's cgroup
  slice, so the user's memory/CPU quota is enforced by the kernel across all of their sites.
  Adding or removing a site of that user and version recreates the container (about a second); settings changes and
  version moves reload FPM gracefully and keep sockets open.
- **Routing:** each site has a ~1 MB route-holder container (`nd-phpr-<site>`, busybox) that carries the Caddy
  labels. Changing one site's domains recreates only that holder, so other sites are not touched. Only well-formed
  host names ever reach the labels.
- **Hardening of the PHP container:** read-only root filesystem, all capabilities dropped except
  `SETUID`/`SETGID`/`DAC_READ_SEARCH`/`KILL` (needed by the root FPM master only; workers lose them on `setuid`),
  `no-new-privileges`, a PID limit, only the site directories mounted, and the database reachable only through a
  private `nd-db` network.
- **File ownership:** files written through the panel (file manager, uploads, archive extraction, API) are handed to
  the site's UID shortly afterwards, so PHP can always read and write them. *Settings → Repair file permissions*
  does it on demand.
- **Cron:** a scheduler in the panel runs due jobs with `docker exec` as the site user inside the site's PHP container
  (15 minute timeout, no overlapping runs, last status/output kept).
- **Backups:** the regular *App files* backup of a PHP site also contains a `.nextdeploy-db/` folder with a dump of every
  site database (dumped before archiving, removed afterwards) and keeps `vendor/`. Restoring extracts the files, reloads
  the dumps, hands ownership back and restarts the pool. The database server must be running for both.
- **Quotas** are enforced in one place (`internal/quota`): the app limit is checked inside a write transaction, so
  concurrent requests cannot exceed it; memory limits for `memory_limit` may not exceed the owner's quota.
- **Suspended users** go offline (holders removed, runtimes stopped) and come back when reactivated. Deleting a user
  hands their PHP sites to the admin who deletes them. Transferring ownership moves the runtime to the new owner.

## Code map

| Area | Location |
|---|---|
| Docker orchestration, pools, MariaDB, cron, WordPress | `internal/phphosting/` |
| Caddy labels for PHP/static sites | `internal/caddy/php_labels.go` |
| Schema and queries | `internal/db/store_php.go` |
| Quota service | `internal/quota/` |
| HTTP handlers | `internal/handlers/php_*.go` (routes in `main.go`) |
| PHP image sources | `internal/phphosting/docker/` |
| UI | `src/features/php/`, wiring in `src/App.tsx` and `src/features/workspace/AppWorkspace.tsx` |

An end-to-end backup/restore test lives behind the `e2e` build tag (`internal/handlers/php_e2e_test.go`).

## Known limits

- Static and PHP sites are served by Caddy; Nginx/Apache rewrite rules are not translated. Use the *Root directory*
  setting and framework front controllers (`try_files` fallback to `index.php` is built in).
- No SFTP/SSH daemon, Varnish cache, ImageMagick (`imagick`) or `git` in the PHP image — they add 60+ MB per version.
  Use the file manager, the web terminal and GD instead.
- Per-user bandwidth shaping (QoS) maps containers to users through their Compose project, so it does not apply to PHP sites.
- Database size is not counted in the user's storage quota, only files are.
- Only `app` (files + databases) backups are supported for PHP sites; volume/full backups do not apply.
- A preview `*.sslip.io` address embeds the server IP at creation time; add a real domain for anything permanent.

## phpMyAdmin (single sign-on)

One shared container `nd-phpmyadmin` from the official Docker Hub image `phpmyadmin:5-apache`, enabled by an administrator in **PHP Hosting → Instance**. It publishes no port. It lives on a private network `nd-pma` (shared only with the panel) plus the database network.

- **No login screen.** The Databases tab of a site has an *phpMyAdmin* button. The panel creates a temporary MariaDB user (`ndpma_…`, max 10 connections, valid 4 hours, dropped on expiry/restart) and returns an `nd_pma` cookie. All `/phpmyadmin/*` requests go through the panel, which requires the NextDeploy session, checks the cookie was issued to that same user, and injects the temporary login as HTTP auth. The panel's own cookies are never forwarded.
- **Scoping.** A site's phpMyAdmin is granted only that site's databases, so other users' and other sites' databases do not exist for it. The administrator's *Open (all databases)* grants everything.
- phpMyAdmin is configured with `auth_type=http`, `AllowArbitraryServer=false`, served under `/pma` via an Apache `Alias`; both are applied by a boot script at container start, so the stock image is used unmodified.

## Where the PHP images come from

PHP runtimes are built `FROM php:<version>-fpm-alpine` (official, pulled from Docker Hub). The stock image has no `mysqli`, `pdo_mysql`, `gd`, `intl` or `zip`, which WordPress and most apps need, so a thin layer adds those extensions (plus wp-cli, composer, mariadb-client). Nothing is fetched from anywhere except Docker Hub, Alpine and PECL/GitHub release downloads used by `install-php-extensions`.
