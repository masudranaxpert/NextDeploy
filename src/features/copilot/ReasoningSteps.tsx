import React, { useState } from 'react';
import { Brain, ChevronDown } from 'lucide-react';

interface ReasoningStepsProps {
  thinking: string;
  durationSec?: number;
  isStreaming?: boolean;
}

export const ReasoningSteps: React.FC<ReasoningStepsProps> = ({
  thinking,
  durationSec = 0,
  isStreaming = false,
}) => {
  const [isOpen, setIsOpen] = useState(isStreaming);

  if (!thinking) return null;

  return (
    <div data-slot="reasoning-steps" className="mb-3 flex flex-col gap-1.5">
      <div data-slot="collapsible" className="overflow-hidden rounded-lg border border-white/[0.06] bg-zinc-950/40">
        <button
          type="button"
          data-slot="collapsible-trigger"
          onClick={() => setIsOpen((prev) => !prev)}
          className="flex w-full items-center justify-between px-3 py-2 text-xs font-medium text-zinc-400 hover:text-zinc-200 transition-colors select-none"
        >
          <span className="flex items-center gap-2">
            <Brain
              className={`h-3.5 w-3.5 ${
                isStreaming ? 'animate-pulse text-amber-400' : 'text-zinc-500'
              }`}
            />
            <span>
              {isStreaming
                ? 'Thinking...'
                : `Thought for ${durationSec > 0 ? durationSec : 1} second${durationSec === 1 ? '' : 's'}`}
            </span>
          </span>
          <ChevronDown
            className={`h-3.5 w-3.5 text-zinc-500 transition-transform duration-200 ${
              isOpen ? 'rotate-180 text-zinc-300' : ''
            }`}
          />
        </button>

        {isOpen && (
          <div
            data-slot="collapsible-content"
            className="border-t border-white/[0.04] bg-black/30 px-3.5 py-2.5 text-xs font-mono text-zinc-400/90 leading-relaxed whitespace-pre-wrap max-h-60 overflow-y-auto"
          >
            {thinking}
            {isStreaming && (
              <span className="inline-block h-3 w-1.5 ml-1 bg-amber-400/80 animate-pulse align-middle" />
            )}
          </div>
        )}
      </div>
    </div>
  );
};
