<div align="center">

# NextDeploy

**Self-hosted Docker deployment panel** — Compose stacks, automatic HTTPS, file manager, live logs, and AI-agent access from one clean UI.

[![Release](https://img.shields.io/github/v/release/masudranaxpert/NextDeploy?style=flat-square&color=4f46e5)](https://github.com/masudranaxpert/NextDeploy/releases)
[![Docker Pulls](https://img.shields.io/docker/pulls/masudranaxpert/nextdeploy?style=flat-square&color=0ea5e9)](https://hub.docker.com/r/masudranaxpert/nextdeploy)
[![License: MIT](https://img.shields.io/badge/license-MIT-green?style=flat-square)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.24-00ADD8?style=flat-square&logo=go)](https://go.dev)

![NextDeploy overview](image/readme.png)

</div>

---

## Features

| Category | Capabilities |
|---|---|
| **Deploy** | Upload ZIP/files or connect a Git repo; one-click deploy via `docker-compose.yml`; webhook auto-deploy |
| **HTTPS & Domains** | Caddy reverse proxy with Let's Encrypt / ZeroSSL; per-app domain routing; no Caddyfile needed |
| **Files & Logs** | In-browser file manager; real-time deploy logs; container log tailing with filters |
| **Docker Resources** | Browse and remove containers, images, and volumes; scheduled auto-prune |
| **Multi-user** | Admin + user roles; per-user resource limits; audit log |
| **MCP Server** | Native [Model Context Protocol](skills/nextdeploy-mcp/SKILL.md) server — let Cursor, Claude Code or Windsurf edit, deploy, and inspect apps directly over your local API token |
| **Panel Migration** | Export selected apps to a `.nd-migrate` bundle and restore on another VPS |
| **Development Mode** | *(Beta — enable in Settings)* Bind-mount workspace into containers for live hot-reload without image rebuilds |

## Requirements

- **Linux** host; **Docker 24+** and **Compose V2**
- Ports **80**, **443**, and **8080** open

---

## Install

```bash
curl -fsSL https://raw.githubusercontent.com/masudranaxpert/NextDeploy/main/install.sh | sudo bash
```

With options (use `bash -s --` to pass flags):

```bash
curl -fsSL https://raw.githubusercontent.com/masudranaxpert/NextDeploy/main/install.sh | sudo bash -s -- \
  --domain panel.example.com --email admin@example.com
```

| Option | Default | Description |
|--------|---------|-------------|
| `--domain` | *(none)* | Panel domain hint (configure HTTPS in-panel after install) |
| `--email` | *(none)* | Let's Encrypt / ACME email reminder |
| `--dir` | `/opt/nextdeploy` | Install directory |
| `--data-dir` | `/data` | Host data path |

Open **`http://<server-ip>:8080`** after install and create the first admin user.

### Manual (without install script)

```bash
mkdir -p /data
curl -fsSL https://raw.githubusercontent.com/masudranaxpert/NextDeploy/main/docker-compose.yml -o docker-compose.yml
docker compose up -d
```

### Build from source

```bash
git clone https://github.com/masudranaxpert/NextDeploy.git && cd NextDeploy
docker compose -f docker-compose.local.yml up -d --build
```

---

## MCP Server (AI Integration)

NextDeploy ships a native **Model Context Protocol** server at `/mcp/sse`. AI coding assistants (Cursor, Claude Code, Windsurf, Claude Desktop) can connect using a panel API token and get 18 tools — `file_write`, `deploy`, `container_logs`, `env_set`, and more — without touching SSH or installing anything on the VPS.

**Quick start:**
1. Open **MCP Server** in the sidebar → generate an API token.
2. Add the SSE URL + token to your assistant's MCP config.
3. The assistant can now edit files, trigger deploys, and tail logs directly.

See [`skills/nextdeploy-mcp/SKILL.md`](skills/nextdeploy-mcp/SKILL.md) for the full agent skill guide.

---

## Development Mode *(Beta)*

Turns on a **Dev** tab per app. Enable globally in **Settings → Enable Development Mode**.

When Dev mode is active for an app:
- Workspace files are **bind-mounted** into the container — changes are visible immediately, no rebuild needed.
- `Deploy` runs `docker compose up -d` without rebuilding the image.
- Git auto-deploy is **paused** so local edits are never overwritten by a push.

> **You supply the reload command** — use `npm run dev`, `uvicorn ... --reload`, or `air` in your compose file.

---

## Panel Migration (VPS-to-VPS)

1. **Export** — Admin → System → Migration → select apps → *Create export bundle* (valid 3 hours).
2. **Import** on the new VPS:

```bash
curl -fsSL https://raw.githubusercontent.com/masudranaxpert/NextDeploy/main/migrate.sh | sudo bash -s -- \
  --url "https://panel.example.com/migrate/download/TOKEN"
```

---

## Domains & HTTPS

Panel writes Caddy labels into `.nextdeploy.generated.compose.yml`. Add domains from each app's **Domains** tab — no hand-written Caddyfile needed.

---

## Uninstall

```bash
curl -fsSL https://raw.githubusercontent.com/masudranaxpert/NextDeploy/main/uninstall.sh | sudo bash
```

| Option | Description |
|--------|-------------|
| `--keep-data` | Preserve workspaces and SQLite DB |
| `--force` / `-f` | Skip confirmation |

---

## Troubleshooting

See **[docs/troubleshooting.md](docs/troubleshooting.md)** for common issues.

**Cloudflare users:** uploads are capped at the edge (~100 MB on Free/Pro). Access the panel directly at `http://<server-ip>:8080` for large uploads.

---

## License

MIT — see [LICENSE](LICENSE).
