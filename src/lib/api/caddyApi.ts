import { requestJson } from './http';

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
  enabled: boolean;
  note: string;
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
  caddyOptionsSave: (options: CaddyOptions, apply: boolean) =>
    requestJson<{ ok: boolean; applying: boolean; options: CaddyOptions }>('/caddy/options', json({ ...options, apply })),
  caddyRoutes: () => requestJson<{ ok: boolean; routes: ProxyRoute[] }>('/caddy/routes'),
  caddyRouteSave: (route: Partial<ProxyRoute>) =>
    requestJson<{ ok: boolean; route: ProxyRoute }>(route.id ? `/caddy/routes/${route.id}` : '/caddy/routes', json(route)),
  caddyRouteToggle: (id: number, enabled: boolean) =>
    requestJson<{ ok: boolean }>(`/caddy/routes/${id}/toggle`, json({ enabled })),
  caddyRouteDelete: (id: number) => requestJson<{ ok: boolean }>(`/caddy/routes/${id}/delete`, { method: 'POST' }),
};
