import React, { useState } from 'react';
import { Check, Code2, Copy, RefreshCw } from 'lucide-react';
import { copyToClipboard } from '../../../lib/clipboard';
import { toast } from 'sonner';
import { normalizeProxyEngine } from '../../../lib/api/proxyApi';

interface ProxyConfigViewerCardProps {
  engine: string;
  rootCompose: string;
  liveConfig: string;
  loading: boolean;
  error: string | null;
  onRefresh: () => void;
}

export const ProxyConfigViewerCard: React.FC<ProxyConfigViewerCardProps> = ({
  engine,
  rootCompose,
  liveConfig,
  loading,
  error,
  onRefresh,
}) => {
  const [tab, setTab] = useState<'live_config' | 'root_compose'>('live_config');
  const [copied, setCopied] = useState(false);

  const normalized = normalizeProxyEngine(engine);
  const liveTabLabel =
    normalized === 'angie'
      ? 'Live angie.conf'
      : normalized === 'ferron'
      ? 'Live ferron.yaml'
      : 'Live Caddy Config';

  const activeText = tab === 'live_config' ? liveConfig : rootCompose;

  const handleCopy = async () => {
    if (!activeText) return;
    const ok = await copyToClipboard(activeText);
    if (ok) {
      setCopied(true);
      toast.success('Copied proxy configuration');
      window.setTimeout(() => setCopied(false), 1800);
    }
  };

  return (
    <div className="rounded-xl border border-white/[0.08] bg-[#111115] p-4 sm:p-5 space-y-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="flex items-center gap-2">
          <Code2 className="h-4 w-4 text-zinc-300" />
          <h2 className="text-sm font-semibold text-zinc-100">Generated Runtime Files</h2>
        </div>

        <div className="flex items-center gap-2">
          <div className="flex items-center rounded-lg border border-white/[0.08] bg-[#09090b] p-0.5 text-xs">
            <button
              type="button"
              onClick={() => setTab('live_config')}
              className={`min-h-[32px] px-2.5 rounded-md font-medium transition-colors cursor-pointer ${
                tab === 'live_config'
                  ? 'bg-zinc-800 text-zinc-100'
                  : 'text-zinc-400 hover:text-zinc-200'
              }`}
            >
              {liveTabLabel}
            </button>
            <button
              type="button"
              onClick={() => setTab('root_compose')}
              className={`min-h-[32px] px-2.5 rounded-md font-medium transition-colors cursor-pointer ${
                tab === 'root_compose'
                  ? 'bg-zinc-800 text-zinc-100'
                  : 'text-zinc-400 hover:text-zinc-200'
              }`}
            >
              Root Compose
            </button>
          </div>

          <button
            type="button"
            onClick={onRefresh}
            disabled={loading}
            aria-label="Refresh generated config"
            className="h-8 w-8 rounded-lg border border-white/[0.08] bg-zinc-900 flex items-center justify-center text-zinc-400 hover:text-zinc-100 hover:bg-zinc-800 disabled:opacity-50 cursor-pointer"
          >
            <RefreshCw className={`h-3.5 w-3.5 ${loading ? 'animate-spin' : ''}`} />
          </button>

          <button
            type="button"
            onClick={handleCopy}
            disabled={!activeText}
            aria-label="Copy config"
            className="h-8 w-8 rounded-lg border border-white/[0.08] bg-zinc-900 flex items-center justify-center text-zinc-400 hover:text-zinc-100 hover:bg-zinc-800 disabled:opacity-40 cursor-pointer"
          >
            {copied ? <Check className="h-3.5 w-3.5 text-emerald-300" /> : <Copy className="h-3.5 w-3.5" />}
          </button>
        </div>
      </div>

      {error ? (
        <div className="rounded-lg border border-rose-500/30 bg-rose-500/10 p-3 flex items-center justify-between gap-2 text-xs text-rose-200">
          <span>{error}</span>
          <button
            type="button"
            onClick={onRefresh}
            className="px-2.5 py-1 rounded border border-rose-500/30 bg-zinc-950 text-rose-200 hover:bg-zinc-900 cursor-pointer"
          >
            Retry
          </button>
        </div>
      ) : loading && !activeText ? (
        <div className="h-40 rounded-lg border border-white/[0.06] bg-[#09090b] animate-pulse" />
      ) : activeText ? (
        <pre className="max-h-80 overflow-auto rounded-lg border border-white/[0.07] bg-[#09090b] p-3.5 font-mono text-[11px] leading-relaxed text-zinc-300 whitespace-pre">
          {activeText}
        </pre>
      ) : (
        <div className="rounded-lg border border-white/[0.07] bg-[#09090b] p-6 text-center text-xs text-zinc-400">
          No {tab === 'live_config' ? 'live proxy config' : 'root compose file'} returned by the server yet.
        </div>
      )}
    </div>
  );
};
