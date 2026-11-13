# Apps, Git, Sync & Environment (`nd`)

Reference for Docker Compose applications, Git workflows, delta file synchronization, domains, environment variables, files, and container execution.

---

## Application Lifecycle & Containers

```bash
nd apps [--json]                   # List all accessible applications
nd create <name>                   # Create a Compose app and link current directory
nd info [app_id] [--json]          # App metadata, domains, Git sync state, container summary
nd status [app_id] [--json]        # Alias of nd info
nd open [app_id]                   # Open primary app URL in browser
nd delete [app_id] [-f]            # Delete application (-f skips confirmation)

nd ps [app_id] [--json]            # List services, containers, health, and ports
nd containers [app_id] [-a]        # Inspect host containers
nd images [--json]                 # List Docker images on the host
```

---

## Git Repository Integration (`nd git`)

Every app (including PHP/WordPress sites) can switch between Git deployment mode and manual workspace file mode (`nd push` / File Manager edits).

```bash
# Inspect and toggle Git sync mode
nd git status [app_id] [--json]
nd git off [app_id]                # Deploy from workspace files (pauses webhook auto-deploys)
nd git on [app_id] [--force]       # Resume Git deploys (--force discards local edits)

# Providers, repo discovery, and webhook history
nd git providers [--json]
nd git repos <provider_id> [app_id] [--json]
nd git branches [app_id] [--provider <id>] --repo <url|owner/repo> [--json]
nd git webhooks [--limit N] [--json]

# Connect, pull, push workspace/htdocs to Git, or disconnect
nd git connect [app_id] <repo_url> [--branch main] [--provider <id>] [--public] [--no-auto-deploy] [--skip-sync]
nd git sync [app_id]               # Pull latest commits into server workspace (alias: pull)
nd git push [app_id] [-m "<msg>"] [--repo <url>] [--branch <br>] [--provider <id>] [--public] [--force]
nd git disconnect [app_id]
```

> **WordPress / PHP note**: `nd git push` on PHP sites commits and pushes `htdocs/` while automatically preserving `.gitignore` protections for `wp-config.php` and `.env`.

---

## Delta Sync & Deployments

`nd push` computes local file hashes and transfers only added/modified files (respecting `.gitignore` and `.ndignore`).

```bash
nd diff [app_id] [path]                              # Preview local vs remote delta
nd push [app_id] [path] [--deploy] [--prune] [-y]    # Upload changed files (--prune deletes extra remote files)
nd pull [app_id] [target_dir]                        # Download remote workspace (preserves local .env)

nd deploy [app_id] [--rebuild] [-f|--force]          # Build & deploy (--force pulls latest Git first)
nd restart [app_id] [service] [--recreate]           # Restart stack or single service
nd stop [app_id] [service]                           # Stop containers cleanly
nd down [app_id]                                     # Stop and remove containers (volumes retained)
nd logs [app_id] [-s svc] [-n 100] [-f] [--deploy]   # Container logs (--deploy shows build/deploy log; --error shows error log)
```

---

## Domains, Env Vars & Remote Files

```bash
# App custom domains
nd domain list [app_id] [--json]
nd domain add [app_id] <domain> [--service web] [--port 80] [--www] [--shield <id> | --direct]
nd domain rm [app_id] <domain|id>

# Environment variables
nd env list [app_id] [--json]
nd env get [app_id] <KEY>            # Reveal value (alias: reveal)
nd env set [app_id] KEY=VAL [K2=V2]
nd env unset [app_id] <KEY>          # Remove key (alias: rm)

# Remote workspace files
nd files [path] [-r] [--json]        # Alias: nd file list, nd ls
nd file read <path> [--full] [-o out]
nd file write <path> [content] [--from localfile]   # Supports piped stdin
nd file edit <path>                  # Open remote file in $EDITOR
nd file rm <path> [-r] [-f]
nd folder rm <path> [-f]
```

---

## Container & Host Shell Execution

```bash
nd exec [app_id] [-s svc] [-t 30s] [-i] -- <command...>   # Run command inside app container
nd shell [app_id] [-c container]                           # Interactive bash/sh TTY into container or PHP site
nd server-exec <command...>                                # Run command on VPS host (requires allow_server_exec)
nd server-shell                                            # Interactive root TTY on VPS host (admin)
```
