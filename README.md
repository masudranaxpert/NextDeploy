<div align="center">

# NextDeploy v2

**Self-hosted Docker deployment & CloudPanel-style PHP hosting panel** — Compose stacks, instant pre-built PHP runtimes with CVE alerts, automated HTTPS, native AI MCP server, and live logs from one clean UI.

[![Release](https://img.shields.io/github/v/release/masudranaxpert/NextDeploy?style=flat-square&color=4f46e5)](https://github.com/masudranaxpert/NextDeploy/releases)
[![Docker Pulls](https://img.shields.io/docker/pulls/masudranaxpert/nextdeploy-v2?style=flat-square&color=0ea5e9)](https://hub.docker.com/r/masudranaxpert/nextdeploy-v2)
[![License: MIT](https://img.shields.io/badge/license-MIT-green?style=flat-square)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.24-00ADD8?style=flat-square&logo=go)](https://go.dev)

![NextDeploy overview](image/readme.png)

</div>

---

## What's New in v2

- ⚡ **Instant PHP Hosting (5–10s provision)**: Pre-built Docker Hub runtimes (`masudranaxpert/nextdeploy-php:7.4` to `8.5`) pre-baked with MongoDB, Redis, MySQL, GD, Imagick, OPcache, Composer, and WP-CLI. No more waiting 15 minutes to compile on small VPS servers.
- 🛡️ **Active CVE & EOL Security Auditing**: Real-time vulnerability badges and CVSS ratings (including critical 2026 PHP2Shell CVE-2026-109618/109619 RCE warnings and EOL status for 7.4/8.0/8.1) right in the PHP version manager.
- 🚀 **Advanced Asset Optimization**: Pre-compressed Gzip assets, vendor chunking, and sub-millisecond virtual tree rendering for large workspaces.
- 🤖 **Native MCP Server**: Directly connect Cursor, Claude Code, or Windsurf to inspect, deploy, and edit code with 18+ tools over SSE.

---

## Quick Install (One Command)

```bash
curl -fsSL https://raw.githubusercontent.com/masudranaxpert/NextDeploy/main-v2/install.sh | sudo bash
```

With custom domain and Let's Encrypt email:

```bash
curl -fsSL https://raw.githubusercontent.com/masudranaxpert/NextDeploy/main-v2/install.sh | sudo bash -s -- \
  --domain panel.example.com --email admin@example.com
```

| Option | Default | Description |
|---|---|---|
| `--domain` | *(none)* | Panel domain (automatic HTTPS via Caddy) |
| `--email` | *(none)* | Let's Encrypt / ACME notification email |
| `--dir` | `/opt/nextdeploy` | Host installation directory |
| `--data-dir` | `/data` | Host persistent data path |

After install, open **`http://<server-ip>:8080`** to set up your admin account.

---

## Core Capabilities

| Category | Features |
|---|---|
| **PHP & WordPress** | PHP 7.4–8.5 runtimes, MariaDB databases, WP-CLI, cron jobs, php.ini tuning, php-fpm pools, CVE indicators |
| **Deploy Stacks** | Git webhook auto-deploy or manual file upload; automatic `docker-compose.yml` generation |
| **HTTPS & Routing** | Embedded Caddy proxy; zero-config SSL certificates via Let's Encrypt / ZeroSSL |
| **Files & Terminal** | In-browser file editor, upload/download, host & container interactive web terminals |
| **AI Integration** | Native [Model Context Protocol (MCP)](skills/nextdeploy-mcp/SKILL.md) server at `/mcp/sse` for AI dev agents |
| **Resource Limits** | Per-container CPU / Memory limits, live container metrics, and scheduled Docker prune |
| **VPS Migration** | Export apps to a `.nd-migrate` bundle and one-command import onto another VPS |

---

## Manual Run (Docker Compose)

```bash
mkdir -p /data
curl -fsSL https://raw.githubusercontent.com/masudranaxpert/NextDeploy/main-v2/docker-compose.yml -o docker-compose.yml
docker compose up -d
```

### Local Development / Build from Source

```bash
git clone https://github.com/masudranaxpert/NextDeploy.git -b main-v2
cd NextDeploy
npm run build
go build -o panel .
./panel
```

---

## Management & Updates

- **Update to latest v2**: `sudo nextdeploy-update`
- **Stream panel logs**: `sudo nextdeploy-logs`
- **Uninstall**: `curl -fsSL https://raw.githubusercontent.com/masudranaxpert/NextDeploy/main-v2/uninstall.sh | sudo bash`

---

## License

MIT — see [LICENSE](LICENSE).
