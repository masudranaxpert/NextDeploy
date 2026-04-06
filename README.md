# NextDeploy

![NextDeploy overview](image/readme.png)

[![Release](https://img.shields.io/github/v/release/masudranaxpert/NextDeploy?style=flat-square&color=4f46e5)](https://github.com/masudranaxpert/NextDeploy/releases)
[![Docker Pulls](https://img.shields.io/docker/pulls/masudranaxpert/nextdeploy?style=flat-square&color=0ea5e9)](https://hub.docker.com/r/masudranaxpert/nextdeploy)
[![License: MIT](https://img.shields.io/badge/license-MIT-green?style=flat-square)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.22-00ADD8?style=flat-square&logo=go)](https://go.dev)
[![Branch: php-panel](https://img.shields.io/badge/branch-php--panel-orange?style=flat-square)](https://github.com/masudranaxpert/NextDeploy/tree/php-panel)

A lightweight Docker deployment panel built with Go and Caddy. Deploy Docker Compose stacks, manage domains with automatic HTTPS, and monitor your containers — all from a clean web UI.

---

> ### ⚠️ Experimental — PHP Panel Template (`php-panel` branch)
>
> The `php-panel` branch introduces an **experimental one-click PHP hosting template** on top of NextDeploy.
> It is under active development and **not yet merged into `main`**.
>
> ![PHP Panel](image/php.png)
>
> **What it adds:**
> - One-click PHP hosting stack — PHP-FPM (7.4 / 8.1 / 8.2 / 8.3), MySQL 8, phpMyAdmin via Docker Compose
> - Folder-based multi-site hosting (`sites/<slug>/public_html`) per user
> - Caddy label auto-sync — domain add/update/delete triggers a background compose apply (only running services)
> - Per-site PHP version selection (dropdown shows only running FPM versions)
> - Panel-managed MySQL databases & users with cPanel-style privilege grants
> - phpMyAdmin one-click auto-login via encrypted stored credentials (1-hour session)
> - Scoped file browser — drag-and-drop upload, ZIP/Unzip, inline editor, per-site root only
> - Per-user site & database limits, admin impersonation, role-based access
> - DNS status check (Cloudflare detection, development domain detection)
>
> **To try it:**
> ```bash
> git checkout php-panel
> docker compose up -d --build panel
> ```
>
> **Status:** Experimental — APIs and database schema may change without notice.

---

> **GitHub:** [github.com/masudranaxpert/NextDeploy](https://github.com/masudranaxpert/NextDeploy)
> **Docker Hub:** [hub.docker.com/r/masudranaxpert/nextdeploy](https://hub.docker.com/r/masudranaxpert/nextdeploy)

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

### From Docker Hub (recommended)

```bash
# Create the data directory on your host
mkdir -p /data

# Run the panel + Caddy proxy
curl -fsSL https://raw.githubusercontent.com/masudranaxpert/NextDeploy/main/docker-compose.yml \
  | docker compose -f - up -d
```

Or with a local clone:

```bash
git clone https://github.com/masudranaxpert/NextDeploy.git
cd NextDeploy
docker compose up -d
```

Open `http://localhost:8080` — you will be prompted to create an admin account on first visit.

### Pull image manually

```bash
docker pull masudranaxpert/nextdeploy:latest
```

## Requirements

- Docker with Compose plugin
- A server with ports 80 and 443 open (for Caddy HTTPS)

## Configuration

All settings are stored in a SQLite database at `/data/panel.db` inside the container. The bind mount `/data` persists data across restarts.

| Environment variable | Default | Description |
|---|---|---|
| `DATA_DIR` | `/data` | Path to SQLite DB and workspaces |
| `WORKSPACES_ROOT` | `/data/workspaces` | Where app files are stored |
| `LISTEN_ADDR` | `:8080` | Panel listen address |
| `PANEL_DEV` | `false` | Reload templates on every request |

## Caddy proxy

The panel uses [caddy-docker-proxy](https://github.com/lucaslorentz/caddy-docker-proxy) for automatic HTTPS. Add a domain to any app from the **Domains** tab — the panel writes the correct Caddy labels into the generated compose file. No manual Caddyfile editing required.

For local/development domains (`.test`, `.localhost`, etc.) the panel automatically uses `tls internal` when HTTPS is enabled.

## Releases

New releases are published automatically when a version tag is pushed:

```bash
git tag v1.0.0
git push origin v1.0.0
```

GitHub Actions will:
1. Build a multi-arch Docker image (`linux/amd64` + `linux/arm64`)
2. Push it to Docker Hub with version tags (`v1.0.0`, `latest`)
3. Create a GitHub Release with auto-generated changelog
4. Clean up old Docker Hub tags (keeps the 5 most recent)

## License

MIT — see [LICENSE](LICENSE)
