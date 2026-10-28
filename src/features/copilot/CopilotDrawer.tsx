import React, { useState } from 'react';
import { Maximize2, Minimize2, X } from 'lucide-react';
import { HextaAICanvas } from './HextaAICanvas';

interface CopilotDrawerProps {
  isOpen: boolean;
  onClose: () => void;
}

export const CopilotDrawer: React.FC<CopilotDrawerProps> = ({ isOpen, onClose }) => {
  const [isExpanded, setIsExpanded] = useState(false);

  if (!isOpen) return null;

  return (
    <div className="fixed inset-0 z-50 overflow-hidden bg-black/40 backdrop-blur-xs transition-opacity animate-in fade-in duration-200">
      <aside
        data-slot="copilot-drawer"
        className={`fixed inset-y-0 right-0 z-50 flex flex-col bg-[#090a0b] border-l border-white/[0.08] shadow-2xl transition-all duration-200 ease-out ${
          isExpanded ? 'w-full md:w-[860px]' : 'w-full sm:w-[540px] md:w-[580px]'
        }`}
      >
        {/* Top Control Bar for Drawer Sizing */}
        <div className="absolute top-2.5 right-12 z-20 flex items-center gap-1">
          <button
            type="button"
            onClick={() => setIsExpanded((prev) => !prev)}
            className="flex size-7 items-center justify-center rounded-lg text-zinc-400 hover:bg-white/[0.06] hover:text-white transition-colors"
            title={isExpanded ? 'Restore size' : 'Expand drawer'}
          >
            {isExpanded ? <Minimize2 className="size-3.5" /> : <Maximize2 className="size-3.5" />}
          </button>
        </div>

        <div className="flex-1 min-h-0">
          <HextaAICanvas
            initialSidebarOpen={false}
            isDrawer={true}
            onCloseDrawer={onClose}
          />
        </div>
      </aside>
    </div>
  );
};
