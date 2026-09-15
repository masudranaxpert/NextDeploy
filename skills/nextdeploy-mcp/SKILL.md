---
name: nextdeploy-mcp
description: Guide and tool reference for AI coding agents connecting to NextDeploy via Model Context Protocol (MCP). Covers inspecting apps, managing workspace files, editing environment variables, running non-blocking deployments, and polling deploy logs.
version: 1.0.0
---

# NextDeploy MCP (Model Context Protocol) Skill

This skill teaches AI coding assistants (Claude Code, Cursor, Windsurf, Antigravity, Claude Desktop) how to interact with NextDeploy applications through the native Model Context Protocol server.

---

## 1. Architecture & The Closed Loop

Unlike basic SSH bind mounts where an agent can edit files but cannot deploy or read container logs, NextDeploy MCP completes the entire development feedback loop:

```
┌─────────────────────────────────────────────────────────────┐
│                   Local AI Coding Agent                     │
│               (Laptop: Cursor / Claude Code)                │
└──────────────┬──────────────────────────────▲───────────────┘
               │ Tool Calls                   │ JSON-RPC
               ▼                              │ Responses
┌─────────────────────────────────────────────┴───────────────┐
│                    NextDeploy Panel (VPS)                   │
│                                                             │
│   1. Read code & env      →   file_read, env_list          │
│   2. Apply code edits     →   file_write (direct to workspace) │
│   3. Trigger deployment   →   deploy / redeploy (async)     │
│   4. Poll build output    →   deploy_status(job_id)         │
│   5. Read runtime logs    →   container_logs, deploy_tail   │
│   6. Terminal execution   →   container_exec, server_exec   │
└─────────────────────────────────────────────────────────────┘
```

### Advantages over traditional SSH / local container development:
1. **Zero VPS Overhead**: The AI agent runs on the developer's laptop. No need to install Node.js, npm, or Claude Code on a constrained 1GB–4GB VPS.
2. **Permission Safety**: The panel writes all workspace files directly under its managed user ID. No root-owned file issues or host permission conflicts.
3. **Scoped Security**: Access is granted through revocable API tokens (`nd_...`) mapped to user accounts, rather than root SSH keys.
4. **Non-Blocking Deployments**: Builds taking 2–5 minutes never cause HTTP timeouts because the deploy command returns immediately with a `job_id` that the agent polls.

---

## 2. Setup & Connection

NextDeploy exposes two standard MCP transports:
- **Server-Sent Events (SSE)**: `https://<YOUR_PANEL_DOMAIN>/mcp/sse`
- **Streamable HTTP POST**: `https://<YOUR_PANEL_DOMAIN>/mcp`

### Connecting from Cursor
Add to your `~/.cursor/mcp.json` or through **Cursor Settings > Features > MCP > Add New MCP Server**:
```json
{
  "mcpServers": {
    "nextdeploy": {
      "url": "https://panel.example.com/mcp/sse",
      "headers": {
        "Authorization": "Bearer nd_your_token_here"
      }
    }
  }
}
```

### Connecting from Claude Code
Run from your local terminal:
```bash
claude mcp add --transport sse nextdeploy https://panel.example.com/mcp/sse --header "Authorization: Bearer nd_your_token_here"
```

### Connecting from Windsurf
Add to `~/.codeium/windsurf/mcp_config.json`:
```json
{
  "mcpServers": {
    "nextdeploy": {
      "url": "https://panel.example.com/mcp/sse",
      "headers": {
        "Authorization": "Bearer nd_your_token_here"
      }
    }
  }
}
```

---

## 3. Tool Reference

The NextDeploy MCP server provides **23 tools** designed for high accuracy and optimized to stay comfortably below Cursor's 40-tool hard limit:

### Area 1: Application Discovery & Lifecycle
- **`app_list`**: Lists all applications the user can access.
  - Returns: `id`, `name`, `status`, `domains`, and compose `services`.
- **`app_get`**: Detailed metadata, compose services, container status (`ps`), and optional health check.
  - Arguments: `app_id` (string, required), `include_health` (boolean, optional).
- **`app_create`**: Provisions a new application directly via MCP.
  - Arguments: `name` (string, required), `source_type` ("upload" or "git"), `repo_url`, `branch`, `compose_content`.

### Area 2: Workspace File Operations
All file paths are strictly sandboxed inside the app workspace.
- **`workspace_manifest`**: High-performance directory scanner and file manifest with short 16-hex SHA-256 caching and .gitignore filtering. Excludes dependency lock files (`package-lock.json`, `yarn.lock`, etc.) and `*.map` by default to minimize context overhead. Pass `local_files: {path: hash}` for instant server-side differential check (saves 98%+ tokens by returning only `{to_upload, to_delete, in_sync_count}`). Pass `depth: 1, hash: false` for instant directory listing (like `ls`).
  - Arguments: `app_id` (string, required), `path` (string, optional), `depth` (integer, optional), `hash` (boolean, default true), `full_hash` (boolean, default false), `include_locks` (boolean, default false), `local_files` (map of {path: hash}, optional).
- **`file_read`**: Reads single file contents as plain text. Files over 256KB are safely truncated with a notice unless `full: true` is passed to protect context windows.
  - Arguments: `app_id` (string, required), `path` (string, required), `offset`, `limit`, `full` (boolean, default false).
- **`file_write`**: Creates or updates a single file. For modifying multiple files atomically, use `workspace_apply`.
  - Arguments: `app_id` (string, required), `path` (string, required), `content` (string, required).
- **`file_delete`**: Deletes a single file or directory.
  - Arguments: `app_id` (string, required), `path` (string, required).
- **`file_patch`**: Applies a unified diff patch to the workspace via git apply.
  - Arguments: `app_id` (string, required), `patch` (string, required).
- **`file_search`**: Fast line-by-line grep and filename glob searching across the workspace.
  - Arguments: `app_id` (string, required), `query`, `pattern`, `path`, `max_results`.
- **`workspace_apply`**: Atomically executes multiple file writes and/or deletions in a single round-trip.
  - Arguments: `app_id` (string, required), `writes` (array of {path, content}), `deletes` (array of string paths), `dry_run` (boolean).

### Area 3: Environment Configuration & Secret Protection
- **`env_list`**: Safe discovery of environment variable keys without exposing sensitive values.
  - Arguments: `app_id` (string, required).
- **`env_reveal`**: Explicitly reveals sensitive values. Requires 'Allow env_reveal' token permission.
  - Arguments: `app_id` (string, required), `keys` (array of strings, optional).
- **`env_set`**: Sets or updates environment variables and synchronizes workspace `.env`. Supports setting a single key-value pair or multiple variables at once.
  - Arguments: `app_id` (string, required), `key` (string, optional), `value` (string, optional), `variables` (map of string key-values, optional).

### Area 4: Deployment & Stack Control
- **`compose_get`**: Fetches effective `docker-compose.yml` (including overrides).
  - Arguments: `app_id` (string, required).
- **`deploy`**: Deploys stack (`docker compose up -d`). Returns `job_id` immediately, or pass `wait_seconds` for synchronous waiting. Pass `rebuild: true` for full container rebuild with image pull. Pass `summary_only: false` if full raw build logs are needed on success. Automatically skips git pull if workspace has local edits.
  - Arguments: `app_id` (string, required), `rebuild` (boolean, optional), `wait_seconds` (integer, optional), `summary_only` (boolean, default true), `git_pull` (boolean, optional).
- **`restart`**: Restarts either a specific service container or the entire stack.
  - Arguments: `app_id` (string, required), `service` (string, optional).
- **`stop`**: Shuts down the stack (`docker compose down`), or stops a specific service container.
  - Arguments: `app_id` (string, required), `service` (string, optional).
- **`deploy_status`**: Polls live output or final result of a deployment job. By default (`summary_only: true`), successful deployments suppress raw log dumping to prevent context bloat.
  - Arguments: `job_id` (string, optional), `app_id` (string, optional), `summary_only` (boolean, default true).

### Area 5: Logs & Diagnosis
- **`container_logs`**: Fetches recent stdout/stderr lines from any service container.
  - Arguments: `app_id` (string, required), `service` (string, optional), `tail` (integer, default 100).
- **`deploy_log_tail`**: Inspects recent historical deployment logs or live ongoing deployment output.
  - Arguments: `app_id` (string, required), `limit` (integer, default 5).

### Area 6: Terminal & Command Execution
- **`container_exec`**: Runs commands inside an application container (or specific compose service) and returns stdout/stderr with exit status. Essential for running database migrations (`php artisan migrate`, `python manage.py migrate`, `npx prisma migrate deploy`), executing test suites (`npm test`, `pytest`, `go test`), and inspecting live container states.
  - Arguments:
    - `app_id` (string, required)
    - `command` (string, required, e.g. `"npm test"` or `"php artisan migrate"`)
    - `service` (string, optional, e.g. `"web"`; defaults to primary running container)
    - `work_dir` (string, optional, e.g. `"/app"`)
    - `timeout_seconds` (integer, optional, default 60, max 300)
  - *RBAC*: Developer or Admin role required. Suspended apps cannot be executed in.
  - Returns: `{"app_id": "...", "container": "...", "service": "...", "command": "...", "ok": true|false, "output": "..."}`
- **`server_exec`**: Runs shell commands in the NextDeploy server / panel environment with Docker CLI access (strictly restricted to Admin role). Useful for host diagnostics, Docker system pruning, and VPS status inspection.
  - Arguments:
    - `command` (string, required, e.g. `"docker ps"`, `"df -h"`, `"uptime"`)
    - `timeout_seconds` (integer, optional, default 60, max 300)
  - *RBAC*: Strictly Admin role required.
  - Returns: `{"command": "...", "ok": true|false, "output": "..."}`

---

## 4. Standard Agent Workflow Recipes

### Recipe A: Diagnosing & Fixing a Broken Deployment

1. **Check Stack State**:
   ```json
   { "tool": "app_get", "arguments": { "app_id": "my-api" } }
   ```
2. **Read Recent Error Logs**:
   ```json
   { "tool": "container_logs", "arguments": { "app_id": "my-api", "tail": 50 } }
   ```
3. **Inspect the Faulty File**:
   ```json
   { "tool": "file_read", "arguments": { "app_id": "my-api", "path": "src/server.py" } }
   ```
4. **Apply Code Fix**:
   ```json
   { "tool": "file_write", "arguments": { "app_id": "my-api", "path": "src/server.py", "content": "..." } }
   ```
5. **Trigger Non-Blocking Redeploy (rebuild: true)**:
   ```json
   { "tool": "deploy", "arguments": { "app_id": "my-api", "rebuild": true } }
   ```
   *Response:* `{"job_id": "job_my-api_1726000000"}`
6. **Poll Deploy Status until Finished**:
   ```json
   { "tool": "deploy_status", "arguments": { "job_id": "job_my-api_1726000000" } }
   ```
   Repeat every 5–10 seconds until `"running": false`.
7. **Verify Container Health**:
   ```json
   { "tool": "container_logs", "arguments": { "app_id": "my-api", "tail": 30 } }
   ```

---

## 5. Security & Safety Rules for Agents

1. **Never write outside the workspace**: Paths starting with `/` or containing `..` will be rejected with `os.ErrInvalid`.
2. **Do not modify `.nextdeploy.generated.compose.yml`**: This file is generated dynamically by NextDeploy for Caddy reverse proxy routing. Always edit `docker-compose.yml`.
3. **Respect Deploy Mutexes**: Never send multiple simultaneous deploy requests for the same app. Wait for `deploy_status` to report `"running": false` before triggering another action.
