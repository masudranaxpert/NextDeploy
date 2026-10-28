import React, { useEffect, useState } from 'react';
import { HextaAICanvas } from './HextaAICanvas';

interface CopilotDrawerProps {
  isOpen: boolean;
  onClose: () => void;
}

export const CopilotDrawer: React.FC<CopilotDrawerProps> = ({ isOpen, onClose }) => {
  const [isExpanded, setIsExpanded] = useState(false);

  useEffect(() => {
    if (!isOpen) return;
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && onClose();
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [isOpen, onClose]);

  if (!isOpen) return null;

  return (
    <div
      className="fixed inset-0 z-50 overflow-hidden bg-black/40 backdrop-blur-xs animate-in fade-in duration-200"
      onMouseDown={(e) => e.target === e.currentTarget && onClose()}
    >
      <aside
        data-slot="copilot-drawer"
        role="dialog"
        aria-label="NextDeploy AI"
        className={`fixed inset-y-0 right-0 z-50 flex h-dvh max-w-full flex-col border-l border-white/[0.08] bg-[#090a0b] shadow-2xl transition-[width] duration-200 ease-out ${
          isExpanded ? 'w-full lg:w-[min(960px,70vw)]' : 'w-full sm:w-[520px] lg:w-[580px]'
        }`}
      >
        <div className="min-h-0 flex-1">
          <HextaAICanvas
            initialSidebarOpen={false}
            isDrawer={true}
            onCloseDrawer={onClose}
            expanded={isExpanded}
            onToggleExpand={() => setIsExpanded((prev) => !prev)}
          />
        </div>
      </aside>
    </div>
  );
};
