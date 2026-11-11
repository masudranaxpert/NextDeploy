import React, { useEffect, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
export { ShadcnSelect } from './ShadcnSelect';
export type { ShadcnSelectOption, ShadcnSelectProps } from './ShadcnSelect';
import {
  X,
  ArrowRight,
  MoreHorizontal,
} from 'lucide-react';

// ----------------------------------------------------------------------------
// 1. shadcn/ui Dialog (Modal) Primitive — Dokploy / Linear 2026 Glass & Obsidian
// ----------------------------------------------------------------------------
interface ShadcnDialogProps {
  open: boolean;
  onClose: () => void;
  title: string;
  description?: string;
  icon?: React.ReactNode;
  headerActions?: React.ReactNode;
  maxWidth?: 'sm' | 'md' | 'lg' | 'xl' | '2xl';
  children: React.ReactNode;
  footer?: React.ReactNode;
}

export const ShadcnDialog: React.FC<ShadcnDialogProps> = ({
  open,
  onClose,
  title,
  description,
  icon,
  headerActions,
  maxWidth = 'md',
  children,
  footer,
}) => {
  useEffect(() => {
    if (!open) return;
    const handleEsc = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose();
    };
    window.addEventListener('keydown', handleEsc);
    return () => window.removeEventListener('keydown', handleEsc);
  }, [open, onClose]);

  if (!open) return null;

  const widthClass = {
    sm: 'max-w-sm',
    md: 'max-w-md',
    lg: 'max-w-lg',
    xl: 'max-w-xl',
    '2xl': 'max-w-2xl',
  }[maxWidth];

  return createPortal(
    <div
      className="fixed inset-0 z-[999] flex items-center justify-center bg-black/75 p-3 sm:p-4 backdrop-blur-md animate-in fade-in duration-150"
      onClick={onClose}
    >
      <div
        role="dialog"
        aria-modal="true"
        onClick={(e) => e.stopPropagation()}
        className={`relative w-full ${widthClass} overflow-hidden rounded-lg border border-white/[0.08] bg-[#0e0e11] text-zinc-100 shadow-[0_20px_60px_rgba(0,0,0,0.8)] animate-in fade-in zoom-in-95 duration-150 ease-out`}
      >
        {/* Header */}
        <div className="flex items-center justify-between gap-3 border-b border-white/[0.07] px-4 py-3 sm:px-5">
          <div className="flex items-center gap-2.5 min-w-0">
            {icon && (
              <div className="flex h-7 w-7 shrink-0 items-center justify-center rounded-md bg-zinc-900 text-zinc-300 border border-white/[0.06]">
                {icon}
              </div>
            )}
            <div className="min-w-0">
              <h2 className="text-sm font-semibold tracking-tight text-zinc-100 truncate">
                {title}
              </h2>
              {description && (
                <p className="mt-0.5 text-xs text-zinc-400 truncate">{description}</p>
              )}
            </div>
          </div>
          <div className="flex shrink-0 items-center gap-1.5">
            {headerActions}
            <button
              type="button"
              onClick={onClose}
              aria-label="Close dialog"
              className="inline-flex h-7 w-7 items-center justify-center rounded-md text-zinc-400 hover:bg-white/[0.06] hover:text-zinc-100 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-emerald-500/60 transition-colors cursor-pointer"
            >
              <X className="h-4 w-4" />
            </button>
          </div>
        </div>

        {/* Body */}
        <div className="max-h-[75vh] overflow-y-auto px-4 py-3.5 sm:px-5 sm:py-4 text-xs">{children}</div>

        {/* Footer */}
        {footer && (
          <div className="flex items-center justify-end gap-2 border-t border-white/[0.07] bg-[#09090b]/60 px-4 py-3 sm:px-5 sm:py-3">
            {footer}
          </div>
        )}
      </div>
    </div>,
    document.body
  );
};

// ----------------------------------------------------------------------------
// 2. shadcn/ui Switch Primitive
// ----------------------------------------------------------------------------
interface ShadcnSwitchProps {
  checked: boolean;
  onChange: (next: boolean) => void;
  label?: string;
  ariaLabel?: string;
}

export const ShadcnSwitch: React.FC<ShadcnSwitchProps> = ({ checked, onChange, label, ariaLabel }) => {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      aria-label={ariaLabel || label}
      onClick={() => onChange(!checked)}
      className="inline-flex items-center gap-2 rounded-md cursor-pointer select-none group focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-orange-500"
    >
      <span
        className={`relative inline-flex h-5 w-9 shrink-0 items-center rounded-full border transition-colors ${
          checked
            ? 'border-orange-500/50 bg-orange-500'
            : 'border-white/[0.1] bg-zinc-900 group-hover:border-white/[0.2]'
        }`}
      >
        <span
          className={`inline-block h-3.5 w-3.5 transform rounded-full bg-white shadow-xs transition-transform ${
            checked ? 'translate-x-4' : 'translate-x-0.5'
          }`}
        />
      </span>
      {label && <span className="text-xs font-medium text-zinc-300">{label}</span>}
    </button>
  );
};

// ----------------------------------------------------------------------------
// 3. Semantic Status Badge — Ultra-Crisp Dokploy / Linear Pill
// ----------------------------------------------------------------------------
interface StatusBadgeProps {
  status:
    | 'running'
    | 'building'
    | 'stopped'
    | 'failed'
    | 'exited'
    | 'restarting'
    | 'ready'
    | 'active'
    | 'provisioning'
    | 'error'
    | 'connected'
    | 'completed'
    | 'in-use'
    | 'unused'
    | 'expired'
    | 'consumed'
    | 'suspended';
  label?: string;
  size?: 'sm' | 'md';
}

export const StatusBadge: React.FC<StatusBadgeProps> = ({ status, label, size = 'sm' }) => {
  const styles: Record<
    StatusBadgeProps['status'],
    { bg: string; text: string; dot: string; ring: string; defaultLabel: string; pulse?: boolean }
  > = {
    running: {
      bg: 'bg-emerald-500/10',
      text: 'text-emerald-400',
      dot: 'bg-emerald-400',
      ring: 'ring-emerald-500/20',
      defaultLabel: 'Running',
      pulse: true,
    },
    ready: {
      bg: 'bg-emerald-500/10',
      text: 'text-emerald-400',
      dot: 'bg-emerald-400',
      ring: 'ring-emerald-500/20',
      defaultLabel: 'Ready',
    },
    active: {
      bg: 'bg-emerald-500/10',
      text: 'text-emerald-400',
      dot: 'bg-emerald-400',
      ring: 'ring-emerald-500/20',
      defaultLabel: 'Active',
      pulse: true,
    },
    connected: {
      bg: 'bg-emerald-500/10',
      text: 'text-emerald-400',
      dot: 'bg-emerald-400',
      ring: 'ring-emerald-500/20',
      defaultLabel: 'Connected',
    },
    completed: {
      bg: 'bg-emerald-500/10',
      text: 'text-emerald-400',
      dot: 'bg-emerald-400',
      ring: 'ring-emerald-500/20',
      defaultLabel: 'Completed',
    },
    'in-use': {
      bg: 'bg-emerald-500/10',
      text: 'text-emerald-400',
      dot: 'bg-emerald-400',
      ring: 'ring-emerald-500/20',
      defaultLabel: 'In Use',
    },
    building: {
      bg: 'bg-amber-500/10',
      text: 'text-amber-300',
      dot: 'bg-amber-400',
      ring: 'ring-amber-500/25',
      defaultLabel: 'Building',
      pulse: true,
    },
    restarting: {
      bg: 'bg-amber-500/10',
      text: 'text-amber-300',
      dot: 'bg-amber-400',
      ring: 'ring-amber-500/25',
      defaultLabel: 'Restarting',
      pulse: true,
    },
    provisioning: {
      bg: 'bg-amber-500/10',
      text: 'text-amber-300',
      dot: 'bg-amber-400',
      ring: 'ring-amber-500/25',
      defaultLabel: 'Provisioning',
      pulse: true,
    },
    stopped: {
      bg: 'bg-zinc-500/10',
      text: 'text-zinc-400',
      dot: 'bg-zinc-500',
      ring: 'ring-white/[0.08]',
      defaultLabel: 'Stopped',
    },
    exited: {
      bg: 'bg-zinc-500/10',
      text: 'text-zinc-400',
      dot: 'bg-zinc-500',
      ring: 'ring-white/[0.08]',
      defaultLabel: 'Exited',
    },
    unused: {
      bg: 'bg-zinc-500/10',
      text: 'text-zinc-400',
      dot: 'bg-zinc-500',
      ring: 'ring-white/[0.08]',
      defaultLabel: 'Unused',
    },
    expired: {
      bg: 'bg-zinc-500/10',
      text: 'text-zinc-400',
      dot: 'bg-zinc-500',
      ring: 'ring-white/[0.08]',
      defaultLabel: 'Expired',
    },
    consumed: {
      bg: 'bg-zinc-500/10',
      text: 'text-zinc-400',
      dot: 'bg-zinc-500',
      ring: 'ring-white/[0.08]',
      defaultLabel: 'Consumed',
    },
    suspended: {
      bg: 'bg-rose-500/10',
      text: 'text-rose-400',
      dot: 'bg-rose-400',
      ring: 'ring-rose-500/25',
      defaultLabel: 'Suspended',
    },
    failed: {
      bg: 'bg-rose-500/10',
      text: 'text-rose-400',
      dot: 'bg-rose-400',
      ring: 'ring-rose-500/25',
      defaultLabel: 'Failed',
    },
    error: {
      bg: 'bg-rose-500/10',
      text: 'text-rose-400',
      dot: 'bg-rose-400',
      ring: 'ring-rose-500/25',
      defaultLabel: 'Error',
    },
  };

  const cfg = styles[status] || styles.stopped;
  const padding = size === 'sm' ? 'px-2 py-0.5 text-xs' : 'px-2.5 py-1 text-xs';

  return (
    <span
      className={`inline-flex items-center gap-1.5 rounded-md font-medium ring-1 ring-inset whitespace-nowrap ${cfg.bg} ${cfg.text} ${cfg.ring} ${padding}`}
    >
      <span className="relative flex h-1.5 w-1.5">
        {cfg.pulse && (
          <span
            className={`absolute inline-flex h-full w-full animate-ping rounded-full opacity-75 ${cfg.dot}`}
          />
        )}
        <span className={`relative inline-flex h-1.5 w-1.5 rounded-full ${cfg.dot}`} />
      </span>
      {label || cfg.defaultLabel}
    </span>
  );
};

// ----------------------------------------------------------------------------
// 4. 3-Dot Action Dropdown Menu (Portal-rendered so tables never clip)
// ----------------------------------------------------------------------------
export interface ActionMenuItem {
  label: string;
  icon?: React.ReactNode;
  onClick: () => void;
  variant?: 'default' | 'danger';
  disabled?: boolean;
}

interface ActionMenuProps {
  items: ActionMenuItem[];
  trigger?: React.ReactNode;
  triggerClassName?: string;
  className?: string;
  ariaLabel?: string;
}

export const ActionMenu: React.FC<ActionMenuProps> = ({
  items,
  trigger,
  triggerClassName,
  className,
  ariaLabel,
}) => {
  const [open, setOpen] = useState(false);
  const [coords, setCoords] = useState<{ top: number; left: number }>({ top: 0, left: 0 });
  const btnRef = useRef<HTMLButtonElement>(null);
  const menuRef = useRef<HTMLDivElement>(null);

  const updateCoords = () => {
    if (!btnRef.current) return;
    const rect = btnRef.current.getBoundingClientRect();
    const menuWidth = 224;
    const estimatedHeight = items.length * 36 + 16;
    const spaceBelow = window.innerHeight - rect.bottom;
    const top =
      spaceBelow < estimatedHeight && rect.top > estimatedHeight
        ? rect.top - estimatedHeight - 6
        : rect.bottom + 6;
    const left = Math.max(12, Math.min(rect.right - menuWidth, window.innerWidth - menuWidth - 12));
    setCoords({ top, left });
  };

  useEffect(() => {
    if (!open) return;
    updateCoords();
    const handleOutside = (e: MouseEvent) => {
      const target = e.target as Node;
      if (
        menuRef.current &&
        !menuRef.current.contains(target) &&
        btnRef.current &&
        !btnRef.current.contains(target)
      ) {
        setOpen(false);
      }
    };
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.stopPropagation();
        setOpen(false);
        btnRef.current?.focus();
      }
    };
    const handleScrollOrResize = () => setOpen(false);

    document.addEventListener('mousedown', handleOutside);
    window.addEventListener('keydown', handleKeyDown);
    window.addEventListener('scroll', handleScrollOrResize, true);
    window.addEventListener('resize', handleScrollOrResize);
    return () => {
      document.removeEventListener('mousedown', handleOutside);
      window.removeEventListener('keydown', handleKeyDown);
      window.removeEventListener('scroll', handleScrollOrResize, true);
      window.removeEventListener('resize', handleScrollOrResize);
    };
  }, [open]);

  return (
    <div className={`text-left ${className || 'inline-block'}`} onClick={(e) => e.stopPropagation()}>
      <button
        ref={btnRef}
        type="button"
        onClick={() => setOpen((prev) => !prev)}
        className={
          triggerClassName ||
          `flex h-8 w-8 items-center justify-center rounded-md border transition-colors cursor-pointer focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-emerald-500/60 ${
            open
              ? 'border-white/[0.18] bg-zinc-800 text-zinc-100'
              : 'border-transparent bg-transparent text-zinc-400 hover:border-white/[0.08] hover:bg-white/[0.05] hover:text-zinc-100'
          }`
        }
        aria-label={ariaLabel || 'Actions'}
        aria-haspopup="menu"
        aria-expanded={open}
      >
        {trigger || <MoreHorizontal className="h-4 w-4" />}
      </button>

      {open &&
        createPortal(
          <div
            ref={menuRef}
            role="menu"
            style={{ top: coords.top, left: coords.left }}
            onClick={(e) => e.stopPropagation()}
            className="fixed z-[9999] w-52 origin-top-right rounded-xl border border-white/[0.09] bg-[#111115]/95 p-1 shadow-[0_16px_48px_rgba(0,0,0,0.85)] backdrop-blur-xl animate-in fade-in zoom-in-95 duration-100"
          >
            {items.map((item, idx) => (
              <button
                key={idx}
                type="button"
                role="menuitem"
                disabled={item.disabled}
                onClick={() => {
                  setOpen(false);
                  item.onClick();
                }}
                className={`flex w-full items-center gap-2.5 rounded-lg px-2.5 py-2 text-left text-xs font-medium transition-colors cursor-pointer focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-emerald-500/60 disabled:opacity-40 disabled:pointer-events-none ${
                  item.variant === 'danger'
                    ? 'text-rose-400 hover:bg-rose-500/12'
                    : 'text-zinc-200 hover:bg-white/[0.06] hover:text-white'
                }`}
              >
                {item.icon && <span className="shrink-0 opacity-80">{item.icon}</span>}
                <span className="truncate">{item.label}</span>
              </button>
            ))}
          </div>,
          document.body
        )}
    </div>
  );
};

// ----------------------------------------------------------------------------
// 5. EvilCharts-Inspired Glowing Area Chart
// ----------------------------------------------------------------------------
import type { EvilAreaChartProps } from './EvilAreaChart';
import { lazyRetry } from '../../lib/lazyRetry';

// The chart library is large, so it is loaded on first use instead of with the first screen.
const EvilAreaChartImpl = lazyRetry(() => import('./EvilAreaChart'));

export const EvilAreaChart: React.FC<EvilAreaChartProps> = (props) => (
  <React.Suspense fallback={<div style={{ height: props.height ?? 190 }} className="w-full animate-pulse rounded-lg bg-white/[0.03]" />}>
    <EvilAreaChartImpl {...props} />
  </React.Suspense>
);

// ----------------------------------------------------------------------------
// 6. Kobra-Inspired OTP Verification Slots
// ----------------------------------------------------------------------------
interface KobraInputOTPProps {
  length?: number;
  value: string;
  onChange: (nextValue: string) => void;
}

export const KobraInputOTP: React.FC<KobraInputOTPProps> = ({ length = 6, value, onChange }) => {
  const inputsRef = useRef<(HTMLInputElement | null)[]>([]);
  const chars = value.padEnd(length, ' ').split('').slice(0, length);

  const handleKeyChange = (index: number, raw: string) => {
    const clean = raw.replace(/[^a-zA-Z0-9]/g, '').toUpperCase();
    if (!clean) return;
    const nextChar = clean.slice(-1);
    const updated = chars.map((c, idx) => (idx === index ? nextChar : c === ' ' ? '' : c)).join('');
    onChange(updated);
    if (index < length - 1) {
      inputsRef.current[index + 1]?.focus();
    }
  };

  const handleKeyDown = (index: number, e: React.KeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'Backspace') {
      e.preventDefault();
      const nextArr = [...chars];
      if (nextArr[index] && nextArr[index] !== ' ') {
        nextArr[index] = ' ';
        onChange(nextArr.map((c) => (c === ' ' ? '' : c)).join(''));
      } else if (index > 0) {
        nextArr[index - 1] = ' ';
        onChange(nextArr.map((c) => (c === ' ' ? '' : c)).join(''));
        inputsRef.current[index - 1]?.focus();
      }
    }
  };

  return (
    <div className="flex items-center gap-2">
      {chars.map((char, i) => {
        const isFilled = char !== ' ' && char !== '';
        return (
          <input
            key={i}
            ref={(el) => {
              inputsRef.current[i] = el;
            }}
            type="text"
            inputMode="text"
            maxLength={2}
            value={char === ' ' ? '' : char}
            onChange={(e) => handleKeyChange(i, e.target.value)}
            onKeyDown={(e) => handleKeyDown(i, e)}
            placeholder="·"
            className={`h-10 w-9 rounded-lg border text-center font-mono text-sm font-semibold transition-all focus:outline-none ${
              isFilled
                ? 'border-orange-500/60 bg-orange-500/10 text-orange-300'
                : 'border-white/[0.1] bg-[#09090b] text-zinc-100 focus:border-orange-500'
            }`}
          />
        );
      })}
    </div>
  );
};

// ----------------------------------------------------------------------------
// 7. Modals re-exports (Split into dedicated modules for bundle optimization)
// ----------------------------------------------------------------------------
export { CommandPaletteModal } from '../modals/CommandPaletteModal';
export type { CommandPaletteProps } from '../modals/CommandPaletteModal';
export { DeployNewAppModal } from '../modals/DeployNewAppModal';
export type { DeployNewAppModalProps } from '../modals/DeployNewAppModal';

export * from './CopyButton';

