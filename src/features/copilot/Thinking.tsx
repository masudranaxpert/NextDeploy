import React, { useState } from 'react';
import { Sparkles, ChevronDown } from 'lucide-react';

interface ThinkingProps {
  thinking: string;
  durationSec?: number;
  isStreaming?: boolean;
}

export const Thinking: React.FC<ThinkingProps> = ({
  thinking,
  durationSec = 0,
  isStreaming = false,
}) => {
  const [isOpen, setIsOpen] = useState(isStreaming);

  if (!thinking) return null;

  return (
    <div data-slot="collapsible" className="my-2 select-none">
      <button
        type="button"
        data-slot="collapsible-trigger"
        onClick={() => setIsOpen((prev) => !prev)}
        className="flex min-h-7 items-center gap-2 rounded-md text-xs text-zinc-400 hover:text-zinc-200 transition-colors cursor-pointer group"
      >
        <span className="flex size-4 shrink-0 items-center justify-center">
          <Sparkles
            className={`size-3.5 ${
              isStreaming
                ? 'animate-pulse text-amber-400'
                : 'text-zinc-500 group-hover:text-zinc-300'
            }`}
          />
        </span>
        <span className="font-medium">
          {isStreaming
            ? 'Thinking...'
            : `Thought for ${durationSec > 0 ? durationSec : 1}s`}
        </span>
        <ChevronDown
          className={`size-3 text-zinc-500 transition-transform duration-200 ${
            isOpen ? 'rotate-180 text-zinc-300' : ''
          }`}
        />
      </button>

      {isOpen && (
        <div
          data-slot="collapsible-content"
          className="mt-1.5 rounded-xl border border-white/[0.06] bg-zinc-950/60 p-3.5 text-xs font-mono text-zinc-400/90 leading-relaxed whitespace-pre-wrap max-h-64 overflow-y-auto"
        >
          {thinking}
          {isStreaming && (
            <span className="inline-block h-3 w-1.5 ml-1 bg-amber-400/80 animate-pulse align-middle" />
          )}
        </div>
      )}
    </div>
  );
};
