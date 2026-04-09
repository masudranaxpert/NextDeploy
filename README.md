<div align="center">

# NextDeploy

**Self-hosted Docker deployment panel** — Compose stacks, automatic HTTPS, domains, and ops from one clean UI.

[![Release](https://img.shields.io/github/v/release/masudranaxpert/NextDeploy?style=flat-square&color=4f46e5)](https://github.com/masudranaxpert/NextDeploy/releases)
[![Docker Pulls](https://img.shields.io/docker/pulls/masudranaxpert/nextdeploy?style=flat-square&color=0ea5e9)](https://hub.docker.com/r/masudranaxpert/nextdeploy)
[![License: MIT](https://img.shields.io/badge/license-MIT-green?style=flat-square)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.24-00ADD8?style=flat-square&logo=go)](https://go.dev)

[GitHub](https://github.com/masudranaxpert/NextDeploy) · [Docker Hub](https://hub.docker.com/r/masudranaxpert/nextdeploy)

![NextDeploy overview](image/readme.png)

</div>

> **GitHub:** [github.com/masudranaxpert/NextDeploy](https://github.com/masudranaxpert/NextDeploy)  
> **Docker Hub:** [hub.docker.com/r/masudranaxpert/nextdeploy](https://hub.docker.com/r/masudranaxpert/nextdeploy)

> **PHP hosting template** (multi-version FPM, MySQL, phpMyAdmin, per-user limits) lives on the [`php-panel`](https://github.com/masudranaxpert/NextDeploy/tree/php-panel) branch — not included in `main`.

---

## Install (recommended)

One command downloads `docker-compose.yml`, creates `/data`, pulls images, starts **Caddy** + **panel**, optionally registers **systemd** auto-start, and installs **`nextdeploy-update`** / **`nextdeploy-logs`** helpers.

```bash
curl -fsSL https://raw.githubusercontent.com/masudranaxpert/NextDeploy/main/install.sh | sudo bash
```

With install options:

```bash
curl -fsSL https://raw.githubusercontent.com/masudranaxpert/NextDeploy/main/install.sh | sudo bash -s -- --domain panel.example.com --email admin@example.com
```

Or clone the repo and run locally:

```bash
git clone https://github.com/masudranaxpert/NextDeploy.git
cd NextDeploy
sudo bash install.sh
```

### Install script options

| Option | Description |
|--------|-------------|
| `--domain <host>` | Shown in the success summary (configure DNS + HTTPS in the panel after install) |
| `--email <addr>` | Reminder for Let's Encrypt / ACME email (set in panel settings when ready) |
| `--dir <path>` | Install directory (default: `/opt/nextdeploy`) |
| `--data-dir <path>` | Host data path patched into compose (default: `/data`) |
| `--help` | Usage |

Examples:

```bash
sudo bash install.sh --domain panel.example.com --email admin@example.com
sudo bash install.sh --dir /srv/nextdeploy --data-dir /mnt/nextdeploy-data
```

After install, open **`http://<server-ip>:8080`** and create the first admin user.

---

## Uninstall

Interactive (you must type `yes` to confirm):

```bash
curl -fsSL https://raw.githubusercontent.com/masudranaxpert/NextDeploy/main/uninstall.sh | sudo bash
```

**Non-interactive / force** (no confirmation prompt): arguments after `curl … | bash` are **not** passed to the script unless you use **`bash -s --`**. The `--` ends bash options; everything after it is passed to `uninstall.sh`:

```bash
curl -fsSL https://raw.githubusercontent.com/masudranaxpert/NextDeploy/main/uninstall.sh | sudo bash -s -- --force
```

Same idea with `--keep-data`:

```bash
curl -fsSL https://raw.githubusercontent.com/masudranaxpert/NextDeploy/main/uninstall.sh | sudo bash -s -- --force --keep-data
```

If you already saved the script on disk:

```bash
sudo bash uninstall.sh --keep-data
sudo bash uninstall.sh --force
```

| Option | Description |
|--------|-------------|
| `--keep-data` | Keeps the data directory (workspaces, SQLite DB, uploads) |
| `--force` / `-f` | Skip the interactive `yes` confirmation |
| `--dir`, `--data-dir` | Must match your install if non-default |

---

## Helper commands

| Command | Purpose |
|---------|---------|
| `nextdeploy-update` | `docker compose pull` + `up -d` in the install directory |
| `nextdeploy-logs` | `docker compose logs -f --tail=100` |
| `systemctl status nextdeploy` | Systemd unit status (if enabled during install) |

---

## Manual quick start (Docker Compose only)

```bash
mkdir -p /data
curl -fsSL https://raw.githubusercontent.com/masudranaxpert/NextDeploy/main/docker-compose.yml \
  -o docker-compose.yml
docker compose up -d
```

Or from a clone:

```bash
git clone https://github.com/masudranaxpert/NextDeploy.git
cd NextDeploy
docker compose up -d
```

For a local Dockerfile build instead of Docker Hub:

```bash
mkdir -p data/workspaces
docker compose -f docker-compose.local.yml up -d --build
```

```bash
docker pull masudranaxpert/nextdeploy:latest
```

Open `http://localhost:8080` — first visit creates the admin account.

---

## Features

- **Deploy apps** — Upload a ZIP or files, configure `docker-compose.yml`, deploy with one click
- **Automatic HTTPS** — Caddy reverse proxy with Let's Encrypt / ZeroSSL via labels ([caddy-docker-proxy](https://github.com/lucaslorentz/caddy-docker-proxy))
- **Domain routing** — Per-app domains; the panel generates Caddy labels and merges them into the generated compose file
- **File manager** — Browse, upload, view, and delete workspace files in the browser
- **Live deploy logs** — Real-time output while Docker Compose runs
- **Container logs** — Tail with level filtering, timestamps, and download
- **Docker resources** — List and remove containers, images, and volumes
- **Scheduled cleanup** — Auto-prune unused Docker data on a configurable interval
- **Multi-user auth** — First-run admin; admins manage users and roles
- **Responsive UI** — Works on phones and tablets

---

## Requirements

- **Linux** host (install script target); **Docker** 24+ and **Compose V2**
- Ports **80**, **443**, and **8080** (panel UI) available

---

## Troubleshooting

Common issues (including **Cloudflare upload limits** and **volume restore** workarounds) are documented in **[docs/troubleshooting.md](docs/troubleshooting.md)**.

**Short version:** If the panel is behind **Cloudflare proxy** (orange cloud), uploads are capped at the edge (typically **100 MB** on Free/Pro). Large volume backup restores may stall or fail through the public domain. Use **`http://<server-ip>:8080`** (or your configured port) to reach the panel **directly**, or use a **DNS-only** hostname for uploads. The panel itself allows up to **2 GiB** request bodies for restore.

---

## Configuration

Persistent state uses the host **`/data`** bind mount (or your `--data-dir`): SQLite at `/data/panel.db`, workspaces under `/data/workspaces`.

| Variable | Default | Description |
|----------|---------|-------------|
| `DATA_DIR` | `/data` | Panel data root inside the container |
| `WORKSPACES_ROOT` | `/data/workspaces` | App file storage |
| `LISTEN_ADDR` | `:8080` | Panel HTTP listen |
| `PANEL_DEV` | `false` | Reload templates on every request (dev only) |

---

## Caddy proxy

The panel writes Caddy labels into **`.nextdeploy.generated.compose.yml`**. Add domains from the **Domains** tab — no hand-written Caddyfile for app routing. For local-style names (`.test`, `.localhost`, etc.) the panel can use **internal TLS** when HTTPS is enabled.

---

## Releases

New releases are published when a version tag is pushed:

```bash
git tag v1.0.0
git push origin v1.0.0
```

GitHub Actions will:

1. Build a multi-arch Docker image (`linux/amd64` + `linux/arm64`)
2. Push it to Docker Hub with version tags (`v1.0.0`, `latest`)
3. Create a GitHub Release with auto-generated changelog
4. Clean up old Docker Hub tags (keeps the 5 most recent)

---

## License

MIT — see [LICENSE](LICENSE).
