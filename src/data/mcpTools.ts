import { McpToolItem } from '../types/nextdeploy';

/** The tools the MCP server exposes (documentation shown on the MCP page). */
export const MCP_TOOLS_CATALOG: McpToolItem[] = [
  { name: 'apps_list', category: 'Deploy & Compose', description: 'List all Compose stacks, status, domains, and resource usage.', signature: 'apps_list()', readOnly: true },
  { name: 'app_inspect', category: 'Deploy & Compose', description: 'Get full stack details including services, domains, and Git commit.', signature: 'app_inspect(app_id: string)', readOnly: true },
  { name: 'deploy', category: 'Deploy & Compose', description: 'Trigger a zero-downtime docker compose build & rollout for an app.', signature: 'deploy(app_id: string, no_cache?: boolean)', readOnly: false },
  { name: 'compose_action', category: 'Deploy & Compose', description: 'Run start, stop, restart, pull, or down on an application stack.', signature: 'compose_action(app_id: string, action: "up"|"stop"|"restart"|"pull")', readOnly: false },
  { name: 'file_tree', category: 'Workspace & Files', description: 'List files and directories inside an app workspace.', signature: 'file_tree(app_id: string, path?: string)', readOnly: true },
  { name: 'file_read', category: 'Workspace & Files', description: 'Read contents of docker-compose.yml, Dockerfile, or source files.', signature: 'file_read(app_id: string, path: string)', readOnly: true },
  { name: 'file_write', category: 'Workspace & Files', description: 'Write or patch workspace files directly from Cursor / Claude Code.', signature: 'file_write(app_id: string, path: string, content: string)', readOnly: false },
  { name: 'file_delete', category: 'Workspace & Files', description: 'Remove an unused file or directory from the app workspace.', signature: 'file_delete(app_id: string, path: string)', readOnly: false },
  { name: 'container_logs', category: 'Logs & Telemetry', description: 'Fetch or tail structured container logs with level & line filters.', signature: 'container_logs(app_id: string, service?: string, tail?: number)', readOnly: true },
  { name: 'monitor_snapshot', category: 'Logs & Telemetry', description: 'Retrieve host CPU, RAM, Disk, and per-container resource metrics.', signature: 'monitor_snapshot()', readOnly: true },
  { name: 'env_list', category: 'Domains & Env', description: 'List environment variable keys and non-secret values for an app.', signature: 'env_list(app_id: string)', readOnly: true },
  { name: 'env_set', category: 'Domains & Env', description: 'Upsert one or more .env variables and optionally restart services.', signature: 'env_set(app_id: string, vars: Record<string, string>)', readOnly: false },
  { name: 'domain_add', category: 'Domains & Env', description: 'Bind a domain to a container port and provision Caddy HTTPS automatically.', signature: 'domain_add(app_id: string, domain: string, service: string, port: number)', readOnly: false },
  { name: 'domain_remove', category: 'Domains & Env', description: 'Detach a custom domain and regenerate Caddy reverse proxy labels.', signature: 'domain_remove(app_id: string, domain: string)', readOnly: false },
  { name: 'git_sync', category: 'Workspace & Files', description: 'Pull latest commit from configured Git remote and diff workspace.', signature: 'git_sync(app_id: string, branch?: string)', readOnly: false },
  { name: 'volume_backup', category: 'Deploy & Compose', description: 'Create a compressed .tar.gz volume snapshot or push to rclone destination.', signature: 'volume_backup(app_id: string, volume_name: string)', readOnly: false },
];
