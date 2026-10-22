import React from 'react';
import { Terminal } from 'lucide-react';
import { StatusBadge } from '../../components/ui/Primitives';
import { XTermConsole } from '../../components/ui/XTermConsole';

export const VpsTerminalView: React.FC = () => {
  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="flex items-center gap-2.5">
          <div className="flex h-8 w-8 items-center justify-center rounded-lg border border-white/[0.08] bg-zinc-900">
            <Terminal className="h-4 w-4 text-orange-400" />
          </div>
          <h1 className="text-base font-semibold tracking-tight text-zinc-100">Host Terminal</h1>
          <StatusBadge status="running" label="PTY Connected" />
        </div>
        <div className="flex items-center gap-2 font-mono text-[11px] text-zinc-400">
          <span className="rounded-lg border border-white/[0.07] bg-[#111115] px-2.5 py-1">
            Ubuntu 24.04 LTS · Docker 27.3
          </span>
        </div>
      </div>

      <XTermConsole
        hostLabel="root@nextdeploy"
        promptPrefix="root@nextdeploy:~# "
        wsPath="/terminal/ws"
        height={420}
      />
    </div>
  );
};
