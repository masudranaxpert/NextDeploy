# NextDeploy

![NextDeploy overview](image/readme.png)

A lightweight Docker deployment panel built with Go and Caddy. Deploy Docker Compose stacks, manage domains with automatic HTTPS, and monitor your containers — all from a clean web UI.

## Features

- **Deploy apps** — upload a ZIP or files, configure `docker-compose.yml`, and deploy with one click
- **Automatic HTTPS** — Caddy reverse proxy with Let's Encrypt / ZeroSSL certificates via labels
- **Domain routing** — add domains per app; the panel generates Caddy labels and merges them into the compose file automatically
- **File manager** — browse, upload, view, and delete workspace files in the browser
- **Live deploy logs** — real-time output while Docker Compose runs
- **Container logs** — tail any container with level filtering, timestamps, and download
- **Docker resources** — list and delete containers, images, and volumes
- **Scheduled cleanup** — auto-prune unused Docker images and containers on a configurable interval
- **Multi-user auth** — first-time setup creates an admin account; admins can add/remove users and change roles
- **Mobile responsive** — works on phones and tablets

## Quick start

```bash
git clone <repo>
cd nextdeploy
docker compose up -d --build
```

Open `http://localhost:8080` — you will be prompted to create an admin account on first visit.

## Requirements

- Docker with Compose plugin
- A server with ports 80 and 443 open (for Caddy HTTPS)

## Configuration

All settings are stored in a SQLite database at `/data/panel.db` inside the container. The Docker volume `pass_panel_data` persists data across restarts.

| Environment variable | Default | Description |
|---|---|---|
| `DATA_DIR` | `/data` | Path to SQLite DB and workspaces |
| `WORKSPACES_ROOT` | `/data/workspaces` | Where app files are stored |
| `LISTEN_ADDR` | `:8080` | Panel listen address |
| `PANEL_DEV` | `false` | Reload templates on every request |

## Caddy proxy

The panel uses [caddy-docker-proxy](https://github.com/lucaslorentz/caddy-docker-proxy) for automatic HTTPS. Add a domain to any app from the **Domains** tab — the panel writes the correct Caddy labels into the generated compose file. No manual Caddyfile editing required.

For local/development domains (`.test`, `.localhost`, etc.) the panel automatically uses `tls internal` when HTTPS is enabled.

## License

MIT
