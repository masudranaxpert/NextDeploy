import { notifyMutation } from '../dataSync';
import { requestJson, toFormBody, type ApiResult } from './http';
import type { ShieldDomainStatus } from '../../types/shield';

export type ProxyEngineName = 'caddy' | 'angie' | 'ferron';

export interface ProxyCapabilities {
  auto_tls: boolean;
  internal_tls: boolean;
  on_demand_tls: boolean;
  docker_upstream_discovery: boolean;
  php_fastcgi: boolean;
  basic_auth_bcrypt: boolean;
  regex_routing: boolean;
  require_target_file: boolean;
  h2_idle_timeout: boolean;
  write_timeout: boolean;
  read_header_timeout: boolean;
  read_body_timeout: boolean;
  grace_period: boolean;
  max_header_size_http1: boolean;
  trusted_strict: boolean;
  long_streams_on_reload: boolean;
  admin_api: boolean;
  experimental: boolean;
}

export const DEFAULT_ENGINE_CAPABILITIES: Record<ProxyEngineName, ProxyCapabilities> = {
  caddy: {
    auto_tls: true,
    internal_tls: true,
    on_demand_tls: true,
    docker_upstream_discovery: true,
    php_fastcgi: true,
    basic_auth_bcrypt: true,
    regex_routing: true,
    require_target_file: true,
    h2_idle_timeout: true,
    write_timeout: true,
    read_header_timeout: true,
    read_body_timeout: true,
    grace_period: true,
    max_header_size_http1: true,
    trusted_strict: true,
    long_streams_on_reload: true,
    admin_api: true,
    experimental: false,
  },
  angie: {
    auto_tls: true,
    internal_tls: true,
    on_demand_tls: false,
    docker_upstream_discovery: false,
    php_fastcgi: true,
    basic_auth_bcrypt: true,
    regex_routing: true,
    require_target_file: true,
    h2_idle_timeout: true,
    write_timeout: true,
    read_header_timeout: true,
    read_body_timeout: true,
    grace_period: true,
    max_header_size_http1: true,
    trusted_strict: true,
    long_streams_on_reload: true,
    admin_api: false,
    experimental: false,
  },
  ferron: {
    auto_tls: true,
    internal_tls: true,
    on_demand_tls: false,
    docker_upstream_discovery: false,
    php_fastcgi: true,
    basic_auth_bcrypt: true,
    regex_routing: true,
    require_target_file: false,
    h2_idle_timeout: false,
    write_timeout: true,
    read_header_timeout: false,
    read_body_timeout: false,
    grace_period: false,
    max_header_size_http1: true,
    trusted_strict: false,
    long_streams_on_reload: true,
    admin_api: false,
    experimental: true,
  },
};

export function normalizeProxyEngine(engine?: string | null): ProxyEngineName {
  const raw = (engine || '').trim().toLowerCase();
  if (raw === 'angie') return 'angie';
  if (raw === 'ferron') return 'ferron';
  return 'caddy';
}

export function resolveCapabilities(
  engine?: string | null,
  caps?: Partial<ProxyCapabilities> | null
): ProxyCapabilities {
  const key = normalizeProxyEngine(engine);
  return {
    ...DEFAULT_ENGINE_CAPABILITIES[key],
    ...(caps || {}),
  };
}

export interface ProxyOptions {
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

export type CaddyOptions = ProxyOptions;

export interface ProxyRoute {
  id: number;
  user_id?: number;
  username?: string;
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

export interface ProxyAffectedSite {
  site_id: string;
  label: string;
  detail?: string;
}

export interface ProxyPreflightIssue {
  capability: string;
  message: string;
  affected_sites?: ProxyAffectedSite[];
}

export interface ProxyPreflightCheck {
  name: string;
  ok: boolean;
  status?: string;
  detail?: string;
}

export interface ProxyPreflightResponse {
  ok: boolean;
  target: string;
  can_switch: boolean;
  requires_experimental_ack: boolean;
  hard_blocks?: ProxyPreflightIssue[];
  soft_warnings?: ProxyPreflightIssue[];
  checks?: ProxyPreflightCheck[];
  error?: string;
}

export interface ProxyLastSwitch {
  from?: string;
  to?: string;
  status?: string;
  ok?: boolean;
  rolled_back?: boolean;
  error?: string;
  message?: string;
  timestamp?: string;
  updated_at?: string;
}

export interface ProxyApplyRecord {
  generation?: string;
  driver?: string;
  status?: string;
  error?: string;
  updated_at?: string;
}

export interface ProxyEngineOptionInfo {
  id: ProxyEngineName | string;
  label: string;
  description?: string;
  experimental?: boolean;
}

export interface ProxyEngineStatusResponse {
  ok: boolean;
  engine: string;
  version?: string;
  running?: boolean;
  up?: boolean;
  status?: string;
  experimental?: boolean;
  capabilities?: Partial<ProxyCapabilities>;
  last_apply?: ProxyApplyRecord | null;
  last_switch?: ProxyLastSwitch | null;
  available_engines?: ProxyEngineOptionInfo[];
}

export interface ProxySwitchResponse {
  ok: boolean;
  from?: string;
  to?: string;
  status?: string;
  error?: string;
  last_switch?: ProxyLastSwitch | null;
}

export interface ProxyOverviewResponse {
  ok: boolean;
  engine?: string;
  container_name?: string;
  image?: string;
  running?: boolean;
  up?: boolean;
  status?: string;
  version?: string;
  experimental?: boolean;
  capabilities?: Partial<ProxyCapabilities>;
  last_apply?: ProxyApplyRecord | null;
  last_switch?: ProxyLastSwitch | null;
  live_config: string;
  root_compose: string;
  panel_domain: string;
  panel_enable_https: boolean;
  panel_enable_www: boolean;
  shared_mount_prefix: string;
  shared_volumes: string[];
  options?: Partial<ProxyOptions>;
  config?: Record<string, string>;
}

export interface ProxyOptionsResponse {
  ok: boolean;
  engine?: string;
  capabilities?: Partial<ProxyCapabilities>;
  options: ProxyOptions;
  profiles: Record<string, ProxyOptions>;
  cloudflare_ranges: string[];
  labels: Record<string, string>;
  rendered_config?: string;
}

export interface DomainProxyConfigResponse {
  ok?: boolean;
  engine?: string;
  domain?: string;
  port?: number;
  filename?: string;
  config?: string;
  rendered_config?: string;
  labels?: Record<string, string>;
  yaml?: string;
  snippet?: string;
}

const jsonInit = (body: unknown): RequestInit => ({
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify(body),
});

async function withRouteFallback<T>(
  primary: () => Promise<ApiResult<T>>,
  fallback: () => Promise<ApiResult<T>>
): Promise<ApiResult<T>> {
  const res = await primary();
  if (!res.ok && (res.status === 404 || res.status === 405)) {
    return fallback();
  }
  return res;
}

export const proxyApi = {
  proxyOverview: () =>
    withRouteFallback(
      () => requestJson<ProxyOverviewResponse>('/proxy/overview'),
      () => requestJson<ProxyOverviewResponse>('/caddy/overview')
    ),

  proxyEngineStatus: async (): Promise<ApiResult<ProxyEngineStatusResponse>> => {
    const res = await requestJson<ProxyEngineStatusResponse>('/proxy/engine');
    if (res.ok || (res.status !== 404 && res.status !== 405)) {
      return res;
    }
    const overview = await requestJson<ProxyOverviewResponse>('/caddy/overview');
    if (!overview.ok) {
      return {
        ok: false,
        status: overview.status,
        error: overview.error || res.error || 'Proxy engine status endpoint is unavailable',
      };
    }
    const ov = overview.data;
    const engine = normalizeProxyEngine(ov?.engine || 'caddy');
    return {
      ok: true,
      status: overview.status,
      engine,
      version: ov?.version,
      running: ov?.running ?? ov?.up,
      up: ov?.up ?? ov?.running,
      experimental: ov?.experimental ?? engine === 'ferron',
      capabilities: resolveCapabilities(engine, ov?.capabilities),
      last_apply: ov?.last_apply,
      last_switch: ov?.last_switch,
      data: {
        ok: true,
        engine,
        version: ov?.version,
        running: ov?.running ?? ov?.up,
        up: ov?.up ?? ov?.running,
        status: ov?.status,
        experimental: ov?.experimental ?? engine === 'ferron',
        capabilities: resolveCapabilities(engine, ov?.capabilities),
        last_apply: ov?.last_apply,
        last_switch: ov?.last_switch,
      },
    };
  },

  proxyPreflight: (target: ProxyEngineName | string) =>
    requestJson<ProxyPreflightResponse>('/proxy/preflight', jsonInit({ target })),

  proxySwitch: async (payload: {
    target: ProxyEngineName | string;
    experimental_ack?: boolean;
    yes?: boolean;
  }) => {
    const res = await requestJson<ProxySwitchResponse>('/proxy/switch', jsonInit(payload));
    if (res?.ok) notifyMutation();
    return res;
  },

  proxyOptions: () =>
    withRouteFallback(
      () => requestJson<ProxyOptionsResponse>('/proxy/options'),
      () => requestJson<ProxyOptionsResponse>('/caddy/options')
    ),

  proxyOptionsSave: async (options: ProxyOptions, apply: boolean) => {
    const res = await withRouteFallback(
      () =>
        requestJson<{ ok: boolean; applying: boolean; options: ProxyOptions }>(
          '/proxy/options',
          jsonInit({ ...options, apply })
        ),
      () =>
        requestJson<{ ok: boolean; applying: boolean; options: ProxyOptions }>(
          '/caddy/options',
          jsonInit({ ...options, apply })
        )
    );
    if (res?.ok) notifyMutation();
    return res;
  },

  proxySaveConfig: (
    payload:
      | string
      | {
          admin_api?: string;
          email: string;
          proxy_image?: string;
          caddy_image?: string;
        },
    _mode?: string
  ) => {
    const formValues =
      typeof payload === 'string'
        ? {
            admin_api: 'off',
            email: '',
            caddy_image: payload,
            proxy_image: payload,
          }
        : {
            admin_api: payload.admin_api || 'off',
            email: payload.email || '',
            caddy_image: payload.proxy_image || payload.caddy_image || '',
            proxy_image: payload.proxy_image || payload.caddy_image || '',
          };
    const init: RequestInit = {
      method: 'POST',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: toFormBody(formValues),
    };
    return withRouteFallback(
      () => requestJson('/proxy/config', init),
      () => requestJson('/caddy/config', init)
    );
  },

  proxyContainerAction: (action: 'start' | 'force_start' | 'restart' | 'stop' | 'reconcile_network') => {
    const init: RequestInit = {
      method: 'POST',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: toFormBody({ action }),
    };
    return withRouteFallback(
      () => requestJson('/proxy/container', init),
      () => requestJson('/caddy/container', init)
    );
  },

  proxyLogs: async (tail = 300) => {
    const res = await withRouteFallback(
      () => requestJson<{ ok: boolean; lines: string[]; engine?: string }>(`/proxy/logs?tail=${tail}`),
      () => requestJson<{ ok: boolean; lines: string[]; engine?: string }>(`/caddy/logs?tail=${tail}`)
    );
    return {
      ok: res.ok,
      status: res.status,
      error: res.error,
      engine: res.data?.engine,
      lines: res.data?.lines,
      logs: res.data?.lines ? res.data.lines.join('\n') : undefined,
    };
  },

  proxyRoutes: () =>
    withRouteFallback(
      () => requestJson<{ ok: boolean; routes: ProxyRoute[] }>('/proxy/routes'),
      () => requestJson<{ ok: boolean; routes: ProxyRoute[] }>('/caddy/routes')
    ),

  proxyRouteSave: async (route: Partial<ProxyRoute>) => {
    const res = await withRouteFallback(
      () =>
        requestJson<{ ok: boolean; route: ProxyRoute }>(
          route.id ? `/proxy/routes/${route.id}` : '/proxy/routes',
          jsonInit(route)
        ),
      () =>
        requestJson<{ ok: boolean; route: ProxyRoute }>(
          route.id ? `/caddy/routes/${route.id}` : '/caddy/routes',
          jsonInit(route)
        )
    );
    if (res?.ok) notifyMutation();
    return res;
  },

  proxyRouteToggle: async (id: number, enabled: boolean) => {
    const res = await withRouteFallback(
      () => requestJson<{ ok: boolean }>(`/proxy/routes/${id}/toggle`, jsonInit({ enabled })),
      () => requestJson<{ ok: boolean }>(`/caddy/routes/${id}/toggle`, jsonInit({ enabled }))
    );
    if (res?.ok) notifyMutation();
    return res;
  },

  proxyRouteDelete: async (id: number) => {
    const res = await withRouteFallback(
      () => requestJson<{ ok: boolean }>(`/proxy/routes/${id}/delete`, { method: 'POST' }),
      () => requestJson<{ ok: boolean }>(`/caddy/routes/${id}/delete`, { method: 'POST' })
    );
    if (res?.ok) notifyMutation();
    return res;
  },

  domainProxyConfig: async (appId: string, domainId: string | number) => {
    const encodedApp = encodeURIComponent(appId);
    const res = await withRouteFallback(
      () => requestJson<DomainProxyConfigResponse>(`/apps/${encodedApp}/domains/${domainId}/config`),
      () => requestJson<DomainProxyConfigResponse>(`/apps/${encodedApp}/domains/${domainId}/labels`)
    );
    return {
      ...res,
      engine: res.data?.engine,
      domain: res.data?.domain,
      port: res.data?.port,
      filename: res.data?.filename,
      labels: res.data?.labels,
      yaml: res.data?.yaml,
      configText: res.data?.rendered_config || res.data?.config || res.data?.yaml || res.data?.snippet,
      snippet: res.data?.rendered_config || res.data?.config || res.data?.yaml || res.data?.snippet,
    };
  },
};
