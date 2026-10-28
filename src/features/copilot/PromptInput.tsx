import React, { useState, useRef, useEffect } from 'react';
import { ArrowUp, Square, Sparkles } from 'lucide-react';
import { ModelSelector } from './ModelSelector';
import type { AIModel, ThinkingLevel } from './types';

interface PromptInputProps {
  onSend: (message: string) => void;
  onAbort: () => void;
  isProcessing: boolean;
  models: AIModel[];
  activeModel: AIModel | null;
  thinkingLevel?: ThinkingLevel;
  onSelectModel: (provider: string, modelId: string) => void;
  onSelectThinking?: (level: ThinkingLevel) => void;
  onOpenSettings: () => void;
  disabled?: boolean;
}

const QUICK_SUGGESTIONS = [
  'Check server resource usage & load',
  'List all running sites and ports',
  'Diagnose PHP performance & OPcache',
  'Check Edge Shield proxy status',
];

export const PromptInput: React.FC<PromptInputProps> = ({
  onSend,
  onAbort,
  isProcessing,
  models,
  activeModel,
  thinkingLevel,
  onSelectModel,
  onSelectThinking,
  onOpenSettings,
  disabled = false,
}) => {
  const [input, setInput] = useState('');
  const textareaRef = useRef<HTMLTextAreaElement>(null);

  // Auto-resize textarea
  useEffect(() => {
    const el = textareaRef.current;
    if (!el) return;
    el.style.height = 'auto';
    el.style.height = `${Math.min(el.scrollHeight, 180)}px`;
  }, [input]);

  const handleSubmit = (e?: React.FormEvent) => {
    if (e) e.preventDefault();
    if (!input.trim() || isProcessing || disabled) return;
    onSend(input.trim());
    setInput('');
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

  const handleSelectSuggestion = (text: string) => {
    if (isProcessing || disabled) return;
    onSend(text);
  };

  return (
    <div className="flex flex-col gap-2.5">
      {/* Suggestions Pills (Show when input is empty & not processing) */}
      {!input && !isProcessing && (
        <div className="flex flex-wrap items-center gap-1.5 px-1">
          {QUICK_SUGGESTIONS.map((sug, i) => (
            <button
              key={i}
              type="button"
              onClick={() => handleSelectSuggestion(sug)}
              className="inline-flex items-center gap-1.5 rounded-full border border-white/[0.06] bg-white/[0.02] px-2.5 py-1 text-[11px] text-zinc-400 hover:border-white/[0.12] hover:bg-white/[0.06] hover:text-zinc-200 transition-colors"
            >
              <Sparkles className="size-2.5 text-orange-400/80" />
              <span>{sug}</span>
            </button>
          ))}
        </div>
      )}

      {/* Main Composer Box */}
      <div
        data-slot="prompt-input"
        className="relative flex min-w-0 flex-col rounded-2xl border border-white/[0.1] bg-[#0c0d0e]/95 p-2 shadow-lg backdrop-blur-md transition-all focus-within:border-orange-500/50 focus-within:ring-1 focus-within:ring-orange-500/20"
      >
        {/* Top Tools Bar: Model Selector */}
        <div data-slot="prompt-input-tools" className="flex items-center justify-between pb-1 px-1">
          <ModelSelector
            models={models}
            activeModel={activeModel}
            thinkingLevel={thinkingLevel}
            onSelect={onSelectModel}
            onSelectThinking={onSelectThinking}
            onOpenSettings={onOpenSettings}
          />
          <span className="text-[10px] text-zinc-500 font-mono">NextDeploy AI</span>
        </div>

        {/* Text Area */}
        <textarea
          ref={textareaRef}
          data-slot="prompt-input-textarea"
          value={input}
          onChange={(e) => setInput(e.target.value)}
          onKeyDown={handleKeyDown}
          placeholder="Ask NextDeploy AI to inspect containers, configure domains, or tune performance..."
          rows={1}
          disabled={disabled}
          className="w-full resize-none bg-transparent px-2.5 py-2 text-sm text-zinc-100 placeholder:text-zinc-500 outline-none leading-relaxed min-h-[44px] max-h-[180px]"
        />

        {/* Bottom Actions Bar */}
        <div data-slot="prompt-input-actions" className="flex items-center justify-between pt-1 px-1">
          <div className="text-[11px] text-zinc-500">
            <span className="hidden sm:inline">Press </span>
            <kbd className="rounded border border-white/[0.1] bg-white/[0.04] px-1 py-0.5 text-[10px] text-zinc-400 font-mono">
              ↵
            </kbd>{' '}
            <span className="hidden sm:inline">to send, </span>
            <kbd className="rounded border border-white/[0.1] bg-white/[0.04] px-1 py-0.5 text-[10px] text-zinc-400 font-mono">
              ⇧↵
            </kbd>{' '}
            <span className="hidden sm:inline">for newline</span>
          </div>

          <div>
            {isProcessing ? (
              <button
                type="button"
                onClick={onAbort}
                className="group/button inline-flex size-8 items-center justify-center rounded-full bg-red-500/20 text-red-400 border border-red-500/30 hover:bg-red-500/30 hover:text-red-300 transition-all shadow-xs"
                title="Stop Generation"
              >
                <Square className="size-3.5 fill-current" />
              </button>
            ) : (
              <button
                type="button"
                onClick={() => handleSubmit()}
                disabled={!input.trim() || disabled}
                className="group/button inline-flex size-8 items-center justify-center rounded-full bg-orange-600 text-white hover:bg-orange-500 disabled:opacity-30 disabled:hover:bg-orange-600 transition-all shadow-xs"
                title="Send Message"
              >
                <ArrowUp className="size-4 stroke-[2.5]" />
              </button>
            )}
          </div>
        </div>
      </div>
    </div>
  );
};
