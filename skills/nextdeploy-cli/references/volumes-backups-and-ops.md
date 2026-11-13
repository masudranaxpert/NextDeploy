# Volumes, Backups & Operations (`nd`)

Reference for Docker volumes, backup snapshots/schedules/destinations, system monitoring, pruning, audit logs, and CLI troubleshooting.

---

## Docker Volumes (`nd volume`)

```bash
nd volume list [app_id] [--json]
nd volume browse <volume_name> [path]
nd volume download <volume_name> [output.tar.gz]
nd volume restore <volume_name> <backup.tar.gz>
nd volume rm <volume_name>
nd volume prune                      # Admin: remove unused volumes
```

---

## Backups (`nd backup`)

```bash
# Manual backups, logs, and restore
nd backup list [app_id] [--json]
nd backup create [app_id] [dest_id]
nd backup log [app_id] <history_id>
nd backup restore [app_id] <history_id>

# Automated schedules
nd backup schedule list [app_id]
nd backup schedule add [app_id] <dest_id> "<cron_expr>" [--retention 7]
nd backup schedule rm <schedule_id>

# Cloud/S3 destinations
nd backup dest list
```

---

## System Monitoring, Cleanup & Audit

```bash
nd top                               # Live CPU, RAM, disk, and top container usage (alias: nd stats)
nd clean [--all]                     # Prune build cache & dangling images (--all includes stopped containers/unused images)
nd audit                             # View panel security audit log (admin)
nd update                            # Self-update nd CLI from GitHub Releases
nd completion bash|zsh|powershell    # Output shell autocompletion script
```

---

## Troubleshooting

| Symptom | Likely Cause & Fix |
| --- | --- |
| `no app ID specified` | Run `nd link <app_id>` inside the project folder or pass `--app <app_id>`. |
| `401 Unauthorized` | Token expired or revoked. Generate a new API token in panel Automation and re-run `nd login --url ... --token ...`. |
| `nd push` fails on Git-synced app | Check `nd git status [app_id]`. When Git auto-sync is ON, either run `nd git off` to switch to manual file deploys or commit and `nd git push`. |
| WordPress shows 404 on pretty permalinks | Run `nd php rules scan --apply` or set `nd php rules fallback default`. |
| Real client IP shows Cloudflare/Shield IP | Run `nd proxy options set --real-ip cloudflare` (or enable `--trusted-proxy` on `nd proxy add`). |
