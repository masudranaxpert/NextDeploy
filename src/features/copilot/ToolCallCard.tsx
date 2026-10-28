import React, { useState } from 'react';
import { Terminal, CheckCircle2, AlertCircle, Loader2, ChevronDown, Wrench } from 'lucide-react';
import { ToolExecution } from './types';

interface ToolCallCardProps {
  tool: ToolExecution;
}

export const ToolCallCard: React.FC<ToolCallCardProps> = ({ tool }) => {
  const [isOpen, setIsOpen] = useState(false);
  const duration =
    tool.endedAt && tool.startedAt
      ? ((tool.endedAt - tool.startedAt) / 1000).toFixed(1) + 's'
      : null;

  return (
    <div
      data-slot="tool-call"
      className="my-1.5 overflow-hidden rounded-lg border border-white/[0.08] bg-zinc-950/60 shadow-xs text-xs"
    >
      <button
        type="button"
        onClick={() => setIsOpen((prev) => !prev)}
        className="flex w-full items-center justify-between px-3 py-2 text-left hover:bg-white/[0.02] transition-colors"
      >
        <div className="flex items-center gap-2 min-w-0">
          {tool.status === 'running' ? (
            <Loader2 className="h-3.5 w-3.5 text-blue-400 animate-spin shrink-0" />
          ) : tool.status === 'completed' ? (
            <CheckCircle2 className="h-3.5 w-3.5 text-emerald-400 shrink-0" />
          ) : (
            <AlertCircle className="h-3.5 w-3.5 text-rose-400 shrink-0" />
          )}

          <span className="font-mono font-medium text-zinc-200 truncate">
            {tool.name}
          </span>

          {duration && (
            <span className="text-[10px] text-zinc-500 font-mono">
              ({duration})
            </span>
          )}
        </div>

        <div className="flex items-center gap-1.5 shrink-0 ml-2">
          <span className="rounded bg-white/[0.05] px-1.5 py-0.5 font-mono text-[9px] uppercase tracking-wider text-zinc-400">
            tool
          </span>
          <ChevronDown
            className={`h-3 w-3 text-zinc-500 transition-transform duration-200 ${
              isOpen ? 'rotate-180' : ''
            }`}
          />
        </div>
      </button>

      {isOpen && (
        <div className="border-t border-white/[0.05] bg-black/40 p-3 space-y-2">
          {tool.args && Object.keys(tool.args).length > 0 && (
            <div>
              <div className="text-[10px] font-semibold uppercase tracking-wider text-zinc-500 mb-1">
                Parameters
              </div>
              <pre className="p-2 rounded bg-zinc-900/80 border border-white/[0.04] text-[11px] font-mono text-zinc-300 overflow-x-auto">
                {JSON.stringify(tool.args, null, 2)}
              </pre>
            </div>
          )}

          {tool.result && (
            <div>
              <div className="text-[10px] font-semibold uppercase tracking-wider text-zinc-500 mb-1">
                Output
              </div>
              <pre className="p-2 rounded bg-zinc-900/80 border border-white/[0.04] text-[11px] font-mono text-zinc-300 max-h-48 overflow-y-auto whitespace-pre-wrap">
                {tool.result}
              </pre>
            </div>
          )}
        </div>
      )}
    </div>
  );
};

export const ToolGroup: React.FC<{ tools: ToolExecution[] }> = ({ tools }) => {
  if (!tools || tools.length === 0) return null;

  return (
    <div data-slot="tool-group" className="mb-2 space-y-1">
      {tools.map((t) => (
        <ToolCallCard key={t.id} tool={t} />
      ))}
    </div>
  );
};
