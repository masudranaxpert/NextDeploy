import React, { useCallback, useEffect, useRef, useState } from 'react';
import { Check, Copy, Loader2 } from 'lucide-react';
import { toast } from 'sonner';
import { copyToClipboard } from '../../lib/clipboard';
import { backendApi } from '../../lib/backendApi';
import type { PhpSiteDetail } from '../../lib/api/phpApi';

// Refined, responsive input styles with anti-slop visual standards
export const inputCls =
  'h-8.5 w-full rounded-lg border border-white/[0.08] bg-[#09090b] px-3 text-xs text-zinc-100 placeholder:text-zinc-600 outline-none transition-colors focus:border-orange-500/50 focus:ring-1 focus:ring-orange-500/20 disabled:opacity-50 disabled:cursor-not-allowed';
export const selectCls = inputCls + ' cursor-pointer pr-7 appearance-none bg-no-repeat bg-[right_0.5rem_center]';
export const textareaCls =
  'w-full rounded-lg border border-white/[0.08] bg-[#09090b] px-3 py-2 font-mono text-[11px] leading-relaxed text-zinc-100 placeholder:text-zinc-600 outline-none transition-colors focus:border-orange-500/50 focus:ring-1 focus:ring-orange-500/20 disabled:opacity-50';

export const Card: React.FC<{
  title?: React.ReactNode;
  description?: React.ReactNode;
  actions?: React.ReactNode;
  children: React.ReactNode;
  className?: string;
  bodyClassName?: string;
}> = ({ title, description, actions, children, className = '', bodyClassName = 'p-4 sm:p-5' }) => (
  <section className={`rounded-xl border border-white/[0.07] bg-[#111115]/90 shadow-sm ${className}`}>
    {(title || actions) && (
      <header className="flex flex-wrap items-center justify-between gap-3 border-b border-white/[0.06] px-4 sm:px-5 py-3.5">
        <div className="min-w-0 flex-1">
          {title && (typeof title === 'string' ? <h2 className="text-sm font-semibold text-zinc-100 tracking-tight">{title}</h2> : title)}
          {description && (typeof description === 'string' ? <p className="mt-0.5 text-xs text-zinc-400">{description}</p> : description)}
        </div>
        {actions && <div className="flex flex-wrap items-center gap-2 shrink-0">{actions}</div>}
      </header>
    )}
    <div className={bodyClassName}>{children}</div>
  </section>
);

export const Field: React.FC<{ label: string; hint?: string; error?: string; children: React.ReactNode; className?: string }> = ({
  label,
  hint,
  error,
  children,
  className = '',
}) => (
  <label className={`block space-y-1.5 ${className}`}>
    <span className="block text-[11px] font-medium text-zinc-300">{label}</span>
    {children}
    {error ? (
      <span className="block text-[11px] text-rose-400">{error}</span>
    ) : hint ? (
      <span className="block text-[11px] leading-snug text-zinc-500">{hint}</span>
    ) : null}
  </label>
);

type BtnProps = React.ButtonHTMLAttributes<HTMLButtonElement> & { busy?: boolean };

export const PrimaryButton: React.FC<BtnProps> = ({ busy, children, className = '', disabled, ...rest }) => (
  <button
    type="button"
    {...rest}
    disabled={disabled || busy}
    className={`inline-flex h-8 items-center justify-center gap-1.5 rounded-lg bg-orange-500 px-3.5 text-xs font-semibold text-white shadow-xs transition-all hover:bg-orange-600 active:scale-[0.98] disabled:cursor-not-allowed disabled:opacity-50 cursor-pointer whitespace-nowrap ${className}`}
  >
    {busy && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
    {children}
  </button>
);

export const GhostButton: React.FC<BtnProps & { tone?: 'default' | 'danger' | 'success' }> = ({
  busy,
  tone = 'default',
  children,
  className = '',
  disabled,
  ...rest
}) => {
  const toneClasses = {
    default: 'border-white/[0.08] bg-zinc-900 text-zinc-200 hover:bg-zinc-800 hover:text-white',
    danger: 'border-red-500/25 bg-red-500/10 text-red-300 hover:bg-red-500/20 hover:text-red-200',
    success: 'border-emerald-500/25 bg-emerald-500/10 text-emerald-300 hover:bg-emerald-500/20 hover:text-emerald-200',
  };

  return (
    <button
      type="button"
      {...rest}
      disabled={disabled || busy}
      className={`inline-flex h-8 items-center justify-center gap-1.5 rounded-lg border px-3 text-xs font-medium transition-all active:scale-[0.98] disabled:cursor-not-allowed disabled:opacity-50 cursor-pointer whitespace-nowrap ${toneClasses[tone]} ${className}`}
    >
      {busy && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
      {children}
    </button>
  );
};

export const Pill: React.FC<{
  tone?: 'green' | 'amber' | 'red' | 'zinc' | 'orange' | 'violet' | 'sky';
  children: React.ReactNode;
  className?: string;
}> = ({ tone = 'zinc', children, className = '' }) => {
  const tones: Record<string, string> = {
    green: 'border-emerald-500/30 bg-emerald-500/10 text-emerald-300',
    amber: 'border-amber-500/30 bg-amber-500/10 text-amber-300',
    red: 'border-red-500/30 bg-red-500/10 text-red-300',
    orange: 'border-orange-500/30 bg-orange-500/10 text-orange-300',
    violet: 'border-violet-500/30 bg-violet-500/10 text-violet-300',
    sky: 'border-sky-500/30 bg-sky-500/10 text-sky-300',
    zinc: 'border-white/[0.08] bg-white/[0.04] text-zinc-300',
  };
  return (
    <span
      className={`inline-flex items-center gap-1 rounded-md border px-2 py-0.5 font-mono text-[10px] font-medium ${tones[tone]} ${className}`}
    >
      {children}
    </span>
  );
};

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

  return <span className={`inline-block h-1.5 w-1.5 rounded-full shrink-0 ${color} ${className}`} />;
};

export const MetricStat: React.FC<{
  label: string;
  value: React.ReactNode;
  subtext?: React.ReactNode;
  icon?: React.ReactNode;
  tone?: 'emerald' | 'amber' | 'orange' | 'violet' | 'zinc';
}> = ({ label, value, subtext, icon, tone = 'zinc' }) => {
  const iconTones = {
    emerald: 'text-emerald-400 bg-emerald-500/10 border-emerald-500/20',
    amber: 'text-amber-400 bg-amber-500/10 border-amber-500/20',
    orange: 'text-orange-400 bg-orange-500/10 border-orange-500/20',
    violet: 'text-violet-400 bg-violet-500/10 border-violet-500/20',
    zinc: 'text-zinc-400 bg-zinc-800/80 border-white/[0.08]',
  };

  return (
    <div className="flex items-center justify-between gap-3 rounded-xl border border-white/[0.07] bg-[#111115]/90 p-4">
      <div className="min-w-0">
        <span className="text-[11px] font-medium text-zinc-400">{label}</span>
        <div className="mt-1 font-mono text-base font-semibold text-zinc-100 truncate">{value}</div>
        {subtext && <div className="mt-0.5 text-[11px] text-zinc-500 truncate">{subtext}</div>}
      </div>
      {icon && (
        <div className={`flex h-9 w-9 shrink-0 items-center justify-center rounded-lg border ${iconTones[tone]}`}>
          {icon}
        </div>
      )}
    </div>
  );
};

export const CopyField: React.FC<{ label?: string; value: string; secret?: boolean }> = ({ label, value, secret }) => {
  const [done, setDone] = useState(false);
  const [shown, setShown] = useState(!secret);
  return (
    <div className="space-y-1">
      {label && <div className="text-[11px] font-medium text-zinc-400">{label}</div>}
      <div className="flex items-center gap-1.5">
        <code className="min-w-0 flex-1 truncate rounded-lg border border-white/[0.08] bg-[#09090b] px-3 py-1.5 font-mono text-[11px] text-zinc-200">
          {shown ? value : '•'.repeat(Math.min(value.length, 24))}
        </code>
        {secret && (
          <GhostButton onClick={() => setShown((s) => !s)} className="!px-2.5">
            {shown ? 'Hide' : 'Show'}
          </GhostButton>
        )}
        <GhostButton
          className="!px-2.5"
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

export const SITE_TYPE_LABEL: Record<string, string> = {
  php: 'PHP Application',
  wordpress: 'WordPress CMS',
  static: 'Static HTML',
};

/** Loads the PHP detail of one site and exposes a reload function. */
export function usePhpSite(appId: string, pollMs = 0) {
  const [detail, setDetail] = useState<PhpSiteDetail | null>(null);
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(true);
  const alive = useRef(true);

  const reload = useCallback(async () => {
    const res = await backendApi.phpSiteDetail(appId);
    if (!alive.current) return;
    if (res.ok && res.data) {
      setDetail(res.data as PhpSiteDetail);
      setError('');
    } else {
      setError(res.error || 'Failed to load site');
    }
    setLoading(false);
  }, [appId]);

  useEffect(() => {
    alive.current = true;
    setLoading(true);
    void reload();
    return () => {
      alive.current = false;
    };
  }, [reload]);

  useEffect(() => {
    if (!pollMs) return;
    const t = setInterval(() => void reload(), pollMs);
    return () => clearInterval(t);
  }, [pollMs, reload]);

  return { detail, error, loading, reload };
}

export const LoadingBlock: React.FC<{ label?: string }> = ({ label = 'Loading…' }) => (
  <div className="flex items-center justify-center gap-2 rounded-xl border border-white/[0.07] bg-[#111115]/90 py-12 text-xs text-zinc-400">
    <Loader2 className="h-4 w-4 animate-spin text-orange-400" />
    <span>{label}</span>
  </div>
);

export const ErrorBlock: React.FC<{ message: string; onRetry?: () => void }> = ({ message, onRetry }) => (
  <div className="rounded-xl border border-red-500/25 bg-red-500/10 px-4 py-3.5 text-xs text-red-200">
    <div className="font-medium">{message}</div>
    {onRetry && (
      <button type="button" onClick={onRetry} className="mt-2 text-xs font-semibold text-red-300 underline cursor-pointer hover:text-white">
        Retry
      </button>
    )}
  </div>
);

export const EmptyBlock: React.FC<{
  icon: React.ReactNode;
  title: string;
  description: string;
  action?: React.ReactNode;
}> = ({ icon, title, description, action }) => (
  <div className="flex flex-col items-center justify-center rounded-xl border border-dashed border-white/[0.09] bg-[#111115]/40 px-6 py-14 text-center">
    <div className="flex h-10 w-10 items-center justify-center rounded-xl border border-white/[0.08] bg-zinc-900 text-zinc-400">
      {icon}
    </div>
    <h3 className="mt-3.5 text-sm font-semibold text-zinc-200">{title}</h3>
    <p className="mt-1 max-w-sm text-xs leading-relaxed text-zinc-400">{description}</p>
    {action && <div className="mt-4">{action}</div>}
  </div>
);
