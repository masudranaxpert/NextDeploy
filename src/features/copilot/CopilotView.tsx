import React from 'react';
import { HextaAICanvas } from './HextaAICanvas';

export const CopilotView: React.FC = () => {
  return (
    <div className="h-full w-full overflow-hidden bg-background">
      <HextaAICanvas initialSidebarOpen={true} />
    </div>
  );
};
