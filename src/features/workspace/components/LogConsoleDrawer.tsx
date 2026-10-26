import React from 'react';
import { ChevronDown, ChevronRight, CheckCircle2, Loader2 } from 'lucide-react';

interface LogConsoleDrawerProps {
  title: string;
  isRunning?: boolean;
  isExpanded: boolean;
  onToggle: () => void;
  children: React.ReactNode;
  scrollRef?: React.RefObject<HTMLDivElement | null>;
  className?: string;
}

export const LogConsoleDrawer: React.FC<LogConsoleDrawerProps> = ({
  title,
  isRunning = false,
  isExpanded,
  onToggle,
  children,
  scrollRef,
  className = '',
}) => {
  return (
    <div
      className={`overflow-hidden rounded-xl border border-white/[0.08] bg-[#09090b] ${className}`}
    >
      <div
        className="flex items-center justify-between bg-zinc-900/70 px-3.5 py-2 cursor-pointer select-none transition-colors hover:bg-zinc-900"
        onClick={onToggle}
      >
        <div className="flex items-center gap-2 min-w-0">
          {isRunning ? (
            <Loader2 className="h-3.5 w-3.5 animate-spin text-orange-400 shrink-0" />
          ) : (
            <CheckCircle2 className="h-3.5 w-3.5 text-emerald-400 shrink-0" />
          )}
          <span className="font-mono text-xs font-medium text-zinc-200 truncate">
            {title}
          </span>
        </div>
        <button
          type="button"
          onClick={(e) => {
            e.stopPropagation();
            onToggle();
          }}
          className="inline-flex items-center gap-1 rounded px-2 py-0.5 text-[11px] font-medium text-zinc-400 hover:bg-white/[0.06] hover:text-zinc-200 cursor-pointer shrink-0"
        >
          <span>{isExpanded ? 'Hide' : 'Show'}</span>
          {isExpanded ? (
            <ChevronDown className="h-3.5 w-3.5" />
          ) : (
            <ChevronRight className="h-3.5 w-3.5" />
          )}
        </button>
      </div>

      {isExpanded && (
        <div
          ref={scrollRef}
          className="max-h-64 overflow-y-auto border-t border-white/[0.06] p-3 font-mono text-[11px] scroll-smooth"
        >
          {children}
        </div>
      )}
    </div>
  );
};
