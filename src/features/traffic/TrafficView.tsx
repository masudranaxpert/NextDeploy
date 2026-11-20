import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Bar, BarChart, CartesianGrid, Cell, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts';
import {
  Activity,
  AlertTriangle,
  ArrowDown,
  ArrowUp,
  Box,
  Gauge,
  Network,
  Pin,
  RefreshCw,
  Search,
  ServerCrash,
} from 'lucide-react';
import { backendApi } from '../../lib/backendApi';
import { usePersistedState } from '../../lib/usePersistedState';
import { NextDeployLogoMark, SiteTypeIcon } from '../../components/ui/BrandIcons';
import type { TrafficSite, TrafficSiteKind, TrafficSummary } from '../../lib/api/trafficApi';

type WindowMin = 15 | 30 | 60;
type SortKey = 'requests' | 'last' | 'errors';
type KindFilter = 'all' | 'project' | 'php' | 'other';

const WINDOWS: WindowMin[] = [15, 30, 60];
const REFRESH_MS = 60_000;

const nf = new Intl.NumberFormat();
const fmt = (n: number) => nf.format(Math.round(n));
const pct = (part: number, whole: number) =>
  whole > 0 ? `${((part / whole) * 100).toFixed(part / whole < 0.1 ? 1 : 0)}%` : '0%';
const clock = (unixSec: number) => new Date(unixSec * 1000).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });

/** Label, badge colours and accent (sparkline) per site type; PHP brands use their own colours. */
const KIND: Record<TrafficSiteKind, { label: string; badge: string; tile: string; accent: string }> = {
  project: { label: 'Project', badge: 'border-orange-500/30 bg-orange-500/10 text-orange-300', tile: 'bg-orange-500/10 border-orange-500/25 text-orange-300', accent: '#fb923c' },
  php: { label: 'PHP', badge: 'border-[#777BB4]/35 bg-[#777BB4]/10 text-[#B7BBE6]', tile: 'bg-[#777BB4]/10 border-[#777BB4]/30', accent: '#9aa0e0' },
  wordpress: { label: 'WordPress', badge: 'border-[#21759B]/40 bg-[#21759B]/10 text-[#8CC4E0]', tile: 'bg-[#21759B]/10 border-[#21759B]/35', accent: '#5aa9d6' },
  static: { label: 'Static', badge: 'border-[#E34F26]/35 bg-[#E34F26]/10 text-[#F4A58E]', tile: 'bg-[#E34F26]/10 border-[#E34F26]/30', accent: '#f08a6b' },
  panel: { label: 'Panel', badge: 'border-emerald-500/30 bg-emerald-500/10 text-emerald-300', tile: 'bg-emerald-500/10 border-emerald-500/25', accent: '#34d399' },
  route: { label: 'Route', badge: 'border-sky-500/30 bg-sky-500/10 text-sky-300', tile: 'bg-sky-500/10 border-sky-500/25 text-sky-300', accent: '#38bdf8' },
  other: { label: 'Other', badge: 'border-white/10 bg-white/[0.04] text-zinc-400', tile: 'bg-white/[0.04] border-white/10 text-zinc-400', accent: '#a1a1aa' },
};

const kindGroup = (k: TrafficSiteKind): KindFilter =>
  k === 'project' ? 'project' : k === 'php' || k === 'wordpress' || k === 'static' ? 'php' : 'other';

const KindIcon: React.FC<{ kind: TrafficSiteKind; className?: string }> = ({ kind, className = 'h-4 w-4' }) => {
  switch (kind) {
    case 'php':
    case 'wordpress':
    case 'static':
      return <SiteTypeIcon type={kind} className={className} />;
    case 'panel':
      return <NextDeployLogoMark className={className} />;
    case 'route':
      return <Network className={className} />;
    default:
      return <Box className={className} />;
  }
};

const SiteAvatar: React.FC<{ kind: TrafficSiteKind; size?: 'sm' | 'md' }> = ({ kind, size = 'sm' }) => (
  <span
    className={`flex shrink-0 items-center justify-center rounded-lg border ${KIND[kind].tile} ${
      size === 'md' ? 'h-9 w-9' : 'h-8 w-8'
    }`}
  >
    <KindIcon kind={kind} className={size === 'md' ? 'h-[18px] w-[18px]' : 'h-4 w-4'} />
  </span>
);

const KindBadge: React.FC<{ kind: TrafficSiteKind }> = ({ kind }) => (
  <span className={`shrink-0 rounded-md border px-1.5 py-px text-[10px] font-medium ${KIND[kind].badge}`}>
    {KIND[kind].label}
  </span>
);

/** Per-minute trend of one site, drawn as a soft area in the site type's colour. */
const Sparkline: React.FC<{ values: number[]; color: string; width?: number; height?: number }> = ({
  values,
  color,
  width = 96,
  height = 26,
}) => {
  const id = React.useId();
  if (values.length < 2) return <svg width={width} height={height} aria-hidden />;
  const max = Math.max(1, ...values);
  const step = width / (values.length - 1);
  const pts = values.map((v, i) => [i * step, height - 2 - (v / max) * (height - 5)] as const);
  const line = pts.map(([x, y]) => `${x.toFixed(1)},${y.toFixed(1)}`).join(' ');
  const area = `0,${height} ${line} ${width},${height}`;
  return (
    <svg width={width} height={height} viewBox={`0 0 ${width} ${height}`} aria-hidden className="overflow-visible">
      <defs>
        <linearGradient id={id} x1="0" y1="0" x2="0" y2="1">
          <stop offset="0%" stopColor={color} stopOpacity={0.28} />
          <stop offset="100%" stopColor={color} stopOpacity={0} />
        </linearGradient>
      </defs>
      <polygon points={area} fill={`url(#${id})`} />
      <polyline points={line} fill="none" stroke={color} strokeWidth={1.5} strokeLinejoin="round" strokeLinecap="round" />
    </svg>
  );
};

const TONES = {
  orange: { glow: 'from-orange-500/[0.09]', chip: 'bg-orange-500/10 text-orange-300 border-orange-500/20' },
  sky: { glow: 'from-sky-500/[0.08]', chip: 'bg-sky-500/10 text-sky-300 border-sky-500/20' },
  amber: { glow: 'from-amber-500/[0.08]', chip: 'bg-amber-500/10 text-amber-300 border-amber-500/20' },
  rose: { glow: 'from-rose-500/[0.08]', chip: 'bg-rose-500/10 text-rose-300 border-rose-500/20' },
} as const;

const Tile: React.FC<{
  label: string;
  value: string;
  sub?: React.ReactNode;
  icon: React.ReactNode;
  tone: keyof typeof TONES;
}> = ({ label, value, sub, icon, tone }) => (
  <div className={`relative overflow-hidden rounded-xl border border-white/[0.07] bg-gradient-to-br ${TONES[tone].glow} to-transparent bg-[#111115] p-4`}>
    <div className="flex items-center justify-between gap-2">
      <span className="text-[11px] font-medium text-zinc-400">{label}</span>
      <span className={`flex h-7 w-7 items-center justify-center rounded-lg border ${TONES[tone].chip}`}>{icon}</span>
    </div>
    <div className="mt-2 font-mono text-2xl font-semibold tracking-tight text-zinc-50">{value}</div>
    {sub && <div className="mt-1 truncate text-[11px] text-zinc-500">{sub}</div>}
  </div>
);

const ErrorChips: React.FC<{ s4xx: number; s5xx: number }> = ({ s4xx, s5xx }) => {
  if (s4xx === 0 && s5xx === 0) return <span className="text-zinc-600">—</span>;
  return (
    <span className="inline-flex items-center justify-end gap-1">
      {s4xx > 0 && (
        <span title="Client errors (4xx)" className="rounded-md border border-amber-500/25 bg-amber-500/10 px-1.5 py-px font-mono text-[10px] text-amber-300">
          {fmt(s4xx)}
        </span>
      )}
      {s5xx > 0 && (
        <span title="Server errors (5xx)" className="rounded-md border border-rose-500/25 bg-rose-500/10 px-1.5 py-px font-mono text-[10px] text-rose-300">
          {fmt(s5xx)}
        </span>
      )}
    </span>
  );
};

const PinButton: React.FC<{ pinned: boolean; onToggle: () => void; name: string }> = ({ pinned, onToggle, name }) => (
  <button
    type="button"
    onClick={(e) => {
      e.stopPropagation();
      onToggle();
    }}
    aria-pressed={pinned}
    aria-label={pinned ? `Unpin ${name}` : `Pin ${name}`}
    title={pinned ? 'Unpin' : 'Pin to top'}
    className={`flex h-7 w-7 shrink-0 items-center justify-center rounded-md transition-colors cursor-pointer ${
      pinned ? 'text-orange-300 hover:bg-orange-500/10' : 'text-zinc-600 hover:bg-white/[0.04] hover:text-zinc-300'
    }`}
  >
    <Pin className={`h-3.5 w-3.5 ${pinned ? 'fill-orange-300/80' : ''}`} />
  </button>
);

const isStringArray = (v: unknown): v is string[] => Array.isArray(v) && v.every((x) => typeof x === 'string');

interface TrafficViewProps {
  onOpenApp?: (appId: string, kind: TrafficSiteKind) => void;
}

/** Requests per site over the last 15/30/60 minutes, read from the reverse proxy's own counters. */
export const TrafficView: React.FC<TrafficViewProps> = ({ onOpenApp }) => {
  const [win, setWin] = usePersistedState<WindowMin>('nd_traffic_window', 30, { allowed: WINDOWS });
  const [pins, setPins] = usePersistedState<string[]>('nd_traffic_pins', [], { validate: isStringArray });
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
    const t = window.setInterval(() => {
      if (document.visibilityState === 'visible') void load();
    }, REFRESH_MS);
    const onVisible = () => document.visibilityState === 'visible' && void load();
    document.addEventListener('visibilitychange', onVisible);
    return () => {
      window.clearInterval(t);
      document.removeEventListener('visibilitychange', onVisible);
    };
  }, [load]);

  const pinSet = useMemo(() => new Set(pins), [pins]);
  const togglePin = (key: string) =>
    setPins((prev) => (prev.includes(key) ? prev.filter((k) => k !== key) : [...prev, key]));

  const sites = useMemo(() => {
    const words = query.toLowerCase().split(/\s+/).filter(Boolean);
    const list = (data?.sites ?? []).filter((s) => {
      if (kind !== 'all' && kindGroup(s.kind) !== kind) return false;
      if (words.length === 0) return true;
      const text = `${s.name} ${s.app_id ?? ''} ${(s.domains ?? []).join(' ')} ${s.owner ?? ''} ${KIND[s.kind].label}`.toLowerCase();
      return words.every((w) => text.includes(w));
    });
    const val = (s: TrafficSite) =>
      sortKey === 'last' ? s.last_minute : sortKey === 'errors' ? s.total.s4xx + s.total.s5xx : s.total.requests;
    return [...list].sort(
      (a, b) =>
        Number(pinSet.has(b.key)) - Number(pinSet.has(a.key)) ||
        (sortDesc ? val(b) - val(a) : val(a) - val(b)) ||
        a.name.localeCompare(b.name)
    );
  }, [data, query, kind, sortKey, sortDesc, pinSet]);

  // Pinned sites keep the order they were pinned in; a pin whose site is gone simply shows nothing.
  const pinned = useMemo(() => {
    const byKey = new Map((data?.sites ?? []).map((s) => [s.key, s]));
    return pins.map((k) => byKey.get(k)).filter((s): s is TrafficSite => Boolean(s));
  }, [data, pins]);

  const chartData = useMemo(
    () => (data?.minutes ?? []).map((m, i) => ({ minute: m, label: clock(m), requests: data?.per_minute[i] ?? 0 })),
    [data]
  );
  const peak = useMemo(() => Math.max(0, ...(data?.per_minute ?? [0])), [data]);

  const total = data?.total;
  const lastMinute = data?.per_minute?.length ? data.per_minute[data.per_minute.length - 1] : 0;
  const busiest = data?.sites?.find((s) => s.total.requests > 0);
  const partial = data && data.coverage_minutes < data.window;
  const hasGroups = useMemo(() => new Set((data?.sites ?? []).map((s) => kindGroup(s.kind))).size > 1, [data]);
  const engineLabel = data?.engine ? data.engine.charAt(0).toUpperCase() + data.engine.slice(1) : '';

  const toggleSort = (k: SortKey) => {
    if (k === sortKey) setSortDesc((d) => !d);
    else {
      setSortKey(k);
      setSortDesc(true);
    }
  };
  const SortHead: React.FC<{ k: SortKey; children: React.ReactNode; title?: string }> = ({ k, children, title }) => (
    <button
      type="button"
      title={title}
      onClick={() => toggleSort(k)}
      className={`inline-flex items-center gap-1 cursor-pointer hover:text-zinc-200 ${sortKey === k ? 'text-zinc-200' : ''}`}
    >
      {children}
      {sortKey === k && (sortDesc ? <ArrowDown className="h-3 w-3" /> : <ArrowUp className="h-3 w-3" />)}
    </button>
  );

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex flex-wrap items-center gap-2.5">
          <h1 className="text-lg font-semibold text-zinc-100">Traffic</h1>
          {data && (
            <span className="inline-flex items-center gap-1.5 rounded-full border border-emerald-500/20 bg-emerald-500/[0.07] px-2 py-0.5 text-[11px] font-medium text-emerald-300">
              <span className="relative flex h-1.5 w-1.5">
                <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-emerald-400 opacity-60" />
                <span className="relative inline-flex h-1.5 w-1.5 rounded-full bg-emerald-400" />
              </span>
              Live{engineLabel ? ` · ${engineLabel}` : ''}
            </span>
          )}
          {partial && data.coverage_minutes > 0 && (
            <span
              className="rounded-full border border-white/[0.08] bg-white/[0.03] px-2 py-0.5 text-[11px] text-zinc-400"
              title="History is kept in memory and starts again when the panel restarts."
            >
              Since {new Date(data.since).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}
            </span>
          )}
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
                  win === w ? 'bg-orange-500/15 text-orange-200 ring-1 ring-orange-500/25' : 'text-zinc-400 hover:text-zinc-200'
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
      {data && data.coverage_minutes === 0 && (
        <div className="rounded-lg border border-white/[0.07] bg-[#111115] px-3 py-2 text-xs text-zinc-400">
          The first numbers appear within two minutes.
        </div>
      )}

      <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
        <Tile
          tone="orange"
          icon={<Activity className="h-3.5 w-3.5" />}
          label={`Requests · ${win === 60 ? '1 hour' : `${win} min`}`}
          value={total ? fmt(total.requests) : '—'}
          sub={busiest ? <>Busiest: <span className="text-zinc-300">{busiest.name}</span></> : undefined}
        />
        <Tile
          tone="sky"
          icon={<Gauge className="h-3.5 w-3.5" />}
          label="Last minute"
          value={data ? fmt(lastMinute) : '—'}
          sub={peak > 0 ? <>Peak <span className="text-zinc-300">{fmt(peak)}</span> / min</> : undefined}
        />
        <Tile
          tone="amber"
          icon={<AlertTriangle className="h-3.5 w-3.5" />}
          label="Client errors · 4xx"
          value={total ? fmt(total.s4xx) : '—'}
          sub={total ? `${pct(total.s4xx, total.requests)} of requests` : undefined}
        />
        <Tile
          tone="rose"
          icon={<ServerCrash className="h-3.5 w-3.5" />}
          label="Server errors · 5xx"
          value={total ? fmt(total.s5xx) : '—'}
          sub={total ? `${pct(total.s5xx, total.requests)} of requests` : undefined}
        />
      </div>

      {pinned.length > 0 && (
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-4">
          {pinned.map((s) => {
            const openable = Boolean(s.app_id && onOpenApp);
            return (
              <div
                key={s.key}
                onClick={openable ? () => onOpenApp!(s.app_id!, s.kind) : undefined}
                className={`rounded-xl border border-white/[0.07] bg-[#111115] p-3.5 ${openable ? 'cursor-pointer hover:border-white/[0.14]' : ''}`}
              >
                <div className="flex items-center gap-2.5">
                  <SiteAvatar kind={s.kind} />
                  <div className="min-w-0 flex-1">
                    <div className="truncate text-sm font-medium text-zinc-100">{s.name}</div>
                    <div className="truncate font-mono text-[11px] text-zinc-500">{s.domains?.[0] ?? KIND[s.kind].label}</div>
                  </div>
                  <PinButton pinned onToggle={() => togglePin(s.key)} name={s.name} />
                </div>
                <div className="mt-3 flex items-end justify-between gap-2">
                  <div>
                    <div className="font-mono text-xl font-semibold text-zinc-50">{fmt(s.total.requests)}</div>
                    <div className="mt-0.5 flex items-center gap-2 text-[11px] text-zinc-500">
                      <span>{fmt(s.last_minute)} last min</span>
                      <ErrorChips s4xx={s.total.s4xx} s5xx={s.total.s5xx} />
                    </div>
                  </div>
                  <Sparkline values={s.per_minute} color={KIND[s.kind].accent} width={110} height={34} />
                </div>
              </div>
            );
          })}
        </div>
      )}

      <div className="rounded-xl border border-white/[0.07] bg-[#111115] p-4 sm:p-5">
        <div className="flex items-baseline justify-between gap-2">
          <h2 className="text-sm font-semibold text-zinc-100">Requests per minute</h2>
          {peak > 0 && <span className="font-mono text-[11px] text-zinc-500">peak {fmt(peak)}</span>}
        </div>
        <div className="mt-3 h-[200px] w-full min-w-0">
          <ResponsiveContainer width="100%" height={200} minWidth={0}>
            <BarChart data={chartData} margin={{ top: 4, right: 4, left: -18, bottom: 0 }} barCategoryGap={2}>
              <defs>
                <linearGradient id="nd-traffic-bar" x1="0" y1="0" x2="0" y2="1">
                  <stop offset="0%" stopColor="#fb923c" />
                  <stop offset="100%" stopColor="#ea580c" stopOpacity={0.75} />
                </linearGradient>
              </defs>
              <CartesianGrid strokeDasharray="3 3" stroke="rgba(255,255,255,0.05)" vertical={false} />
              <XAxis dataKey="label" stroke="#71717a" fontSize={10} tickLine={false} axisLine={false} interval="preserveStartEnd" minTickGap={28} />
              <YAxis stroke="#71717a" fontSize={10} tickLine={false} axisLine={false} allowDecimals={false} width={44} />
              <Tooltip
                cursor={{ fill: 'rgba(255,255,255,0.04)' }}
                contentStyle={{ background: '#18181b', border: '1px solid rgba(255,255,255,0.08)', borderRadius: 8, fontSize: 12 }}
                labelStyle={{ color: '#a1a1aa' }}
                itemStyle={{ color: '#e4e4e7' }}
                formatter={(v) => [fmt(Number(v) || 0), 'Requests']}
              />
              <Bar dataKey="requests" radius={[3, 3, 0, 0]} maxBarSize={18} isAnimationActive={false}>
                {chartData.map((d) => (
                  <Cell key={d.minute} fill={d.requests === peak && peak > 0 ? '#fdba74' : 'url(#nd-traffic-bar)'} />
                ))}
              </Bar>
            </BarChart>
          </ResponsiveContainer>
        </div>
      </div>

      <div className="rounded-xl border border-white/[0.07] bg-[#111115]">
        <div className="flex flex-wrap items-center justify-between gap-2 border-b border-white/[0.06] p-3 sm:px-5">
          <h2 className="text-sm font-semibold text-zinc-100">
            Sites <span className="font-normal text-zinc-500">{sites.length}</span>
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
            <label className="flex h-8 min-w-0 flex-1 items-center gap-1.5 rounded-lg border border-white/[0.08] bg-[#09090b] px-2 focus-within:border-orange-500/40 sm:w-56 sm:flex-none">
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
                ? 'No requests yet.'
                : 'Per-site numbers are not available on this proxy.'
              : 'No site matches the search.'}
          </div>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full min-w-[680px] text-xs">
              <thead>
                <tr className="text-left text-[11px] text-zinc-500">
                  <th className="w-10 py-2 pl-2 sm:pl-3" aria-label="Pin" />
                  <th className="px-2 py-2 font-medium">Site</th>
                  <th className="px-3 py-2 text-right font-medium">
                    <SortHead k="requests">Requests</SortHead>
                  </th>
                  <th className="px-3 py-2 text-right font-medium">
                    <SortHead k="last">Last min</SortHead>
                  </th>
                  <th className="px-3 py-2 text-right font-medium">
                    <SortHead k="errors" title="Amber: client errors (4xx). Red: server errors (5xx).">
                      Errors
                    </SortHead>
                  </th>
                  <th className="px-3 py-2 font-medium sm:pr-5">Trend</th>
                </tr>
              </thead>
              <tbody>
                {sites.map((s) => {
                  const share = total && total.requests > 0 ? s.total.requests / total.requests : 0;
                  const openable = Boolean(s.app_id && onOpenApp);
                  const isPinned = pinSet.has(s.key);
                  return (
                    <tr
                      key={s.key}
                      onClick={openable ? () => onOpenApp!(s.app_id!, s.kind) : undefined}
                      className={`border-t border-white/[0.04] ${isPinned ? 'bg-orange-500/[0.025]' : ''} ${
                        openable ? 'cursor-pointer hover:bg-white/[0.025]' : ''
                      }`}
                    >
                      <td className="py-2.5 pl-2 sm:pl-3">
                        <PinButton pinned={isPinned} onToggle={() => togglePin(s.key)} name={s.name} />
                      </td>
                      <td className="px-2 py-2.5">
                        <div className="flex min-w-0 items-center gap-2.5">
                          <SiteAvatar kind={s.kind} />
                          <div className="min-w-0">
                            <div className="flex min-w-0 items-center gap-2">
                              <span className="truncate font-medium text-zinc-100">{s.name}</span>
                              <KindBadge kind={s.kind} />
                            </div>
                            <div className="mt-0.5 truncate font-mono text-[11px] text-zinc-500">
                              {s.domains && s.domains.length > 0
                                ? `${s.domains[0]}${s.domains.length > 1 ? ` +${s.domains.length - 1}` : ''}`
                                : s.app_id || ''}
                              {s.owner ? <span className="text-zinc-600"> · {s.owner}</span> : null}
                            </div>
                          </div>
                        </div>
                      </td>
                      <td className="px-3 py-2.5 text-right">
                        <div className="font-mono font-semibold text-zinc-100">{fmt(s.total.requests)}</div>
                        <div className="ml-auto mt-1 h-1 w-16 overflow-hidden rounded-full bg-zinc-800">
                          <div
                            className="h-full rounded-full bg-gradient-to-r from-orange-500 to-amber-300"
                            style={{ width: `${Math.max(share > 0 ? 3 : 0, share * 100)}%` }}
                          />
                        </div>
                      </td>
                      <td className="px-3 py-2.5 text-right font-mono text-zinc-300">{fmt(s.last_minute)}</td>
                      <td className="px-3 py-2.5 text-right">
                        <ErrorChips s4xx={s.total.s4xx} s5xx={s.total.s5xx} />
                      </td>
                      <td className="px-3 py-2.5 sm:pr-5">
                        <Sparkline values={s.per_minute} color={KIND[s.kind].accent} />
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        )}
      </div>
    </div>
  );
};
