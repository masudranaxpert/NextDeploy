---
name: nextdeploy-cli
description: Comprehensive operational guide and CLI reference for AI coding agents and developers using the NextDeploy CLI (nd). Covers incremental workspace synchronization (nd push), short-SHA differential hashing, project linking (nd link), zero-token code deployments, non-interactive CI/CD automation, device session heartbeats, and remote application lifecycle management.
version: 1.0.0
---

# NextDeploy CLI (`nd`) Skill

This skill teaches AI coding assistants (Claude Code, Cursor, Windsurf, Antigravity, Claude Desktop) and autonomous developers how to inspect, synchronize, and deploy applications using the official NextDeploy command-line interface (`nd`).

---

## 1. Architecture & The Incremental Sync Loop

Traditional remote development workflows suffer from severe bottlenecks:
- **Git Push Overhead**: Requires committing temporary debugging changes (`git commit -m "fix"`) to the repository branch, cluttering history.
- **Whole-Repo Re-upload**: Tools like `railway up` package and upload the entire repository (often 20MB–100MB) on every iteration.
- **LLM Context Exhaustion**: Editing remote files over raw Model Context Protocol (`file_write` / `file_patch`) consumes thousands of input/output tokens per file and risks truncation or sync errors.

NextDeploy CLI (`nd`) solves this by moving file discovery and differencing entirely to local disk execution:

```
┌─────────────────────────────────────────────────────────────┐
│                 Local Machine / AI Agent                    │
│      (Fast local disk edits, zero token consumption)        │
└──────────────┬──────────────────────────────▲───────────────┘
               │ 1. nd push <app_id>          │
               │    - Walk local directory    │ 2. GET /api/v1/apps/:id/manifest?hash=true
               │    - Compute local SHA256    │    (Server returns cached file hashes)
               ▼                              │
┌─────────────────────────────────────────────┴───────────────┐
│                    NextDeploy Panel (VPS)                   │
│                                                             │
│   3. Compute Delta (Local vs Remote SHA256)                 │
│      → Only modified/added files are compressed to tar.gz   │
│   4. Upload Archive (POST /api/v1/apps/:id/workspace/archive)│
│      → Panel extracts files directly into app workspace     │
│   5. Delete Removed Files (via file_delete)                 │
│   6. Trigger Zero-Downtime Deployment & Stream Build Logs   │
└─────────────────────────────────────────────────────────────┘
```

### Why AI Agents Should Prefer `nd push`:
1. **Zero LLM Token Consumption**: The AI agent never reads or writes file content through prompt tokens. It executes `nd push` via bash, saving up to 50,000 tokens on multi-file refactors.
2. **Lightning-Fast Iteration**: Only modified files are transmitted. A 2-line code fix takes less than 1 second to upload and extract.
3. **Safe In-Memory Extraction**: Uploaded tarballs are checked for path traversal (`../`) attacks and extracted directly under the container workspace user ID.

---

## 2. Installation & Quick Start

NextDeploy distributes standalone, zero-dependency Go binaries for Linux, macOS, and Windows.

### Linux / macOS One-Liner
```bash
curl -fsSL https://raw.githubusercontent.com/masudranaxpert/NextDeploy/main/install-nd.sh | sh
```

### Windows (PowerShell)
```powershell
irm https://raw.githubusercontent.com/masudranaxpert/NextDeploy/main/install-nd.ps1 | iex
```

### Manual Binary Download
Pre-built binaries are available from the GitHub Releases page:
- `nd-linux-amd64` / `nd-linux-arm64`
- `nd-darwin-amd64` / `nd-darwin-arm64`
- `nd-windows-amd64.exe`

---

## 3. Authentication & Configuration

The CLI authenticates using personal API tokens generated from the NextDeploy Web UI (**Settings → API Tokens**).

### Interactive Login
```bash
nd login https://panel.yourdomain.com
# Enter API token when prompted (token is saved to ~/.nd/config.json with 0600 permissions)
```

### Verification
Check current connection status, authenticated server, and device info:
```bash
nd whoami
```

### Non-Interactive & CI/CD Automation
For headless environments (GitHub Actions, GitLab CI, Docker, or AI Subagents), configure credentials via environment variables without running `nd login`:

```bash
export ND_SERVER_URL="https://panel.yourdomain.com"
export ND_TOKEN="nd_live_xxxxxxxxxxxxxxxxxxxxxxxxxxxx"
```

The CLI automatically reads `ND_SERVER_URL` and `ND_TOKEN` (or `NEXTDEPLOY_URL` / `NEXTDEPLOY_TOKEN`) as first-class fallbacks.

### Logout
Removes saved credentials from `~/.nd/config.json` and cleanly notifies the server to terminate the session:
```bash
nd logout
```

---

## 4. Project Linking (`nd link`)

Instead of repeatedly passing `<app_id>` in every command, link your local working directory to a remote application once:

```bash
# Link current folder to an app
nd link my-web-app

# Check link status
nd whoami

# Unlink current folder
nd unlink
```

When linked, `.nd/project.json` is created in the repository root:
```json
{
  "app_id": "my-web-app"
}
```

Once linked, all commands (`nd push`, `nd logs`, `nd status`, `nd deploy`, `nd restart`, `nd env`) automatically target the linked application without requiring manual arguments.

---

## 5. Command Reference

| Command | Usage | Description |
| :--- | :--- | :--- |
| `login` | `nd login <url>` | Authenticate and register device session in `~/.nd/config.json` |
| `logout` | `nd logout` | Disconnect device session and remove credentials |
| `whoami` | `nd whoami` | Show active server, device ID, token preview, and linked app |
| `apps` | `nd apps` | List all applications accessible by the current token |
| `create` | `nd create <name>` | Provision a new application and link current directory |
| `delete` | `nd delete [app_id]` | Delete an application (requires confirmation) |
| `info` | `nd info [app_id]` | View app details, domains, container states, and health checks |
| `open` | `nd open [app_id]` | Open app domain or panel URL in default browser |
| `ps` | `nd ps [app_id]` | Show container services, states, status, and Docker images |
| `containers` | `nd containers [-a]` | List all Docker containers on the host VPS |
| `images` | `nd images` | List all Docker images on the host VPS |
| `link` | `nd link [app_id]` | Bind local repository to an application |
| `unlink` | `nd unlink` | Unbind local repository |
| `push` | `nd push [app_id] [dir]` | Incrementally diff local files, upload archive, and deploy |
| `status` | `nd status [app_id]` | View container health, status, ports, and metadata (alias: `nd info`) |
| `deploy` | `nd deploy [app_id]` | Trigger remote container redeployment without file sync |
| `stop` | `nd stop [app_id]` | Stop container stack |
| `restart` | `nd restart [app_id]` | Restart container stack |
| `logs` | `nd logs [app_id] [-n lines]` | Stream container stdout/stderr (default: 100 lines) |
| `exec` / `run` | `nd exec [app_id] <cmd...>` | Heroku-style shell execution inside application container |
| `server-exec` | `nd server-exec <cmd...>` | Run arbitrary shell command on host VPS (requires `allow_server_exec`) |
| `env list`| `nd env list [app_id]` | Display configured environment variable keys |
| `env set` | `nd env set [app_id] K=V` | Set or update one or multiple environment variables |
| `version` | `nd version` | Print CLI version, target OS, and architecture |
| `help` | `nd help` | Display command usage and examples |

---

## 6. The Differential Sync Engine (`nd push`) Deep Dive

`nd push` is the primary workhorse for code deployment. Here is exactly how the synchronization protocol operates:

### 1. Local Filesystem Scan
The CLI walks the local directory recursively. It ignores noisy artifacts by default:
- Hardcoded ignores: `.git`, `node_modules`, `dist`, `build`, `__pycache__`, `.venv`, `vendor`, `.next`, and any hidden directories starting with `.` (except `.env`).
- Rules defined in local `.gitignore` and `.ndignore` files are automatically parsed and respected.

### 2. Remote Manifest Request
The CLI executes `GET /api/v1/apps/<app_id>/manifest?hash=true` to fetch the remote workspace snapshot.
- The NextDeploy server maintains an in-memory SHA256 cache keyed by `(appID, path, size, modNano)`.
- Re-reading unchanged files from server disk is avoided, reducing remote scan latency to <50ms.

### 3. Delta Computation
The client compares local short-SHA hashes against the server manifest:
- **Files to Upload**: Files present locally whose hashes differ or do not exist on the server.
- **Files to Delete**: Files that exist on the server but have been deleted locally.

### 4. Tarball Compression & Multipart Upload
- Changed files are bundled into an in-memory `.tar.gz` stream.
- Uploaded to `POST /api/v1/apps/<app_id>/workspace/archive`.
- The server validates all paths against directory traversal (`../`), cleans up temporary files, extracts the content, and returns the number of extracted files.

### 5. Automated Deployment
Once the files are extracted, `nd push` automatically initiates deployment and reports container readiness.

---

## 7. Device Sessions & Live Heartbeats

To provide complete visibility into connected developer machines and AI agents, `nd` features active session tracking:

1. **Persistent Device Identity**: Every client machine maintains a persistent unique Device ID (`nd_<hostname>_<uuid>`). When you run `nd login`, `nd whoami`, or any command, the device registers with hostname, OS, CPU architecture, and CLI version.
2. **Heartbeat & Activity**: Every command updates the device's Last Seen timestamp. In the CLI Sessions dashboard (`/cli-sessions`), devices active within 5 minutes display a live green indicator, while previously connected machines remain visible for up to 7 days until explicitly disconnected.
3. **Explicit Revocation / Logout**: Running `nd logout` unregisters the device, or an admin can revoke it directly from the web panel (**Developer & AI → CLI Sessions**).

---

## 8. AI Agent Decision Matrix: MCP vs. CLI

When pairing with NextDeploy, AI agents should use the right tool for each specific job:

| Scenario | Recommended Tool | Why |
| :--- | :--- | :--- |
| **Editing code files** | Local IDE edits + `nd push` | 100x faster, zero prompt tokens wasted on file payloads. |
| **Reading small remote config** | MCP `file_read` | Direct in-memory tool call without leaving the agent loop. |
| **Listing remote applications** | `nd apps` or MCP `app_list` | Both provide structured application lists. |
| **Running ad-hoc container commands** | MCP `container_exec` | Direct terminal execution inside the running Docker container. |
| **Setting environment variables** | `nd env set KEY=VALUE` | Clean, ergonomic CLI syntax supporting multiple key-value pairs. |
| **Monitoring long deployments** | MCP `deploy_status` or `nd logs`| Instant visibility into build output and container errors. |

---

## 9. Common Troubleshooting

### "Not logged in"
Run `nd login <server_url>` or ensure `ND_SERVER_URL` and `ND_TOKEN` environment variables are exported in your terminal session.

### "app_id required"
Either pass the application ID explicitly (`nd push my-app`) or run `nd link my-app` in the repository root.

### "directory not found"
Ensure the local path exists and you have read permissions to the files being synchronized.

### "401 Unauthorized"
Your API token may have expired or been deleted in the NextDeploy web panel. Generate a new token in **Settings → API Tokens**.
