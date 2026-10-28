import React, { useState } from 'react';
import { Bot, X, Maximize2, Minimize2, Sparkles } from 'lucide-react';
import { CopilotThread } from './CopilotThread';

interface CopilotDrawerProps {
  isOpen: boolean;
  onClose: () => void;
}

export const CopilotDrawer: React.FC<CopilotDrawerProps> = ({
  isOpen,
  onClose,
}) => {
  const [isExpanded, setIsExpanded] = useState(false);

  if (!isOpen) return null;

  return (
    <aside
      data-slot="copilot-drawer"
      className={`fixed inset-y-0 right-0 z-50 flex flex-col bg-[#0c0c0e] border-l border-white/[0.08] shadow-2xl transition-all duration-300 ease-in-out ${
        isExpanded
          ? 'w-full md:w-[720px] lg:w-[840px]'
          : 'w-full sm:w-[480px]'
      }`}
    >
      {/* Drawer Header */}
      <div className="flex h-14 items-center justify-between border-b border-white/[0.07] px-4 bg-zinc-950/70 shrink-0">
        <div className="flex items-center gap-2.5">
          <div className="flex h-7 w-7 items-center justify-center rounded-lg bg-orange-500/15 text-orange-400 border border-orange-500/30">
            <Sparkles className="h-4 w-4" />
          </div>
          <div>
            <div className="flex items-center gap-2">
              <span className="font-semibold text-xs text-zinc-100">
                NextDeploy AI
              </span>
              <span className="rounded bg-white/[0.06] px-1.5 py-0.5 font-mono text-[9px] text-zinc-400">
                omp-rpc
              </span>
            </div>
          </div>
        </div>

        <div className="flex items-center gap-1 text-zinc-400">
          <button
            type="button"
            onClick={() => setIsExpanded((prev) => !prev)}
            className="flex h-8 w-8 items-center justify-center rounded-lg hover:bg-white/[0.06] hover:text-white transition-colors cursor-pointer"
            title={isExpanded ? 'Restore size' : 'Expand drawer'}
          >
            {isExpanded ? (
              <Minimize2 className="h-4 w-4" />
            ) : (
              <Maximize2 className="h-4 w-4" />
            )}
          </button>

          <button
            type="button"
            onClick={onClose}
            className="flex h-8 w-8 items-center justify-center rounded-lg hover:bg-white/[0.06] hover:text-white transition-colors cursor-pointer"
            title="Close drawer"
          >
            <X className="h-4 w-4" />
          </button>
        </div>
      </div>

      {/* Drawer Thread Body */}
      <div className="flex-1 min-h-0">
        <CopilotThread onClose={onClose} />
      </div>
    </aside>
  );
};
