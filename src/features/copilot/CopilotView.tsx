import React from 'react';
import { Bot, Sparkles, Shield, Cpu, Terminal, Zap } from 'lucide-react';
import { CopilotThread } from './CopilotThread';

export const CopilotView: React.FC = () => {
  return (
    <div className="flex flex-col h-[calc(100vh-4rem)] rounded-xl border border-white/[0.08] bg-[#0c0c0e] overflow-hidden shadow-2xl">
      {/* Top Bar */}
      <div className="flex h-14 items-center justify-between border-b border-white/[0.07] px-5 bg-zinc-950/80 shrink-0">
        <div className="flex items-center gap-3">
          <div className="flex h-8 w-8 items-center justify-center rounded-lg bg-orange-500/15 text-orange-400 border border-orange-500/30">
            <Sparkles className="h-4.5 w-4.5" />
          </div>
          <div>
            <div className="flex items-center gap-2">
              <h1 className="text-sm font-semibold text-zinc-100">
                NextDeploy AI Copilot
              </h1>
              <span className="rounded bg-orange-500/10 px-1.5 py-0.5 font-mono text-[10px] text-orange-400 ring-1 ring-orange-500/20">
                oh-my-pi RPC Core
              </span>
            </div>
            <p className="text-[11px] text-zinc-400">
              Autonomous host agent for NextDeploy panel operations, site tuning & diagnostics
            </p>
          </div>
        </div>

        <div className="hidden sm:flex items-center gap-2 text-xs text-zinc-400">
          <div className="flex items-center gap-1.5 rounded-lg border border-white/[0.06] bg-zinc-900/60 px-2.5 py-1">
            <Cpu className="h-3 w-3 text-emerald-400" />
            <span>11 Host Tools</span>
          </div>
          <div className="flex items-center gap-1.5 rounded-lg border border-white/[0.06] bg-zinc-900/60 px-2.5 py-1">
            <Shield className="h-3 w-3 text-blue-400" />
            <span>Safe Mode Guard</span>
          </div>
        </div>
      </div>

      {/* Main Copilot Thread */}
      <div className="flex-1 min-h-0">
        <CopilotThread />
      </div>
    </div>
  );
};
