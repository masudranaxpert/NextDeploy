import React, { useCallback, useEffect, useRef, useState } from 'react';
import { Check, Copy, Loader2 } from 'lucide-react';
import { toast } from 'sonner';
import { copyToClipboard } from '../../lib/clipboard';
import { backendApi } from '../../lib/backendApi';
import type { PhpSiteDetail, PhpStatusData } from '../../lib/api/phpApi';

export const inputCls =
  'h-8 w-full rounded-lg border border-white/[0.08] bg-[#09090b] px-2.5 text-xs text-zinc-100 placeholder:text-zinc-600 outline-none transition-colors focus:border-orange-500/50 disabled:opacity-50';
export const selectCls = inputCls + ' cursor-pointer pr-6';
export const textareaCls =
  'w-full rounded-lg border border-white/[0.08] bg-[#09090b] px-2.5 py-2 font-mono text-[11px] leading-relaxed text-zinc-100 placeholder:text-zinc-600 outline-none transition-colors focus:border-orange-500/50 disabled:opacity-50';

export const Card: React.FC<{
  title?: React.ReactNode;
  description?: React.ReactNode;
  actions?: React.ReactNode;
  children: React.ReactNode;
  className?: string;
}> = ({ title, description, actions, children, className = '' }) => (
  <section className={`rounded-xl border border-white/[0.07] bg-[#111115]/90 ${className}`}>
    {(title || actions) && (
      <header className="flex min-h-[3.25rem] flex-wrap items-center justify-between gap-3 border-b border-white/[0.06] px-4 py-2.5">
        <div className="min-w-0">
          {title && <h2 className="text-sm font-semibold leading-5 text-zinc-100">{title}</h2>}
          {description && <p className="mt-0.5 text-xs leading-4 text-zinc-500">{description}</p>}
        </div>
        {actions && <div className="flex items-center gap-2">{actions}</div>}
      </header>
    )}
    <div className="p-4">{children}</div>
  </section>
);

export const StatusDot: React.FC<{
  status?: 'running' | 'ready' | 'starting' | 'stopped' | 'failed' | 'disabled' | 'active';
  className?: string;
}> = ({ status = 'stopped', className = '' }) => {
  const color =
    status === 'running' || status === 'ready' || status === 'active'
      ? 'bg-emerald-400'
      : status === 'starting'
      ? 'bg-amber-400 animate-pulse'
      : status === 'failed'
      ? 'bg-rose-400'
      : 'bg-zinc-500';
  return <span className={`inline-block h-1.5 w-1.5 shrink-0 rounded-full ${color} ${className}`} />;
};

export const ValueBox: React.FC<{ children: React.ReactNode }> = ({ children }) => (
  <div className="flex h-8 min-w-[11rem] items-center rounded-lg border border-white/[0.08] bg-[#09090b] px-2.5 font-mono text-xs text-zinc-300">
    {children}
  </div>
);

export const EmptyState: React.FC<{ icon: React.ReactNode; text: string }> = ({ icon, text }) => (
  <div className="flex flex-col items-center gap-2 py-10 text-center text-zinc-600">
    {icon}
    <p className="text-xs text-zinc-400">{text}</p>
  </div>
);

export const Field: React.FC<{ label: string; hint?: string; children: React.ReactNode }> = ({
  label,
  hint,
  children,
}) => (
  <label className="block space-y-1.5">
    <span className="block text-[11px] font-medium text-zinc-300">{label}</span>
    {children}
    {hint && <span className="block text-[11px] leading-snug text-zinc-500">{hint}</span>}
  </label>
);

type BtnProps = React.ButtonHTMLAttributes<HTMLButtonElement> & { busy?: boolean };

export const PrimaryButton: React.FC<BtnProps> = ({ busy, children, className = '', disabled, ...rest }) => (
  <button
    type="button"
    {...rest}
    disabled={disabled || busy}
    className={`inline-flex h-8 items-center justify-center gap-1.5 rounded-lg bg-orange-500 px-3.5 text-xs font-semibold text-white shadow-lg shadow-orange-500/15 transition-all hover:bg-orange-600 disabled:cursor-not-allowed disabled:opacity-50 cursor-pointer ${className}`}
  >
    {busy && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
    {children}
  </button>
);

export const GhostButton: React.FC<BtnProps & { tone?: 'default' | 'danger' }> = ({
  busy,
  tone = 'default',
  children,
  className = '',
  disabled,
  ...rest
}) => (
  <button
    type="button"
    {...rest}
    disabled={disabled || busy}
    className={`inline-flex h-8 items-center justify-center gap-1.5 rounded-lg border px-3 text-xs font-medium transition-colors disabled:cursor-not-allowed disabled:opacity-50 cursor-pointer ${
      tone === 'danger'
        ? 'border-red-500/25 bg-red-500/10 text-red-300 hover:bg-red-500/20'
        : 'border-white/[0.08] bg-zinc-900 text-zinc-200 hover:bg-zinc-800'
    } ${className}`}
  >
    {busy && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
    {children}
  </button>
);

export const Pill: React.FC<{ tone?: 'green' | 'amber' | 'red' | 'zinc' | 'orange' | 'violet'; children: React.ReactNode }> = ({
  tone = 'zinc',
  children,
}) => {
  const tones: Record<string, string> = {
    green: 'border-emerald-500/30 bg-emerald-500/10 text-emerald-300',
    amber: 'border-amber-500/30 bg-amber-500/10 text-amber-300',
    red: 'border-red-500/30 bg-red-500/10 text-red-300',
    orange: 'border-orange-500/30 bg-orange-500/10 text-orange-300',
    violet: 'border-violet-500/30 bg-violet-500/10 text-violet-300',
    zinc: 'border-white/[0.08] bg-white/[0.04] text-zinc-300',
  };
  return (
    <span className={`inline-flex items-center gap-1 rounded-md border px-1.5 py-0.5 font-mono text-[10px] font-medium ${tones[tone]}`}>
      {children}
    </span>
  );
};

export const CopyField: React.FC<{ label?: string; value: string; secret?: boolean }> = ({ label, value, secret }) => {
  const [done, setDone] = useState(false);
  const [shown, setShown] = useState(!secret);
  return (
    <div className="space-y-1">
      {label && <div className="text-[11px] font-medium text-zinc-400">{label}</div>}
      <div className="flex items-center gap-1.5">
        <code className="min-w-0 flex-1 truncate rounded-lg border border-white/[0.08] bg-[#09090b] px-2.5 py-1.5 font-mono text-[11px] text-zinc-200">
          {shown ? value : '•'.repeat(Math.min(value.length, 24))}
        </code>
        {secret && (
          <GhostButton onClick={() => setShown((s) => !s)} className="!px-2">
            {shown ? 'Hide' : 'Show'}
          </GhostButton>
        )}
        <GhostButton
          className="!px-2"
          onClick={async () => {
            if (await copyToClipboard(value)) {
              setDone(true);
              setTimeout(() => setDone(false), 1500);
            } else toast.error('Could not copy');
          }}
        >
          {done ? <Check className="h-3.5 w-3.5 text-emerald-400" /> : <Copy className="h-3.5 w-3.5" />}
        </GhostButton>
      </div>
    </div>
  );
};

export function formatBytes(n: number): string {
  if (!n) return '0 B';
  const u = ['B', 'KB', 'MB', 'GB', 'TB'];
  const i = Math.min(u.length - 1, Math.floor(Math.log(n) / Math.log(1024)));
  return `${(n / Math.pow(1024, i)).toFixed(i === 0 ? 0 : 1)} ${u[i]}`;
}

export function formatTime(iso: string): string {
  if (!iso) return 'never';
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? iso : d.toLocaleString();
}

export const SITE_TYPE_LABEL: Record<string, string> = { php: 'PHP', wordpress: 'WordPress', static: 'Static HTML' };

// Site details are cached per site so switching tabs shows the last known data instantly and
// refreshes it in the background (stale-while-revalidate) instead of showing a spinner each time.
const detailCache = new Map<string, PhpSiteDetail>();
const detailInflight = new Map<string, Promise<PhpSiteDetail | string>>();

function fetchDetail(appId: string, force = false): Promise<PhpSiteDetail | string> {
  const running = detailInflight.get(appId);
  if (running && !force) return running;
  const p: Promise<PhpSiteDetail | string> = backendApi.phpSiteDetail(appId).then((res) => {
    if (detailInflight.get(appId) === p) detailInflight.delete(appId);
    if (res.ok && res.data) {
      detailCache.set(appId, res.data as PhpSiteDetail);
      return res.data as PhpSiteDetail;
    }
    return res.error || 'Failed to load site';
  });
  detailInflight.set(appId, p);
  return p;
}

/** Starts loading a site's details early, e.g. when the site page opens. */
export function prefetchPhpSite(appId: string) {
  void fetchDetail(appId);
}

/** Loads the PHP detail of one site and exposes a reload function. */
export function usePhpSite(appId: string, pollMs = 0) {
  const [detail, setDetail] = useState<PhpSiteDetail | null>(() => detailCache.get(appId) ?? null);
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(() => !detailCache.has(appId));
  const alive = useRef(true);

  const reload = useCallback(async (force = true) => {
    const res = await fetchDetail(appId, force);
    if (!alive.current) return;
    if (typeof res === 'string') {
      setError(res);
    } else {
      setDetail(res);
      setError('');
    }
    setLoading(false);
  }, [appId]);

  useEffect(() => {
    alive.current = true;
    const cached = detailCache.get(appId);
    setDetail(cached ?? null);
    setLoading(!cached);
    void reload(false);
    return () => {
      alive.current = false;
    };
  }, [appId, reload]);

  useEffect(() => {
    if (!pollMs) return;
    const t = setInterval(() => void reload(), pollMs);
    return () => clearInterval(t);
  }, [pollMs, reload]);

  return { detail, error, loading, reload };
}

let statusCache: PhpStatusData | null = null;

/** The module status (limits, choices); cached for the session and refreshed on mount. */
export function usePhpStatus() {
  const [status, setStatus] = useState<PhpStatusData | null>(statusCache);
  useEffect(() => {
    let live = true;
    void backendApi.phpStatus().then((r) => {
      if (!live || !r.ok || !r.data) return;
      statusCache = r.data;
      setStatus(r.data);
    });
    return () => {
      live = false;
    };
  }, []);
  return status;
}

/** A quiet placeholder with the shape of the page, instead of a spinner in an empty box. */
export const LoadingBlock: React.FC<{ label?: string }> = ({ label = 'Loading' }) => (
  <div className="space-y-5" aria-busy="true" aria-label={label}>
    {[0, 1].map((i) => (
      <div key={i} className="rounded-xl border border-white/[0.07] bg-[#111115]/90">
        <div className="border-b border-white/[0.06] px-4 py-3.5">
          <div className="h-4 w-32 animate-pulse rounded bg-white/[0.07]" />
        </div>
        <div className="grid gap-4 p-4 sm:grid-cols-2">
          <div className="h-8 animate-pulse rounded-lg bg-white/[0.04]" />
          <div className="h-8 animate-pulse rounded-lg bg-white/[0.04]" />
          <div className="h-8 animate-pulse rounded-lg bg-white/[0.04]" />
          <div className="h-8 animate-pulse rounded-lg bg-white/[0.04]" />
        </div>
      </div>
    ))}
  </div>
);

export const ErrorBlock: React.FC<{ message: string; onRetry?: () => void }> = ({ message, onRetry }) => (
  <div className="rounded-xl border border-red-500/25 bg-red-500/10 px-4 py-3 text-xs text-red-200">
    <div>{message}</div>
    {onRetry && (
      <button type="button" onClick={onRetry} className="mt-2 cursor-pointer text-red-100 underline">
        Retry
      </button>
    )}
  </div>
);
