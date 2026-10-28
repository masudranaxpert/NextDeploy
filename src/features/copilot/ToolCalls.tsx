import React, { useState } from 'react';
import {
  Activity,
  Globe,
  Database,
  Terminal,
  Shield,
  HardDrive,
  Cpu,
  CheckCircle2,
  AlertCircle,
  Loader2,
  ChevronDown,
  Wrench,
} from 'lucide-react';
import { ToolExecution } from './types';

interface ToolCallsProps {
  tools: ToolExecution[];
}

function getToolMeta(name: string) {
  switch (name) {
    case 'diagnose_site_performance':
      return {
        label: 'Diagnosing Site Performance',
        icon: <Activity className="size-3.5 text-emerald-400" />,
      };
    case 'list_sites':
      return {
        label: 'Listing Hosting Sites',
        icon: <Globe className="size-3.5 text-blue-400" />,
      };
    case 'get_site_details':
      return {
        label: 'Inspecting Site Configuration',
        icon: <Globe className="size-3.5 text-blue-400" />,
      };
    case 'add_domain':
      return {
        label: 'Configuring Domain & TLS',
        icon: <Globe className="size-3.5 text-amber-400" />,
      };
    case 'remove_domain':
      return {
        label: 'Removing Domain Route',
        icon: <Globe className="size-3.5 text-rose-400" />,
      };
    case 'update_php_config':
      return {
        label: 'Tuning PHP Runtime',
        icon: <Cpu className="size-3.5 text-violet-400" />,
      };
    case 'restart_site':
      return {
        label: 'Restarting PHP-FPM Service',
        icon: <Cpu className="size-3.5 text-amber-400" />,
      };
    case 'get_site_logs':
      return {
        label: 'Inspecting Service Logs',
        icon: <Terminal className="size-3.5 text-zinc-400" />,
      };
    case 'check_edge_shield':
      return {
        label: 'Checking Edge Shield Node',
        icon: <Shield className="size-3.5 text-indigo-400" />,
      };
    case 'get_server_resources':
      return {
        label: 'Inspecting Server Resources',
        icon: <HardDrive className="size-3.5 text-emerald-400" />,
      };
    case 'execute_bash':
      return {
        label: 'Running Site Command',
        icon: <Terminal className="size-3.5 text-orange-400" />,
      };
    default:
      return {
        label: name.replace(/_/g, ' '),
        icon: <Wrench className="size-3.5 text-zinc-400" />,
      };
  }
}

export const ToolCalls: React.FC<ToolCallsProps> = ({ tools }) => {
  const [groupOpen, setGroupOpen] = useState(true);
  const [expandedToolId, setExpandedToolId] = useState<string | null>(null);

  if (!tools || tools.length === 0) return null;

  const runningCount = tools.filter((t) => t.status === 'running').length;
  const summaryText =
    tools.length === 1
      ? getToolMeta(tools[0].name).label
      : `Executed ${tools.length} tasks${runningCount > 0 ? ` (${runningCount} active)` : ''}`;

  return (
    <ol className="my-2 flex min-w-0 flex-col gap-1 select-none" data-slot="tool-calls">
      <li>
        <div className="flex min-w-0 flex-col gap-1.5" data-slot="tool-group">
          {/* Group Header Toggle */}
          <button
            type="button"
            data-slot="collapsible-trigger"
            onClick={() => setGroupOpen((prev) => !prev)}
            className="flex min-h-7 w-full min-w-0 items-center gap-2 rounded-md text-start text-xs text-zinc-400 hover:text-zinc-200 transition-colors cursor-pointer group"
          >
            <span className="flex size-4 shrink-0 items-center justify-center">
              {runningCount > 0 ? (
                <Loader2 className="size-3.5 text-blue-400 animate-spin" />
              ) : (
                <CheckCircle2 className="size-3.5 text-emerald-400" />
              )}
            </span>
            <span className="min-w-0 flex-1 truncate font-medium">
              {summaryText}
            </span>
            <ChevronDown
              className={`size-3 text-zinc-500 transition-transform duration-200 ${
                groupOpen ? 'rotate-180 text-zinc-300' : ''
              }`}
            />
          </button>

          {/* Group Items */}
          {groupOpen && (
            <div className="flex flex-col gap-1.5 pl-6 border-l border-white/[0.06] ml-2">
              {tools.map((tool) => {
                const meta = getToolMeta(tool.name);
                const isExpanded = expandedToolId === tool.id;
                const duration =
                  tool.endedAt && tool.startedAt
                    ? ((tool.endedAt - tool.startedAt) / 1000).toFixed(1) + 's'
                    : null;

                return (
                  <div
                    key={tool.id}
                    data-slot="tool-call"
                    className="overflow-hidden rounded-lg border border-white/[0.06] bg-zinc-950/50 text-xs"
                  >
                    <button
                      type="button"
                      onClick={() =>
                        setExpandedToolId(isExpanded ? null : tool.id)
                      }
                      className="flex w-full items-center justify-between px-3 py-2 text-left hover:bg-white/[0.02] transition-colors cursor-pointer"
                    >
                      <div className="flex items-center gap-2 min-w-0">
                        {meta.icon}
                        <span className="font-medium text-zinc-300 truncate">
                          {meta.label}
                        </span>
                        {duration && (
                          <span className="text-[10px] text-zinc-500 font-mono">
                            ({duration})
                          </span>
                        )}
                      </div>

                      <div className="flex items-center gap-2 shrink-0">
                        {tool.status === 'running' ? (
                          <span className="flex items-center gap-1 rounded bg-blue-500/10 px-1.5 py-0.5 text-[10px] text-blue-400 font-mono">
                            running
                          </span>
                        ) : tool.status === 'completed' ? (
                          <span className="flex items-center gap-1 rounded bg-emerald-500/10 px-1.5 py-0.5 text-[10px] text-emerald-400 font-mono">
                            done
                          </span>
                        ) : (
                          <span className="flex items-center gap-1 rounded bg-rose-500/10 px-1.5 py-0.5 text-[10px] text-rose-400 font-mono">
                            failed
                          </span>
                        )}
                        <ChevronDown
                          className={`size-3 text-zinc-500 transition-transform ${
                            isExpanded ? 'rotate-180' : ''
                          }`}
                        />
                      </div>
                    </button>

                    {isExpanded && (
                      <div className="border-t border-white/[0.04] bg-black/40 p-3 space-y-2">
                        {tool.args && Object.keys(tool.args).length > 0 && (
                          <div>
                            <span className="text-[10px] font-semibold uppercase tracking-wider text-zinc-500">
                              Parameters
                            </span>
                            <pre className="mt-1 p-2 rounded-md bg-zinc-900 border border-white/[0.04] text-[11px] font-mono text-zinc-300 overflow-x-auto">
                              {JSON.stringify(tool.args, null, 2)}
                            </pre>
                          </div>
                        )}

                        {tool.result && (
                          <div>
                            <span className="text-[10px] font-semibold uppercase tracking-wider text-zinc-500">
                              Output
                            </span>
                            <pre className="mt-1 p-2 rounded-md bg-zinc-900 border border-white/[0.04] text-[11px] font-mono text-zinc-300 max-h-48 overflow-y-auto whitespace-pre-wrap">
                              {tool.result}
                            </pre>
                          </div>
                        )}
                      </div>
                    )}
                  </div>
                );
              })}
            </div>
          )}
        </div>
      </li>
    </ol>
  );
};
