import React from 'react';
import { HextaAICanvas } from './HextaAICanvas';

// The page shell grows with its content, so the chat sizes itself to the
// viewport (below the 3.5rem top bar); otherwise a long thread pushes the
// composer off-screen instead of scrolling inside the thread.
export const CopilotView: React.FC = () => {
  return (
    <div className="h-[calc(100dvh-3.5rem)] max-h-[calc(100dvh-3.5rem)] min-h-0 w-full overflow-hidden bg-background">
      <HextaAICanvas initialSidebarOpen={true} />
    </div>
  );
};
