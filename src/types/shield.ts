export interface ShieldNode {
  id: number;
  name: string;
  ip: string;
  token: string;
  status: 'pending' | 'connected' | 'offline';
  assigned_domains: number;
  last_heartbeat_at?: string;
  created_at: string;
}

export interface ShieldNodesResponse {
  ok: boolean;
  nodes: ShieldNode[];
  origin_ip: string;
  error?: string;
}

export interface CreateShieldNodeResponse {
  ok: boolean;
  node?: ShieldNode;
  setup_command?: string;
  error?: string;
}

export interface TestShieldNodeResponse {
  ok: boolean;
  reachable: boolean;
  http_status?: number;
  error?: string;
}
