import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Bar, BarChart, CartesianGrid, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts';
import { ArrowDown, ArrowUp, RefreshCw, Search } from 'lucide-react';
import { backendApi } from '../../lib/backendApi';
import { usePersistedState } from '../../lib/usePersistedState';
import type { TrafficSite, TrafficSiteKind, TrafficSummary } from '../../lib/api/trafficApi';

type WindowMin = 15 | 30 | 60;
type SortKey = 'requests' | 'last' | 'errors';
type KindFilter = 'all' | 'project' | 'php' | 'other';

const WINDOWS: WindowMin[] = [15, 30, 60];
const REFRESH_MS = 60_000;

const nf = new Intl.NumberFormat();
const fmt = (n: number) => nf.format(Math.round(n));
const pct = (part: number, whole: number) => (whole > 0 ? `${((part / whole) * 100).toFixed(part / whole < 0.1 ? 1 : 0)}%` : '0%');
const clock = (unixSec: number) =>
  new Date(unixSec * 1000).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });

const KIND_LABEL: Record<TrafficSiteKind, string> = {
  project: 'Project',
  php: 'PHP',
  wordpress: 'WordPress',
  static: 'Static',
  panel: 'Panel',
  route: 'Route',
  other: 'Other',
};

const kindGroup = (k: TrafficSiteKind): KindFilter =>
  k === 'project' ? 'project' : k === 'php' || k === 'wordpress' || k === 'static' ? 'php' : 'other';

/** Tiny per-minute trend line for a table row; values are the site's requests per minute. */
const Sparkline: React.FC<{ values: number[] }> = ({ values }) => {
  const w = 96;
  const h = 24;
  const max = Math.max(1, ...values);
  if (values.length < 2) return <svg width={w} height={h} aria-hidden />;
  const step = w / (values.length - 1);
  const pts = values.map((v, i) => `${(i * step).toFixed(1)},${(h - 2 - (v / max) * (h - 4)).toFixed(1)}`).join(' ');
  return (
    <svg width={w} height={h} viewBox={`0 0 ${w} ${h}`} aria-hidden className="overflow-visible">
      <polyline points={pts} fill="none" stroke="#f97316" strokeWidth={1.5} strokeLinejoin="round" strokeLinecap="round" />
    </svg>
  );
};

const Tile: React.FC<{ label: string; value: string; sub?: string; tone?: 'default' | 'warn' | 'bad' }> = ({
  label,
  value,
  sub,
  tone = 'default',
}) => (
  <div className="rounded-xl border border-white/[0.07] bg-[#111115]/90 p-4">
    <div className="text-[11px] font-medium text-zinc-500">{label}</div>
    <div className="mt-1.5 font-mono text-xl font-semibold text-zinc-100">{value}</div>
    {sub && (
      <div
        className={`mt-0.5 text-[11px] ${
          tone === 'bad' ? 'text-rose-300' : tone === 'warn' ? 'text-amber-300' : 'text-zinc-500'
        }`}
      >
        {sub}
      </div>
    )}
  </div>
);

interface TrafficViewProps {
  onOpenApp?: (appId: string, kind: TrafficSiteKind) => void;
}

/** Requests per site over the last 15/30/60 minutes, read from the reverse proxy's own counters. */
export const TrafficView: React.FC<TrafficViewProps> = ({ onOpenApp }) => {
  const [win, setWin] = usePersistedState<WindowMin>('nd_traffic_window', 30, { allowed: WINDOWS });
  const [data, setData] = useState<TrafficSummary | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  const [query, setQuery] = useState('');
  const [kind, setKind] = useState<KindFilter>('all');
  const [sortKey, setSortKey] = useState<SortKey>('requests');
  const [sortDesc, setSortDesc] = useState(true);
  const reqRef = useRef(0);

  const load = useCallback(async () => {
    const id = ++reqRef.current;
    setLoading(true);
    const res = await backendApi.trafficSummary(win);
    if (id !== reqRef.current) return; // a newer request (window change) already started
    setLoading(false);
    if (res.ok && res.data) {
      setData(res.data);
      setError(null);
    } else {
      setError(res.error || 'Could not load traffic.');
    }
  }, [win]);

  // Refresh every minute while the page is visible (the server adds one data point per minute).
  useEffect(() => {
    void load();
    const tick = () => {
      if (document.visibilityState === 'visible') void load();
    };
    const t = window.setInterval(tick, REFRESH_MS);
    const onVisible = () => document.visibilityState === 'visible' && void load();
    document.addEventListener('visibilitychange', onVisible);
    return () => {
      window.clearInterval(t);
      document.removeEventListener('visibilitychange', onVisible);
    };
  }, [load]);

  const sites = useMemo(() => {
    const words = query.toLowerCase().split(/\s+/).filter(Boolean);
    const list = (data?.sites ?? []).filter((s) => {
      if (kind !== 'all' && kindGroup(s.kind) !== kind) return false;
      if (words.length === 0) return true;
      const text = `${s.name} ${s.app_id ?? ''} ${(s.domains ?? []).join(' ')} ${s.owner ?? ''} ${KIND_LABEL[s.kind]}`.toLowerCase();
      return words.every((w) => text.includes(w));
    });
    const val = (s: TrafficSite) =>
      sortKey === 'last' ? s.last_minute : sortKey === 'errors' ? s.total.s4xx + s.total.s5xx : s.total.requests;
    return [...list].sort((a, b) => (sortDesc ? val(b) - val(a) : val(a) - val(b)) || a.name.localeCompare(b.name));
  }, [data, query, kind, sortKey, sortDesc]);

  const chartData = useMemo(
    () => (data?.minutes ?? []).map((m, i) => ({ minute: m, label: clock(m), requests: data?.per_minute[i] ?? 0 })),
    [data]
  );

  const total = data?.total;
  const lastMinute = data?.per_minute?.length ? data.per_minute[data.per_minute.length - 1] : 0;
  const busiest = data?.sites?.[0];
  const partial = data && data.coverage_minutes < data.window;
  const hasGroups = useMemo(() => {
    const g = new Set((data?.sites ?? []).map((s) => kindGroup(s.kind)));
    return g.size > 1;
  }, [data]);

  const toggleSort = (k: SortKey) => {
    if (k === sortKey) setSortDesc((d) => !d);
    else {
      setSortKey(k);
      setSortDesc(true);
    }
  };
  const SortHead: React.FC<{ k: SortKey; children: React.ReactNode }> = ({ k, children }) => (
    <button
      type="button"
      onClick={() => toggleSort(k)}
      className={`inline-flex items-center gap-1 cursor-pointer hover:text-zinc-200 ${sortKey === k ? 'text-zinc-200' : ''}`}
    >
      {children}
      {sortKey === k && (sortDesc ? <ArrowDown className="h-3 w-3" /> : <ArrowUp className="h-3 w-3" />)}
    </button>
  );

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div className="min-w-0">
          <h1 className="text-lg font-semibold text-zinc-100">Traffic</h1>
          <p className="text-xs text-zinc-500">
            Requests per site, counted by the reverse proxy. Updated every minute.
          </p>
        </div>
        <div className="flex items-center gap-2">
          <div className="flex rounded-lg border border-white/[0.08] bg-[#09090b] p-0.5 text-xs" role="tablist" aria-label="Time window">
            {WINDOWS.map((w) => (
              <button
                key={w}
                type="button"
                role="tab"
                aria-selected={win === w}
                onClick={() => setWin(w)}
                className={`h-7 rounded-md px-3 font-medium cursor-pointer transition-colors ${
                  win === w ? 'bg-zinc-800 text-zinc-100' : 'text-zinc-400 hover:text-zinc-200'
                }`}
              >
                {w === 60 ? '1 hour' : `${w} min`}
              </button>
            ))}
          </div>
          <button
            type="button"
            onClick={() => void load()}
            aria-label="Refresh"
            title={data?.updated ? `Last reading ${new Date(data.updated).toLocaleTimeString()}` : 'Refresh'}
            className="flex h-8 w-8 items-center justify-center rounded-lg border border-white/[0.08] bg-zinc-900 text-zinc-400 hover:bg-zinc-800 hover:text-zinc-200 cursor-pointer"
          >
            <RefreshCw className={`h-3.5 w-3.5 ${loading ? 'animate-spin' : ''}`} />
          </button>
        </div>
      </div>

      {error && (
        <div className="rounded-lg border border-rose-500/20 bg-rose-500/[0.06] px-3 py-2 text-xs text-rose-200">{error}</div>
      )}
      {data && !data.per_site && data.note && (
        <div className="rounded-lg border border-amber-500/20 bg-amber-500/[0.06] px-3 py-2 text-xs text-amber-200">{data.note}</div>
      )}
      {data && partial && (
        <div className="rounded-lg border border-white/[0.07] bg-[#111115]/90 px-3 py-2 text-xs text-zinc-400">
          {data.coverage_minutes === 0
            ? 'Collecting: the first numbers appear within two minutes.'
            : `Collecting since ${new Date(data.since).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}, so these numbers cover the last ${data.coverage_minutes} of ${data.window} minutes. History is kept in memory and starts again when the panel restarts.`}
        </div>
      )}

      <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
        <Tile
          label={`Requests, last ${win === 60 ? 'hour' : `${win} min`}`}
          value={total ? fmt(total.requests) : '—'}
          sub={busiest && busiest.total.requests > 0 ? `Busiest: ${busiest.name}` : undefined}
        />
        <Tile label="Last minute" value={data ? fmt(lastMinute) : '—'} sub="requests" />
        <Tile
          label="Client errors (4xx)"
          value={total ? fmt(total.s4xx) : '—'}
          sub={total ? `${pct(total.s4xx, total.requests)} of requests` : undefined}
          tone={total && total.requests > 0 && total.s4xx / total.requests > 0.2 ? 'warn' : 'default'}
        />
        <Tile
          label="Server errors (5xx)"
          value={total ? fmt(total.s5xx) : '—'}
          sub={total ? `${pct(total.s5xx, total.requests)} of requests` : undefined}
          tone={total && total.s5xx > 0 ? (total.s5xx / Math.max(1, total.requests) > 0.05 ? 'bad' : 'warn') : 'default'}
        />
      </div>

      <div className="rounded-xl border border-white/[0.07] bg-[#111115]/90 p-4 sm:p-5">
        <h2 className="text-sm font-semibold text-zinc-100">Requests per minute</h2>
        <div className="mt-3 h-[200px] w-full min-w-0">
          <ResponsiveContainer width="100%" height={200} minWidth={0}>
            <BarChart data={chartData} margin={{ top: 4, right: 4, left: -18, bottom: 0 }} barCategoryGap={2}>
              <CartesianGrid strokeDasharray="3 3" stroke="rgba(255,255,255,0.05)" vertical={false} />
              <XAxis
                dataKey="label"
                stroke="#71717a"
                fontSize={10}
                tickLine={false}
                axisLine={false}
                interval="preserveStartEnd"
                minTickGap={28}
              />
              <YAxis stroke="#71717a" fontSize={10} tickLine={false} axisLine={false} allowDecimals={false} width={44} />
              <Tooltip
                cursor={{ fill: 'rgba(255,255,255,0.04)' }}
                contentStyle={{
                  background: '#18181b',
                  border: '1px solid rgba(255,255,255,0.08)',
                  borderRadius: 8,
                  fontSize: 12,
                }}
                labelStyle={{ color: '#a1a1aa' }}
                itemStyle={{ color: '#e4e4e7' }}
                formatter={(v) => [fmt(Number(v) || 0), 'Requests']}
              />
              <Bar dataKey="requests" fill="#f97316" radius={[3, 3, 0, 0]} maxBarSize={18} isAnimationActive={false} />
            </BarChart>
          </ResponsiveContainer>
        </div>
      </div>

      <div className="rounded-xl border border-white/[0.07] bg-[#111115]/90">
        <div className="flex flex-wrap items-center justify-between gap-2 border-b border-white/[0.06] p-3 sm:px-5">
          <h2 className="text-sm font-semibold text-zinc-100">
            Sites <span className="font-normal text-zinc-500">({sites.length})</span>
          </h2>
          <div className="flex w-full flex-wrap items-center gap-2 sm:w-auto">
            {hasGroups && (
              <select
                value={kind}
                onChange={(e) => setKind(e.target.value as KindFilter)}
                aria-label="Site type"
                className="h-8 rounded-lg border border-white/[0.08] bg-[#09090b] px-2 text-xs text-zinc-200 outline-none"
              >
                <option value="all">All types</option>
                <option value="project">Projects</option>
                <option value="php">PHP sites</option>
                <option value="other">Other</option>
              </select>
            )}
            <label className="flex h-8 min-w-0 flex-1 items-center gap-1.5 rounded-lg border border-white/[0.08] bg-[#09090b] px-2 sm:w-56 sm:flex-none">
              <Search className="h-3.5 w-3.5 shrink-0 text-zinc-500" />
              <input
                value={query}
                onChange={(e) => setQuery(e.target.value)}
                placeholder="Search sites or domains"
                aria-label="Search sites or domains"
                className="min-w-0 flex-1 bg-transparent text-xs text-zinc-100 placeholder:text-zinc-500 outline-none"
              />
            </label>
          </div>
        </div>

        {sites.length === 0 ? (
          <div className="p-8 text-center text-xs text-zinc-500">
            {!data
              ? 'Loading…'
              : (data.sites ?? []).length === 0
              ? data.per_site
                ? 'No requests counted yet.'
                : 'Per-site numbers are not available on this proxy.'
              : 'No site matches the search.'}
          </div>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full min-w-[640px] text-xs">
              <thead>
                <tr className="text-left text-[11px] text-zinc-500">
                  <th className="px-3 py-2 font-medium sm:pl-5">Site</th>
                  <th className="px-3 py-2 text-right font-medium">
                    <SortHead k="requests">Requests</SortHead>
                  </th>
                  <th className="px-3 py-2 text-right font-medium">
                    <SortHead k="last">Last min</SortHead>
                  </th>
                  <th className="px-3 py-2 text-right font-medium">
                    <SortHead k="errors">Errors</SortHead>
                  </th>
                  <th className="px-3 py-2 font-medium sm:pr-5">Trend</th>
                </tr>
              </thead>
              <tbody>
                {sites.map((s) => {
                  const share = total && total.requests > 0 ? s.total.requests / total.requests : 0;
                  const errors = s.total.s4xx + s.total.s5xx;
                  const openable = Boolean(s.app_id && onOpenApp);
                  return (
                    <tr
                      key={s.key}
                      onClick={openable ? () => onOpenApp!(s.app_id!, s.kind) : undefined}
                      className={`border-t border-white/[0.04] ${openable ? 'cursor-pointer hover:bg-white/[0.02]' : ''}`}
                    >
                      <td className="px-3 py-2.5 sm:pl-5">
                        <div className="flex items-center gap-2 min-w-0">
                          <span className="truncate font-medium text-zinc-100">{s.name}</span>
                          <span className="shrink-0 rounded border border-white/[0.08] px-1.5 py-px text-[10px] text-zinc-400">
                            {KIND_LABEL[s.kind]}
                          </span>
                        </div>
                        <div className="mt-0.5 truncate font-mono text-[11px] text-zinc-500">
                          {s.domains && s.domains.length > 0
                            ? `${s.domains[0]}${s.domains.length > 1 ? ` +${s.domains.length - 1}` : ''}`
                            : s.app_id || ''}
                          {s.owner ? ` · ${s.owner}` : ''}
                        </div>
                      </td>
                      <td className="px-3 py-2.5 text-right">
                        <div className="font-mono font-semibold text-zinc-100">{fmt(s.total.requests)}</div>
                        <div className="ml-auto mt-1 h-1 w-16 overflow-hidden rounded-full bg-zinc-800">
                          <div className="h-full rounded-full bg-orange-400/80" style={{ width: `${Math.max(share > 0 ? 3 : 0, share * 100)}%` }} />
                        </div>
                      </td>
                      <td className="px-3 py-2.5 text-right font-mono text-zinc-300">{fmt(s.last_minute)}</td>
                      <td className="px-3 py-2.5 text-right font-mono">
                        {errors === 0 ? (
                          <span className="text-zinc-600">0</span>
                        ) : (
                          <span title={`4xx: ${fmt(s.total.s4xx)} · 5xx: ${fmt(s.total.s5xx)}`}>
                            <span className="text-zinc-300">{fmt(s.total.s4xx)}</span>
                            <span className="text-zinc-600"> / </span>
                            <span className={s.total.s5xx > 0 ? 'text-rose-300' : 'text-zinc-300'}>{fmt(s.total.s5xx)}</span>
                          </span>
                        )}
                      </td>
                      <td className="px-3 py-2.5 sm:pr-5">
                        <Sparkline values={s.per_minute} />
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        )}
        <div className="border-t border-white/[0.06] px-3 py-2 text-[11px] text-zinc-500 sm:px-5">
          Errors show 4xx / 5xx. Redirects (like HTTP to HTTPS) count as requests.
        </div>
      </div>
    </div>
  );
};
