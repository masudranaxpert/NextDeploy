import React, { useMemo, useState } from 'react';
import { Download, RefreshCw, Terminal } from 'lucide-react';
import { parseProxyLogLine } from './proxyLogUtils';

interface ProxyLogsCardProps {
  lines: string[];
  loading: boolean;
  error: string | null;
  onRefresh: () => void;
}

export const ProxyLogsCard: React.FC<ProxyLogsCardProps> = ({
  lines,
  loading,
  error,
  onRefresh,
}) => {
  const [levelFilter, setLevelFilter] = useState<'all' | 'error' | 'warn' | 'info'>('all');
  const [showTimestamps, setShowTimestamps] = useState(true);

  const parsed = useMemo(() => lines.map(parseProxyLogLine), [lines]);
  const visible = useMemo(() => {
    if (levelFilter === 'all') return parsed;
    return parsed.filter((item) => item.level === levelFilter);
  }, [parsed, levelFilter]);

  const handleDownload = () => {
    const content = parsed.map((l) => l.raw).join('\n');
    const blob = new Blob([content], { type: 'text/plain;charset=utf-8' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = `proxy-logs-${new Date().toISOString().slice(0, 19).replace(/[:T]/g, '-')}.log`;
    a.click();
    URL.revokeObjectURL(url);
  };

  return (
    <div className="rounded-xl border border-white/[0.08] bg-[#111115] p-4 sm:p-5 space-y-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="flex items-center gap-2">
          <Terminal className="h-4 w-4 text-zinc-300" />
          <h2 className="text-sm font-semibold text-zinc-100">Reverse Proxy Logs</h2>
          <span className="font-mono text-[11px] text-zinc-400">({visible.length})</span>
        </div>

        <div className="flex flex-wrap items-center gap-2">
          <div className="flex items-center rounded-lg border border-white/[0.08] bg-[#09090b] p-0.5 text-xs">
            {(['all', 'error', 'warn', 'info'] as const).map((lvl) => (
              <button
                key={lvl}
                type="button"
                onClick={() => setLevelFilter(lvl)}
                className={`min-h-[30px] px-2.5 rounded-md capitalize font-medium cursor-pointer ${
                  levelFilter === lvl
                    ? 'bg-zinc-800 text-zinc-100'
                    : 'text-zinc-400 hover:text-zinc-200'
                }`}
              >
                {lvl}
              </button>
            ))}
          </div>

          <button
            type="button"
            onClick={() => setShowTimestamps((v) => !v)}
            className="min-h-[34px] px-2.5 rounded-lg border border-white/[0.08] bg-zinc-900 text-xs text-zinc-300 hover:bg-zinc-800 cursor-pointer"
          >
            {showTimestamps ? 'Hide time' : 'Show time'}
          </button>

          <button
            type="button"
            onClick={onRefresh}
            disabled={loading}
            className="inline-flex items-center gap-1.5 min-h-[34px] px-2.5 rounded-lg border border-white/[0.08] bg-zinc-900 text-xs text-zinc-200 hover:bg-zinc-800 disabled:opacity-50 cursor-pointer"
          >
            <RefreshCw className={`h-3.5 w-3.5 ${loading ? 'animate-spin' : ''}`} />
            Refresh
          </button>

          <button
            type="button"
            onClick={handleDownload}
            disabled={parsed.length === 0}
            className="inline-flex items-center gap-1.5 min-h-[34px] px-2.5 rounded-lg border border-white/[0.08] bg-zinc-900 text-xs text-zinc-200 hover:bg-zinc-800 disabled:opacity-40 cursor-pointer"
          >
            <Download className="h-3.5 w-3.5" />
            Export
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
      ) : loading && lines.length === 0 ? (
        <div className="h-44 rounded-lg border border-white/[0.06] bg-[#09090b] animate-pulse" />
      ) : visible.length === 0 ? (
        <div className="rounded-lg border border-white/[0.07] bg-[#09090b] p-6 text-center text-xs text-zinc-400">
          No log entries match the selected filter.
        </div>
      ) : (
        <div className="max-h-80 overflow-auto rounded-lg border border-white/[0.07] bg-[#09090b] p-3 font-mono text-[11px] space-y-1">
          {visible.map((entry, idx) => (
            <div key={idx} className="flex items-start gap-2 leading-relaxed">
              {showTimestamps && entry.timestamp && (
                <span className="text-zinc-400 shrink-0 tabular-nums">{entry.timestamp}</span>
              )}
              <span
                className={`uppercase text-[10px] font-semibold shrink-0 px-1.5 py-0.5 rounded ${
                  entry.level === 'error'
                    ? 'bg-rose-500/15 text-rose-200'
                    : entry.level === 'warn'
                    ? 'bg-amber-500/15 text-amber-200'
                    : 'bg-zinc-800 text-zinc-300'
                }`}
              >
                {entry.level}
              </span>
              <span className="text-zinc-200 break-all">
                {entry.logger ? `[${entry.logger}] ` : ''}
                {entry.message}
                {entry.meta ? ` (${entry.meta})` : ''}
              </span>
            </div>
          ))}
        </div>
      )}
    </div>
  );
};
