import React, { useState, useRef, useEffect, useMemo } from 'react';
import { ArrowUp, Square, Terminal, Cpu, Database } from 'lucide-react';
import { ModelSelector } from './ModelSelector';
import type { AIModel, ThinkingLevel, SlashCommand, ContextUsage } from './types';

interface PromptInputProps {
  onSend: (message: string) => void;
  onAbort: () => void;
  isProcessing: boolean;
  models: AIModel[];
  activeModel: AIModel | null;
  thinkingLevel?: ThinkingLevel;
  contextUsage?: ContextUsage | null;
  slashCommands?: SlashCommand[];
  onSelectModel: (provider: string, modelId: string) => void;
  onSelectThinking?: (level: ThinkingLevel) => void;
  onOpenSettings: () => void;
  disabled?: boolean;
}

export const PromptInput: React.FC<PromptInputProps> = ({
  onSend,
  onAbort,
  isProcessing,
  models,
  activeModel,
  thinkingLevel,
  contextUsage,
  slashCommands = [],
  onSelectModel,
  onSelectThinking,
  onOpenSettings,
  disabled = false,
}) => {
  const [input, setInput] = useState('');
  const [selectedCommandIndex, setSelectedCommandIndex] = useState(0);
  const textareaRef = useRef<HTMLTextAreaElement>(null);

  // Auto-resize textarea
  useEffect(() => {
    const el = textareaRef.current;
    if (!el) return;
    el.style.height = 'auto';
    el.style.height = `${Math.min(el.scrollHeight, 180)}px`;
  }, [input]);

  // Slash commands popup filter
  const isSlashActive = input.startsWith('/') && !input.includes(' ');
  const slashQuery = isSlashActive ? input.slice(1).toLowerCase() : '';

  const filteredCommands = useMemo(() => {
    if (!isSlashActive) return [];
    if (!slashQuery) return slashCommands.slice(0, 8);
    return slashCommands
      .filter((cmd) => cmd.name.toLowerCase().includes(slashQuery))
      .slice(0, 8);
  }, [isSlashActive, slashQuery, slashCommands]);

  const handleSubmit = (e?: React.FormEvent) => {
    if (e) e.preventDefault();
    if (!input.trim() || isProcessing || disabled) return;
    onSend(input.trim());
    setInput('');
    if (textareaRef.current) {
      textareaRef.current.style.height = 'auto';
    }
  };

  const handleSelectCommand = (cmdName: string) => {
    setInput(`/${cmdName} `);
    textareaRef.current?.focus();
  };

  const handleKeyDown = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    if (isSlashActive && filteredCommands.length > 0) {
      if (e.key === 'ArrowDown') {
        e.preventDefault();
        setSelectedCommandIndex((prev) => (prev + 1) % filteredCommands.length);
        return;
      }
      if (e.key === 'ArrowUp') {
        e.preventDefault();
        setSelectedCommandIndex(
          (prev) => (prev - 1 + filteredCommands.length) % filteredCommands.length
        );
        return;
      }
      if (e.key === 'Tab' || e.key === 'Enter') {
        e.preventDefault();
        const cmd = filteredCommands[selectedCommandIndex];
        if (cmd) {
          handleSelectCommand(cmd.name);
        }
        return;
      }
    }

    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      handleSubmit();
    }
  };

  // Format context usage
  const contextDisplay = useMemo(() => {
    if (!contextUsage) return null;
    const tokens = contextUsage.tokens > 1000 ? `${(contextUsage.tokens / 1000).toFixed(1)}k` : `${contextUsage.tokens}`;
    const window = contextUsage.contextWindow > 1000 ? `${(contextUsage.contextWindow / 1000).toFixed(0)}k` : `${contextUsage.contextWindow}`;
    const percent = Math.round(contextUsage.percent * 10) / 10;
    return { tokens, window, percent };
  }, [contextUsage]);

  return (
    <div className="flex flex-col gap-2 relative">
      {/* Slash Commands Dropdown Popup */}
      {isSlashActive && filteredCommands.length > 0 && (
        <div className="absolute bottom-full left-0 mb-2 w-full max-w-md rounded-xl border border-white/[0.12] bg-[#0c0d0e]/95 p-1 shadow-2xl backdrop-blur-md z-40 animate-in fade-in zoom-in-95 duration-100">
          <div className="px-2.5 py-1 text-[10px] font-semibold uppercase tracking-wider text-zinc-500 border-b border-white/[0.04]">
            OMP Slash Commands
          </div>
          <div className="max-h-56 overflow-y-auto divide-y divide-white/[0.02]">
            {filteredCommands.map((cmd, idx) => {
              const isSelected = idx === selectedCommandIndex;
              return (
                <button
                  key={cmd.name}
                  type="button"
                  onClick={() => handleSelectCommand(cmd.name)}
                  className={`flex w-full items-center justify-between px-2.5 py-1.5 text-xs text-left rounded-lg transition-colors ${
                    isSelected
                      ? 'bg-orange-500/20 text-orange-300 font-medium'
                      : 'text-zinc-300 hover:bg-white/[0.04]'
                  }`}
                >
                  <div className="flex items-center gap-2 min-w-0">
                    <Terminal className="size-3 text-orange-400 shrink-0" />
                    <span className="font-mono font-semibold">/{cmd.name}</span>
                    {cmd.description && (
                      <span className="text-[11px] text-zinc-500 truncate max-w-[220px]">
                        {cmd.description}
                      </span>
                    )}
                  </div>
                  {cmd.input?.hint && (
                    <span className="text-[10px] text-zinc-600 font-mono">
                      {cmd.input.hint}
                    </span>
                  )}
                </button>
              );
            })}
          </div>
        </div>
      )}

      {/* Main Composer Box */}
      <div
        data-slot="prompt-input"
        className="relative flex min-w-0 flex-col rounded-2xl border border-white/[0.1] bg-[#0c0d0e]/95 p-2 shadow-lg backdrop-blur-md transition-all focus-within:border-orange-500/50 focus-within:ring-1 focus-within:ring-orange-500/20"
      >
        {/* Top Tools Bar: Model Selector & Live Context Usage */}
        <div data-slot="prompt-input-tools" className="flex items-center justify-between pb-1 px-1">
          <div className="flex items-center gap-2">
            <ModelSelector
              models={models}
              activeModel={activeModel}
              thinkingLevel={thinkingLevel}
              onSelect={onSelectModel}
              onSelectThinking={onSelectThinking}
              onOpenSettings={onOpenSettings}
            />
          </div>

          {/* Context Window Usage Counter */}
          {contextDisplay ? (
            <div className="flex items-center gap-1.5 text-[10px] font-mono text-zinc-400 bg-white/[0.03] px-2 py-0.5 rounded-full border border-white/[0.06]">
              <Database className="size-2.5 text-orange-400" />
              <span>
                {contextDisplay.tokens}/{contextDisplay.window} tokens ({contextDisplay.percent}%)
              </span>
            </div>
          ) : (
            <span className="text-[10px] text-zinc-500 font-mono">Type / for commands</span>
          )}
        </div>

        {/* Text Area */}
        <textarea
          ref={textareaRef}
          data-slot="prompt-input-textarea"
          value={input}
          onChange={(e) => setInput(e.target.value)}
          onKeyDown={handleKeyDown}
          placeholder="Ask NextDeploy AI or type / for OMP commands..."
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
              /
            </kbd>{' '}
            <span className="hidden sm:inline">for commands</span>
          </div>

          <div>
            {isProcessing ? (
              <button
                type="button"
                onClick={onAbort}
                className="group/button inline-flex size-8 items-center justify-center rounded-full bg-red-500/20 text-red-400 border border-red-500/30 hover:bg-red-500/30 hover:text-red-300 transition-all shadow-xs"
              >
                <Square className="size-3.5 fill-current" />
              </button>
            ) : (
              <button
                type="button"
                onClick={() => handleSubmit()}
                disabled={!input.trim() || disabled}
                className="group/button inline-flex size-8 items-center justify-center rounded-full bg-orange-500 text-white hover:bg-orange-600 disabled:opacity-30 disabled:pointer-events-none transition-all shadow-xs"
              >
                <ArrowUp className="size-4" />
              </button>
            )}
          </div>
        </div>
      </div>
    </div>
  );
};
