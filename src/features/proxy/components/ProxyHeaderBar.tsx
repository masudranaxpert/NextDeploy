import React from 'react';
import { Globe, CheckCircle2, AlertTriangle, Loader2 } from 'lucide-react';
import { ProxyEngineBadge } from '../../../components/proxy/ProxyEngineBadge';
import type { ProxyApplyRecord } from '../../../lib/api/proxyApi';

export interface ApplyBannerState {
  status: 'idle' | 'running' | 'success' | 'error';
  message: string;
  updatedAt?: string;
}

interface ProxyHeaderBarProps {
  engine: string;
  version?: string;
  running: boolean;
  experimental?: boolean;
  lastApply?: ProxyApplyRecord | null;
  applyBanner: ApplyBannerState;
}

export const ProxyHeaderBar: React.FC<ProxyHeaderBarProps> = ({
  engine,
  version,
  running,
  experimental,
  lastApply,
  applyBanner,
}) => {
  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex flex-wrap items-center gap-2.5">
          <div className="flex h-8 w-8 items-center justify-center rounded-lg border border-white/[0.08] bg-zinc-900">
            <Globe className="h-4 w-4 text-emerald-300" />
          </div>
          <div>
            <div className="flex flex-wrap items-center gap-2">
              <h1 className="text-base font-semibold tracking-tight text-zinc-100">
                Reverse Proxy &amp; TLS
              </h1>
              <ProxyEngineBadge
                engine={engine}
                version={version}
                running={running}
                experimental={experimental}
                size="sm"
              />
            </div>
          </div>
        </div>

        {lastApply?.status && (
          <div className="rounded-lg border border-white/[0.08] bg-[#111115] px-3 py-1.5 font-mono text-[11px] text-zinc-300">
            <span className="text-zinc-400">Last apply: </span>
            <span
              className={
                lastApply.status === 'failed' || lastApply.error
                  ? 'text-rose-200'
                  : 'text-emerald-300'
              }
            >
              {lastApply.status}
            </span>
            {lastApply.generation && (
              <span className="text-zinc-400"> ({lastApply.generation.slice(0, 8)})</span>
            )}
          </div>
        )}
      </div>

      {applyBanner.status !== 'idle' && (
        <div
          className={`rounded-lg border px-3.5 py-2.5 text-xs flex items-start gap-2.5 ${
            applyBanner.status === 'error'
              ? 'border-rose-500/30 bg-rose-500/10 text-rose-200'
              : applyBanner.status === 'success'
              ? 'border-emerald-500/25 bg-emerald-500/10 text-emerald-200'
              : 'border-cyan-500/25 bg-cyan-500/10 text-cyan-200'
          }`}
        >
          {applyBanner.status === 'running' ? (
            <Loader2 className="h-4 w-4 animate-spin text-cyan-300 shrink-0 mt-0.5" />
          ) : applyBanner.status === 'success' ? (
            <CheckCircle2 className="h-4 w-4 text-emerald-300 shrink-0 mt-0.5" />
          ) : (
            <AlertTriangle className="h-4 w-4 text-rose-300 shrink-0 mt-0.5" />
          )}
          <div className="min-w-0 flex-1">
            <div className="font-medium break-words">{applyBanner.message}</div>
            {applyBanner.updatedAt && (
              <div className="font-mono text-[11px] opacity-85 mt-0.5">{applyBanner.updatedAt}</div>
            )}
          </div>
        </div>
      )}
    </div>
  );
};
