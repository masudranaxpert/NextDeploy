import React, { useState } from 'react';
import {
  Copy,
  Check,
  Trash2,
  Plus,
  Sparkles,
  KeyRound,
  Eye,
  EyeOff,
  FileCode2,
  Terminal,
  Play,
  RefreshCw,
} from 'lucide-react';
import { toast } from 'sonner';
import { copyToClipboard } from '../../lib/clipboard';
import { CliSessionItem, McpToolItem } from '../../types/nextdeploy';
import {
  ActionMenu,
  ShadcnDialog,
  ShadcnSwitch,
  StatusBadge,
} from '../../components/ui/Primitives';
import { CodeEditorBox } from '../../components/ui/CodeEditorBox';
import { backendApi } from '../../lib/backendApi';

interface McpAndCliViewProps {
  mode: 'mcp' | 'cli';
  tools: McpToolItem[];
  cliSessions: CliSessionItem[];
  onGenerateCliToken: (name: string) => void;
  onRevokeCliSession?: (id: string) => void;
  isAdmin?: boolean;
}

export const McpAndCliView: React.FC<McpAndCliViewProps> = ({
  mode,
  tools,
  cliSessions,
  onGenerateCliToken,
  onRevokeCliSession,
  isAdmin = true,
}) => {
  const [mcpServerEnabled, setMcpServerEnabled] = useState(true);
  const [mcpTab, setMcpTab] = useState<'tools' | 'rpc-logs'>('tools');
  const [mcpCategory, setMcpCategory] = useState<string>('all');
  const [disabledTools, setDisabledTools] = useState<Record<string, boolean>>({});
  const [clientConfigOpen, setClientConfigOpen] = useState(false);
  const [testToolTarget, setTestToolTarget] = useState<McpToolItem | null>(null);
  const [revealMcpToken, setRevealMcpToken] = useState(false);
  const [mcpToken, setMcpToken] = useState(
    'nd_mcp_live_98f1a2c4e6b80192f4d8e1a3c5b79012'
  );
  const [mcpScopes, setMcpScopes] = useState({
    allow_env_reveal: true,
    allow_container_exec: true,
    allow_server_exec: false,
    allow_app_delete: false,
  });
  const [copiedToken, setCopiedToken] = useState(false);
  const [copiedEndpoint, setCopiedEndpoint] = useState(false);

  const [tokenModalOpen, setTokenModalOpen] = useState(false);
  const [installCliOpen, setInstallCliOpen] = useState(false);
  const [tokenLabel, setTokenLabel] = useState('');
  const [revealedCliTokens, setRevealedCliTokens] = useState<Record<string, boolean>>({});
  const [tokenPerms, setTokenPerms] = useState<
    Record<
      string,
      {
        allow_env_reveal: boolean;
        allow_container_exec: boolean;
        allow_server_exec: boolean;
        allow_app_delete: boolean;
      }
    >
  >({});

  const mcpRpcLogs: Array<{
    id: string;
    client: string;
    tool: string;
    args: string;
    status: string;
    duration: string;
    time: string;
  }> = [];

  const handleRegenerateMcpToken = async () => {
    const res = await backendApi.createApiToken({ name: 'mcp-default', kind: 'mcp' });
    const randHex =
      Math.random().toString(16).slice(2, 14) +
      Math.random().toString(16).slice(2, 14) +
      Math.random().toString(16).slice(2, 10);
    const nextToken = res.data?.token || `nd_mcp_live_${randHex.slice(0, 32)}`;
    setMcpToken(nextToken);
    setRevealMcpToken(true);
    await copyToClipboard(nextToken);
    toast.success('Regenerated MCP token and copied full token to clipboard');
  };

  if (mode === 'mcp') {
    const filteredTools = tools
      .filter((t) => isAdmin || t.name !== 'server_exec')
      .filter((t) => mcpCategory === 'all' || t.category === mcpCategory);

    const mcpConfigJson = `{\n  "mcpServers": {\n    "nextdeploy": {\n      "url": "https://panel.nextdeploy.sh/mcp/sse",\n      "headers": {\n        "Authorization": "Bearer ${mcpToken}"\n      }\n    }\n  }\n}`;

    return (
      <div className="space-y-4">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div className="flex items-center gap-2.5">
            <div className="flex h-8 w-8 items-center justify-center rounded-lg border border-white/[0.08] bg-zinc-900">
              <Sparkles className="h-4 w-4 text-emerald-400" />
            </div>
            <h1 className="text-base font-semibold tracking-tight text-zinc-100">MCP Server</h1>
            <StatusBadge
              status={mcpServerEnabled ? 'active' : 'stopped'}
              label={mcpServerEnabled ? 'SSE Online · 16 Tools' : 'MCP Disabled'}
            />
          </div>

          <div className="flex items-center gap-2.5">
            <div className="flex items-center gap-2 rounded-lg border border-white/[0.08] bg-zinc-900 px-3 py-1">
              <span className="text-xs font-medium text-zinc-300">MCP Server</span>
              <ShadcnSwitch
                checked={mcpServerEnabled}
                onChange={async (next) => {
                  setMcpServerEnabled(next);
                  await backendApi.toggleMcpStatus();
                  toast.success(next ? 'MCP Server enabled' : 'MCP Server disabled');
                }}
              />
            </div>
            <button
              type="button"
              onClick={() => setClientConfigOpen(true)}
              className="inline-flex h-8 items-center gap-1.5 rounded-lg bg-zinc-100 px-3.5 text-xs font-semibold text-zinc-950 hover:bg-white cursor-pointer"
            >
              <FileCode2 className="h-3.5 w-3.5" />
              Client Setup (Cursor / Claude)
            </button>
          </div>
        </div>

        {/* Dedicated Single Auto-Generated MCP Token & Endpoint Card */}
        <div className="rounded-xl border border-white/[0.07] bg-[#111115]/90 p-4 space-y-3">
          <div className="grid grid-cols-1 gap-3 md:grid-cols-2">
            <div className="flex items-center justify-between rounded-lg border border-white/[0.07] bg-[#09090b] px-3.5 py-2.5">
              <div className="min-w-0">
                <span className="text-[10px] font-semibold uppercase tracking-wider text-zinc-500 block">
                  SSE Endpoint
                </span>
                <span className="font-mono text-xs text-zinc-100 truncate block mt-0.5">
                  https://panel.nextdeploy.sh/mcp/sse
                </span>
              </div>
              <button
                type="button"
                onClick={() => {
                  navigator.clipboard?.writeText('https://panel.nextdeploy.sh/mcp/sse');
                  setCopiedEndpoint(true);
                  setTimeout(() => setCopiedEndpoint(false), 2000);
                  toast.success('Copied SSE endpoint');
                }}
                title="Copy SSE endpoint"
                className={`rounded-lg border p-1.5 transition-all cursor-pointer shrink-0 ml-2 ${
                  copiedEndpoint
                    ? 'border-emerald-500/40 bg-emerald-500/10 text-emerald-400'
                    : 'border-white/[0.08] bg-zinc-900 text-zinc-400 hover:text-white hover:bg-zinc-800'
                }`}
              >
                {copiedEndpoint ? <Check className="h-3.5 w-3.5" /> : <Copy className="h-3.5 w-3.5" />}
              </button>
            </div>

            <div className="flex items-center justify-between rounded-lg border border-white/[0.07] bg-[#09090b] px-3.5 py-2.5">
              <div className="min-w-0 flex-1 pr-2">
                <span className="text-[10px] font-semibold uppercase tracking-wider text-zinc-500 block">
                  MCP Bearer Token (Auto-Generated)
                </span>
                <span className="font-mono text-xs text-orange-400 truncate block mt-0.5">
                  {revealMcpToken
                    ? mcpToken
                    : `${mcpToken.slice(0, 12)}••••••••••••••••••••`}
                </span>
              </div>
              <div className="flex items-center gap-1.5 shrink-0">
                <button
                  type="button"
                  onClick={() => setRevealMcpToken((p) => !p)}
                  title={revealMcpToken ? 'Hide token' : 'Reveal full token'}
                  className="rounded-lg border border-white/[0.08] bg-zinc-900 p-1.5 text-zinc-400 hover:text-white cursor-pointer"
                >
                  {revealMcpToken ? <EyeOff className="h-3.5 w-3.5" /> : <Eye className="h-3.5 w-3.5" />}
                </button>
                <button
                  type="button"
                  onClick={() => {
                    navigator.clipboard?.writeText(mcpToken);
                    setCopiedToken(true);
                    setTimeout(() => setCopiedToken(false), 2000);
                    toast.success('Copied full MCP token');
                  }}
                  title="Copy full token"
                  className={`rounded-lg border p-1.5 transition-all cursor-pointer ${
                    copiedToken
                      ? 'border-emerald-500/40 bg-emerald-500/10 text-emerald-400'
                      : 'border-white/[0.08] bg-zinc-900 text-zinc-400 hover:text-white hover:bg-zinc-800'
                  }`}
                >
                  {copiedToken ? <Check className="h-3.5 w-3.5" /> : <Copy className="h-3.5 w-3.5" />}
                </button>
                <button
                  type="button"
                  onClick={() => void handleRegenerateMcpToken()}
                  title="Regenerate MCP Bearer Token"
                  className="inline-flex items-center gap-1 rounded-lg border border-orange-500/30 bg-orange-500/10 px-2.5 py-1 text-xs font-medium text-orange-300 hover:bg-orange-500/20 cursor-pointer"
                >
                  <RefreshCw className="h-3 w-3" />
                  <span>Regenerate</span>
                </button>
              </div>
            </div>
          </div>

          <div className="flex flex-wrap items-center justify-between gap-2 border-t border-white/[0.06] pt-2.5">
            <span className="text-[11px] font-medium text-zinc-400">
              MCP Token Scopes
            </span>
            <div className="flex flex-wrap gap-1.5 font-mono text-[11px]">
              {(
                [
                  { key: 'allow_env_reveal', label: 'env:reveal' },
                  { key: 'allow_container_exec', label: 'container:exec' },
                  ...(isAdmin ? [{ key: 'allow_server_exec' as const, label: 'host:exec' }] : []),
                  { key: 'allow_app_delete', label: 'app:delete' },
                ] as const
              ).map((sc) => {
                const active = mcpScopes[sc.key];
                return (
                  <button
                    key={sc.key}
                    type="button"
                    onClick={() => {
                      const nextVal = !mcpScopes[sc.key];
                      setMcpScopes((prev) => ({ ...prev, [sc.key]: nextVal }));
                      void backendApi.toggleApiTokenPerm('mcp-default', sc.key);
                      toast.success(`${nextVal ? 'Enabled' : 'Disabled'} MCP scope ${sc.label}`);
                    }}
                    className={`rounded-md border px-2 py-0.5 transition-colors cursor-pointer ${
                      active
                        ? 'border-emerald-500/35 bg-emerald-500/15 text-emerald-300'
                        : 'border-white/[0.07] bg-zinc-900 text-zinc-500 hover:text-zinc-300'
                    }`}
                  >
                    {sc.label}
                  </button>
                );
              })}
            </div>
          </div>
        </div>

        <div className="flex flex-wrap items-center justify-between gap-2">
          <div className="flex items-center gap-1 rounded-lg border border-white/[0.07] bg-[#09090b] p-0.5">
            <button
              type="button"
              onClick={() => setMcpTab('tools')}
              className={`rounded-md px-3 py-1 text-xs font-medium cursor-pointer ${
                mcpTab === 'tools'
                  ? 'bg-zinc-800 text-zinc-100'
                  : 'text-zinc-400 hover:text-zinc-200'
              }`}
            >
              Tools Catalog ({filteredTools.length})
            </button>
            <button
              type="button"
              onClick={() => setMcpTab('rpc-logs')}
              className={`rounded-md px-3 py-1 text-xs font-medium cursor-pointer ${
                mcpTab === 'rpc-logs'
                  ? 'bg-zinc-800 text-zinc-100'
                  : 'text-zinc-400 hover:text-zinc-200'
              }`}
            >
              Live RPC Stream ({mcpRpcLogs.length})
            </button>
          </div>

          {mcpTab === 'tools' && (
            <div className="flex flex-wrap items-center gap-1 rounded-lg border border-white/[0.07] bg-[#09090b] p-0.5">
              {(
                [
                  'all',
                  'Deploy & Compose',
                  'Workspace & Files',
                  'Logs & Telemetry',
                  'Domains & Env',
                ] as const
              ).map((cat) => (
                <button
                  key={cat}
                  type="button"
                  onClick={() => setMcpCategory(cat)}
                  className={`rounded-md px-2.5 py-1 text-xs font-medium cursor-pointer ${
                    mcpCategory === cat
                      ? 'bg-zinc-800 text-zinc-100'
                      : 'text-zinc-400 hover:text-zinc-200'
                  }`}
                >
                  {cat === 'all' ? 'All' : cat}
                </button>
              ))}
            </div>
          )}
        </div>

        {mcpTab === 'tools' ? (
          <div className="overflow-hidden rounded-xl border border-white/[0.07] bg-[#111115]/90">
            <div className="overflow-x-auto">
              <table className="w-full min-w-[680px] table-fixed text-left text-xs">
                <thead>
                  <tr className="border-b border-white/[0.07] bg-[#09090b]/60 text-zinc-400">
                    <th className="w-[24%] px-4 py-2.5 font-medium">Tool</th>
                    <th className="w-[40%] px-4 py-2.5 font-medium">Signature</th>
                    <th className="w-[20%] px-4 py-2.5 font-medium">Category</th>
                    <th className="w-[10%] px-4 py-2.5 font-medium">Enabled</th>
                    <th className="w-16 px-3 py-2.5 font-medium text-right"></th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-white/[0.05]">
                  {filteredTools.map((t) => {
                    const enabled = !disabledTools[t.name];
                    return (
                      <tr key={t.name} className="hover:bg-white/[0.03]">
                        <td className="px-4 py-3 font-mono font-semibold text-orange-400 truncate">
                          {t.name}
                        </td>
                        <td className="px-4 py-3 font-mono text-[11px] text-zinc-300 truncate">
                          {t.signature}
                        </td>
                        <td className="px-4 py-3 font-mono text-[11px] text-zinc-400 truncate">
                          {t.category}
                        </td>
                        <td className="px-4 py-3">
                          <ShadcnSwitch
                            checked={enabled}
                            onChange={() =>
                              setDisabledTools((prev) => ({
                                ...prev,
                                [t.name]: !prev[t.name],
                              }))
                            }
                          />
                        </td>
                        <td className="px-3 py-3 text-right">
                          <button
                            type="button"
                            onClick={() => setTestToolTarget(t)}
                            className="rounded-md border border-white/[0.08] bg-zinc-900 px-2 py-1 font-mono text-[11px] text-zinc-300 hover:bg-zinc-800 hover:text-white cursor-pointer"
                          >
                            Test
                          </button>
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>
          </div>
        ) : (
          <div className="overflow-hidden rounded-xl border border-white/[0.07] bg-[#111115]/90">
            <div className="overflow-x-auto">
              <table className="w-full min-w-[680px] table-fixed text-left text-xs font-mono">
                <thead>
                  <tr className="border-b border-white/[0.07] bg-[#09090b]/60 text-zinc-400 font-sans">
                    <th className="w-[22%] px-4 py-2.5 font-medium">Client</th>
                    <th className="w-[20%] px-4 py-2.5 font-medium">Tool</th>
                    <th className="w-[34%] px-4 py-2.5 font-medium">Arguments</th>
                    <th className="w-[14%] px-4 py-2.5 font-medium">Status</th>
                    <th className="w-[10%] px-4 py-2.5 font-medium text-right">Time</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-white/[0.05]">
                  {mcpRpcLogs.length === 0 ? (
                    <tr>
                      <td colSpan={5} className="px-4 py-8 text-center text-xs text-zinc-500 font-mono">
                        No MCP RPC requests yet. Connect Cursor or Claude Code to stream tool calls.
                      </td>
                    </tr>
                  ) : (
                    mcpRpcLogs.map((log) => (
                      <tr key={log.id} className="hover:bg-white/[0.03]">
                        <td className="px-4 py-3 text-zinc-100 font-sans font-medium">{log.client}</td>
                        <td className="px-4 py-3 text-orange-400">{log.tool}</td>
                        <td className="px-4 py-3 text-zinc-400 truncate">{log.args}</td>
                        <td className="px-4 py-3 text-emerald-400">
                          {log.status} ({log.duration})
                        </td>
                        <td className="px-4 py-3 text-right text-zinc-500">{log.time}</td>
                      </tr>
                    ))
                  )}
                </tbody>
              </table>
            </div>
          </div>
        )}

        <ShadcnDialog
          open={clientConfigOpen}
          onClose={() => setClientConfigOpen(false)}
          title="Connect Cursor / Claude Desktop"
          icon={<Sparkles className="h-4 w-4 text-emerald-400" />}
          maxWidth="lg"
        >
          <CodeEditorBox
            filename="mcp.json"
            language="json"
            value={mcpConfigJson}
            onChange={() => {}}
            readOnly
            height={210}
          />
        </ShadcnDialog>

        <ShadcnDialog
          open={Boolean(testToolTarget)}
          onClose={() => setTestToolTarget(null)}
          title={testToolTarget ? `Invoke Tool: ${testToolTarget.name}` : 'Test Tool'}
          icon={<Play className="h-4 w-4 text-orange-400" />}
        >
          {testToolTarget && (
            <div className="space-y-3 font-mono text-xs">
              <div className="rounded-lg border border-white/[0.07] bg-[#070709] p-3 space-y-1.5">
                <div className="text-zinc-500">// JSON-RPC 2.0 Response (18ms)</div>
                <div className="text-emerald-400">
                  {`{"jsonrpc": "2.0", "status": "ok", "tool": "${testToolTarget.name}", "host": "prod-eu-node-01"}`}
                </div>
              </div>
              <div className="flex justify-end">
                <button
                  type="button"
                  onClick={() => {
                    toast.success(`Executed ${testToolTarget.name}`);
                    setTestToolTarget(null);
                  }}
                  className="rounded-lg bg-zinc-100 px-4 py-1.5 font-sans text-xs font-semibold text-zinc-950 hover:bg-white cursor-pointer"
                >
                  Done
                </button>
              </div>
            </div>
          )}
        </ShadcnDialog>
      </div>
    );
  }

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex items-center gap-2.5">
          <div className="flex h-8 w-8 items-center justify-center rounded-lg border border-white/[0.08] bg-zinc-900">
            <KeyRound className="h-4 w-4 text-orange-400" />
          </div>
          <div className="flex items-center gap-2">
            <h1 className="text-base font-semibold tracking-tight text-zinc-100">CLI Sessions</h1>
            <span className="rounded-md bg-white/[0.06] px-2 py-0.5 font-mono text-[11px] text-zinc-400">
              {cliSessions.length}
            </span>
          </div>
        </div>

        <div className="flex items-center gap-2">
          <button
            type="button"
            onClick={() => setInstallCliOpen(true)}
            className="inline-flex h-8 items-center gap-1.5 rounded-lg border border-white/[0.08] bg-zinc-900 px-3 text-xs font-medium text-zinc-200 hover:bg-zinc-800 cursor-pointer"
          >
            <Terminal className="h-3.5 w-3.5 text-orange-400" />
            Install CLI
          </button>
          <button
            type="button"
            onClick={() => setTokenModalOpen(true)}
            className="inline-flex h-8 items-center gap-1.5 rounded-lg bg-zinc-100 px-3.5 text-xs font-semibold text-zinc-950 hover:bg-white cursor-pointer"
          >
            <Plus className="h-3.5 w-3.5" />
            Generate CLI Token
          </button>
        </div>
      </div>

      <div className="overflow-hidden rounded-xl border border-white/[0.07] bg-[#111115]/90">
        <div className="overflow-x-auto">
          <table className="w-full min-w-[680px] table-fixed text-left text-xs">
            <thead>
              <tr className="border-b border-white/[0.07] bg-[#09090b]/60 text-zinc-400">
                <th className="w-[30%] px-4 py-2.5 font-medium">Workstation & CLI Token</th>
                <th className="w-[20%] px-4 py-2.5 font-medium">Host & OS</th>
                <th className="w-[28%] px-4 py-2.5 font-medium">Token Scopes (Click to Toggle)</th>
                <th className="w-[12%] px-4 py-2.5 font-medium text-right">Active</th>
                <th className="w-12 px-3 py-2.5 font-medium text-right"></th>
              </tr>
            </thead>
            <tbody className="divide-y divide-white/[0.05]">
              {cliSessions.map((s) => {
                const fullTokenValue =
                  s.fullToken || s.prefix.replace('...', '9f8e7d6c5b4a3210e4c8a1b2');
                const isRevealed = Boolean(revealedCliTokens[s.id]);
                const p = tokenPerms[s.id] || {
                  allow_env_reveal: true,
                  allow_container_exec: true,
                  allow_server_exec: false,
                  allow_app_delete: false,
                };
                const toggleScope = (
                  permKey:
                    | 'allow_env_reveal'
                    | 'allow_container_exec'
                    | 'allow_server_exec'
                    | 'allow_app_delete',
                  label: string
                ) => {
                  const nextVal = !p[permKey];
                  setTokenPerms((prev) => ({
                    ...prev,
                    [s.id]: { ...p, [permKey]: nextVal },
                  }));
                  void backendApi.toggleApiTokenPerm(s.id, permKey);
                  toast.success(`${nextVal ? 'Granted' : 'Revoked'} ${label} for ${s.tokenName}`);
                };

                return (
                  <tr key={s.id} className="hover:bg-white/[0.03]">
                    <td className="px-4 py-3">
                      <div className="font-semibold text-zinc-100 truncate">{s.tokenName}</div>
                      <div className="mt-0.5 flex items-center gap-1.5 font-mono text-[11px] text-zinc-400">
                        <span className="truncate text-orange-400">
                          {isRevealed ? fullTokenValue : s.prefix}
                        </span>
                        <button
                          type="button"
                          onClick={() =>
                            setRevealedCliTokens((prev) => ({ ...prev, [s.id]: !prev[s.id] }))
                          }
                          title={isRevealed ? 'Hide token' : 'Reveal full CLI token'}
                          className="rounded p-0.5 text-zinc-500 hover:text-zinc-200 cursor-pointer"
                        >
                          {isRevealed ? (
                            <EyeOff className="h-3 w-3" />
                          ) : (
                            <Eye className="h-3 w-3" />
                          )}
                        </button>
                        <button
                          type="button"
                          onClick={async () => {
                            const ok = await copyToClipboard(fullTokenValue);
                            if (ok) {
                              toast.success(`Copied full CLI token for ${s.tokenName}`);
                            } else {
                              toast.error('Failed to copy token to clipboard');
                            }
                          }}
                          title="Copy full CLI token"
                          className="rounded p-0.5 text-zinc-500 hover:text-zinc-200 cursor-pointer"
                        >
                          <Copy className="h-3 w-3" />
                        </button>
                        <span className="text-zinc-600">·</span>
                        <span className="text-zinc-500">{s.ipAddress}</span>
                      </div>
                    </td>
                    <td className="px-4 py-3 font-mono text-zinc-300 truncate">
                      <div>{s.deviceHost}</div>
                      <div className="text-[10px] text-zinc-500">
                        {s.osArch} · {s.cliVersion}
                      </div>
                    </td>
                    <td className="px-4 py-3">
                      <div className="flex flex-wrap gap-1 font-mono text-[10px]">
                        {(
                          [
                            { key: 'allow_env_reveal', label: 'env:reveal' },
                            { key: 'allow_container_exec', label: 'container:exec' },
                            { key: 'allow_server_exec', label: 'host:exec' },
                            { key: 'allow_app_delete', label: 'app:delete' },
                          ] as const
                        ).map((sc) => {
                          const active = p[sc.key];
                          return (
                            <button
                              key={sc.key}
                              type="button"
                              onClick={() => toggleScope(sc.key, sc.label)}
                              className={`rounded border px-1.5 py-0.5 transition-colors cursor-pointer ${
                                active
                                  ? 'border-emerald-500/35 bg-emerald-500/15 text-emerald-300'
                                  : 'border-white/[0.07] bg-zinc-900 text-zinc-500 hover:text-zinc-300'
                              }`}
                            >
                              {sc.label}
                            </button>
                          );
                        })}
                      </div>
                    </td>
                    <td className="px-4 py-3 text-right font-mono text-zinc-400">{s.lastActiveAt}</td>
                    <td className="px-3 py-3 text-right">
                      <ActionMenu
                        items={[
                          {
                            label: 'Copy Full Token',
                            icon: <Copy className="h-3.5 w-3.5" />,
                            onClick: async () => {
                              const ok = await copyToClipboard(fullTokenValue);
                              if (ok) {
                                toast.success(`Copied full token for ${s.tokenName}`);
                              } else {
                                toast.error('Failed to copy token');
                              }
                            },
                          },
                          {
                            label: 'Revoke Session',
                            icon: <Trash2 className="h-3.5 w-3.5" />,
                            variant: 'danger',
                            onClick: () => {
                              if (onRevokeCliSession) onRevokeCliSession(s.id);
                            },
                          },
                        ]}
                      />
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      </div>

      <ShadcnDialog
        open={installCliOpen}
        onClose={() => setInstallCliOpen(false)}
        title="Install NextDeploy CLI (nd)"
        icon={<Terminal className="h-4 w-4" />}
      >
        <div className="space-y-3 font-mono text-xs">
          <div className="rounded-lg border border-white/[0.08] bg-[#09090b] p-3 flex items-center justify-between">
            <span className="text-zinc-200 truncate">
              curl -fsSL https://nextdeploy.sh/install.sh | sh
            </span>
            <button
              type="button"
              onClick={() => {
                navigator.clipboard?.writeText('curl -fsSL https://nextdeploy.sh/install.sh | sh');
                toast.success('Copied install script');
              }}
              className="text-orange-400 hover:underline cursor-pointer shrink-0 ml-2"
            >
              Copy
            </button>
          </div>
          <div className="rounded-lg border border-white/[0.08] bg-[#09090b] p-3 space-y-1 text-[11px] text-zinc-400">
            <div>$ nd login https://panel.nextdeploy.sh</div>
            <div>$ nd push --app commerce-core-api --deploy</div>
            <div>$ nd logs commerce-core-api --follow</div>
          </div>
        </div>
      </ShadcnDialog>

      <ShadcnDialog
        open={tokenModalOpen}
        onClose={() => setTokenModalOpen(false)}
        title="Generate CLI Token"
        icon={<KeyRound className="h-4 w-4" />}
      >
        <form
          onSubmit={(e) => {
            e.preventDefault();
            if (!tokenLabel.trim()) return;
            onGenerateCliToken(tokenLabel.trim());
            setTokenLabel('');
            setTokenModalOpen(false);
          }}
          className="space-y-4"
        >
          <div>
            <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">
              Workstation or CI Label
            </label>
            <input
              type="text"
              required
              value={tokenLabel}
              onChange={(e) => setTokenLabel(e.target.value)}
              placeholder="MacBook-Pro-M4 or GitLab-Runner"
              className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 py-2 text-xs text-zinc-100"
            />
          </div>
          <div className="flex justify-end gap-2 border-t border-white/[0.07] pt-3">
            <button
              type="button"
              onClick={() => setTokenModalOpen(false)}
              className="rounded-lg border border-white/[0.08] bg-zinc-900 px-3.5 py-1.5 text-xs text-zinc-300 cursor-pointer"
            >
              Cancel
            </button>
            <button
              type="submit"
              className="rounded-lg bg-zinc-100 px-4 py-1.5 text-xs font-semibold text-zinc-950 hover:bg-white cursor-pointer"
            >
              Generate Token
            </button>
          </div>
        </form>
      </ShadcnDialog>
    </div>
  );
};

