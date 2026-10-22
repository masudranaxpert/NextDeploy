import React, { useEffect, useRef, useState } from 'react';
import { Terminal as XTerm } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import '@xterm/xterm/css/xterm.css';
import {
  Terminal as TerminalIcon,
  Plus,
  RotateCcw,
  Copy,
  Maximize2,
  Minimize2,
  Play,
  X,
} from 'lucide-react';
import { toast } from 'sonner';
import { StatusBadge } from './Primitives';

interface XTermConsoleProps {
  hostLabel?: string;
  promptPrefix?: string;
  initialBanner?: string[];
  presets?: string[];
  height?: number;
}

interface TermTab {
  id: string;
  title: string;
  shell: string;
}

export const XTermConsole: React.FC<XTermConsoleProps> = ({
  hostLabel = 'root@nextdeploy',
  promptPrefix = 'root@nextdeploy:~# ',
  initialBanner = [
    '\x1b[38;5;208m╭────────────────────────────────────────────────────────────────────╮\x1b[0m',
    '\x1b[38;5;208m│\x1b[0m  \x1b[1;37mNextDeploy Interactive PTY\x1b[0m · Ubuntu 24.04 LTS · Docker 27.3.1     \x1b[38;5;208m│\x1b[0m',
    '\x1b[38;5;208m╰────────────────────────────────────────────────────────────────────╯\x1b[0m',
    '\x1b[90mType \x1b[33mhelp\x1b[90m or click a command below to inspect the host.\x1b[0m',
  ],
  presets = ['docker ps', 'nd status', 'docker stats --no-stream', 'free -h', 'df -h', 'clear'],
  height = 380,
}) => {
  const containerRef = useRef<HTMLDivElement>(null);
  const xtermRef = useRef<XTerm | null>(null);
  const fitAddonRef = useRef<FitAddon | null>(null);
  const currentLineRef = useRef<string>('');
  const historyRef = useRef<string[]>(['docker ps', 'nd status', 'free -h', 'df -h']);
  const historyIdxRef = useRef<number>(-1);

  const [tabs, setTabs] = useState<TermTab[]>([
    { id: 'tab-1', title: hostLabel, shell: 'bash' },
    { id: 'tab-2', title: 'docker-events', shell: 'sh' },
  ]);
  const [activeTabId, setActiveTabId] = useState<string>('tab-1');
  const [expanded, setExpanded] = useState(false);

  const writePrompt = (term: XTerm) => {
    const [userHost, pathPart] = promptPrefix.split(':');
    if (pathPart) {
      term.write(`\r\n\x1b[1;38;5;208m${userHost}\x1b[0m:\x1b[1;34m${pathPart}\x1b[0m`);
    } else {
      term.write(`\r\n\x1b[1;38;5;208m${promptPrefix}\x1b[0m`);
    }
  };

  const executeCommand = (rawCmd: string) => {
    const term = xtermRef.current;
    if (!term) return;
    const cmd = rawCmd.trim();

    if (!cmd) {
      writePrompt(term);
      return;
    }

    historyRef.current = [cmd, ...historyRef.current.filter((c) => c !== cmd)].slice(0, 30);
    historyIdxRef.current = -1;

    if (cmd === 'clear') {
      term.clear();
      writePrompt(term);
      return;
    }

    term.write('\r\n');

    if (cmd === 'help') {
      term.writeln('\x1b[1;37mAvailable NextDeploy & Host Commands:\x1b[0m');
      term.writeln('  \x1b[38;5;208mdocker ps\x1b[0m                 List running compose containers');
      term.writeln('  \x1b[38;5;208mdocker stats --no-stream\x1b[0m  Live container CPU / Memory snapshot');
      term.writeln('  \x1b[38;5;208mnd status\x1b[0m                 NextDeploy control plane & Caddy status');
      term.writeln('  \x1b[38;5;208mfree -h\x1b[0m                   Host memory allocation');
      term.writeln('  \x1b[38;5;208mdf -h\x1b[0m                     NVMe disk usage');
      term.writeln('  \x1b[38;5;208mls -la\x1b[0m                    List workspace files');
      term.writeln('  \x1b[38;5;208mclear\x1b[0m                     Clear terminal buffer');
    } else if (cmd === 'docker ps') {
      term.writeln(
        '\x1b[90mCONTAINER ID   IMAGE                                          STATUS          PORTS                  NAMES\x1b[0m'
      );
      term.writeln(
        '901a8b12c4f1   ghcr.io/masudranaxpert/commerce-api:8c4f19a    \x1b[32mUp 6 days\x1b[0m       127.0.0.1:3000->3000   \x1b[38;5;208mcommerce-core-api\x1b[0m-web-1'
      );
      term.writeln(
        '901a8b13e8a0   postgres:16.4-alpine                           \x1b[32mUp 19 days\x1b[0m      5432/tcp               \x1b[38;5;208mcommerce-core-api\x1b[0m-postgres-1'
      );
      term.writeln(
        '771b201f99a1   ghcr.io/masudranaxpert/studio-web:f42a90c      \x1b[32mUp 12 days\x1b[0m      127.0.0.1:3001->3000   \x1b[38;5;208mstudio-web-dashboard\x1b[0m-app-1'
      );
      term.writeln(
        '551a100c88e2   ghcr.io/plausible/analytics:v2.1               \x1b[32mUp 29 days\x1b[0m      127.0.0.1:8000->8000   \x1b[38;5;208mplausible-analytics\x1b[0m-plausible-1'
      );
    } else if (cmd.startsWith('docker stats')) {
      term.writeln(
        '\x1b[90mCONTAINER                        CPU %     MEM USAGE / LIMIT     MEM %     NET I/O\x1b[0m'
      );
      term.writeln(
        'commerce-core-api-web-1          \x1b[32m4.20%\x1b[0m     248MiB / 1GiB         24.22%    1.42GB / 890MB'
      );
      term.writeln(
        'commerce-core-api-postgres-1     \x1b[32m2.80%\x1b[0m     184MiB / 1GiB         17.96%    620MB / 410MB'
      );
      term.writeln(
        'studio-web-dashboard-app-1       \x1b[32m5.80%\x1b[0m     518MiB / 1GiB         50.58%    2.10GB / 1.15GB'
      );
    } else if (cmd === 'nd status') {
      term.writeln('\x1b[32m●\x1b[0m \x1b[1;37mnextdeploy.service\x1b[0m - NextDeploy Control Plane Daemon');
      term.writeln('   Loaded: loaded (/etc/systemd/system/nextdeploy.service; enabled)');
      term.writeln('   Active: \x1b[1;32mactive (running)\x1b[0m since Mon 2026-10-01 08:00:12 UTC; 18 days ago');
      term.writeln('    Caddy: \x1b[32mhttp://localhost:2019 (4 TLS routes active)\x1b[0m');
      term.writeln('      MCP: \x1b[32m/mcp/sse (16 tools registered)\x1b[0m');
    } else if (cmd === 'free -h') {
      term.writeln(
        '\x1b[90m               total        used        free      shared  buff/cache   available\x1b[0m'
      );
      term.writeln(
        'Mem:           31.2Gi      15.4Gi       4.2Gi       312Mi      11.6Gi      15.8Gi'
      );
      term.writeln('Swap:          4.0Gi          0B       4.0Gi');
    } else if (cmd === 'df -h') {
      term.writeln('\x1b[90mFilesystem      Size  Used Avail Use% Mounted on\x1b[0m');
      term.writeln('/dev/nvme0n1p1  320G   84G  236G  \x1b[32m27%\x1b[0m /');
      term.writeln('tmpfs            16G  1.2M   16G   1% /run');
    } else if (cmd.startsWith('ls')) {
      term.writeln('drwxr-xr-x 4 root root 4096 Oct 19 18:20 \x1b[1;34m/data/apps\x1b[0m');
      term.writeln('-rw-r--r-- 1 root root 1420 Oct 19 18:20 docker-compose.yml');
      term.writeln(
        '-rw-r--r-- 1 root root  618 Oct 19 18:20 \x1b[38;5;208m.nextdeploy.generated.compose.yml\x1b[0m'
      );
    } else if (cmd === 'whoami') {
      term.writeln('root');
    } else {
      term.writeln(`\x1b[32m✓\x1b[0m Executed: \x1b[1;37m${cmd}\x1b[0m (exit code 0)`);
    }

    writePrompt(term);
  };

  const runPresetCommand = (preset: string) => {
    const term = xtermRef.current;
    if (!term) return;
    while (currentLineRef.current.length > 0) {
      term.write('\b \b');
      currentLineRef.current = currentLineRef.current.slice(0, -1);
    }
    term.write(preset);
    currentLineRef.current = '';
    executeCommand(preset);
    term.focus();
  };

  useEffect(() => {
    if (!containerRef.current) return;

    const term = new XTerm({
      cursorBlink: true,
      cursorStyle: 'bar',
      fontFamily: "'JetBrains Mono', ui-monospace, SFMono-Regular, Menlo, monospace",
      fontSize: 12.5,
      lineHeight: 1.45,
      theme: {
        background: '#070709',
        foreground: '#fafafa',
        cursor: '#f97316',
        cursorAccent: '#070709',
        selectionBackground: 'rgba(249, 115, 22, 0.28)',
        black: '#09090b',
        red: '#fb7185',
        green: '#34d399',
        yellow: '#fbbf24',
        blue: '#60a5fa',
        magenta: '#c084fc',
        cyan: '#22d3ee',
        white: '#fafafa',
        brightBlack: '#71717a',
      },
    });

    const fitAddon = new FitAddon();
    term.loadAddon(fitAddon);
    term.open(containerRef.current);

    setTimeout(() => {
      try {
        fitAddon.fit();
      } catch {
        // ignore layout race
      }
    }, 30);

    initialBanner.forEach((line) => term.writeln(line));
    writePrompt(term);

    term.onData((data) => {
      const code = data.charCodeAt(0);
      if (data === '\r') {
        const line = currentLineRef.current;
        currentLineRef.current = '';
        executeCommand(line);
      } else if (code === 127 || data === '\b') {
        if (currentLineRef.current.length > 0) {
          currentLineRef.current = currentLineRef.current.slice(0, -1);
          term.write('\b \b');
        }
      } else if (data === '\x03') {
        currentLineRef.current = '';
        term.write('^C');
        writePrompt(term);
      } else if (data === '\x0c') {
        currentLineRef.current = '';
        term.clear();
        writePrompt(term);
      } else if (data === '\x1b[A') {
        const hist = historyRef.current;
        if (hist.length > 0 && historyIdxRef.current < hist.length - 1) {
          historyIdxRef.current += 1;
          while (currentLineRef.current.length > 0) {
            term.write('\b \b');
            currentLineRef.current = currentLineRef.current.slice(0, -1);
          }
          const nextCmd = hist[historyIdxRef.current];
          currentLineRef.current = nextCmd;
          term.write(nextCmd);
        }
      } else if (data === '\x1b[B') {
        const hist = historyRef.current;
        while (currentLineRef.current.length > 0) {
          term.write('\b \b');
          currentLineRef.current = currentLineRef.current.slice(0, -1);
        }
        if (historyIdxRef.current > 0) {
          historyIdxRef.current -= 1;
          const nextCmd = hist[historyIdxRef.current];
          currentLineRef.current = nextCmd;
          term.write(nextCmd);
        } else {
          historyIdxRef.current = -1;
        }
      } else if (code >= 32 && code !== 127) {
        currentLineRef.current += data;
        term.write(data);
      }
    });

    xtermRef.current = term;
    fitAddonRef.current = fitAddon;

    const handleResize = () => {
      try {
        fitAddon.fit();
      } catch {
        // ignore
      }
    };
    window.addEventListener('resize', handleResize);

    return () => {
      window.removeEventListener('resize', handleResize);
      term.dispose();
    };
  }, [activeTabId]);

  useEffect(() => {
    setTimeout(() => {
      try {
        fitAddonRef.current?.fit();
      } catch {
        // ignore
      }
    }, 50);
  }, [expanded]);

  const addNewTab = () => {
    const nextNum = tabs.length + 1;
    const newTab: TermTab = {
      id: `tab-${Date.now()}`,
      title: `pts/${nextNum - 1}`,
      shell: 'bash',
    };
    setTabs((prev) => [...prev, newTab]);
    setActiveTabId(newTab.id);
  };

  const closeTab = (id: string, e: React.MouseEvent) => {
    e.stopPropagation();
    if (tabs.length <= 1) return;
    const remaining = tabs.filter((t) => t.id !== id);
    setTabs(remaining);
    if (activeTabId === id) {
      setActiveTabId(remaining[0].id);
    }
  };

  return (
    <div className="rounded-xl border border-white/[0.08] bg-[#070709] overflow-hidden shadow-2xl">
      {/* Ghostty / Warp Inspired Multi-Tab Terminal Header */}
      <div className="flex flex-wrap items-center justify-between gap-2 border-b border-white/[0.07] bg-[#0d0d11] px-3 py-2">
        <div className="flex items-center gap-2 overflow-x-auto no-scrollbar">
          <div className="flex items-center gap-1.5 pr-2 border-r border-white/[0.08]">
            <span className="h-2.5 w-2.5 rounded-full bg-rose-500/80" />
            <span className="h-2.5 w-2.5 rounded-full bg-amber-500/80" />
            <span className="h-2.5 w-2.5 rounded-full bg-emerald-500/80" />
          </div>

          {tabs.map((t) => {
            const active = t.id === activeTabId;
            return (
              <button
                key={t.id}
                type="button"
                onClick={() => setActiveTabId(t.id)}
                className={`group inline-flex h-7 items-center gap-2 rounded-lg px-2.5 font-mono text-xs transition-colors cursor-pointer whitespace-nowrap ${
                  active
                    ? 'bg-zinc-800/90 text-zinc-100 ring-1 ring-white/[0.12]'
                    : 'text-zinc-400 hover:bg-white/[0.04] hover:text-zinc-200'
                }`}
              >
                <TerminalIcon className="h-3 w-3 text-orange-400" />
                <span>{t.title}</span>
                {tabs.length > 1 && (
                  <span
                    onClick={(e) => closeTab(t.id, e)}
                    className="rounded p-0.5 text-zinc-500 hover:bg-white/[0.08] hover:text-white"
                  >
                    <X className="h-2.5 w-2.5" />
                  </span>
                )}
              </button>
            );
          })}

          <button
            type="button"
            onClick={addNewTab}
            className="inline-flex h-7 w-7 items-center justify-center rounded-lg text-zinc-400 hover:bg-white/[0.06] hover:text-white cursor-pointer"
            title="New PTY Session"
          >
            <Plus className="h-3.5 w-3.5" />
          </button>
        </div>

        <div className="flex items-center gap-1.5">
          <StatusBadge status="connected" label="PTY Connected" />
          <button
            type="button"
            onClick={() => runPresetCommand('clear')}
            className="inline-flex h-7 items-center gap-1 rounded-lg border border-white/[0.08] bg-zinc-900 px-2 text-[11px] font-medium text-zinc-300 hover:bg-zinc-800 hover:text-white cursor-pointer"
            title="Clear terminal"
          >
            <RotateCcw className="h-3 w-3" />
            Clear
          </button>
          <button
            type="button"
            onClick={() => {
              navigator.clipboard?.writeText(historyRef.current.join('\n'));
              toast.success('Copied command history');
            }}
            className="inline-flex h-7 items-center gap-1 rounded-lg border border-white/[0.08] bg-zinc-900 px-2 text-[11px] font-medium text-zinc-300 hover:bg-zinc-800 hover:text-white cursor-pointer"
          >
            <Copy className="h-3 w-3" />
            Copy
          </button>
          <button
            type="button"
            onClick={() => setExpanded((prev) => !prev)}
            className="inline-flex h-7 w-7 items-center justify-center rounded-lg border border-white/[0.08] bg-zinc-900 text-zinc-300 hover:bg-zinc-800 hover:text-white cursor-pointer"
            title={expanded ? 'Compact height' : 'Expand height'}
          >
            {expanded ? <Minimize2 className="h-3.5 w-3.5" /> : <Maximize2 className="h-3.5 w-3.5" />}
          </button>
        </div>
      </div>

      {/* Quick Command Bar */}
      <div className="flex items-center gap-1.5 overflow-x-auto no-scrollbar border-b border-white/[0.05] bg-[#0a0a0d] px-3 py-1.5">
        <span className="text-[10px] font-semibold uppercase tracking-wider text-zinc-500 mr-1 shrink-0">
          Quick Run
        </span>
        {presets.map((preset) => (
          <button
            key={preset}
            type="button"
            onClick={() => runPresetCommand(preset)}
            className="inline-flex items-center gap-1 rounded-md border border-white/[0.07] bg-zinc-900/80 px-2 py-0.5 font-mono text-[11px] text-zinc-300 hover:border-orange-500/40 hover:text-orange-300 transition-colors cursor-pointer whitespace-nowrap shrink-0"
          >
            <Play className="h-2.5 w-2.5 text-orange-400" />
            {preset}
          </button>
        ))}
      </div>

      {/* XTerm.js Viewport */}
      <div
        ref={containerRef}
        style={{ height: expanded ? 540 : height }}
        className="w-full px-3 py-2"
      />
    </div>
  );
};
