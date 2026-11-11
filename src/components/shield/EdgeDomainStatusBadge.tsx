import React from 'react';
import {
  AlertCircle,
  AlertTriangle,
  CheckCircle2,
  Clock,
  Info,
  Loader2,
  ShieldAlert,
} from 'lucide-react';
import type { ShieldDomainStatus } from '../../types/shield';

export interface EdgeDomainStatusProps {
  shieldNodeId?: number | null;
  shieldIp?: string;
  edgeStatus?: ShieldDomainStatus | null;
  pendingOriginApply?: boolean;
  compact?: boolean;
}

export interface EdgeDomainHintsProps {
  shieldNodeId?: number | null;
  edgeStatus?: ShieldDomainStatus | null;
  isCloudflareProxied?: boolean;
  cfFlexibleLoop?: boolean;
  cfHint?: string;
}

const FOURTEEN_DAYS_MS = 14 * 24 * 60 * 60 * 1000;
const FIFTEEN_MIN_MS = 15 * 60 * 1000;

function formatCertExpiry(iso?: string): { text: string; renewOverdue: boolean; full: string } {
  if (!iso) return { text: '', renewOverdue: false, full: '' };
  const ms = Date.parse(iso);
  if (Number.isNaN(ms)) return { text: iso, renewOverdue: false, full: iso };
  const date = new Date(ms);
  const renewOverdue = ms - Date.now() < FOURTEEN_DAYS_MS;
  return {
    text: date.toLocaleDateString(undefined, { month: 'short', day: 'numeric', year: 'numeric' }),
    renewOverdue,
    full: date.toLocaleString(),
  };
}

function isPendingOver15Min(updatedAt?: string): boolean {
  if (!updatedAt) return false;
  const ms = Date.parse(updatedAt);
  if (Number.isNaN(ms)) return false;
  return Date.now() - ms > FIFTEEN_MIN_MS;
}

export const EdgeDomainStatusBadge: React.FC<EdgeDomainStatusProps> = ({
  shieldNodeId,
  shieldIp,
  edgeStatus,
  pendingOriginApply,
  compact = false,
}) => {
  if (!shieldNodeId) return null;

  const state = edgeStatus?.state || '';
  const detail = edgeStatus?.detail?.trim() || '';

  if (pendingOriginApply && state !== 'active') {
    return (
      <span
        className="inline-flex items-center gap-1 rounded-md border border-cyan-500/25 bg-cyan-500/[0.08] px-2 py-0.5 text-[11px] font-medium text-cyan-300"
        title="Updating origin Caddy internal TLS before exposing host on Shield"
      >
        <Loader2 className="h-3 w-3 animate-spin shrink-0" />
        Applying origin TLS…
      </span>
    );
  }

  if (!edgeStatus) return null;

  switch (state) {
    case 'active': {
      const exp = formatCertExpiry(edgeStatus.cert_not_after);
      const overdue = exp.renewOverdue || detail === 'cert_renew_overdue';
      return overdue ? (
        <span
          className="inline-flex items-center gap-1 rounded-md border border-amber-500/30 bg-amber-500/10 px-2 py-0.5 text-[11px] font-medium text-amber-300"
          title={exp.full ? `Certificate renew overdue (${exp.full})` : undefined}
        >
          <AlertTriangle className="h-3 w-3 shrink-0" />
          Active · Renew overdue
        </span>
      ) : (
        <span
          className="inline-flex items-center gap-1 rounded-md border border-emerald-500/25 bg-emerald-500/10 px-2 py-0.5 text-[11px] font-medium text-emerald-400"
          title="SSL Active (Auto-renewed by Edge)"
        >
          <CheckCircle2 className="h-3 w-3 shrink-0" />
          Active
        </span>
      );
    }
    case 'pending_dns':
      return (
        <span
          className="inline-flex items-center gap-1 rounded-md border border-amber-500/30 bg-amber-500/10 px-2 py-0.5 text-[11px] font-medium text-amber-300"
          title={detail || undefined}
        >
          <Clock className="h-3 w-3 shrink-0" />
          <span>
            Waiting for DNS → set A record to <strong className="font-mono">{shieldIp || 'Shield IP'}</strong>
            {!compact && detail ? ` (${detail})` : ''}
          </span>
        </span>
      );
    case 'pending_caa':
      return (
        <span
          className="inline-flex items-center gap-1 rounded-md border border-amber-500/30 bg-amber-500/10 px-2 py-0.5 text-[11px] font-medium text-amber-300"
          title={detail || undefined}
        >
          <ShieldAlert className="h-3 w-3 shrink-0" />
          Waiting for CAA → allow letsencrypt.org
        </span>
      );
    case 'issuing':
      return (
        <span
          className="inline-flex items-center gap-1 rounded-md border border-cyan-500/30 bg-cyan-500/10 px-2 py-0.5 text-[11px] font-medium text-cyan-300"
          title={detail || undefined}
        >
          <Loader2 className="h-3 w-3 animate-spin shrink-0" />
          Issuing certificate…
        </span>
      );
    case 'cert_error':
      return (
        <span
          className="inline-flex items-center gap-1 rounded-md border border-rose-500/30 bg-rose-500/10 px-2 py-0.5 text-[11px] font-medium text-rose-300"
          title={detail || 'Certificate issuance failed'}
        >
          <AlertCircle className="h-3 w-3 shrink-0" />
          <span className={compact ? 'truncate max-w-[16rem]' : ''}>
            Certificate error: {detail || 'issuance failed'}
          </span>
        </span>
      );
    case 'conflict':
      return (
        <span
          className="inline-flex items-center gap-1 rounded-md border border-rose-500/30 bg-rose-500/10 px-2 py-0.5 text-[11px] font-medium text-rose-300"
          title={detail ? `Claimed by panel ${detail}` : 'Claimed by another panel'}
        >
          <ShieldAlert className="h-3 w-3 shrink-0" />
          Conflict: claimed by {detail || 'another panel'}
        </span>
      );
    case 'dns_drift':
      return (
        <span
          className="inline-flex items-center gap-1 rounded-md border border-amber-500/30 bg-amber-500/10 px-2 py-0.5 text-[11px] font-medium text-amber-300"
          title={detail || 'DNS changed away after certificate was issued'}
        >
          <AlertTriangle className="h-3 w-3 shrink-0" />
          DNS drift{!compact && detail ? `: ${detail}` : ''}
        </span>
      );
    case 'queued_rate_limit':
      return (
        <span
          className="inline-flex items-center gap-1 rounded-md border border-amber-500/30 bg-amber-500/10 px-2 py-0.5 text-[11px] font-medium text-amber-300"
          title={detail || 'Deferred by Let’s Encrypt admission throttle'}
        >
          <Clock className="h-3 w-3 shrink-0" />
          Queued (rate limit)
        </span>
      );
    case 'unsupported_wildcard':
      return (
        <span
          className="inline-flex items-center gap-1 rounded-md border border-zinc-700 bg-zinc-800/80 px-2 py-0.5 text-[11px] font-medium text-zinc-300"
          title={detail || 'Wildcard certificates require DNS-01 and are unsupported on Shield'}
        >
          <AlertTriangle className="h-3 w-3 shrink-0 text-amber-400" />
          Unsupported (wildcard)
        </span>
      );
    case 'excluded_internal':
    case 'excluded_loop':
    case 'excluded_quota':
    case 'held_mass_delete': {
      const labels: Record<string, string> = {
        excluded_internal: 'Excluded (internal host)',
        excluded_loop: 'Excluded (loop detected)',
        excluded_quota: 'Excluded (panel quota)',
        held_mass_delete: 'Held (mass-delete guard)',
      };
      return (
        <span
          className="inline-flex items-center gap-1 rounded-md border border-amber-500/30 bg-amber-500/10 px-2 py-0.5 text-[11px] font-medium text-amber-300"
          title={detail || undefined}
        >
          <AlertTriangle className="h-3 w-3 shrink-0" />
          {labels[state] || state}
        </span>
      );
    }
    default:
      return (
        <span
          className="inline-flex items-center gap-1 rounded-md border border-white/[0.08] bg-zinc-900 px-2 py-0.5 text-[11px] text-zinc-300"
          title={detail || undefined}
        >
          {state}
          {!compact && detail ? `: ${detail}` : ''}
        </span>
      );
  }
};

export const EdgeDomainHints: React.FC<EdgeDomainHintsProps> = ({
  shieldNodeId,
  edgeStatus,
  isCloudflareProxied,
  cfFlexibleLoop,
  cfHint,
}) => {
  const state = edgeStatus?.state || '';
  const detail = edgeStatus?.detail?.trim() || '';

  const showFlexibleHint = Boolean(cfFlexibleLoop || cfHint);
  const showCfAlwaysHttpsHint =
    Boolean(shieldNodeId) &&
    (state === 'issuing' || state === 'cert_error') &&
    (Boolean(isCloudflareProxied) ||
      /cloudflare|always use https|403|unauthorized/i.test(detail) ||
      (state === 'issuing' && isPendingOver15Min(edgeStatus?.updated_at)));

  if (!showFlexibleHint && !showCfAlwaysHttpsHint) return null;

  return (
    <div className="mt-2 space-y-1.5">
      {showFlexibleHint && (
        <div className="flex items-start gap-1.5 rounded-md border border-rose-500/30 bg-rose-500/10 px-2.5 py-1.5 text-[11px] text-rose-300">
          <AlertTriangle className="h-3.5 w-3.5 shrink-0 mt-0.5 text-rose-400" />
          <span>
            {cfHint ||
              'Cloudflare Flexible SSL mode detected (redirect loop). Set SSL mode to Full or Full (strict) and turn Always Use HTTPS off.'}
          </span>
        </div>
      )}
      {showCfAlwaysHttpsHint && (
        <div className="flex items-start gap-1.5 rounded-md border border-amber-500/25 bg-amber-500/[0.08] px-2.5 py-1.5 text-[11px] text-amber-300">
          <Info className="h-3.5 w-3.5 shrink-0 mt-0.5 text-amber-400" />
          <span>
            If Cloudflare &apos;Always Use HTTPS&apos; + Full (strict) is on, turn Always Use HTTPS off; we redirect to HTTPS ourselves.
          </span>
        </div>
      )}
    </div>
  );
};
