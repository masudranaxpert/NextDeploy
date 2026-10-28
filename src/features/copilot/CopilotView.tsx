import React from 'react';
import { HextaAICanvas } from './HextaAICanvas';

export const CopilotView: React.FC = () => {
  return (
    <div className="h-[calc(100vh-4rem)] w-full overflow-hidden rounded-2xl border border-white/[0.08] bg-[#090a0b] shadow-2xl">
      <HextaAICanvas initialSidebarOpen={true} />
    </div>
  );
};
