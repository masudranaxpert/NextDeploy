import React, { useState } from 'react';
import { Search, Clock, WrapText, Copy } from 'lucide-react';
import { toast } from 'sonner';
import { AppStack, LogEntry } from '../../../types/nextdeploy';
import { copyToClipboard } from '../../../lib/clipboard';

interface ProjectLogsTabProps {
  app: AppStack;
  logContainerFilter: string;
  setLogContainerFilter: (filter: string) => void;
  logLevelFilter: 'all' | 'info' | 'warn' | 'error' | 'debug';
  setLogLevelFilter: (lvl: 'all' | 'info' | 'warn' | 'error' | 'debug') => void;
  logSearch: string;
  setLogSearch: (s: string) => void;
  logLiveTail: boolean;
  setLogLiveTail: React.Dispatch<React.SetStateAction<boolean>>;
  logTimeRange: 'live' | '15m' | '1h' | '24h';
  setLogTimeRange: (range: 'live' | '15m' | '1h' | '24h') => void;
  filteredLogs: LogEntry[];
  logCounts: Record<'all' | 'info' | 'warn' | 'error' | 'debug', number>;
  renderLogMessage: (msg: string) => React.ReactNode;
}

export const ProjectLogsTab: React.FC<ProjectLogsTabProps> = ({
  app,
  logContainerFilter,
  setLogContainerFilter,
  logLevelFilter,
  setLogLevelFilter,
  logSearch,
  setLogSearch,
  logLiveTail,
  setLogLiveTail,
  logTimeRange,
  setLogTimeRange,
  filteredLogs,
  logCounts,
  renderLogMessage,
}) => {
  const [logShowTimestamps, setLogShowTimestamps] = useState(true);
  const [logWordWrap, setLogWordWrap] = useState(true);

  return (
    <div className="space-y-3">
      {/* Multi-Dimensional Filter Toolbar */}
      <div className="flex flex-wrap items-center justify-between gap-2 rounded-xl border border-white/[0.07] bg-[#111115]/90 p-2.5">
        <div className="flex flex-wrap items-center gap-2">
          {/* Service Pills */}
          <div className="flex items-center gap-0.5 rounded-lg border border-white/[0.07] bg-[#09090b] p-0.5">
            <button
              type="button"
              onClick={() => setLogContainerFilter('all')}
              className={`rounded-md px-2.5 py-1 text-xs font-medium cursor-pointer ${
                logContainerFilter === 'all'
                  ? 'bg-zinc-800 text-zinc-100'
                  : 'text-zinc-400 hover:text-zinc-200'
              }`}
            >
              All Services
            </button>
            {app.services.map((s) => (
              <button
                key={s.id}
                type="button"
                onClick={() => setLogContainerFilter(s.service)}
                className={`inline-flex items-center gap-1.5 rounded-md px-2.5 py-1 font-mono text-xs cursor-pointer ${
                  logContainerFilter === s.service
                    ? 'bg-zinc-800 text-zinc-100'
                    : 'text-zinc-400 hover:text-zinc-200'
                }`}
              >
                <span
                  className={`h-1.5 w-1.5 rounded-full ${
                    s.state === 'running' ? 'bg-emerald-400' : 'bg-zinc-500'
                  }`}
                />
                {s.service}
              </button>
            ))}
          </div>

          {/* Severity Filter Pills */}
          <div className="flex items-center gap-0.5 rounded-lg border border-white/[0.07] bg-[#09090b] p-0.5 font-mono text-[11px]">
            {(
              [
                { id: 'all', label: 'ALL', count: logCounts.all, color: 'text-zinc-300' },
                { id: 'info', label: 'INFO', count: logCounts.info, color: 'text-sky-400' },
                { id: 'warn', label: 'WARN', count: logCounts.warn, color: 'text-amber-400' },
                { id: 'error', label: 'ERR', count: logCounts.error, color: 'text-rose-400' },
                { id: 'debug', label: 'DBG', count: logCounts.debug, color: 'text-zinc-400' },
              ] as const
            ).map((lvl) => (
              <button
                key={lvl.id}
                type="button"
                onClick={() => setLogLevelFilter(lvl.id)}
                className={`inline-flex items-center gap-1 rounded-md px-2 py-1 transition-colors cursor-pointer ${
                  logLevelFilter === lvl.id
                    ? 'bg-zinc-800 font-semibold text-white'
                    : `${lvl.color} hover:bg-white/[0.04]`
                }`}
              >
                <span>{lvl.label}</span>
                <span className="text-[10px] opacity-65">{lvl.count}</span>
              </button>
            ))}
          </div>

          {/* Time Range Selector */}
          <select
            value={logTimeRange}
            onChange={(e) =>
              setLogTimeRange(e.target.value as 'live' | '15m' | '1h' | '24h')
            }
            className="h-7 rounded-lg border border-white/[0.07] bg-[#09090b] px-2 font-mono text-xs text-zinc-300 focus:outline-none"
          >
            <option value="live">Live Tail</option>
            <option value="15m">Last 15m</option>
            <option value="1h">Last 1h</option>
            <option value="24h">Last 24h</option>
          </select>
        </div>

        {/* Right Search & Stream Controls */}
        <div className="flex flex-wrap items-center gap-1.5">
          <div className="relative">
            <Search className="pointer-events-none absolute left-2.5 top-2 h-3.5 w-3.5 text-zinc-500" />
            <input
              type="text"
              value={logSearch}
              onChange={(e) => setLogSearch(e.target.value)}
              placeholder="Filter / regex..."
              className="h-7 w-44 rounded-lg border border-white/[0.08] bg-[#09090b] pl-8 pr-2.5 font-mono text-xs text-zinc-100 placeholder-zinc-500 focus:border-orange-500 focus:outline-none"
            />
          </div>

          <button
            type="button"
            onClick={() => setLogShowTimestamps((p) => !p)}
            className={`inline-flex h-7 items-center gap-1 rounded-lg border px-2 text-[11px] font-mono cursor-pointer ${
              logShowTimestamps
                ? 'border-orange-500/40 bg-orange-500/10 text-orange-300'
                : 'border-white/[0.08] bg-zinc-900 text-zinc-400'
            }`}
            title="Toggle timestamps"
          >
            <Clock className="h-3 w-3" />
            TS
          </button>

          <button
            type="button"
            onClick={() => setLogWordWrap((p) => !p)}
            className={`inline-flex h-7 items-center gap-1 rounded-lg border px-2 text-[11px] font-mono cursor-pointer ${
              logWordWrap
                ? 'border-orange-500/40 bg-orange-500/10 text-orange-300'
                : 'border-white/[0.08] bg-zinc-900 text-zinc-400'
            }`}
            title="Toggle word wrap"
          >
            <WrapText className="h-3 w-3" />
          </button>

          <button
            type="button"
            onClick={() => setLogLiveTail((p) => !p)}
            className={`inline-flex h-7 items-center gap-1.5 rounded-lg border px-2.5 text-[11px] font-medium cursor-pointer ${
              logLiveTail
                ? 'border-emerald-500/30 bg-emerald-500/10 text-emerald-400'
                : 'border-white/[0.08] bg-zinc-900 text-zinc-400'
            }`}
          >
            <span
              className={`h-1.5 w-1.5 rounded-full ${
                logLiveTail ? 'bg-emerald-400 animate-pulse' : 'bg-zinc-500'
              }`}
            />
            {logLiveTail ? 'Streaming' : 'Paused'}
          </button>
        </div>
      </div>

      {/* Log Console Stream */}
      <div className="overflow-hidden rounded-xl border border-white/[0.08] bg-[#07070a]">
        <div className="divide-y divide-white/[0.03] font-mono text-xs max-h-[420px] overflow-y-auto">
          {filteredLogs.length === 0 ? (
            <div className="p-8 text-center text-xs text-zinc-500 font-mono">
              {logLiveTail ? 'Listening for container log output...' : 'No logs recorded'}
            </div>
          ) : (
            filteredLogs.map((l) => (
              <div
                key={l.id}
                className={`group flex items-start justify-between gap-3 px-3.5 py-2 hover:bg-white/[0.03] transition-colors ${
                  l.level === 'error'
                    ? 'bg-rose-500/[0.04]'
                    : l.level === 'warn'
                      ? 'bg-amber-500/[0.03]'
                      : ''
                }`}
              >
                <div className="flex items-start gap-2.5 min-w-0 flex-1">
                  {logShowTimestamps && (
                    <span className="text-zinc-500 shrink-0 select-none text-[11px]">
                      {l.timestamp.slice(11, 23) || l.timestamp}
                    </span>
                  )}

                  <span
                    className={`shrink-0 rounded px-1.5 py-0.2 text-[10px] font-bold uppercase ${
                      l.level === 'error'
                        ? 'bg-rose-500/20 text-rose-400 ring-1 ring-rose-500/30'
                        : l.level === 'warn'
                          ? 'bg-amber-500/20 text-amber-300 ring-1 ring-amber-500/30'
                          : l.level === 'debug'
                            ? 'bg-zinc-800 text-zinc-400'
                            : 'bg-sky-500/15 text-sky-400'
                    }`}
                  >
                    {l.level}
                  </span>

                  <span className="rounded bg-white/[0.06] px-1.5 py-0.2 text-[10px] text-orange-400 shrink-0">
                    {l.container}
                  </span>

                  <span
                    className={`${
                      logWordWrap ? 'break-all whitespace-pre-wrap' : 'truncate whitespace-pre'
                    } ${
                      l.level === 'error'
                        ? 'text-rose-300'
                        : l.level === 'warn'
                          ? 'text-amber-200'
                          : l.level === 'debug'
                            ? 'text-zinc-400'
                            : 'text-zinc-100'
                    }`}
                  >
                    {renderLogMessage(l.message)}
                  </span>
                </div>

                <button
                  type="button"
                  onClick={() => {
                    void copyToClipboard(
                      `${l.timestamp} [${l.level.toUpperCase()}] [${l.container}] ${l.message}`
                    );
                    toast.success('Copied log line');
                  }}
                  className="opacity-0 group-hover:opacity-100 rounded p-1 text-zinc-500 hover:bg-white/[0.08] hover:text-zinc-200 transition-opacity cursor-pointer shrink-0"
                  title="Copy log line"
                >
                  <Copy className="h-3 w-3" />
                </button>
              </div>
            ))
          )}
        </div>
      </div>
    </div>
  );
};
