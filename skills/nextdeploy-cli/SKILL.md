---
name: nextdeploy-cli
description: Operational reference and automation instructions for AI coding assistants using the NextDeploy CLI (nd) to manage application lifecycles, incremental code syncs (nd push), deployments, logs, interactive TTY shells, PHP/WordPress sites, MariaDB databases, Docker volumes, backups, Caddy proxy routes, edge shields, and environment variables.
version: 2.5.0
---

# NextDeploy CLI (`nd`) Skill

This skill teaches AI coding assistants (Claude Code, Cursor, Windsurf, Antigravity, Claude Desktop) how to inspect, synchronize, and control remote applications and panel infrastructure via the official NextDeploy CLI (`nd`).

---

## 1. Authentication & Multi-Account Management

In agent and headless environments, configure connection credentials directly via environment variables:

```bash
export ND_SERVER_URL="https://panel.yourdomain.com"
export ND_TOKEN="nd_live_xxxxxxxxxxxxxxxxxxxxxxxxxxxx"
# Optionally target a specific saved account profile:
export ND_ACCOUNT="production"
```

Or log in with multi-account profile support:
```bash
# Log in with automatic account naming (username@host)
nd login https://panel.yourdomain.com

# Log in with a custom account alias
nd login https://panel.yourdomain.com --name prod

# Manage saved accounts
nd account list                     # List all saved accounts and active status
nd account switch staging           # Switch active account
nd account rename staging stage-v2  # Rename an account alias
nd account delete old-acc           # Delete an account (auto-switches if active)
nd account current                  # Show active account profile
nd whoami                           # View active user, server, and permissions
nd logout [name] [--all]            # Logout active or specified account (--all clears all)
```

Run commands against a specific account without switching active context:
```bash
nd apps --account staging
nd status -A prod
```

---

## 2. Project Linking (`nd link`)

To avoid repeating `[app_id]` across calls, link the current workspace folder once:

```bash
nd link <app_id>      # Writes .nd/project.json
nd whoami             # Confirms current linked target
nd unlink             # Clears local link binding
```

When linked, all commands (`nd push`, `nd pull`, `nd diff`, `nd deploy`, `nd stop`, `nd logs`, `nd shell`, `nd exec`, `nd down`, etc.) automatically target the linked application.

---

## 3. Command Reference

| Command | Syntax | Purpose |
| :--- | :--- | :--- |
| **push** | `nd push [app_id] [path] [--deploy] [--prune] [-y]` | Differential sync (SHA256 delta) of workspace files or push single file (pass `--deploy` to auto-deploy; auto-targets `htdocs/` for PHP sites) |
| **git** | `nd git [status\|off\|on] [app_id] [--force]` | Choose Git or workspace files as deploy source without deleting repo settings |
| **pull** | `nd pull [app_id] [target_dir]` | Download remote workspace files to local directory (safeguards local `.env` and `.nd/`) |
| **diff** | `nd diff [app_id] [path]` | Compare local workspace or file hashes against remote workspace before syncing |
| **deploy** | `nd deploy [app_id] [--rebuild] [-f/--force]` | Trigger remote deployment (`-r`/`--rebuild` for full rebuild, `-f`/`--force` to force Git pull; alias: `nd redeploy`) |
| **logs** | `nd logs [app_id] [-s service] [-n lines] [-f] [-e]` | Live container logs (`-f` follows real-time WebSocket stream; `-e`/`--error` streams PHP error log) |
| **logs (deploy)** | `nd logs [app_id] --deploy [-f]` | Stream build & deployment output in real time |
| **status** | `nd status [app_id]` | Application health, uptime, VPS IP, direct access port URLs, and configuration |
| **info** | `nd info [app_id]` | Comprehensive app inspect view (VPS IP, domains, direct access URLs, container state, health) |
| **ps** | `nd ps [app_id]` | List containers and microservices with state, image, and exposed port mappings |
| **containers** | `nd containers [app_id] [-a]` | List containers for an app, or VPS host containers if omitted |
| **stop** | `nd stop [app_id] [service]` | Stop container stack, or stop a specific service container |
| **restart** | `nd restart [app_id] [service] [--recreate]` | Restart container stack or container (`--recreate` forces container rebuild without rebuilding image) |
| **down** | `nd down [app_id]` | Stop and remove application container stack |
| **exec** | `nd exec [flags] [app_id] <cmd...>` | Execute command inside container with safe quoting, stdin piping (`-i`), interactive TTY (`-it`), and custom timeout (`-t`) |
| **shell** | `nd shell [app_id] [-c container]` | Open interactive bash/sh TTY terminal into container or PHP runtime with raw mode & resize |
| **server-shell**| `nd server-shell` | Open interactive admin root terminal directly on host VPS |
| **server-exec** | `nd server-exec <cmd...>` | Execute command directly on host VPS (requires `allow_server_exec`) |
| **apps** | `nd apps` | List all provisioned applications on the connected server |
| **create** | `nd create <name>` | Provision a new application on the server and link locally |
| **delete** | `nd delete [app_id]` | Delete an application from the server |
| **open** | `nd open [app_id]` | Open app domain or panel URL in default browser |
| **domain list** | `nd domain list [app_id] [--json]` | List custom domains mapped to an application (alias: `nd domains`) |
| **domain add** | `nd domain add [app_id] <domain> [--service svc] [--port p] [--no-https] [--www]` | Add and route custom domain to app (supports custom service/port, Caddy auto-TLS, and optional www subdomain) |
| **domain rm** | `nd domain rm [app_id] <domain\|id>` | Remove custom domain mapping and reload Caddy reverse proxy (alias: `nd domain delete`) |
| **php list** | `nd php list` | List all PHP and WordPress sites |
| **php create** | `nd php create <name> <domain> [flags]` | Create PHP/WordPress site (`--php 8.3`, `--type wordpress`, `--db`) |
| **php info** | `nd php info [app_id]` | Show PHP runtime, memory limits, docroot, and pool status |
| **php set** | `nd php set [app_id] [flags]` | Update PHP version, memory limits, and upload limits |
| **php restart**| `nd php restart [app_id]` | Restart PHP-FPM runtime container |
| **php fix-perms**| `nd php fix-perms [app_id]` | Fix file ownership and permissions for web server |
| **php pma** | `nd php pma [app_id]` | Generate one-click SSO auto-login URL for phpMyAdmin |
| **php db** | `nd php db (list\|create\|user\|grant\|export\|import)` | Manage MariaDB databases, users, and SQL dumps |
| **php cron** | `nd php cron (list\|add\|rm\|run)` | Manage scheduled cron jobs for PHP sites |
| **volume list**| `nd volume list [app_id]` | List persistent Docker volumes and disk sizes |
| **volume browse**| `nd volume browse <vol> [path]` | Browse files and directories inside a Docker volume |
| **volume download**| `nd volume download <vol> <path>` | Stream volume backup archive to local machine |
| **volume restore**| `nd volume restore <vol> <tar.gz>` | Restore volume contents from local tar.gz backup |
| **volume rm** | `nd volume rm <vol>` | Remove a Docker volume |
| **volume prune**| `nd volume prune` | Prune unused Docker volumes |
| **backup list**| `nd backup list [app_id]` | List backup snapshots and statuses |
| **backup create**| `nd backup create [app_id]` | Trigger immediate manual snapshot backup |
| **backup log** | `nd backup log <id>` | View backup execution log |
| **backup restore**| `nd backup restore <id>` | Restore application or system from backup snapshot |
| **backup schedule**| `nd backup schedule (list\|add\|rm)` | Manage automated backup schedules |
| **backup dest**| `nd backup dest list` | Manage remote S3/storage backup destinations |
| **proxy list** | `nd proxy list` | List custom reverse proxy upstream routes |
| **proxy add** | `nd proxy add <domain> <upstream> [flags]` | Add custom reverse proxy upstream route |
| **proxy rm** | `nd proxy rm <id>` | Remove custom proxy route |
| **proxy reload**| `nd proxy reload` | Reload Caddy reverse proxy without downtime |
| **proxy logs** | `nd proxy logs [-f]` | View live Caddy reverse proxy logs |
| **shield list**| `nd shield list` | List connected Edge Shield proxy nodes |
| **shield test**| `nd shield test <node_id>` | Test Edge Shield node connectivity and latency |
| **shield setup-script**| `nd shield setup-script` | Output setup command for edge VPS provisioning |
| **shield reassign**| `nd shield reassign <app> <node>` | Reassign app traffic through shield node |
| **top / stats**| `nd top` / `nd stats` | Live system resource monitor (CPU, RAM, disk, containers) |
| **clean** | `nd clean [--all]` | Prune Docker build cache, dangling images, and temporary files |
| **audit** | `nd audit` | Audit panel security, active sessions, and permissions |
| **images** | `nd images` | List Docker images present on the host VPS |
| **env list**| `nd env list [app_id]` | List configured environment variable keys |
| **env set** | `nd env set [app_id] K=V...` | Add or update environment variables |
| **files** | `nd files [path] [-r] [--json]` | List workspace files & folders (alias: `nd file list`, `nd ls`) |
| **file read**| `nd file read <path> [--full] [-o file]` | Read or download remote file content (alias: `nd cat`) |
| **file write**| `nd file write <path> [content\|file]` | Write/upload file to workspace (supports stdin, local file, `--from`) |
| **file edit**| `nd file edit <path>` | Interactively edit remote file in `$EDITOR` (alias: `nd edit`) |
| **file rm** | `nd file rm <path> [-r] [-f]` | Delete remote file or folder (alias: `nd rm`) |
| **folder rm**| `nd folder rm <path> [-f]` | Delete remote folder and all contents (alias: `nd folder delete`) |
| **whoami** | `nd whoami [--json]` | Inspect active account, server URL, device ID, and permissions |
| **account list**| `nd account list [--json]` | List all saved accounts and active profile (alias: `nd account`) |
| **account switch**| `nd account switch <name>` | Switch active account (alias: `nd account use`) |
| **account rename**| `nd account rename <old> <new>` | Rename account alias |
| **account delete**| `nd account delete <name> [-f]` | Delete an account (alias: `nd account rm`) |
| **account current**| `nd account current [--json]` | Display active account profile |
| **logout** | `nd logout [name] [--all]` | Log out of active or specified account (`--all` clears all) |
| **update** | `nd update` | Self-update `nd` CLI binary to the latest GitHub release |
| **version** | `nd version` | Print `nd` CLI version, OS, and architecture |
| **completion** | `nd completion [bash\|zsh\|ps1]` | Generate shell autocompletion script |

### Global Flags

| Flag | Purpose |
| :--- | :--- |
| `-j, --json` | Output structured JSON (supported on `apps`, `ps`, `info`, `domains`, `env`, `files`, `whoami`, `account`) |
| `-A, --account <name>` | Target specific saved account profile (overrides active account) |
| `-a, --app <app_id>` | Target application ID (overrides linked workspace) |
| `-s, --service <svc>` | Target specific Compose service (for `exec`, `logs`, `stop`, `restart`) |
| `-c, --container <name>` | Target specific container name (for `exec`, `shell`) |
| `-w, --workdir <dir>` | Working directory inside container (for `exec`) |
| `-t, --timeout <secs>` | Execution timeout in seconds (default: 300s, max: 3600s) |
| `-i, --stdin` | Forward stdin into container process |
| `-it, --interactive` | Run interactive TTY session (for `exec`) |
| `-e, --error` | Stream PHP error log (for `logs`) |
| `-f, --force` | Force Git pull on `nd deploy` (discards local edits); force Git sync ON on `nd git on` |
| `-r, --rebuild` | Force Docker image pull and rebuild on `nd deploy` |
| `--recreate` | Force recreate containers on `nd restart` (does not rebuild image; use `nd deploy` for that) |
| `--server` | Execute command directly on host VPS instead of container |

---

## 4. Interactive TTY Shells (`nd shell`, `nd server-shell`)

`nd` provides true interactive PTY terminal access using `golang.org/x/term` with terminal raw mode and `SIGWINCH` window resizing:

```bash
# Open interactive bash/sh inside the linked application or PHP runtime
nd shell

# Target a specific compose service or container
nd shell -c worker
nd shell my-api -s web

# Execute an interactive tool inside the container (e.g. Laravel Tinker, Django shell, MySQL)
nd exec -it php artisan tinker
nd exec -it python manage.py shell
nd exec -it mysql -u root -p

# Open direct root terminal on host VPS (requires allow_server_exec / admin role)
nd server-shell
```

---

## 5. PHP & WordPress Hosting Management (`nd php`)

Manage PHP-FPM sites, isolated MariaDB databases, and background cron schedules directly from the CLI:

### 5.1 Site Lifecycle
```bash
# List all PHP sites with PHP version, docroot, and health
nd php list

# Create a new PHP or WordPress site with optional database
nd php create my-blog example.com --type wordpress --php 8.3 --db
nd php create my-api api.example.com --type php --php 8.4

# Inspect PHP site runtime, extensions, and limits
nd php info my-blog

# Adjust PHP runtime configuration
nd php set my-blog --memory-limit 512M --upload-max 64M --version 8.4

# Restart PHP-FPM runtime container
nd php restart my-blog

# Fix permissions in htdocs/ according to site UID
nd php fix-perms my-blog
```

### 5.2 MariaDB Databases & Users
```bash
# List databases for a site
nd php db list my-blog

# Create database and grant access
nd php db create my-blog blog_db
nd php db user my-blog blog_user "StrongPassword123"
nd php db grant my-blog blog_user blog_db

# Stream SQL dump export to local file
nd php db export my-blog 1 -o backup.sql
nd php db export my-blog 1 -o backup.sql.gz --gzip

# Import local SQL dump into database
nd php db import my-blog 1 backup.sql
nd php db import my-blog 1 backup.sql.gz

# Launch phpMyAdmin with one-click SSO token in default browser
nd php pma my-blog
```

### 5.3 Scheduled Cron Jobs
```bash
# List cron jobs for a PHP site
nd php cron list my-blog

# Add a scheduled job (e.g. Laravel scheduler)
nd php cron add my-blog --schedule "*/5 * * * *" --command "php artisan schedule:run"

# Trigger a cron job immediately for testing
nd php cron run my-blog 1

# Remove a cron job
nd php cron rm my-blog 1
```

---

## 6. Docker Volume Management (`nd volume`)

Inspect and safeguard persistent container storage:

```bash
# List Docker volumes with sizes and application labels
nd volume list

# Browse files inside a volume without attaching containers
nd volume browse my-app_data
nd volume browse my-app_data /var/log

# Stream volume contents directly to a local tarball (zero memory overhead)
nd volume download my-app_data volume_backup.tar.gz

# Restore volume contents from a local tar.gz archive
nd volume restore my-app_data volume_backup.tar.gz

# Remove or prune volumes
nd volume rm my-app_data
nd volume prune
```

---

## 7. Backups & Disaster Recovery (`nd backup`)

```bash
# List backup history and snapshots for an application
nd backup list
nd backup list my-app

# Trigger an immediate manual backup snapshot
nd backup create
nd backup create my-app

# View backup run logs
nd backup log 42
nd backup log my-app 42

# Restore application state from a backup snapshot
nd backup restore 42

# Manage automated backup schedules
nd backup schedule list
nd backup schedule add --cron "0 2 * * *" --dest 1
nd backup schedule rm 5

# View connected remote S3 / Cloud storage destinations
nd backup dest list
```

---

## 8. Caddy Reverse Proxy & Edge Shield (`nd proxy`, `nd shield`)

```bash
# List custom reverse proxy upstream routes
nd proxy list

# Route custom domain to an upstream server or internal port
nd proxy add api.example.com http://127.0.0.1:4000 --https
nd proxy rm 10

# Zero-downtime Caddy configuration reload
nd proxy reload

# Stream live Caddy access and error logs
nd proxy logs -f

# Edge Shield nodes: test latency and manage cluster
nd shield list
nd shield test 1
nd shield setup-script
nd shield reassign my-app 1
```

---

## 9. Live Monitoring & Maintenance (`nd top`, `nd clean`, `nd audit`)

```bash
# Live interactive dashboard showing CPU, RAM, disk, and container processes
nd top
nd stats

# Prune unused Docker build caches, dangling images, and stopped containers
nd clean
nd clean --all

# View security audit logs and active session events
nd audit
```

---

## 10. Differential Sync Engine (`nd push`)

`nd push` is the primary workhorse for zero-token, fast code deployment:

1. **Local Hash Cache**:
   - Stores mtime and SHA256 hashes in `.nd/hash_cache.json`.
   - On repeat pushes, files whose size and modification time haven't changed skip SHA256 re-computation, providing a 2–3x speedup on large workspaces.
2. **PHP Site Detection**:
   - Automatically synchronizes files to `htdocs/` when targeting a PHP/WordPress site.
3. **Single-File Push**:
   - Push individual modified files without scanning the whole repository:
     ```bash
     nd push main.py
     nd push src/app.js --deploy
     ```
4. **Local Scan & Ignore**:
   - Recursively scans directory while ignoring build artifacts (`.git`, `.nd`, `.claude`, `node_modules`, `dist`, `build`, `__pycache__`, `.venv`, `vendor`, `.next`), plus local `.gitignore` / `.ndignore` rules. Files named `.env` or `.env.*` are never uploaded; use `nd env set` or `nd file write .env --from <file>`.
5. **Remote Manifest**:
   - Fetches cached SHA256 snapshot via `GET /api/v1/apps/<app_id>/manifest?hash=true` (<50ms remote scan latency).
6. **Delta Computation**:
   - Computes exact diff using short-SHA hashes:
     - **Upload**: New or locally modified files.
     - **Delete**: Files removed locally that still exist remotely (when `--prune` is passed).
7. **Git-connected apps are blocked**:
   - If the app deploys from a Git repository, `nd push` stops with a hint instead of uploading. Run `nd git off` first.
8. **Streaming In-Memory Archive**:
   - Compresses delta into an in-memory `.tar.gz`, uploads to `POST /api/v1/apps/<app_id>/workspace/archive`, validates safe paths (anti-traversal), and extracts directly into app workspace.

---

## 11. Recommended AI Agent Workflow

When an AI coding assistant (Antigravity, Claude Code, Cursor, Windsurf) is deploying or modifying an application via `nd`, execute tasks in this structured sequence:

### Step 1: Verify Connection & Project Link
```bash
# Confirm active connection and linked application
nd whoami --json

# If not linked to the workspace, link once:
nd link <app_id>
```

### Step 2: Check Git Sync State
Before attempting file uploads or local edits, check if Git sync is controlling deploys:
```bash
nd git status
# If Git sync is ON and you are pushing local changes directly:
nd git off
```

### Step 3: Inspect Differences Before Syncing
Always run `nd diff` to preview what files will be changed or uploaded:
```bash
nd diff
```

### Step 4: Differential Sync & Deploy
Push modified files and trigger container deployment:
```bash
# Push modified files and deploy in a single step
nd push --deploy

# Or push with server-side cleanup of deleted files
nd push --deploy --prune -y
```

### Step 5: Follow Deployment Build Logs
Never assume deploy completed silently — monitor the live build:
```bash
nd logs --deploy -f
```

### Step 6: Verify Service Health & Endpoints
```bash
# Check container status and URLs
nd status
nd ps --json
```

### Step 7: Post-Deploy Tasks (Migrations, Seed, Tests)
```bash
# Execute within container using target service or interactive shell
nd exec -s web python manage.py migrate
nd exec -s web npm run build
nd shell
```
