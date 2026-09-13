---
name: nextdeploy-mcp
description: Guide and tool reference for AI coding agents connecting to NextDeploy via Model Context Protocol (MCP). Covers inspecting apps, managing workspace files, editing environment variables, running non-blocking deployments, polling deploy logs, and toggling development mode.
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
│   6. Dev hot-reload       →   dev_mode_set, reset_dev_deps  │
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

The NextDeploy MCP server provides **18 tools** across 6 core functional areas:

### Area 1: Application Discovery
- **`app_list`**: Lists all applications the user can access.
  - Returns: `id`, `name`, `status`, `dev_mode`, `domains`, and compose `services`.
  - Example Call: `{}`
- **`app_get`**: Detailed metadata, compose services, and container status (`ps`).
  - Arguments: `app_id` (string, required).

### Area 2: Workspace File Operations
All file paths are strictly sandboxed inside `/data/workspaces/<app_id>`. Path traversal attempts (`../`) and access to internal metadata (`.panel-meta`) are automatically blocked.
- **`file_list`**: Lists directory contents.
  - Arguments: `app_id` (string, required), `path` (string, optional, e.g. `"src"`).
- **`file_read`**: Reads file contents as plain text.
  - Arguments: `app_id` (string, required), `path` (string, required).
- **`file_write`**: Creates or updates a file.
  - Arguments: `app_id` (string, required), `path` (string, required), `content` (string, required).
  - *Safety Guard*: If writing `docker-compose.yml`, the content is automatically verified against NextDeploy security rules (e.g. privileged mode, host device binds, and unmanaged networks are rejected).
- **`file_delete`**: Deletes a file or directory.
  - Arguments: `app_id` (string, required), `path` (string, required).

### Area 3: Environment Configuration
- **`env_list`**: Retrieves all panel environment variables as parsed key-value pairs and raw dotenv text.
  - Arguments: `app_id` (string, required).
- **`env_set`**: Sets or updates an environment variable. Updates both the panel database and the workspace `.env` file automatically.
  - Arguments: `app_id` (string, required), `key` (string, required), `value` (string, required).

### Area 4: Deployment & Stack Control
Deployments are **non-blocking**. They acquire the app's `ComposeMu` lock, trigger the build/up process in the background, and return a `job_id` immediately.
- **`compose_get`**: Fetches the effective `docker-compose.yml` (including active overrides).
  - Arguments: `app_id` (string, required).
- **`deploy`**: Starts standard stack deployment (`docker compose up -d`). If Dev Mode is active, uses `ComposeApply` to avoid unnecessary image rebuilds.
  - Arguments: `app_id` (string, required).
  - Returns: `{"job_id": "job_myapp_123...", "status": "started"}`.
- **`redeploy`**: Pulls latest images and forces a full rebuild (`docker compose up -d --build`).
  - Arguments: `app_id` (string, required).
- **`restart`**: Restarts either a specific service container or the entire stack.
  - Arguments: `app_id` (string, required), `service` (string, optional).
- **`stop`**: Shuts down the stack (`docker compose down`).
  - Arguments: `app_id` (string, required).
- **`deploy_status`**: Polls the live output or final result of a deployment job.
  - Arguments: `job_id` (string, optional), `app_id` (string, optional).
  - Returns: `{"job_id": "...", "running": true|false, "ok": true|false, "output": "..."}`.

### Area 5: Logs & Diagnosis
- **`container_logs`**: Fetches recent stdout/stderr lines from any service container.
  - Arguments: `app_id` (string, required), `service` (string, optional), `tail` (integer, default 100).
- **`deploy_log_tail`**: Inspects recent historical deployment logs or live ongoing deployment output.
  - Arguments: `app_id` (string, required), `limit` (integer, default 5).

### Area 6: Development Mode & Anti-Shadowing
- **`dev_mode_set`**: Configures bind-mounting the workspace directly into the container so local code edits reflect immediately.
  - Arguments:
    - `app_id` (string, required)
    - `enabled` (boolean, required)
    - `service` (string, optional, e.g. `"web"`)
    - `target` (string, optional, e.g. `"/app"`)
    - `command` (string, optional, e.g. `"npm run dev"` or `"uvicorn main:app --reload"`)
- **`reset_dev_deps`**: Deletes app-scoped named volumes (`nddev_<appID>_*`) and recreates containers with fresh image dependencies with zero database downtime.
  - Arguments: `app_id` (string, required).

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
5. **Trigger Non-Blocking Redeploy**:
   ```json
   { "tool": "redeploy", "arguments": { "app_id": "my-api" } }
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

### Recipe B: Enabling Instant Live-Reload (Dev Mode)

1. Turn on Dev Mode with bind mount:
   ```json
   {
     "tool": "dev_mode_set",
     "arguments": {
       "app_id": "my-web",
       "enabled": true,
       "service": "web",
       "target": "/app",
       "command": "npm run dev"
     }
   }
   ```
2. Now, whenever you call `file_write(path, content)`, the changes are instantly reflected inside the running container without rebuilding!
3. If new npm packages were added to the image and you need fresh `node_modules`:
   ```json
   { "tool": "reset_dev_deps", "arguments": { "app_id": "my-web" } }
   ```

---

## 5. Security & Safety Rules for Agents

1. **Never write outside the workspace**: Paths starting with `/` or containing `..` will be rejected with `os.ErrInvalid`.
2. **Do not modify `.nextdeploy.generated.compose.yml`**: This file is generated dynamically by NextDeploy for Caddy reverse proxy routing. Always edit `docker-compose.yml`.
3. **Respect Deploy Mutexes**: Never send multiple simultaneous deploy requests for the same app. Wait for `deploy_status` to report `"running": false` before triggering another action.
4. **Protect Database Volumes**: Dev mode uses app-scoped named volumes (`nddev_*`) for anti-shadowing paths like `node_modules` or `.venv`. Databases use persistent standard named volumes and are never wiped by dev resets.
