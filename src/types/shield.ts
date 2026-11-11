export type ShieldNodeStatus = 'pending' | 'connected' | 'offline' | 'unregistered';

export type ShieldSyncState =
  | 'ok'
  | 'stale'
  | 'auth_failed'
  | 'outdated'
  | 'panel_outdated'
  | 'pending'
  | 'unregistered';

export type ShieldDomainState =
  | 'active'
  | 'issuing'
  | 'cert_error'
  | 'pending_dns'
  | 'pending_caa'
  | 'dns_drift'
  | 'conflict'
  | 'queued_rate_limit'
  | 'unsupported_wildcard'
  | 'excluded_internal'
  | 'excluded_loop'
  | 'excluded_quota'
  | 'held_mass_delete';

export interface ShieldDomainStatus {
  node_id: number;
  host: string;
  state: ShieldDomainState | string;
  detail?: string;
  cert_not_after?: string;
  updated_at: string;
}

export interface ShieldNode {
  id: number;
  name: string;
  ip: string;
  token: string;
  status: ShieldNodeStatus;
  assigned_domains: number;
  last_heartbeat_at?: string;
  edge_id?: string;
  edge_version?: string;
  caddy_version?: string;
  registered_at?: string;
  last_sync_at?: string;
  last_applied_etag?: string;
  last_error?: string;
  edge_status_json?: string;
  sync_state?: ShieldSyncState | string;
  conflicts_count?: number;
  domain_statuses?: ShieldDomainStatus[];
  created_at: string;
}

export interface ShieldNodesResponse {
  ok: boolean;
  panel_id?: string;
  nodes: ShieldNode[];
  origin_ip: string;
  error?: string;
}

export interface CreateShieldNodeResponse {
  ok: boolean;
  panel_id?: string;
  node?: ShieldNode;
  setup_command?: string;
  uninstall_command?: string;
  error?: string;
}

export interface RotateShieldTokenResponse {
  ok: boolean;
  node?: ShieldNode;
  token?: string;
  token_prev_expires_at?: string;
  setup_command?: string;
  uninstall_command?: string;
  error?: string;
}

export interface ShieldCFCheckResponse {
  ok: boolean;
  domain: string;
  cf_flexible_loop: boolean;
  cf_hint?: string;
  error?: string;
}

export interface TestShieldNodeResponse {
  ok: boolean;
  reachable: boolean;
  http_status?: number;
  error?: string;
}
