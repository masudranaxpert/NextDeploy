---
name: nextdeploy-cli
version: 2.9.10
description: Manage NextDeploy Docker Compose apps (including direct TCP/UDP ports), PHP/WordPress sites, MariaDB databases, Caddy/Angie/Ferron reverse proxy engines and routes, Edge Shield nodes and manual domains, optional AI assistant, delta file syncs (nd push), Git integration, volumes, and backups using the nd CLI.
---

# NextDeploy CLI (`nd`)

Use this skill to manage NextDeploy servers and applications via the `nd` CLI.

> **Installation**: If `nd` is not installed, see [INSTALLATION.md](INSTALLATION.md). Verify with `nd whoami` or `nd version`.

---

## Reference Docs (Load Only What the Task Needs)

Read the matching reference file before running domain-specific commands:

| Topic | When to Read | Reference |
| --- | --- | --- |
| **Apps, Git, Sync & Env** | Compose apps, direct TCP/UDP ports (`nextdeploy.ports.direct`), `nd git`, `nd push`/`pull`/`diff`, `nd deploy`, `nd domain`, `nd env`, `nd files`, `nd exec`/`shell` | [apps-git-and-sync.md](references/apps-git-and-sync.md) |
| **PHP, WordPress, DB & Routing** | PHP/WordPress/static sites, `nd db` / `nd php db`, cron, `fix-perms`, `pma`, `.htaccess` -> proxy rules, fallback modes | [php-routing-and-htaccess.md](references/php-routing-and-htaccess.md) |
| **Proxy Engines & Edge Shield** | `nd proxy` routes (`add`, `toggle`, `logs`, `options`), engine switching (`nd proxy engine` for Caddy/Angie/Ferron), `nd shield`, Shield VPS `nd edge` (`switch-proxy`, `domain` manual/user-config domains) | [proxy-and-edge.md](references/proxy-and-edge.md) |
| **AI, Volumes, Backups & Ops** | Optional AI assistant (`nd ai`), `nd volume`, `nd backup`, `nd top`, `nd clean`, `nd audit`, `nd quota`, `nd users`, troubleshooting | [volumes-backups-and-ops.md](references/volumes-backups-and-ops.md) |

---

## Auth, Accounts & Project Context

```bash
# Authenticate against a panel (API token preferred for automation/CI)
nd login --url https://panel.example.com --token nd_xxx --name prod

# Multi-panel accounts
nd account list
nd account switch prod

# Bind current folder to an app (.nd/project.json) so [app_id] can be omitted
nd link my-app
nd unlink
```

Global conventions:
- Any command taking `[app_id]` auto-resolves from `.nd/project.json` when inside a linked directory, or accepts `--app <id>` / `-a <id>`.
- Pass `-A <account>` / `--account <account>` (or `ND_ACCOUNT`) to target a specific saved panel account without switching the default.
- Pass `--json` on `list`, `info`, `ps`, `env`, `files`, `git`, `proxy`, `shield`, `edge`, `ai`, `php` commands for structured output.
- Pass `-y` / `--yes` on destructive or push prompts for non-interactive scripts.

---

## Quick Command Matrix

| Domain | Core Commands |
| --- | --- |
| **Apps & Status** | `nd apps`, `nd create <name>`, `nd info [app_id]`, `nd ps [app_id]`, `nd delete [app_id] [-f]` |
| **Git Integration** | `nd git status\|on\|off\|providers\|repos\|branches\|webhooks\|connect\|sync\|push\|disconnect` |
| **Sync & Deploy** | `nd diff`, `nd push [--deploy] [--prune]`, `nd pull`, `nd deploy [--rebuild] [-f]`, `nd restart`, `nd logs -f` |
| **Shell & Files** | `nd exec <cmd>`, `nd shell`, `nd files [path]`, `nd file read\|write\|edit\|rm` |
| **Domains & Env** | `nd domain list\|add\|rm`, `nd env list\|get\|set\|unset` |
| **PHP & Databases** | `nd php list\|create\|info\|set\|start\|stop\|restart\|rules\|cron\|fix-perms\|pma`, `nd db list\|create\|grant\|export\|import\|user` |
| **Proxy & Shield** | `nd proxy list\|add\|enable\|disable\|toggle\|rm\|reload\|logs\|status\|options\|engine`, `nd shield ...`, `nd edge install\|switch-proxy\|domain\|...` |
| **AI, Storage & Ops** | `nd ai status\|enable\|disable`, `nd volume ...`, `nd backup ...`, `nd top`, `nd clean`, `nd audit`, `nd quota`, `nd users` |

---

## Standard Workflows

### Deploy or Update an App
1. Verify account and link: `nd whoami` and `nd info` (or `nd link <app_id>`).
2. Preview changes before uploading: `nd diff`.
3. Upload changed files and trigger deploy: `nd push --deploy -y`.
4. Stream startup logs and check health: `nd logs -f` then `nd ps`.

### PHP / WordPress Site with Custom `.htaccess`
1. Create site + database: `nd php create <name> --domain <domain> --db`.
2. Push local code: `nd push -y` (note WordPress lives in `htdocs/`).
3. Scan and convert `.htaccess` to Caddy rules: `nd php rules scan --apply`.
4. Repair permissions if needed: `nd php fix-perms`.
