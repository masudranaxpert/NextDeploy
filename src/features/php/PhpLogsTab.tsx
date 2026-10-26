import React, { useCallback, useEffect, useRef, useState, useMemo } from 'react';
import { RefreshCw, Search, Clock, SlidersHorizontal, Copy, Check, ChevronDown } from 'lucide-react';
import { AppStack } from '../../types/nextdeploy';
import { backendApi } from '../../lib/backendApi';
import { copyToClipboard } from '../../lib/clipboard';
import { toast } from 'sonner';

interface PhpLogsTabProps {
  app: AppStack;
}

function getServicePill(name: string) {
  const s = (name || '').toLowerCase();
  if (s.includes('fpm') || s.includes('runtime')) {
    return {
      dotColor: 'bg-sky-400',
      badgeCls: 'bg-[#0a1e2e] border border-sky-800/60 text-sky-400',
    };
  }
  return {
    dotColor: 'bg-emerald-400',
    badgeCls: 'bg-[#241708] border border-orange-800/60 text-orange-400',
  };
}

function getLevelConfig(level: string) {
  const l = (level || 'info').toLowerCase();
  if (l === 'error' || l === 'err' || l === 'fatal' || l === 'panic') {
    return {
      label: 'ERR',
      borderCls: 'border-l-rose-500',
      bgCls: 'bg-rose-500/[0.04]',
      badgeCls: 'bg-[#2b0d14] border border-rose-600/70 text-rose-400',
      textCls: 'text-rose-300',
    };
  }
  if (l === 'warn' || l === 'warning') {
    return {
      label: 'WARN',
      borderCls: 'border-l-amber-500',
      bgCls: 'bg-amber-500/[0.03]',
      badgeCls: 'bg-[#291e0a] border border-amber-600/70 text-amber-300',
      textCls: 'text-amber-300',
    };
  }
  if (l === 'debug' || l === 'dbg') {
    return {
      label: 'DBG',
      borderCls: 'border-l-zinc-600',
      bgCls: '',
      badgeCls: 'bg-zinc-800 border border-zinc-700 text-zinc-400',
      textCls: 'text-zinc-400',
    };
  }
  return {
    label: 'INFO',
    borderCls: 'border-l-sky-500',
    bgCls: '',
    badgeCls: 'bg-[#0b1c2b] border border-sky-600/70 text-sky-400',
    textCls: 'text-zinc-200',
  };
}

function formatTimestamp(ts: string): string {
  if (!ts) return '';
  const timeRegexMatch = ts.match(/(\d{2}:\d{2}:\d{2}(?:\.\d{3})?)/);
  if (timeRegexMatch) {
    const raw = timeRegexMatch[1];
    return raw.includes('.') ? raw : `${raw}.000`;
  }
  const date = new Date(ts);
  if (!isNaN(date.getTime())) {
    const h = String(date.getHours()).padStart(2, '0');
    const m = String(date.getMinutes()).padStart(2, '0');
    const s = String(date.getSeconds()).padStart(2, '0');
    const ms = String(date.getMilliseconds()).padStart(3, '0');
    return `${h}:${m}:${s}.${ms}`;
  }
  return ts.slice(11, 23) || ts;
}

export const PhpLogsTab: React.FC<PhpLogsTabProps> = ({ app }) => {
  const [source, setSource] = useState<'php' | 'runtime'>('php');
  const [lines, setLines] = useState(200);
  const [live, setLive] = useState(false);
  const [text, setText] = useState('');
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);

  const [logLevelFilter, setLogLevelFilter] = useState<'all' | 'info' | 'warn' | 'error' | 'debug'>('all');
  const [logSearch, setLogSearch] = useState('');
  const [logShowTimestamps, setLogShowTimestamps] = useState(true);
  const [logWordWrap, setLogWordWrap] = useState(true);
  const [copiedId, setCopiedId] = useState<string | null>(null);

  const load = useCallback(async () => {
    setBusy(true);
    const res = await backendApi.phpLogs(app.id, source, lines);
    setBusy(false);
    if (res.ok && res.data) {
      setText(res.data.text);
      setError('');
    } else {
      setError(res.error || 'Could not load logs');
    }
  }, [app.id, source, lines]);

  useEffect(() => {
    void load();
  }, [load]);

  useEffect(() => {
    if (!live) return;
    const t = setInterval(() => void load(), 3000);
    return () => clearInterval(t);
  }, [live, load]);

  const parsedEntries = useMemo(() => {
    if (!text) return [];
    const rawLines = text.split(/\r?\n/).filter((l) => l.trim().length > 0);
    return rawLines.map((line, idx) => {
      let ts = '';
      let msg = line;
      let level: 'info' | 'warn' | 'error' | 'debug' = 'info';

      // Check PHP error log timestamp: [26-Oct-2026 02:14:28 UTC] ...
      const phpTsMatch = line.match(/^\[([^\]]+)\]\s*(.*)$/);
      if (phpTsMatch) {
        ts = phpTsMatch[1];
        msg = phpTsMatch[2];
      }

      const lower = msg.toLowerCase();
      if (lower.includes('fatal') || lower.includes('error') || lower.includes('exception') || lower.includes('parse')) {
        level = 'error';
      } else if (lower.includes('warning') || lower.includes('notice') || lower.includes('deprecated') || lower.includes('warn')) {
        level = 'warn';
      } else if (lower.includes('debug') || lower.includes('trace')) {
        level = 'debug';
      }

      return {
        id: `php-log-${idx}`,
        timestamp: ts,
        container: source === 'php' ? 'php' : 'fpm',
        level,
        message: msg,
      };
    });
  }, [text, source]);

  const filteredLogs = useMemo(() => {
    return parsedEntries.filter((l) => {
      if (logLevelFilter !== 'all' && l.level !== logLevelFilter) return false;
      if (logSearch && !l.message.toLowerCase().includes(logSearch.toLowerCase())) {
        return false;
      }
      return true;
    });
  }, [parsedEntries, logLevelFilter, logSearch]);

  const logCounts = useMemo(() => {
    return {
      all: parsedEntries.length,
      info: parsedEntries.filter((l) => l.level === 'info').length,
      warn: parsedEntries.filter((l) => l.level === 'warn').length,
      error: parsedEntries.filter((l) => l.level === 'error').length,
      debug: parsedEntries.filter((l) => l.level === 'debug').length,
    };
  }, [parsedEntries]);

  const handleCopyLog = (id: string, fullText: string) => {
    void copyToClipboard(fullText);
    setCopiedId(id);
    toast.success('Copied log line');
    setTimeout(() => {
      setCopiedId((current) => (current === id ? null : current));
    }, 1500);
  };

  return (
    <div className="space-y-3">
      {/* Top Filter & Toolbar Panel */}
      <div className="rounded-xl border border-zinc-800/80 bg-[#0d0d11] p-3 space-y-3">
        {/* Row 1: Source Selection & Stream Controls */}
        <div className="flex flex-wrap items-center gap-2">
          {/* Source Selector */}
          <div className="relative">
            <select
              value={source}
              onChange={(e) => setSource(e.target.value as 'php' | 'runtime')}
              className="h-8 appearance-none rounded-lg border border-zinc-800 bg-zinc-900/90 pl-3 pr-7 font-mono text-xs text-zinc-300 focus:border-zinc-700 focus:outline-none cursor-pointer"
            >
              <option value="php">PHP errors</option>
              <option value="runtime">PHP-FPM output</option>
            </select>
            <ChevronDown className="pointer-events-none absolute right-2 top-2.5 h-3.5 w-3.5 text-zinc-500" />
          </div>

          {/* Line Limit */}
          <div className="relative">
            <select
              value={lines}
              onChange={(e) => setLines(Number(e.target.value))}
              className="h-8 appearance-none rounded-lg border border-zinc-800 bg-zinc-900/90 pl-3 pr-7 font-mono text-xs text-zinc-300 focus:border-zinc-700 focus:outline-none cursor-pointer"
            >
              {[100, 200, 500, 1000, 2000].map((n) => (
                <option key={n} value={n}>
                  {n} lines
                </option>
              ))}
            </select>
            <ChevronDown className="pointer-events-none absolute right-2 top-2.5 h-3.5 w-3.5 text-zinc-500" />
          </div>

          {/* Live Streaming Toggle */}
          <button
            type="button"
            onClick={() => setLive((p) => !p)}
            className={`inline-flex h-8 items-center gap-1.5 rounded-lg border px-3 text-xs font-medium cursor-pointer transition-colors ${
              live
                ? 'border-emerald-500/30 bg-emerald-950/40 text-emerald-400 shadow-sm'
                : 'border-zinc-800 bg-zinc-900/60 text-zinc-400 hover:text-zinc-200'
            }`}
          >
            <span
              className={`h-2 w-2 rounded-full ${
                live ? 'bg-emerald-400 animate-pulse' : 'bg-zinc-600'
              }`}
            />
            {live ? 'Streaming' : 'Paused'}
          </button>

          {/* Manual Refresh */}
          <button
            type="button"
            onClick={() => void load()}
            disabled={busy}
            className="inline-flex h-8 items-center gap-1.5 rounded-lg border border-zinc-800 bg-zinc-900/60 px-3 text-xs font-medium text-zinc-300 hover:bg-zinc-800 hover:text-white transition-colors cursor-pointer disabled:opacity-50"
          >
            <RefreshCw className={`h-3.5 w-3.5 ${busy ? 'animate-spin' : ''}`} />
            Refresh
          </button>
        </div>

        {/* Row 2: Severity Filter Segment */}
        <div className="flex flex-wrap items-center gap-1 rounded-lg border border-zinc-800/80 bg-zinc-950/80 p-1 font-mono text-xs w-fit">
          {(
            [
              {
                id: 'all',
                label: 'ALL',
                count: logCounts.all,
                activeCls: 'bg-zinc-800 text-white font-semibold',
                textCls: 'text-zinc-300',
              },
              {
                id: 'info',
                label: 'INFO',
                count: logCounts.info,
                activeCls: 'bg-sky-950/80 border border-sky-800/80 text-sky-300 font-semibold',
                textCls: 'text-sky-400',
              },
              {
                id: 'warn',
                label: 'WARN',
                count: logCounts.warn,
                activeCls: 'bg-amber-950/80 border border-amber-800/80 text-amber-200 font-semibold',
                textCls: 'text-amber-400',
              },
              {
                id: 'error',
                label: 'ERR',
                count: logCounts.error,
                activeCls: 'bg-rose-950/80 border border-rose-800/80 text-rose-200 font-semibold',
                textCls: 'text-rose-400',
              },
              {
                id: 'debug',
                label: 'DBG',
                count: logCounts.debug,
                activeCls: 'bg-zinc-800 border border-zinc-700 text-zinc-200 font-semibold',
                textCls: 'text-zinc-400',
              },
            ] as const
          ).map((lvl) => {
            const isSelected = logLevelFilter === lvl.id;
            return (
              <button
                key={lvl.id}
                type="button"
                onClick={() => setLogLevelFilter(lvl.id)}
                className={`inline-flex items-center gap-1.5 rounded-md px-2.5 py-1 transition-all cursor-pointer ${
                  isSelected
                    ? lvl.activeCls
                    : `${lvl.textCls} hover:bg-zinc-900/60`
                }`}
              >
                <span>{lvl.label}</span>
                <span className="text-[11px] opacity-75">{lvl.count}</span>
              </button>
            );
          })}
        </div>

        {/* Row 3: Search & Display Toggles */}
        <div className="flex items-center gap-2">
          <div className="relative flex-1">
            <Search className="pointer-events-none absolute left-3 top-2.5 h-3.5 w-3.5 text-zinc-500" />
            <input
              type="text"
              value={logSearch}
              onChange={(e) => setLogSearch(e.target.value)}
              placeholder="Filter / regex..."
              className="h-8 w-full rounded-lg border border-zinc-800/90 bg-zinc-950 pl-9 pr-3 font-mono text-xs text-zinc-100 placeholder-zinc-500 focus:border-zinc-700 focus:outline-none"
            />
          </div>

          <button
            type="button"
            onClick={() => setLogShowTimestamps((p) => !p)}
            className={`inline-flex h-8 items-center gap-1.5 rounded-lg border px-2.5 text-xs font-mono cursor-pointer transition-colors ${
              logShowTimestamps
                ? 'border-amber-500/40 bg-amber-500/10 text-amber-300'
                : 'border-zinc-800 bg-zinc-900/60 text-zinc-400 hover:text-zinc-200'
            }`}
            title="Toggle timestamps"
          >
            <Clock className="h-3.5 w-3.5" />
            <span>TS</span>
          </button>

          <button
            type="button"
            onClick={() => setLogWordWrap((p) => !p)}
            className={`inline-flex h-8 items-center justify-center rounded-lg border px-2.5 text-xs font-mono cursor-pointer transition-colors ${
              logWordWrap
                ? 'border-amber-500/40 bg-amber-500/10 text-amber-300'
                : 'border-zinc-800 bg-zinc-900/60 text-zinc-400 hover:text-zinc-200'
            }`}
            title="Toggle word wrap"
          >
            <SlidersHorizontal className="h-3.5 w-3.5" />
          </button>
        </div>
      </div>

      {error && (
        <div className="rounded-lg border border-rose-500/30 bg-rose-500/10 p-3 text-xs text-rose-300 font-mono">
          {error}
        </div>
      )}

      {/* Log Entries Stream Box */}
      <div className="overflow-hidden rounded-xl border border-zinc-800/80 bg-[#09090d]">
        <div className="divide-y divide-zinc-800/50 max-h-[560px] overflow-y-auto">
          {filteredLogs.length === 0 ? (
            <div className="py-12 text-center text-xs text-zinc-500 font-mono">
              {source === 'php' ? 'No PHP errors logged yet.' : 'No runtime output recorded.'}
            </div>
          ) : (
            filteredLogs.map((l, index) => {
              const svcStyle = getServicePill(l.container);
              const lvlStyle = getLevelConfig(l.level);
              const idxStr = `#${String(index + 1).padStart(2, '0')}`;
              const timeStr = formatTimestamp(l.timestamp);
              const isCopied = copiedId === l.id;

              return (
                <div
                  key={l.id}
                  className={`group relative px-3.5 py-2.5 border-l-[3px] transition-colors hover:bg-zinc-900/40 ${lvlStyle.borderCls} ${lvlStyle.bgCls}`}
                >
                  {/* Top Meta Header: Index + Time on Left, Badges on Right */}
                  <div className="flex items-center justify-between gap-2">
                    <div className="flex items-center gap-2 font-mono text-xs">
                      <span className="text-zinc-500 select-none font-medium">{idxStr}</span>
                      {logShowTimestamps && timeStr && (
                        <span className="text-zinc-400 select-none">{timeStr}</span>
                      )}
                    </div>

                    <div className="flex items-center gap-1.5">
                      {/* Service Badge */}
                      <span
                        className={`rounded px-1.5 py-0.5 font-mono text-[10px] font-medium leading-none ${svcStyle.badgeCls}`}
                      >
                        {l.container}
                      </span>

                      {/* Severity Badge */}
                      <span
                        className={`rounded px-1.5 py-0.5 font-mono text-[10px] font-bold leading-none ${lvlStyle.badgeCls}`}
                      >
                        {lvlStyle.label}
                      </span>

                      {/* Copy Log Line Button */}
                      <button
                        type="button"
                        onClick={() => handleCopyLog(l.id, `${l.timestamp ? `[${l.timestamp}] ` : ''}[${l.level.toUpperCase()}] ${l.message}`)}
                        className="rounded p-1 text-zinc-500 hover:bg-zinc-800 hover:text-zinc-200 transition-colors cursor-pointer"
                        title="Copy log line"
                      >
                        {isCopied ? (
                          <Check className="h-3.5 w-3.5 text-emerald-400" />
                        ) : (
                          <Copy className="h-3.5 w-3.5" />
                        )}
                      </button>
                    </div>
                  </div>

                  {/* Bottom Log Message Body */}
                  <div
                    className={`mt-1.5 font-mono text-xs sm:text-[13px] leading-relaxed ${
                      logWordWrap ? 'break-all whitespace-pre-wrap' : 'truncate whitespace-pre'
                    } ${lvlStyle.textCls}`}
                  >
                    {l.message}
                  </div>
                </div>
              );
            })
          )}
        </div>
      </div>
    </div>
  );
};
