---
name: nextdeploy-cli
description: Operational reference and automation instructions for AI coding assistants using the NextDeploy CLI (nd) to manage application lifecycles, incremental code syncs (nd push), deployments, logs, containers, and environment variables.
version: 1.2.0
---

# NextDeploy CLI (`nd`) Skill

This skill teaches AI coding assistants (Claude Code, Cursor, Windsurf, Antigravity, Claude Desktop) how to inspect, synchronize, and control remote applications via the official NextDeploy CLI (`nd`).

---

## 1. Authentication & Environment

In agent and headless environments, configure connection credentials directly via environment variables:

```bash
export ND_SERVER_URL="https://panel.yourdomain.com"
export ND_TOKEN="nd_live_xxxxxxxxxxxxxxxxxxxxxxxxxxxx"
```

Or interactively log in:
```bash
nd login https://panel.yourdomain.com
nd whoami
```

---

## 2. Project Linking (`nd link`)

To avoid repeating `[app_id]` across calls, link the current workspace folder once:

```bash
nd link <app_id>      # Writes .nd/project.json
nd whoami             # Confirms current linked target
nd unlink             # Clears local link binding
```

When linked, all commands (`nd push`, `nd pull`, `nd diff`, `nd deploy`, `nd stop`, `nd logs`, `nd down`, etc.) automatically target the linked application.

---

## 3. Command Reference

| Command | Syntax | Purpose |
| :--- | :--- | :--- |
| **push** | `nd push [app_id] [path] [--deploy] [--prune] [-y]` | Differential sync (SHA256 delta) of workspace files or push single file (pass `--deploy` to auto-deploy) |
| **pull** | `nd pull [app_id] [target_dir]` | Download remote workspace files to local directory (safeguards local `.env` and `.nd/`) |
| **diff** | `nd diff [app_id] [path]` | Compare local workspace or file hashes against remote workspace before syncing |
| **deploy** | `nd deploy [app_id] [--rebuild]` | Trigger remote container deployment (pass `-r`/`--rebuild` for full rebuild; alias: `nd redeploy`) |
| **logs** | `nd logs [app_id] [-s service] [-n lines] [-f]` | Tail container runtime stdout/stderr (default: 50 lines; supports `-s` service filter) |
| **logs (deploy)** | `nd logs [app_id] --deploy [-f]` | Stream build & deployment output in real time |
| **status** | `nd status [app_id]` | Application health, uptime, exposed ports, and configuration |
| **info** | `nd info [app_id]` | Comprehensive app inspect view (domains, container state, health) |
| **ps** | `nd ps [app_id]` | List containers and microservices with state, image, and exposed port mappings |
| **containers** | `nd containers [app_id] [-a]` | List containers for an app, or VPS host containers if omitted |
| **stop** | `nd stop [app_id] [service]` | Stop container stack, or stop a specific service container |
| **restart** | `nd restart [app_id] [service] [--recreate]` | Restart container stack or service container (pass `--recreate` to force rebuild containers) |
| **down** | `nd down [app_id]` | Stop and remove application container stack |
| **exec** | `nd exec [flags] [app_id] <cmd...>` | Execute command inside container with safe quoting, stdin piping (`-i`), and custom timeout (`-t`) |
| **apps** | `nd apps` | List all provisioned applications on the connected server |
| **create** | `nd create <name>` | Provision a new application on the server and link locally |
| **delete** | `nd delete [app_id]` | Delete an application from the server |
| **open** | `nd open [app_id]` | Open app domain or panel URL in default browser |
| **images** | `nd images` | List Docker images present on the host VPS |
| **env list**| `nd env list [app_id]` | List configured environment variable keys |
| **env set** | `nd env set [app_id] K=V...` | Add or update environment variables |
| **files** | `nd files [path] [-r] [--json]` | List workspace files & folders (alias: `nd file list`, `nd ls`) |
| **file read**| `nd file read <path> [--full] [-o file]` | Read or download remote file content (alias: `nd cat`) |
| **file write**| `nd file write <path> [content|file]` | Write/upload file to workspace (supports stdin, local file, `--from`) |
| **file edit**| `nd file edit <path>` | Interactively edit remote file in `$EDITOR` (alias: `nd edit`) |
| **file rm** | `nd file rm <path> [-r] [-f]` | Delete remote file or folder (alias: `nd rm`) |
| **folder rm**| `nd folder rm <path> [-f]` | Delete remote folder and all contents (alias: `nd folder delete`) |
| **server-exec** | `nd server-exec <cmd...>` | Execute command on host VPS (requires `allow_server_exec`) |
| **whoami** | `nd whoami` | Inspect active session, server URL, device ID, and linked app |
| **logout** | `nd logout` | Invalidate device session and clear local credentials |

---

## 4. Differential Sync Engine (`nd push`)

`nd push` is the primary workhorse for zero-token, fast code deployment:

1. **Local Hash Cache**:
   - Stores mtime and SHA256 hashes in `.nd/hash_cache.json`.
   - On repeat pushes, files whose size and modification time haven't changed skip SHA256 re-computation, providing a 2–3x speedup on large workspaces.
2. **Single-File Push**:
   - Push individual modified files without scanning the whole repository:
     ```bash
     nd push main.py
     nd push src/app.js --deploy
     ```
3. **Local Scan & Ignore**:
   - Recursively scans directory while ignoring build artifacts (`.git`, `node_modules`, `dist`, `build`, `__pycache__`, `.venv`, `vendor`, `.next`, and hidden folders except `.env`), plus local `.gitignore` / `.ndignore` rules.
4. **Remote Manifest**:
   - Fetches cached SHA256 snapshot via `GET /api/v1/apps/<app_id>/manifest?hash=true` (<50ms remote scan latency).
5. **Delta Computation**:
   - Computes exact diff using short-SHA hashes:
     - **Upload**: New or locally modified files.
     - **Delete**: Files removed locally that still exist remotely (when `--prune` is passed).
6. **Streaming In-Memory Archive**:
   - Compresses delta into an in-memory `.tar.gz`, uploads to `POST /api/v1/apps/<app_id>/workspace/archive`, validates safe paths (anti-traversal), and extracts directly into app workspace.

---

## 5. Previewing Workspace Differences (`nd diff`)

Before running `nd push` or `nd deploy`, compare your local files against the remote server state:

```bash
# Compare entire workspace against remote server
nd diff

# Check diff for a specific file or subfolder
nd diff src/server.py

# Specify remote app target explicitly
nd diff my-app src/
```

Output highlights:
- `+ local only` (will be uploaded)
- `~ modified` (different content hash, will be overwritten)
- `- server only` (exists on server but not locally; deleted if `--prune` is passed)

---

## 6. Pulling Remote Workspaces (`nd pull`)

When collaborating with teammates or bootstrapping on a new machine, pull the server workspace:

```bash
# Pull current linked app workspace into current directory
nd pull

# Pull specific app to a target folder
nd pull my-app ./my-app-backup
```

Safety protections:
- Existing local `.env` and `.env.*` files are preserved and never overwritten without explicit prompt.
- Local `.nd/` project settings are retained.

---

## 7. Advanced Execution & Stdin Pipes (`nd exec`)

`nd exec` provides direct command execution inside container stacks:

### 7.1 Complex Shell Quoting
Parentheses, quotes, and complex one-liners execute properly without remote syntax errors:
```bash
nd exec -s web python -c "import os; print(os.environ.get('PORT'))"
nd exec bash -c 'echo "hello $(whoami)"'
```

### 7.2 Stdin Piping & Scripts
Pipe scripts, queries, or data into container processes:
```bash
# Run a local Python script inside the container
cat script.py | nd exec -s web python -

# Run a local SQL dump into a database container
cat dump.sql | nd exec -s db mysql -u root -psecret app_db

# Pass stdin explicitly using flag
cat payload.json | nd exec --stdin -s worker node process.js
```

### 7.3 Custom Execution Timeouts
Long-running commands (migrations, backups, model downloads) default to 300s, adjustable up to 3600s:
```bash
nd exec -t 600 python manage.py migrate
nd exec --timeout 1800 ./long-backup.sh
```

---

## 8. Container Rebuilds & Force Recreate (`nd restart`)

```bash
# Standard service restart
nd restart web

# Force recreate containers (docker compose up -d --force-recreate)
nd restart --recreate
nd restart web --recreate
```

---

## 9. Remote File & Folder Management (`nd files`, `nd file`, `nd folder`)

AI assistants and developers can inspect, read, write, edit, and delete individual files or entire folders without doing a full `nd push`:

### 9.1 Listing Workspace Files
```bash
# List root workspace files for the linked app
nd files

# List subfolder contents
nd files src/

# List all files recursively
nd files -r

# Target a specific app when unlinked
nd files --app <app_id>
```

### 9.2 Reading Remote Files
```bash
# Print remote file content directly to stdout
nd file read docker-compose.yml
nd cat nginx.conf

# Save / download remote file to a local destination
nd file read remote_config.json -o local_config.json
```

### 9.3 Writing & Updating Remote Files
```bash
# Write directly from command-line argument
nd file write config.json '{"debug": false, "port": 8080}'

# Upload a local file to remote workspace
nd file write src/app.py local_app.py
nd file write src/app.py --from local_app.py

# Pipe content from stdin
cat migration.sql | nd file write db/migration.sql
echo "NEW_SETTING=true" | nd file write .env.example

# Interactively edit remote file in $EDITOR (nano / vim / vi / notepad)
nd edit docker-compose.yml
nd file edit main.go
```

### 9.4 Deleting Files & Folders
```bash
# Delete a remote file
nd file rm obsolete.js
nd rm unused.log

# Delete a remote folder and all nested contents
nd folder rm cache/
nd file rm -r build/

# Skip confirmation prompt (-f / --force / -y)
nd file rm -f junk.txt
nd folder rm -f tmp/
```

