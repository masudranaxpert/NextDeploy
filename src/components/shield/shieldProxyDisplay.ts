import { normalizeProxyEngine, type ProxyEngineName } from '../../lib/api/proxyApi';
import type { ShieldNode } from '../../types/shield';

export interface ShieldProxyDisplay {
  engine: ProxyEngineName;
  version: string;
  running: boolean | undefined;
}

export function resolveShieldProxyDisplay(
  node: Pick<ShieldNode, 'proxy_engine' | 'proxy_version' | 'proxy_running' | 'caddy_version'>
): ShieldProxyDisplay {
  const engine = normalizeProxyEngine(node.proxy_engine || 'caddy');
  const version = (node.proxy_version || node.caddy_version || '').trim();
  const running = typeof node.proxy_running === 'boolean' ? node.proxy_running : undefined;
  return { engine, version, running };
}

export function formatProxyEngineLabel(engine?: string | null): string {
  const normalized = normalizeProxyEngine(engine);
  if (normalized === 'angie') return 'Angie';
  if (normalized === 'ferron') return 'Ferron';
  return 'Caddy';
}
