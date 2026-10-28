import React, { useState, useRef, useEffect } from 'react';
import { ChevronDown, Check, Sparkles, Settings2, Cpu, BrainCircuit } from 'lucide-react';
import type { AIModel, ThinkingLevel } from './types';

interface ModelSelectorProps {
  models: AIModel[];
  activeModel: AIModel | null;
  thinkingLevel?: ThinkingLevel;
  onSelect: (provider: string, modelId: string) => void;
  onSelectThinking?: (level: ThinkingLevel) => void;
  onOpenSettings: () => void;
}

const THINKING_LEVELS: { id: ThinkingLevel; label: string }[] = [
  { id: 'off', label: 'Thinking Off' },
  { id: 'low', label: 'Thinking Low (~2k)' },
  { id: 'medium', label: 'Thinking Medium (~8k)' },
  { id: 'high', label: 'Thinking High (~16k)' },
  { id: 'max', label: 'Thinking Max (Full)' },
];

export const ModelSelector: React.FC<ModelSelectorProps> = ({
  models,
  activeModel,
  thinkingLevel = 'low',
  onSelect,
  onSelectThinking,
  onOpenSettings,
}) => {
  const [isOpen, setIsOpen] = useState(false);
  const containerRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const handleOutsideClick = (event: MouseEvent) => {
      if (containerRef.current && !containerRef.current.contains(event.target as Node)) {
        setIsOpen(false);
      }
    };
    if (isOpen) {
      document.addEventListener('mousedown', handleOutsideClick);
    }
    return () => {
      document.removeEventListener('mousedown', handleOutsideClick);
    };
  }, [isOpen]);

  // Group models by provider
  const grouped = models.reduce((acc, m) => {
    const p = m.provider.toLowerCase();
    if (!acc[p]) acc[p] = [];
    acc[p].push(m);
    return acc;
  }, {} as Record<string, AIModel[]>);

  const displayName = activeModel ? activeModel.name : 'Select Model';

  return (
    <div className="relative inline-flex items-center gap-1.5" ref={containerRef}>
      {/* Model Button Trigger */}
      <button
        type="button"
        onClick={() => setIsOpen(!isOpen)}
        aria-haspopup="true"
        aria-expanded={isOpen}
        data-slot="dropdown-menu-trigger"
        className="group/button inline-flex items-center gap-1.5 rounded-full border border-white/[0.08] bg-white/[0.04] px-2.5 py-1 text-xs font-medium text-zinc-300 hover:border-white/[0.14] hover:bg-white/[0.08] hover:text-white transition-all shadow-xs"
      >
        <Sparkles className="size-3 text-orange-400 shrink-0" />
        <span className="max-w-[130px] truncate sm:max-w-[170px]">{displayName}</span>
        <ChevronDown
          className={`size-3 text-zinc-400 transition-transform duration-150 ${
            isOpen ? 'rotate-180' : ''
          }`}
        />
      </button>

      {/* Dropdown Popover */}
      {isOpen && (
        <div
          data-slot="dropdown-menu"
          className="absolute bottom-full left-0 mb-2 w-72 origin-bottom-left rounded-xl border border-white/[0.1] bg-[#0e1012] p-1.5 text-zinc-200 shadow-xl backdrop-blur-md z-50 animate-in fade-in zoom-in-95 duration-100"
        >
          {/* Thinking Level Section */}
          {onSelectThinking && (
            <div className="border-b border-white/[0.06] pb-2 mb-1 px-1">
              <div className="flex items-center gap-1.5 text-[10px] font-semibold uppercase tracking-wider text-zinc-400 mb-1.5">
                <BrainCircuit className="size-3 text-orange-400" />
                <span>Reasoning Effort</span>
              </div>
              <div className="grid grid-cols-2 gap-1">
                {THINKING_LEVELS.map((tl) => (
                  <button
                    key={tl.id}
                    type="button"
                    onClick={() => onSelectThinking(tl.id)}
                    className={`rounded-lg px-2 py-1 text-[11px] text-left transition-colors ${
                      thinkingLevel === tl.id
                        ? 'bg-orange-500/20 text-orange-300 font-medium'
                        : 'text-zinc-400 hover:bg-white/[0.04] hover:text-zinc-200'
                    }`}
                  >
                    {tl.label}
                  </button>
                ))}
              </div>
            </div>
          )}

          {/* Model List */}
          <div className="max-h-64 overflow-y-auto divide-y divide-white/[0.04]">
            {Object.keys(grouped).length === 0 ? (
              <div className="p-4 text-center text-xs text-zinc-500">
                <p>No authenticated models found.</p>
                <p className="mt-1 text-[11px] text-zinc-600">Connect a provider in Settings.</p>
              </div>
            ) : (
              Object.entries(grouped).map(([providerKey, provModels]) => (
                <div key={providerKey} className="py-1">
                  <div className="px-2 py-1 text-[10px] font-semibold uppercase tracking-wider text-zinc-500">
                    {providerKey}
                  </div>
                  {provModels.map((m) => {
                    const isSelected =
                      activeModel?.id === m.id && activeModel?.provider === m.provider;
                    return (
                      <button
                        key={`${m.provider}:${m.id}`}
                        type="button"
                        onClick={() => {
                          onSelect(m.provider, m.id);
                          setIsOpen(false);
                        }}
                        className={`flex w-full items-center justify-between rounded-lg px-2.5 py-1.5 text-xs text-left transition-colors ${
                          isSelected
                            ? 'bg-orange-500/15 text-orange-300 font-medium'
                            : 'hover:bg-white/[0.06] text-zinc-300'
                        }`}
                      >
                        <div className="flex items-center gap-2 min-w-0">
                          <Cpu className="size-3 text-zinc-500 shrink-0" />
                          <span className="truncate">{m.name}</span>
                          {m.reasoning && (
                            <span className="rounded bg-orange-400/10 px-1 py-0.2 text-[9px] text-orange-400">
                              think
                            </span>
                          )}
                        </div>
                        {isSelected && <Check className="size-3.5 text-orange-400 shrink-0" />}
                      </button>
                    );
                  })}
                </div>
              ))
            )}
          </div>

          {/* Settings link */}
          <div className="border-t border-white/[0.06] pt-1 mt-1">
            <button
              type="button"
              onClick={() => {
                setIsOpen(false);
                onOpenSettings();
              }}
              className="flex w-full items-center gap-2 rounded-lg px-2.5 py-1.5 text-xs text-zinc-400 hover:bg-white/[0.06] hover:text-zinc-200 transition-colors"
            >
              <Settings2 className="size-3.5 text-zinc-400" />
              <span>Authentication & API Keys...</span>
            </button>
          </div>
        </div>
      )}
    </div>
  );
};
