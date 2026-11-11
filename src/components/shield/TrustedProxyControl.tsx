import React from 'react';
import { ShieldCheck } from 'lucide-react';

export const TRUSTED_PROXY_LABEL = 'Trusted Proxy (Cloudflare Real IP)';

export const TrustedProxyToggle: React.FC<{
  checked: boolean;
  onChange: (next: boolean) => void;
  disabled?: boolean;
  variant?: 'card' | 'inline';
  className?: string;
}> = ({ checked, onChange, disabled = false, variant = 'card', className = '' }) => {
  const handleToggle = () => {
    if (!disabled) {
      onChange(!checked);
    }
  };

  if (variant === 'inline') {
    return (
      <button
        type="button"
        role="switch"
        aria-checked={checked}
        disabled={disabled}
        onClick={handleToggle}
        className={`inline-flex items-center gap-2 text-xs select-none cursor-pointer disabled:cursor-not-allowed disabled:opacity-50 ${className}`}
      >
        <span
          className={`relative inline-flex h-4 w-7 shrink-0 cursor-pointer rounded-full border border-transparent transition-colors duration-200 ease-in-out ${
            checked ? 'bg-amber-500' : 'bg-zinc-800'
          }`}
        >
          <span
            className={`pointer-events-none inline-block h-3 w-3 transform rounded-full bg-white shadow-xs ring-0 transition duration-200 ease-in-out mt-0.5 ml-0.5 ${
              checked ? 'translate-x-3' : 'translate-x-0'
            }`}
          />
        </span>
        <span className="text-xs font-medium text-zinc-300">{TRUSTED_PROXY_LABEL}</span>
      </button>
    );
  }

  return (
    <div
      onClick={handleToggle}
      role="button"
      tabIndex={disabled ? -1 : 0}
      onKeyDown={(e) => {
        if (e.key === ' ' || e.key === 'Enter') {
          e.preventDefault();
          handleToggle();
        }
      }}
      className={`flex items-center justify-between rounded-lg border border-white/[0.08] bg-zinc-950/70 px-3 py-2 transition-all hover:border-white/[0.16] cursor-pointer select-none ${
        checked ? 'border-amber-500/35 bg-amber-500/[0.05]' : ''
      } ${disabled ? 'cursor-not-allowed opacity-50' : ''} ${className}`}
    >
      <div className="flex items-center gap-2 min-w-0">
        <ShieldCheck className={`h-4 w-4 shrink-0 transition-colors ${checked ? 'text-amber-400' : 'text-zinc-500'}`} />
        <span className="text-xs font-medium text-zinc-200">{TRUSTED_PROXY_LABEL}</span>
      </div>

      <div
        className={`relative inline-flex h-5 w-9 shrink-0 cursor-pointer rounded-full border border-transparent transition-colors duration-200 ease-in-out ${
          checked ? 'bg-amber-500' : 'bg-zinc-800'
        }`}
      >
        <span
          className={`pointer-events-none inline-block h-4 w-4 transform rounded-full bg-white shadow-sm ring-0 transition duration-200 ease-in-out mt-0.5 ml-0.5 ${
            checked ? 'translate-x-4' : 'translate-x-0'
          }`}
        />
      </div>
    </div>
  );
};

export const TrustedProxyBadge: React.FC<{ enabled?: boolean }> = ({ enabled }) => {
  if (!enabled) return null;
  return (
    <span className="inline-flex items-center rounded-md border border-amber-500/30 bg-amber-500/10 px-2 py-0.5 text-[11px] font-medium text-amber-300">
      Trusted Proxy
    </span>
  );
};
