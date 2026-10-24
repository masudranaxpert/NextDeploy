# Admin / user separation

Admins see and manage everything. Every other user sees only what belongs to them (their own apps and the apps
shared with them as collaborator) and cannot see, use or change anything of another user or of an admin.

| Area | Rule |
|---|---|
| Apps, files, domains, logs, terminal, PHP sites | `AppAccessMiddleware` / `CanAccessApp`: owner, collaborator (viewer/developer) or admin |
| Containers, images, volumes | Listed and actionable only when they belong to the user's apps (`internal/handlers/tenant.go`); PHP sites own no Docker volumes |
| Backup destinations, Git providers | Owned by the creator (admins included); users list/use/delete only their own. Rows without an owner (created by older versions) are admin-only |
| Registries | Owned by the creator and never shared. Registries created by older versions as "global" are given to the first admin at startup |
| Credentials | Destination configs, provider tokens, app Git tokens/keys and registry secrets never reach the browser |
| Host information | CPU/RAM telemetry, monitoring, host terminal, Caddy, migration, users, settings, audit log, CLI sessions, PHP instance: admin only (UI hides them, API answers 403) |
| Audit trail | Admins see every entry; a user sees only their own actions plus anything done to their own apps (`/api/audit-logs`) |
| Resources | A user's Overview shows their own limits and live usage (memory, CPU, storage, apps), never the host's. Limits above what the server really has (RAM, CPUs, disk) are refused; new accounts get defaults clamped to the server |
| Webhook deliveries | Only for the user's own apps |
| Backups of PHP sites | Only `app` backups (files + databases); volume/full backups are rejected |

Regression tests: `internal/db/tenant_scope_test.go`, `internal/quota`, and the end-to-end checks described in
`docs/php-hosting.md`.
