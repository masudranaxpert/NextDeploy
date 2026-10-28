import React from 'react';
import { ShieldAlert, Check, X } from 'lucide-react';
import { ApprovalRequest } from './types';

interface ApprovalCardProps {
  approval: ApprovalRequest;
  onConfirm: (confirmed: boolean) => void;
}

export const ApprovalCard: React.FC<ApprovalCardProps> = ({
  approval,
  onConfirm,
}) => {
  return (
    <div
      data-slot="approval-card"
      className="my-3 overflow-hidden rounded-xl border border-amber-500/30 bg-amber-500/5 p-4 shadow-lg backdrop-blur-xs"
    >
      <div className="flex items-start gap-3">
        <div className="flex h-8 w-8 items-center justify-center rounded-lg bg-amber-500/15 text-amber-400 shrink-0">
          <ShieldAlert className="h-4 w-4" />
        </div>

        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-2">
            <span className="font-semibold text-xs uppercase tracking-wider text-amber-400">
              Action Approval Required
            </span>
            <span className="rounded bg-amber-400/10 px-1.5 py-0.5 font-mono text-[9px] text-amber-300">
              Safe Mode
            </span>
          </div>

          <p className="mt-1 font-mono text-xs font-medium text-zinc-100">
            {approval.title}
          </p>

          {approval.message && (
            <p className="mt-1 text-xs text-zinc-400 leading-relaxed">
              {approval.message}
            </p>
          )}

          <div className="mt-3 flex items-center gap-2">
            <button
              type="button"
              onClick={() => onConfirm(true)}
              className="inline-flex items-center gap-1.5 rounded-lg bg-emerald-500/20 px-3 py-1.5 text-xs font-semibold text-emerald-300 ring-1 ring-emerald-500/30 hover:bg-emerald-500/30 active:scale-[0.98] transition-all cursor-pointer"
            >
              <Check className="h-3.5 w-3.5" />
              <span>Approve & Run</span>
            </button>

            <button
              type="button"
              onClick={() => onConfirm(false)}
              className="inline-flex items-center gap-1.5 rounded-lg border border-white/10 bg-white/5 px-3 py-1.5 text-xs font-medium text-zinc-300 hover:bg-white/10 active:scale-[0.98] transition-all cursor-pointer"
            >
              <X className="h-3.5 w-3.5 text-zinc-400" />
              <span>Reject</span>
            </button>
          </div>
        </div>
      </div>
    </div>
  );
};
