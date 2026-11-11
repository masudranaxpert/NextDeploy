import React from 'react';

export const TRUSTED_PROXY_LABEL = 'Trusted Proxy (Cloudflare Real IP)';
export const TRUSTED_PROXY_HELPER = 'Trusts CF-Connecting-IP for real visitor IP.';

export const TrustedProxyToggle: React.FC<{
  checked: boolean;
  onChange: (next: boolean) => void;
  disabled?: boolean;
  variant?: 'card' | 'inline';
  className?: string;
}> = ({ checked, onChange, disabled = false, variant = 'card', className = '' }) => {
  if (variant === 'inline') {
    return (
      <label
        title={TRUSTED_PROXY_HELPER}
        className={`inline-flex items-center gap-2 text-xs text-zinc-300 select-none ${
          disabled ? 'cursor-not-allowed opacity-50' : 'cursor-pointer'
        } ${className}`}
      >
        <input
          type="checkbox"
          checked={checked}
          disabled={disabled}
          onChange={(e) => onChange(e.target.checked)}
          className="rounded border-zinc-700 bg-zinc-800 accent-orange-500 text-orange-500 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-orange-500 cursor-pointer disabled:cursor-not-allowed"
        />
        <span>{TRUSTED_PROXY_LABEL}</span>
      </label>
    );
  }

  return (
    <label
      className={`flex items-start gap-2.5 rounded-lg border border-white/[0.08] bg-[#09090b] p-3 transition-colors hover:border-white/[0.14] ${
        disabled ? 'cursor-not-allowed opacity-50' : 'cursor-pointer'
      } ${className}`}
    >
      <input
        type="checkbox"
        checked={checked}
        disabled={disabled}
        onChange={(e) => onChange(e.target.checked)}
        className="mt-0.5 rounded border-zinc-700 bg-zinc-800 accent-orange-500 text-orange-500 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-orange-500 cursor-pointer disabled:cursor-not-allowed"
      />
      <div className="min-w-0">
        <div className="text-xs font-medium text-zinc-200">{TRUSTED_PROXY_LABEL}</div>
        <div className="mt-0.5 text-[11px] leading-snug text-zinc-400">{TRUSTED_PROXY_HELPER}</div>
      </div>
    </label>
  );
};

export const TrustedProxyBadge: React.FC<{ enabled?: boolean }> = ({ enabled }) => {
  if (!enabled) return null;
  return (
    <span
      title={TRUSTED_PROXY_HELPER}
      className="inline-flex items-center rounded-md border border-sky-500/25 bg-sky-500/[0.08] px-2 py-0.5 text-[11px] font-medium text-sky-300"
    >
      Trusted Proxy
    </span>
  );
};
