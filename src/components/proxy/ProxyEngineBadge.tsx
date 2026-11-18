import React from 'react';
import { normalizeProxyEngine, type ProxyEngineName } from '../../lib/api/proxyApi';

interface ProxyEngineBadgeProps {
  engine?: string | null;
  version?: string | null;
  running?: boolean | null;
  experimental?: boolean;
  size?: 'xs' | 'sm';
  className?: string;
}

const ENGINE_META: Record<
  ProxyEngineName,
  {
    label: string;
    ringClass: string;
    dotClass: string;
  }
> = {
  caddy: {
    label: 'Caddy',
    ringClass: 'bg-emerald-500/10 text-emerald-300 ring-1 ring-emerald-500/20',
    dotClass: 'bg-emerald-400',
  },
  angie: {
    label: 'Angie',
    ringClass: 'bg-cyan-500/10 text-cyan-300 ring-1 ring-cyan-500/20',
    dotClass: 'bg-cyan-400',
  },
  ferron: {
    label: 'Ferron',
    ringClass: 'bg-amber-500/10 text-amber-200 ring-1 ring-amber-500/25',
    dotClass: 'bg-amber-300',
  },
};

export const ProxyEngineBadge: React.FC<ProxyEngineBadgeProps> = ({
  engine,
  version,
  running,
  experimental,
  size = 'xs',
  className = '',
}) => {
  const normalized = normalizeProxyEngine(engine);
  const meta = ENGINE_META[normalized];
  const isExperimental = experimental ?? normalized === 'ferron';
  const isDown = running === false;

  const sizeClass =
    size === 'sm'
      ? 'px-2.5 py-1 text-xs gap-1.5 rounded-md'
      : 'px-2 py-0.5 text-[11px] gap-1.5 rounded';

  const toneClass = isDown
    ? 'bg-rose-500/10 text-rose-200 ring-1 ring-rose-500/25'
    : meta.ringClass;

  const dotClass = isDown ? 'bg-rose-400' : meta.dotClass;

  return (
    <span
      className={`inline-flex items-center font-mono font-medium shrink-0 ${sizeClass} ${toneClass} ${className}`}
      title={`${meta.label}${isExperimental ? ' (experimental)' : ''}${version ? ` ${version}` : ''}${isDown ? ' - stopped' : ''}`}
    >
      <span className={`h-1.5 w-1.5 rounded-full shrink-0 ${dotClass}`} />
      <span>{meta.label}</span>
      {isExperimental && (
        <span className={isDown ? 'text-rose-200/90' : 'text-amber-200/90'}>
          · experimental
        </span>
      )}
      {version && (
        <span className="text-zinc-300/85 tabular-nums">{version}</span>
      )}
      {isDown && <span className="text-rose-200">· down</span>}
    </span>
  );
};
