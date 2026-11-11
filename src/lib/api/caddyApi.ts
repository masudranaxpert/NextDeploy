import { requestJson } from './http';
import { notifyMutation } from '../dataSync';
import type { ShieldDomainStatus } from '../../types/shield';

const json = (body: unknown): RequestInit => ({
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify(body),
});

export interface CaddyOptions {
  profile: string;
  read_header: string;
  read_body_idle: string;
  idle: string;
  grace_period: string;
  max_header_size: string;
  real_ip: 'none' | 'cloudflare' | 'custom';
  trusted_proxies: string[];
  trusted_strict: boolean;
  shield_proxies?: string[];
  direct_cloudflare?: boolean;
}

export interface ProxyRoute {
  id: number;
  kind: 'proxy' | 'redirect';
  domain: string;
  target: string;
  redirect_code: number;
  keep_path: boolean;
  enable_https: boolean;
  enable_www: boolean;
  skip_tls_verify: boolean;
  trusted_proxy?: boolean;
  enabled: boolean;
  note: string;
  shield_node_id?: number | null;
  pending_origin_apply?: boolean;
  edge_status?: ShieldDomainStatus | null;
  created_at: string;
}

export const caddyApi = {
  caddyOptions: () =>
    requestJson<{
      ok: boolean;
      options: CaddyOptions;
      profiles: Record<string, CaddyOptions>;
      cloudflare_ranges: string[];
      labels: Record<string, string>;
    }>('/caddy/options'),
  caddyOptionsSave: async (options: CaddyOptions, apply: boolean) => {
    const res = await requestJson<{ ok: boolean; applying: boolean; options: CaddyOptions }>('/caddy/options', json({ ...options, apply }));
    if (res?.ok) notifyMutation();
    return res;
  },
  caddyRoutes: () => requestJson<{ ok: boolean; routes: ProxyRoute[] }>('/caddy/routes'),
  caddyRouteSave: async (route: Partial<ProxyRoute>) => {
    const res = await requestJson<{ ok: boolean; route: ProxyRoute }>(route.id ? `/caddy/routes/${route.id}` : '/caddy/routes', json(route));
    if (res?.ok) notifyMutation();
    return res;
  },
  caddyRouteToggle: async (id: number, enabled: boolean) => {
    const res = await requestJson<{ ok: boolean }>(`/caddy/routes/${id}/toggle`, json({ enabled }));
    if (res?.ok) notifyMutation();
    return res;
  },
  caddyRouteDelete: async (id: number) => {
    const res = await requestJson<{ ok: boolean }>(`/caddy/routes/${id}/delete`, { method: 'POST' });
    if (res?.ok) notifyMutation();
    return res;
  },
};
