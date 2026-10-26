import React from 'react';
import { RotateCcw, Pin } from 'lucide-react';

interface SlotBadgeProps {
  type: 'prod' | 'preview' | 'standby' | 'slot' | 'history';
  slot?: string;
  isRollback?: boolean;
  isPinned?: boolean;
  className?: string;
}

export const SlotBadge: React.FC<SlotBadgeProps> = ({
  type,
  slot,
  isRollback = false,
  isPinned = false,
  className = '',
}) => {
  const slotUpper = slot?.toUpperCase() || '';

  if (type === 'prod') {
    return (
      <span
        className={`inline-flex items-center gap-1 rounded border border-emerald-500/30 bg-emerald-500/15 px-1.5 py-0.5 font-mono text-[10px] font-semibold text-emerald-400 shrink-0 ${className}`}
      >
        {isRollback && <RotateCcw className="h-2.5 w-2.5 text-emerald-300" />}
        <span>PROD · {slotUpper}</span>
        {isPinned && <Pin className="h-2.5 w-2.5 text-amber-300 ml-0.5" />}
      </span>
    );
  }

  if (type === 'preview') {
    return (
      <span
        className={`inline-flex items-center gap-1 rounded border border-violet-500/30 bg-violet-500/15 px-1.5 py-0.5 font-mono text-[10px] font-semibold text-violet-300 shrink-0 ${className}`}
      >
        PREVIEW · {slotUpper}
      </span>
    );
  }

  if (type === 'standby') {
    return (
      <span
        className={`inline-flex items-center gap-1 rounded border border-sky-500/25 bg-sky-500/10 px-1.5 py-0.5 font-mono text-[10px] font-medium text-sky-300 shrink-0 ${className}`}
      >
        STANDBY · {slotUpper}
      </span>
    );
  }

  return (
    <span
      className={`inline-flex items-center gap-1 rounded border border-white/[0.08] bg-zinc-800/60 px-1.5 py-0.5 font-mono text-[10px] text-zinc-400 shrink-0 ${className}`}
    >
      Slot {slotUpper || 'BLUE'}
    </span>
  );
};
