import React, { useEffect, useMemo, useRef, useState } from 'react';
import { Terminal as XTerm } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import '@xterm/xterm/css/xterm.css';
import {
  RotateCcw,
  Copy,
  Maximize2,
  Minimize2,
  Box,
  ChevronDown,
  Terminal as TerminalIcon,
} from 'lucide-react';
import { toast } from 'sonner';
import { StatusBadge } from './Primitives';

export interface TerminalServiceItem {
  id: string;
  service: string;
  name: string;
  state: string;
  image?: string;
}

interface XTermConsoleProps {
  hostLabel?: string;
  promptPrefix?: string;
  initialBanner?: string[];
  presets?: string[];
  height?: number;
  services?: TerminalServiceItem[];
  appSlug?: string;
  initialService?: string;
}

const BASE_COMMANDS: string[] = [
  'clear',
  'docker compose ps',
  'docker compose logs --tail=50',
  'docker compose restart',
  'docker compose up -d',
  'docker compose down',
  'docker ps',
  'docker stats --no-stream',
  'ls -la',
  'env',
  'ps aux',
  'cat docker-compose.yml',
  'cat .env',
  'cat .nextdeploy.generated.compose.yml',
  'df -h',
  'free -h',
  'netstat -tulnp',
  'nd status',
  'top -b -n 1 | head -n 12',
  'whoami',
  'pwd',
  'help',
  'git status',
  'git log -n 5 --oneline',
  'npm run build',
  'redis-cli ping',
  'psql -U postgres -l',
];

export const XTermConsole: React.FC<XTermConsoleProps> = ({
  hostLabel = 'root@nextdeploy',
  promptPrefix = 'root@nextdeploy:~# ',
  initialBanner,
  height = 400,
  services = [],
  appSlug,
  initialService,
}) => {
  const hasMultiServices = services.length > 0;

  const [selectedService, setSelectedService] = useState<string>(() => {
    if (initialService && services.some((s) => s.service === initialService)) {
      return initialService;
    }
    return services[0]?.service || '';
  });

  const [expanded, setExpanded] = useState(false);
  const [serviceMenuOpen, setServiceMenuOpen] = useState(false);
  const [wsConnectedState, setWsConnectedState] = useState(false);

  const containerRef = useRef<HTMLDivElement>(null);
  const dropdownRef = useRef<HTMLDivElement>(null);
  const xtermRef = useRef<XTerm | null>(null);
  const fitAddonRef = useRef<FitAddon | null>(null);
  const currentLineRef = useRef<string>('');
  const historyRef = useRef<string[]>([
    'docker compose ps',
    'ls -la',
    'env',
    'docker stats --no-stream',
    'df -h',
  ]);
  const historyIdxRef = useRef<number>(-1);

  // Sync when parent passes a new initialService (e.g. clicking Shell on a service row)
  useEffect(() => {
    if (initialService && services.some((s) => s.service === initialService)) {
      setSelectedService(initialService);
    }
  }, [initialService, services]);

  // Close dropdown on outside click
  useEffect(() => {
    const handleClickOutside = (e: MouseEvent) => {
      if (dropdownRef.current && !dropdownRef.current.contains(e.target as Node)) {
        setServiceMenuOpen(false);
      }
    };
    document.addEventListener('mousedown', handleClickOutside);
    return () => document.removeEventListener('mousedown', handleClickOutside);
  }, []);

  const activeServiceObj = useMemo(() => {
    if (!hasMultiServices) return undefined;
    return services.find((s) => s.service === selectedService) || services[0];
  }, [hasMultiServices, services, selectedService]);

  const effectivePrompt = useMemo(() => {
    if (hasMultiServices && activeServiceObj) {
      const slugPart = appSlug || 'app';
      return `root@${slugPart}-${activeServiceObj.service}:/app# `;
    }
    return promptPrefix;
  }, [hasMultiServices, activeServiceObj, appSlug, promptPrefix]);

  const effectivePromptRef = useRef(effectivePrompt);
  effectivePromptRef.current = effectivePrompt;

  const activeServiceRef = useRef(activeServiceObj);
  activeServiceRef.current = activeServiceObj;

  // Build candidate commands for inline ghost completion
  const candidateCommands = useMemo<string[]>(() => {
    const dynamic: string[] = [];
    if (hasMultiServices) {
      services.forEach((s) => {
        dynamic.push(`docker compose logs -f ${s.service}`);
        dynamic.push(`docker compose restart ${s.service}`);
        dynamic.push(`docker compose exec ${s.service} sh`);
      });
    }
    return [...BASE_COMMANDS, ...dynamic];
  }, [hasMultiServices, services]);

  const candidateCommandsRef = useRef<string[]>(candidateCommands);
  candidateCommandsRef.current = candidateCommands;

  const formatPromptAnsi = (p: string): string => {
    const [userHost, pathPart] = p.split(':');
    if (pathPart) {
      return `\x1b[1;38;5;208m${userHost}\x1b[0m:\x1b[1;34m${pathPart}\x1b[0m`;
    }
    return `\x1b[1;38;5;208m${p}\x1b[0m`;
  };

  // Find inline ghost suffix right at the typing cursor (VS Code / Fish shell style)
  const getInlineGhostSuffix = (input: string): string => {
    if (!input || input.length === 0) return '';
    const lower = input.toLowerCase();

    // 1. Check command history first for exact prefix match
    for (const h of historyRef.current) {
      if (h.length > input.length && h.toLowerCase().startsWith(lower)) {
        return h.slice(input.length);
      }
    }

    // 2. Check dictionary of commands for prefix match
    for (const cmd of candidateCommandsRef.current) {
      if (cmd.length > input.length && cmd.toLowerCase().startsWith(lower)) {
        return cmd.slice(input.length);
      }
    }

    return '';
  };

  // Redraws current prompt + user input + dimmed inline ghost completion at cursor
  const renderActiveLine = (term: XTerm, showGhost = true) => {
    const promptAnsi = formatPromptAnsi(effectivePromptRef.current);
    const line = currentLineRef.current;
    const ghost = showGhost ? getInlineGhostSuffix(line) : '';

    if (ghost.length > 0) {
      term.write(
        `\r\x1b[2K${promptAnsi}${line}\x1b[38;5;240m${ghost}\x1b[0m\x1b[${ghost.length}D`
      );
    } else {
      term.write(`\r\x1b[2K${promptAnsi}${line}`);
    }
  };

  const writeNewPromptLine = (term: XTerm, leadingNewline = true) => {
    const promptAnsi = formatPromptAnsi(effectivePromptRef.current);
    if (leadingNewline) {
      term.write(`\r\n${promptAnsi}`);
    } else {
      term.write(promptAnsi);
    }
  };

  const performClear = (term: XTerm) => {
    currentLineRef.current = '';
    // ANSI sequence: \x1b[2J (erase screen) + \x1b[3J (erase scrollback) + \x1b[H (home cursor 1,1)
    term.write('\x1b[2J\x1b[3J\x1b[H');
    term.clear();
    writeNewPromptLine(term, false);
  };

  const executeCommand = (rawCmd: string) => {
    const term = xtermRef.current;
    if (!term) return;
    const cmd = rawCmd.trim();
    const svc = activeServiceRef.current;

    if (!cmd) {
      writeNewPromptLine(term, true);
      return;
    }

    historyRef.current = [cmd, ...historyRef.current.filter((c) => c !== cmd)].slice(0, 40);
    historyIdxRef.current = -1;

    if (cmd === 'clear' || cmd === 'cls') {
      performClear(term);
      return;
    }

    term.write('\r\n');

    // Without a live connection nothing can run: say so instead of pretending.
    term.writeln('\x1b[31mNot connected to the server terminal.\x1b[0m Reload the page and try again.');

    writeNewPromptLine(term, false);
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

    if (initialBanner && initialBanner.length > 0) {
      initialBanner.forEach((line) => term.writeln(line));
    }
    currentLineRef.current = '';

    // Connect to live backend PTY WebSocket (/apps/:id/ws/terminal or /terminal/ws)
    let ws: WebSocket | null = null;
    let wsConnected = false;
    try {
      const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
      const containerParam = activeServiceObj?.name || activeServiceObj?.service || '';
      const wsPath = appSlug
        ? `/apps/${encodeURIComponent(appSlug)}/ws/terminal${
            containerParam ? `?container=${encodeURIComponent(containerParam)}` : ''
          }`
        : `/terminal/ws`;
      ws = new WebSocket(`${proto}//${window.location.host}${wsPath}`);
      ws.binaryType = 'arraybuffer';
      ws.onopen = () => {
        wsConnected = true;
        setWsConnectedState(true);
        term.focus();
      };
      ws.onmessage = (ev) => {
        wsConnected = true;
        setWsConnectedState(true);
        if (typeof ev.data === 'string') {
          term.write(ev.data);
        } else if (ev.data instanceof ArrayBuffer) {
          term.write(new Uint8Array(ev.data));
        }
      };
      ws.onerror = () => {
        setWsConnectedState(false);
        if (!wsConnected) {
          writeNewPromptLine(term, false);
        }
      };
      ws.onclose = () => {
        setWsConnectedState(false);
        if (!wsConnected) {
          writeNewPromptLine(term, false);
        }
        wsConnected = false;
      };
    } catch {
      setWsConnectedState(false);
      writeNewPromptLine(term, false);
    }

    term.onData((data) => {
      if (ws && wsConnected && ws.readyState === WebSocket.OPEN) {
        ws.send(data);
        return;
      }

      const code = data.charCodeAt(0);

      if (data === '\t' || data === '\x1b[C') {
        // Tab or Right Arrow: accept inline ghost completion if available
        const ghost = getInlineGhostSuffix(currentLineRef.current);
        if (ghost.length > 0) {
          currentLineRef.current += ghost;
          renderActiveLine(term, false);
        }
        return;
      }

      if (data === '\x1b') {
        // Escape: hide ghost completion on current line
        renderActiveLine(term, false);
        return;
      }

      if (data === '\r') {
        // Enter: strip any unaccepted ghost suffix and execute strictly what the user typed
        const line = currentLineRef.current;
        renderActiveLine(term, false);
        currentLineRef.current = '';
        executeCommand(line);
      } else if (code === 127 || data === '\b') {
        if (currentLineRef.current.length > 0) {
          currentLineRef.current = currentLineRef.current.slice(0, -1);
          renderActiveLine(term, true);
        }
      } else if (data === '\x03') {
        // Ctrl+C
        renderActiveLine(term, false);
        currentLineRef.current = '';
        term.write('^C');
        writeNewPromptLine(term, true);
      } else if (data === '\x0c') {
        // Ctrl+L: Full clear
        performClear(term);
      } else if (data === '\x1b[A') {
        // Up arrow: cycle command history
        const hist = historyRef.current;
        if (hist.length > 0 && historyIdxRef.current < hist.length - 1) {
          historyIdxRef.current += 1;
          currentLineRef.current = hist[historyIdxRef.current];
          renderActiveLine(term, false);
        }
      } else if (data === '\x1b[B') {
        // Down arrow: cycle command history
        const hist = historyRef.current;
        if (historyIdxRef.current > 0) {
          historyIdxRef.current -= 1;
          currentLineRef.current = hist[historyIdxRef.current];
          renderActiveLine(term, false);
        } else {
          historyIdxRef.current = -1;
          currentLineRef.current = '';
          renderActiveLine(term, false);
        }
      } else if (code >= 32 && code !== 127 && !data.startsWith('\x1b')) {
        currentLineRef.current += data;
        renderActiveLine(term, true);
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
      if (ws) {
        try {
          ws.close();
        } catch {
          // ignore
        }
      }
      term.dispose();
    };
  }, [activeServiceObj?.service, activeServiceObj?.name, appSlug]);

  useEffect(() => {
    setTimeout(() => {
      try {
        fitAddonRef.current?.fit();
      } catch {
        // ignore
      }
    }, 50);
  }, [expanded]);

  const handleSelectService = (serviceName: string) => {
    setSelectedService(serviceName);
    setServiceMenuOpen(false);
    currentLineRef.current = '';
    toast.success(`Switched terminal to "${serviceName}"`);
  };

  const handleClearTerminal = () => {
    const term = xtermRef.current;
    if (!term) return;
    performClear(term);
    term.focus();
  };

  return (
    <div className="rounded-xl border border-white/[0.08] bg-[#070709] overflow-hidden shadow-2xl">
      {/* Clean Terminal Header */}
      <div className="flex flex-wrap items-center justify-between gap-2 border-b border-white/[0.07] bg-[#0d0d11] px-3 py-2">
        <div className="flex items-center gap-2.5">
          <div className="flex items-center gap-1.5 pr-2 border-r border-white/[0.08]">
            <span className="h-2.5 w-2.5 rounded-full bg-rose-500/80" />
            <span className="h-2.5 w-2.5 rounded-full bg-amber-500/80" />
            <span className="h-2.5 w-2.5 rounded-full bg-emerald-500/80" />
          </div>

          {hasMultiServices && activeServiceObj ? (
            <div className="relative" ref={dropdownRef}>
              <button
                type="button"
                onClick={() => setServiceMenuOpen((prev) => !prev)}
                className="inline-flex h-7 items-center gap-2 rounded-lg border border-white/[0.08] bg-zinc-900 px-2.5 text-xs font-medium text-zinc-200 hover:bg-zinc-800 cursor-pointer"
              >
                <span
                  className={`h-1.5 w-1.5 rounded-full ${
                    activeServiceObj.state === 'running' ? 'bg-emerald-400' : 'bg-zinc-500'
                  }`}
                />
                <Box className="h-3.5 w-3.5 text-orange-400" />
                <span>{activeServiceObj.service}</span>
                {activeServiceObj.image && (
                  <span className="hidden sm:inline text-[11px] font-mono text-zinc-500">
                    ({activeServiceObj.image})
                  </span>
                )}
                <ChevronDown className="h-3.5 w-3.5 text-zinc-400" />
              </button>

              {serviceMenuOpen && (
                <div className="absolute left-0 top-8 z-30 w-64 rounded-xl border border-white/[0.08] bg-[#111115] p-1.5 shadow-2xl">
                  <div className="px-2 py-1 text-[10px] font-semibold uppercase tracking-wider text-zinc-500">
                    Select Service Container
                  </div>
                  {services.map((s) => {
                    const selected = s.service === activeServiceObj.service;
                    return (
                      <button
                        key={s.id}
                        type="button"
                        onClick={() => handleSelectService(s.service)}
                        className={`flex w-full items-center justify-between rounded-lg px-2.5 py-1.5 text-left text-xs cursor-pointer ${
                          selected
                            ? 'bg-orange-500/15 text-orange-300 font-medium'
                            : 'text-zinc-300 hover:bg-white/[0.05]'
                        }`}
                      >
                        <div className="flex items-center gap-2 min-w-0">
                          <span
                            className={`h-1.5 w-1.5 rounded-full shrink-0 ${
                              s.state === 'running' ? 'bg-emerald-400' : 'bg-zinc-500'
                            }`}
                          />
                          <span className="truncate">{s.service}</span>
                        </div>
                        <span className="text-[10px] font-mono text-zinc-500 truncate max-w-[110px]">
                          {s.image || s.state}
                        </span>
                      </button>
                    );
                  })}
                </div>
              )}
            </div>
          ) : (
            <div className="inline-flex h-7 items-center gap-2 rounded-lg bg-zinc-900/80 px-2.5 text-xs font-medium text-zinc-200 border border-white/[0.06]">
              <TerminalIcon className="h-3.5 w-3.5 text-orange-400" />
              <span className="font-mono">{hostLabel}</span>
            </div>
          )}
        </div>

        {/* Right Controls */}
        <div className="flex items-center gap-1.5">
          <StatusBadge
            status={wsConnectedState ? 'connected' : 'stopped'}
            label={wsConnectedState ? 'Connected' : 'Connecting...'}
          />
          <button
            type="button"
            onClick={handleClearTerminal}
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

      {/* XTerm.js Viewport */}
      <div
        ref={containerRef}
        onClick={() => xtermRef.current?.focus()}
        style={{ height: expanded ? 540 : height }}
        className="w-full px-3 py-2 cursor-text"
      />
    </div>
  );
};
