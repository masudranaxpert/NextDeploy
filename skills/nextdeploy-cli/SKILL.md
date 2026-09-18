---
name: nextdeploy-cli
description: Operational reference and automation instructions for AI coding assistants using the NextDeploy CLI (nd) to manage application lifecycles, incremental code syncs (nd push), deployments, logs, containers, and environment variables.
version: 1.1.0
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

When linked, all commands (`nd push`, `nd deploy`, `nd stop`, `nd logs`, `nd down`, etc.) automatically target the linked application.

---

## 3. Command Reference

| Command | Syntax | Purpose |
| :--- | :--- | :--- |
| **push** | `nd push [app_id] [dir] [--deploy]` | Differential sync (SHA256 delta) of workspace files (pass `--deploy` to auto-deploy) |
| **deploy** | `nd deploy [app_id] [--rebuild]` | Trigger remote container deployment (pass `-r`/`--rebuild` for full rebuild) |
| **logs** | `nd logs [app_id] [-n lines] [-f]` | Tail container runtime stdout/stderr (default: 100 lines) |
| **logs (deploy)** | `nd logs [app_id] --deploy [-f]` | Stream build & deployment output in real time |
| **status** | `nd status [app_id]` | Application health, uptime, exposed ports, and configuration |
| **info** | `nd info [app_id]` | Comprehensive app inspect view (domains, container state, health) |
| **ps** | `nd ps [app_id]` | List containers and microservices belonging to an application |
| **containers** | `nd containers [app_id] [-a]` | List containers for an app, or VPS host containers if omitted |
| **stop** | `nd stop [app_id] [service]` | Stop container stack, or stop a specific service container |
| **restart** | `nd restart [app_id] [service]` | Restart container stack, or restart a specific service container |
| **down** | `nd down [app_id]` | Stop and remove application container stack |
| **exec** | `nd exec [app_id] [-s service/-c container] <cmd...>` | Execute command inside container (primary, or specified service/container) |
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

## 4. Operational Best Practices for AI Agents

1. **Fast Local Code Sync & Deploy**:
   - Make edits directly to local files in the workspace.
   - Run `nd push` to synchronize files only, or `nd push --deploy` to sync and automatically deploy. NextDeploy computes short-SHA differential hashes, uploads only delta files, and skips full-repo re-uploading.
2. **Rebuilding Stacks**:
   - To force a container rebuild and image re-pull without modifying files, run `nd deploy [app_id] --rebuild` (or `-r`).
3. **Inspecting Build Failures vs. Runtime Errors**:
   - To inspect container runtime crashes: `nd logs` or `nd logs -n 200`.
   - To inspect build/deployment failures: `nd logs --deploy` (or `nd logs -d -f`).
4. **Service Level Management**:
   - Check containers with `nd containers <app_id>` or `nd ps`.
   - Stop a specific service: `nd stop <app_id> <service>`.
   - Restart a specific service: `nd restart <app_id> <service>`.
   - Tear down and remove containers: `nd down <app_id>`.
5. **Session Heartbeats**:
   - Every CLI execution automatically updates the device heartbeat. AI agents appear in the Web UI under **CLI Sessions** with active status.

---

## 5. Differential Sync Engine (`nd push`)

`nd push` is the primary workhorse for zero-token code deployment:

1. **Local Scan & Ignore**: Recursively scans directory while ignoring build artifacts (`.git`, `node_modules`, `dist`, `build`, `__pycache__`, `.venv`, `vendor`, `.next`, and hidden folders except `.env`), plus local `.gitignore` / `.ndignore` rules.
2. **Remote Manifest**: Fetches cached SHA256 snapshot via `GET /api/v1/apps/<app_id>/manifest?hash=true` (<50ms remote scan latency).
3. **Delta Computation**: Computes exact diff using short-SHA hashes:
   - **Upload**: New or locally modified files.
   - **Delete**: Files removed locally that still exist remotely.
4. **Streaming In-Memory Archive**: Compresses delta into an in-memory `.tar.gz`, uploads to `POST /api/v1/apps/<app_id>/workspace/archive`, validates safe paths (anti-traversal), and extracts directly into app workspace.
5. **Auto-Deploy**: Automatically triggers zero-downtime deployment and streams container readiness.

---

## 6. Remote File & Folder Management (`nd files`, `nd file`, `nd folder`)

AI assistants and developers can directly inspect, read, write, edit, and delete individual files or entire folders without doing a full `nd push`:

### 6.1 Listing Workspace Files
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

### 6.2 Reading Remote Files
```bash
# Print remote file content directly to stdout
nd file read docker-compose.yml
nd cat nginx.conf

# Save / download remote file to a local destination
nd file read remote_config.json -o local_config.json
```

### 6.3 Writing & Updating Remote Files
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

### 6.4 Deleting Files & Folders
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

