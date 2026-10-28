import React, { useState, useRef, useEffect } from 'react';
import { ArrowUp, Square, Sparkles, ShieldCheck, Zap } from 'lucide-react';
import { AutonomyMode } from './types';

interface CopilotComposerProps {
  onSend: (message: string) => void;
  onAbort: () => void;
  isProcessing: boolean;
  autonomyMode: AutonomyMode;
  onToggleMode: (mode: AutonomyMode) => void;
}

export const CopilotComposer: React.FC<CopilotComposerProps> = ({
  onSend,
  onAbort,
  isProcessing,
  autonomyMode,
  onToggleMode,
}) => {
  const [text, setText] = useState('');
  const textareaRef = useRef<HTMLTextAreaElement>(null);

  // Auto-focus input
  useEffect(() => {
    if (!isProcessing) {
      textareaRef.current?.focus();
    }
  }, [isProcessing]);

  const handleSubmit = (e?: React.FormEvent) => {
    if (e) e.preventDefault();
    if (!text.trim() || isProcessing) return;
    onSend(text.trim());
    setText('');
    if (textareaRef.current) {
      textareaRef.current.style.height = 'auto';
    }
  };

  const handleKeyDown = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      handleSubmit();
    }
  };

  const handleInput = (e: React.ChangeEvent<HTMLTextAreaElement>) => {
    setText(e.target.value);
    // Auto grow textarea up to 160px
    const el = e.target;
    el.style.height = 'auto';
    el.style.height = `${Math.min(el.scrollHeight, 160)}px`;
  };

  return (
    <div
      data-slot="composer"
      className="p-3 bg-zinc-950/80 border-t border-white/[0.07] backdrop-blur-md"
    >
      {/* Top badges: Model pill & Autonomy toggle */}
      <div className="mb-2 flex items-center justify-between text-xs">
        <div className="flex items-center gap-1.5">
          <div className="flex items-center gap-1.5 rounded-full border border-white/10 bg-white/[0.04] px-2.5 py-1 text-zinc-300 font-medium">
            <Sparkles className="h-3 w-3 text-amber-400" />
            <span className="text-[11px]">omp Core</span>
          </div>

          <button
            type="button"
            onClick={() =>
              onToggleMode(autonomyMode === 'safe' ? 'autonomous' : 'safe')
            }
            title={
              autonomyMode === 'safe'
                ? 'Safe Mode: requires approval before executing mutating commands'
                : 'Autonomous Mode: automatically executes host tools'
            }
            className={`flex items-center gap-1.5 rounded-full px-2.5 py-1 font-mono text-[10px] transition-all cursor-pointer ${
              autonomyMode === 'safe'
                ? 'border border-emerald-500/30 bg-emerald-500/10 text-emerald-400'
                : 'border border-amber-500/30 bg-amber-500/10 text-amber-300'
            }`}
          >
            {autonomyMode === 'safe' ? (
              <>
                <ShieldCheck className="h-3 w-3" />
                <span>Safe Mode</span>
              </>
            ) : (
              <>
                <Zap className="h-3 w-3" />
                <span>Autonomous</span>
              </>
            )}
          </button>
        </div>

        <span className="text-[10px] text-zinc-500 hidden sm:inline">
          Enter to send, Shift+Enter for new line
        </span>
      </div>

      {/* Input box */}
      <div
        data-slot="prompt-input"
        className="relative flex items-end gap-2 rounded-xl border border-white/10 bg-zinc-900/90 px-3 py-2 shadow-inner focus-within:border-white/20 focus-within:ring-1 focus-within:ring-white/20 transition-all"
      >
        <textarea
          ref={textareaRef}
          value={text}
          onChange={handleInput}
          onKeyDown={handleKeyDown}
          placeholder="Ask NextDeploy AI to tune PHP, add domains, or inspect logs..."
          rows={1}
          className="flex-1 max-h-40 resize-none bg-transparent text-xs text-zinc-100 placeholder:text-zinc-500 outline-none leading-relaxed py-1"
        />

        {isProcessing ? (
          <button
            type="button"
            onClick={onAbort}
            className="flex h-7 w-7 items-center justify-center rounded-lg bg-rose-500/20 text-rose-400 hover:bg-rose-500/30 active:scale-95 transition-all cursor-pointer shrink-0"
            title="Stop generation"
          >
            <Square className="h-3.5 w-3.5 fill-current" />
          </button>
        ) : (
          <button
            type="button"
            onClick={() => handleSubmit()}
            disabled={!text.trim()}
            className="flex h-7 w-7 items-center justify-center rounded-lg bg-white text-zinc-950 font-semibold disabled:opacity-30 disabled:cursor-not-allowed hover:bg-zinc-200 active:scale-95 transition-all cursor-pointer shrink-0"
            title="Send prompt"
          >
            <ArrowUp className="h-4 w-4" />
          </button>
        )}
      </div>
    </div>
  );
};
