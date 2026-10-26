import React, { useEffect, useRef, useState } from 'react';
import { Check, ChevronDown } from 'lucide-react';

export interface ShadcnSelectOption {
  value: string | number;
  label: string;
  sublabel?: string;
  icon?: React.ReactNode;
  badge?: {
    text: string;
    tone?: 'default' | 'success' | 'warning' | 'danger' | 'info';
  };
  disabled?: boolean;
}

export interface ShadcnSelectProps {
  value: string | number | null | undefined;
  onChange: (value: any) => void;
  options: ShadcnSelectOption[];
  placeholder?: string;
  disabled?: boolean;
  className?: string;
  buttonClassName?: string;
  menuClassName?: string;
  tone?: 'default' | 'cyan' | 'warning' | 'danger';
  title?: string;
  id?: string;
}

export const ShadcnSelect: React.FC<ShadcnSelectProps> = ({
  value,
  onChange,
  options,
  placeholder = 'Select option...',
  disabled = false,
  className = '',
  buttonClassName = '',
  menuClassName = '',
  tone = 'default',
  title,
  id,
}) => {
  const [open, setOpen] = useState(false);
  const containerRef = useRef<HTMLDivElement>(null);

  const selectedOption = options.find(
    (opt) => String(opt.value) === String(value ?? '')
  );

  useEffect(() => {
    if (!open) return;
    const handleClickOutside = (e: MouseEvent) => {
      if (containerRef.current && !containerRef.current.contains(e.target as Node)) {
        setOpen(false);
      }
    };
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        setOpen(false);
      }
    };
    document.addEventListener('mousedown', handleClickOutside);
    document.addEventListener('keydown', handleKeyDown);
    return () => {
      document.removeEventListener('mousedown', handleClickOutside);
      document.removeEventListener('keydown', handleKeyDown);
    };
  }, [open]);

  const toneClasses = {
    default:
      'border-white/[0.08] bg-zinc-950 text-zinc-200 hover:border-white/[0.15] focus:border-zinc-500',
    cyan:
      'border-cyan-500/30 bg-cyan-500/[0.08] text-cyan-300 hover:border-cyan-500/50 focus:border-cyan-400',
    warning:
      'border-amber-500/30 bg-amber-500/[0.08] text-amber-300 hover:border-amber-500/50 focus:border-amber-400',
    danger:
      'border-rose-500/40 bg-rose-500/[0.08] text-rose-300 hover:border-rose-500/60 focus:border-rose-400',
  }[tone];

  return (
    <div ref={containerRef} className={`relative min-w-0 ${className}`} id={id}>
      <button
        type="button"
        disabled={disabled}
        onClick={() => setOpen((prev) => !prev)}
        title={title}
        aria-haspopup="listbox"
        aria-expanded={open}
        className={`flex w-full items-center justify-between gap-2 rounded-lg border px-2.5 py-1.5 text-xs font-medium transition-all cursor-pointer disabled:cursor-not-allowed disabled:opacity-50 focus:outline-none ${toneClasses} ${buttonClassName}`}
      >
        <span className="flex min-w-0 items-center gap-2 truncate">
          {selectedOption?.icon && <span className="shrink-0">{selectedOption.icon}</span>}
          <span className="truncate">{selectedOption ? selectedOption.label : placeholder}</span>
          {selectedOption?.badge && (
            <span
              className={`shrink-0 rounded px-1.5 py-0.2 text-[10px] font-semibold uppercase tracking-wider ${
                selectedOption.badge.tone === 'danger'
                  ? 'border border-rose-500/30 bg-rose-500/20 text-rose-300'
                  : selectedOption.badge.tone === 'warning'
                  ? 'border border-amber-500/30 bg-amber-500/20 text-amber-300'
                  : selectedOption.badge.tone === 'success'
                  ? 'border border-emerald-500/30 bg-emerald-500/20 text-emerald-300'
                  : 'border border-white/[0.08] bg-zinc-800 text-zinc-400'
              }`}
            >
              {selectedOption.badge.text}
            </span>
          )}
        </span>
        <ChevronDown
          className={`h-3.5 w-3.5 shrink-0 opacity-60 transition-transform duration-200 ${
            open ? 'rotate-180' : ''
          }`}
        />
      </button>

      {open && (
        <div
          role="listbox"
          className={`absolute left-0 right-0 top-full mt-1.5 z-[100] max-h-60 overflow-y-auto rounded-xl border border-white/[0.1] bg-[#0e0e12] p-1 shadow-[0_16px_40px_rgba(0,0,0,0.85),inset_0_1px_0_rgba(255,255,255,0.06)] backdrop-blur-md animate-in fade-in-0 zoom-in-95 duration-100 ${menuClassName}`}
        >
          {options.map((opt) => {
            const isSelected = String(opt.value) === String(value ?? '');
            return (
              <button
                key={String(opt.value)}
                type="button"
                role="option"
                aria-selected={isSelected}
                disabled={opt.disabled}
                onClick={() => {
                  if (opt.disabled) return;
                  onChange(opt.value);
                  setOpen(false);
                }}
                className={`flex w-full items-center justify-between gap-2 rounded-lg px-2.5 py-1.5 text-xs text-left transition-colors cursor-pointer ${
                  opt.disabled
                    ? 'opacity-40 cursor-not-allowed text-zinc-500'
                    : isSelected
                    ? 'bg-white/[0.08] text-white font-medium'
                    : 'text-zinc-300 hover:bg-white/[0.05] hover:text-white'
                }`}
              >
                <div className="flex min-w-0 items-center gap-2">
                  {opt.icon && <span className="shrink-0">{opt.icon}</span>}
                  <span className="truncate">{opt.label}</span>
                  {opt.sublabel && (
                    <span className="font-mono text-[11px] text-zinc-500 truncate">
                      {opt.sublabel}
                    </span>
                  )}
                </div>

                <div className="flex items-center gap-1.5 shrink-0">
                  {opt.badge && (
                    <span
                      className={`rounded px-1.5 py-0.5 text-[10px] font-semibold uppercase tracking-wider ${
                        opt.badge.tone === 'danger'
                          ? 'border border-rose-500/30 bg-rose-500/20 text-rose-300'
                          : opt.badge.tone === 'warning'
                          ? 'border border-amber-500/30 bg-amber-500/20 text-amber-300'
                          : opt.badge.tone === 'success'
                          ? 'border border-emerald-500/30 bg-emerald-500/20 text-emerald-300'
                          : 'border border-white/[0.08] bg-zinc-800 text-zinc-400'
                      }`}
                    >
                      {opt.badge.text}
                    </span>
                  )}
                  {isSelected && <Check className="h-3.5 w-3.5 text-emerald-400" />}
                </div>
              </button>
            );
          })}
        </div>
      )}
    </div>
  );
};
