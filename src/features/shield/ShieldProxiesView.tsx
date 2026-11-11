import React, { useEffect, useState } from 'react';
import {
  Shield,
  ShieldCheck,
  ShieldAlert,
  Copy,
  Check,
  Plus,
  Trash2,
  RefreshCw,
  Terminal,
  Activity,
  Globe,
  Server,
  Lock,
  ArrowLeftRight,
  HelpCircle,
  BookOpen,
  ChevronDown,
  ChevronUp,
  KeyRound,
  Eye,
  EyeOff,
} from 'lucide-react';
import { toast } from 'sonner';
import { backendApi, getPanelId } from '../../lib/backendApi';
import { RotateShieldTokenResponse, ShieldNode } from '../../types/shield';
import { copyToClipboard } from '../../lib/clipboard';
import { ShadcnDialog } from '../../components/ui/Primitives';
import { ManageShieldModal } from '../../components/shield/ManageShieldModal';
import {
  SHIELD_PREP_COMMAND,
  buildInstallCommand,
  buildSetTokenCommand,
  buildUninstallCommand,
  getPanelOrigin,
} from '../../components/shield/shieldCommands';

function formatSyncTime(iso?: string): { short: string; full: string } | null {
  if (!iso) return null;
  const ms = Date.parse(iso);
  if (Number.isNaN(ms)) return { short: iso, full: iso };
  const date = new Date(ms);
  return {
    short: date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' }),
    full: date.toLocaleString(),
  };
}

const SYNC_STATE_STYLES: Record<string, { label: string; cls: string; dot: string }> = {
  ok: {
    label: 'Sync OK',
    cls: 'border-emerald-500/25 bg-emerald-500/10 text-emerald-400',
    dot: 'bg-emerald-400',
  },
  stale: {
    label: 'Stale',
    cls: 'border-amber-500/30 bg-amber-500/10 text-amber-300',
    dot: 'bg-amber-400',
  },
  auth_failed: {
    label: 'Auth Failed',
    cls: 'border-rose-500/30 bg-rose-500/10 text-rose-300',
    dot: 'bg-rose-400',
  },
  outdated: {
    label: 'Edge Outdated',
    cls: 'border-amber-500/30 bg-amber-500/10 text-amber-300',
    dot: 'bg-amber-400',
  },
  panel_outdated: {
    label: 'Panel Outdated',
    cls: 'border-amber-500/30 bg-amber-500/10 text-amber-300',
    dot: 'bg-amber-400',
  },
  pending: {
    label: 'Pending',
    cls: 'border-zinc-700 bg-zinc-800/70 text-zinc-300',
    dot: 'bg-zinc-400',
  },
  unregistered: {
    label: 'Unregistered',
    cls: 'border-zinc-700 bg-zinc-900 text-zinc-400',
    dot: 'bg-zinc-500',
  },
};

const NodeConnectionBadge: React.FC<{ status: ShieldNode['status'] }> = ({ status }) => {
  if (status === 'connected') {
    return (
      <span className="inline-flex items-center gap-1.5 rounded-md bg-emerald-500/10 px-2 py-0.5 text-xs font-medium text-emerald-400 ring-1 ring-emerald-500/20 shrink-0">
        <span className="h-1.5 w-1.5 rounded-full bg-emerald-400 animate-pulse" />
        Connected
      </span>
    );
  }
  if (status === 'pending') {
    return (
      <span className="inline-flex items-center gap-1.5 rounded-md bg-amber-500/10 px-2 py-0.5 text-xs font-medium text-amber-400 ring-1 ring-amber-500/20 shrink-0">
        <span className="h-1.5 w-1.5 rounded-full bg-amber-400" />
        Pending Setup
      </span>
    );
  }
  if (status === 'unregistered') {
    return (
      <span className="inline-flex items-center gap-1.5 rounded-md bg-zinc-800 px-2 py-0.5 text-xs font-medium text-zinc-400 ring-1 ring-white/10 shrink-0">
        <span className="h-1.5 w-1.5 rounded-full bg-zinc-500" />
        Unregistered
      </span>
    );
  }
  return (
    <span className="inline-flex items-center gap-1.5 rounded-md bg-rose-500/10 px-2 py-0.5 text-xs font-medium text-rose-400 ring-1 ring-rose-500/20 shrink-0">
      <span className="h-1.5 w-1.5 rounded-full bg-rose-400" />
      Offline
    </span>
  );
};

const SyncStateBadge: React.FC<{ syncState?: string; lastError?: string }> = ({ syncState, lastError }) => {
  const key = (syncState || 'pending').toLowerCase();
  const style = SYNC_STATE_STYLES[key] || {
    label: syncState || 'Unknown',
    cls: 'border-zinc-700 bg-zinc-900 text-zinc-300',
    dot: 'bg-zinc-400',
  };
  return (
    <span
      className={`inline-flex items-center gap-1.5 rounded-md border px-2 py-0.5 text-[11px] font-medium ${style.cls}`}
      title={lastError || undefined}
    >
      <span className={`h-1.5 w-1.5 rounded-full ${style.dot}`} />
      {style.label}
    </span>
  );
};

const NodeStatusReadout: React.FC<{ node: ShieldNode }> = ({ node }) => {
  return (
    <div className="flex flex-col items-start gap-1">
      <div className="flex flex-wrap items-center gap-1.5">
        <NodeConnectionBadge status={node.status} />
        <SyncStateBadge syncState={node.sync_state} lastError={node.last_error} />
      </div>
      {node.last_error && (
        <span className="text-[11px] text-rose-400/90 max-w-[16rem] truncate" title={node.last_error}>
          {node.last_error}
        </span>
      )}
    </div>
  );
};

const NodeDomainsCell: React.FC<{ node: ShieldNode }> = ({ node }) => {
  const count = node.assigned_domains || 0;
  const conflicts = node.conflicts_count || 0;
  const conflictedHosts = (node.domain_statuses || [])
    .filter((s) => s.state === 'conflict')
    .map((s) => `${s.host}${s.detail ? ` (${s.detail})` : ''}`);

  return (
    <div className="flex flex-col items-start gap-1">
      <span className="inline-flex items-center gap-1 rounded-md bg-zinc-800/80 px-2 py-0.5 text-xs font-medium text-zinc-200 border border-zinc-700/60">
        <Globe className="h-3 w-3 text-cyan-400" />
        {count} {count === 1 ? 'domain' : 'domains'}
      </span>
      {conflicts > 0 ? (
        <span
          className="inline-flex items-center gap-1 rounded-md border border-rose-500/30 bg-rose-500/10 px-2 py-0.5 text-[11px] font-medium text-rose-300"
          title={conflictedHosts.length > 0 ? `Conflicts: ${conflictedHosts.join(', ')}` : undefined}
        >
          <ShieldAlert className="h-3 w-3 shrink-0 text-rose-400" />
          {conflicts} conflict{conflicts === 1 ? '' : 's'}
        </span>
      ) : (
        <span className="text-[11px] text-zinc-500">0 conflicts</span>
      )}
    </div>
  );
};

export function ShieldProxiesView() {
  const [nodes, setNodes] = useState<ShieldNode[]>([]);
  const [originIP, setOriginIP] = useState<string>('');
  const [panelId, setPanelIdState] = useState<string>(() => getPanelId());
  const [loading, setLoading] = useState(true);
  const [showAddModal, setShowAddModal] = useState(false);
  const [showDocsModal, setShowDocsModal] = useState(false);
  const [showGuide, setShowGuide] = useState(false);
  const [selectedScriptNode, setSelectedScriptNode] = useState<ShieldNode | null>(null);

  // Form states
  const [name, setName] = useState('');
  const [ip, setIP] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [testingId, setTestingId] = useState<number | null>(null);
  const [copied, setCopied] = useState<string | null>(null);

  // Failover / Reassign / Delete / Rotate modal state
  const [reassignModalNode, setReassignModalNode] = useState<ShieldNode | null>(null);
  const [deleteModalNode, setDeleteModalNode] = useState<ShieldNode | null>(null);
  const [rotateModalNode, setRotateModalNode] = useState<ShieldNode | null>(null);
  const [rotateResult, setRotateResult] = useState<RotateShieldTokenResponse | null>(null);
  const [rotating, setRotating] = useState(false);
  const [showRotatedToken, setShowRotatedToken] = useState(false);
  const [targetShieldId, setTargetShieldId] = useState<number | null>(null);
  const [reassigning, setReassigning] = useState(false);

  const fetchNodes = async () => {
    try {
      const res = await backendApi.listShieldNodes();
      if (res.ok) {
        setNodes(res.nodes || []);
        if (res.origin_ip) setOriginIP(res.origin_ip);
        if (res.panel_id) setPanelIdState(res.panel_id);
      } else {
        toast.error(res.error || 'Failed to load shield nodes');
      }
    } catch (err: any) {
      toast.error('Failed to load shield nodes: ' + (err.message || 'Network error'));
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchNodes();
  }, []);

  const handleCopy = async (text: string, id: string) => {
    const ok = await copyToClipboard(text);
    if (ok) {
      setCopied(id);
      setTimeout(() => setCopied(null), 2000);
    }
  };

  const handleCreate = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!name.trim() || !ip.trim()) {
      toast.error('Please enter both name and public IP');
      return;
    }
    setSubmitting(true);
    try {
      const res = await backendApi.createShieldNode(name.trim(), ip.trim());
      if (res.ok && res.node) {
        toast.success(`Shield node '${res.node.name}' added successfully!`);
        if (res.panel_id) setPanelIdState(res.panel_id);
        setName('');
        setIP('');
        setShowAddModal(false);
        fetchNodes();
        setSelectedScriptNode(res.node);
      } else {
        toast.error(res.error || 'Failed to create shield node');
      }
    } catch (err: any) {
      toast.error(err.message || 'Failed to create shield node');
    } finally {
      setSubmitting(false);
    }
  };

  const handleTest = async (node: ShieldNode) => {
    setTestingId(node.id);
    try {
      const res = await backendApi.testShieldNode(node.id);
      if (res.ok && res.reachable) {
        toast.success(`Shield '${node.name}' is online! HTTP Status: ${res.http_status || 'Responding'}`);
        fetchNodes();
      } else {
        toast.error(`Shield '${node.name}' unreachable: ${res.error || 'Connection refused'}`);
      }
    } catch (err: any) {
      toast.error('Ping test failed: ' + (err.message || 'Network error'));
    } finally {
      setTestingId(null);
    }
  };

  const handleReassign = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!reassignModalNode) return;
    setReassigning(true);
    try {
      const res = await backendApi.reassignShieldNode(reassignModalNode.id, targetShieldId);
      if (res.ok) {
        toast.success(`Successfully migrated all domains from '${reassignModalNode.name}'`);
        setReassignModalNode(null);
        fetchNodes();
      } else {
        toast.error(res.error || 'Failed to reassign domains');
      }
    } catch (err: any) {
      toast.error(err.message || 'Network error');
    } finally {
      setReassigning(false);
    }
  };

  const handleRotateToken = async () => {
    if (!rotateModalNode) return;
    setRotating(true);
    try {
      const res = await backendApi.rotateShieldNodeToken(rotateModalNode.id);
      if (res.ok && res.token) {
        setRotateResult(res);
        setShowRotatedToken(false);
        toast.success(`Rotated token for '${rotateModalNode.name}'. Previous token remains valid for 10 minutes.`);
        fetchNodes();
      } else {
        toast.error(res.error || 'Failed to rotate shield token');
      }
    } catch (err: any) {
      toast.error(err.message || 'Failed to rotate shield token');
    } finally {
      setRotating(false);
    }
  };

  const openRotateModal = (node: ShieldNode) => {
    setRotateModalNode(node);
    setRotateResult(null);
    setShowRotatedToken(false);
  };

  const activeCount = nodes.filter((n) => n.status === 'connected').length;
  const totalProtectedDomains = nodes.reduce((acc, n) => acc + (n.assigned_domains || 0), 0);
  const panelOrigin = getPanelOrigin();

  return (
    <div className="space-y-5 sm:space-y-6 max-w-7xl mx-auto">
      {/* Header */}
      <div className="flex flex-col gap-3.5 sm:flex-row sm:items-center sm:justify-between">
        <div className="flex items-center gap-3 min-w-0">
          <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg bg-emerald-500/10 text-emerald-400 ring-1 ring-emerald-500/20">
            <Shield className="h-5 w-5" />
          </div>
          <div className="min-w-0">
            <div className="flex items-center gap-2">
              <h1 className="text-lg sm:text-xl font-bold tracking-tight text-white truncate">Shield Proxies</h1>
              <button
                type="button"
                onClick={() => setShowDocsModal(true)}
                className="inline-flex items-center justify-center h-6 w-6 rounded-md border border-zinc-800 bg-zinc-900 text-zinc-400 hover:text-emerald-400 hover:border-emerald-500/30 transition-colors cursor-pointer shrink-0"
                title="How Shield Nodes Work"
                aria-label="How Shield Nodes Work"
              >
                <HelpCircle className="h-3.5 w-3.5" />
              </button>
            </div>
            <p className="mt-0.5 text-xs text-zinc-400 leading-relaxed">
              Route domain traffic through external edge VPS nodes to mask your origin IP.
            </p>
          </div>
        </div>

        <div className="flex flex-wrap items-center gap-2.5">
          <button
            type="button"
            onClick={fetchNodes}
            className="flex-1 sm:flex-none inline-flex items-center justify-center gap-1.5 h-9 min-h-[36px] px-3.5 rounded-lg border border-zinc-800 bg-zinc-900/80 text-xs font-medium text-zinc-300 hover:bg-zinc-800 hover:text-white transition-colors cursor-pointer"
          >
            <RefreshCw className="h-3.5 w-3.5" />
            <span>Refresh</span>
          </button>
          <button
            type="button"
            onClick={() => setShowAddModal(true)}
            className="flex-1 sm:flex-none inline-flex items-center justify-center gap-1.5 h-9 min-h-[36px] px-4 rounded-lg bg-emerald-600 text-xs font-medium text-white shadow-sm hover:bg-emerald-500 transition-colors cursor-pointer"
          >
            <Plus className="h-4 w-4" />
            <span>Add Shield Node</span>
          </button>
        </div>
      </div>

      {/* Metrics Cards */}
      <div className="grid grid-cols-1 gap-3.5 sm:grid-cols-3">
        <div className="rounded-xl border border-zinc-800/80 bg-zinc-900/50 p-4 backdrop-blur-sm">
          <div className="flex items-center justify-between text-xs text-zinc-400">
            <span>Shield Nodes</span>
            <ShieldCheck className="h-4 w-4 text-emerald-400" />
          </div>
          <div className="mt-2 flex items-baseline gap-2">
            <span className="text-2xl font-bold text-white">{nodes.length}</span>
            <span className="text-xs text-emerald-400 font-medium">
              {activeCount} active & connected
            </span>
          </div>
        </div>

        <div className="rounded-xl border border-zinc-800/80 bg-zinc-900/50 p-4 backdrop-blur-sm">
          <div className="flex items-center justify-between text-xs text-zinc-400">
            <span>Protected Domains</span>
            <Globe className="h-4 w-4 text-cyan-400" />
          </div>
          <div className="mt-2 flex items-baseline gap-2">
            <span className="text-2xl font-bold text-white">{totalProtectedDomains}</span>
            <span className="text-xs text-zinc-400">routes masked</span>
          </div>
        </div>

        <div className="rounded-xl border border-zinc-800/80 bg-zinc-900/50 p-4 backdrop-blur-sm">
          <div className="flex items-center justify-between text-xs text-zinc-400">
            <span>Origin IP (Hidden)</span>
            <Server className="h-4 w-4 text-amber-400" />
          </div>
          <div className="mt-2 flex items-center justify-between">
            <span className="font-mono text-sm font-semibold text-zinc-200">
              {originIP || 'Detecting...'}
            </span>
            <span className="rounded-md bg-zinc-800 px-2 py-0.5 text-xs font-medium text-zinc-400">
              Direct Host
            </span>
          </div>
        </div>
      </div>

      {/* Step-by-Step VPS Setup Guide Card */}
      <div className="rounded-xl border border-zinc-800/80 bg-zinc-900/40 backdrop-blur-sm overflow-hidden">
        <button
          type="button"
          onClick={() => setShowGuide((prev) => !prev)}
          aria-expanded={showGuide}
          className="w-full px-4 py-3 border-b border-zinc-800/80 flex items-center justify-between cursor-pointer hover:bg-zinc-800/30 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-emerald-500/60 transition-colors"
        >
          <div className="flex items-center gap-2.5">
            <span className="flex h-6 w-6 items-center justify-center rounded-md bg-emerald-500/10 text-emerald-400">
              <Terminal className="h-3.5 w-3.5" />
            </span>
            <span className="text-xs font-semibold uppercase tracking-wider text-zinc-200">
              Shield VPS Setup
            </span>
          </div>
          <div className="flex items-center gap-2">
            <span className="text-xs text-zinc-400 hidden sm:inline">
              {showGuide ? 'Collapse' : 'Show Guide'}
            </span>
            {showGuide ? (
              <ChevronUp className="h-4 w-4 text-zinc-400" />
            ) : (
              <ChevronDown className="h-4 w-4 text-zinc-400" />
            )}
          </div>
        </button>

        {showGuide && (
          <div className="p-4 sm:p-5 space-y-4">
            <p className="text-xs text-zinc-400 leading-relaxed">
              4 quick steps to connect an external Linux VPS as an explicit-host edge proxy via <code className="text-zinc-200">nd edge</code>.
            </p>

            <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-3">
              {/* Step 1 */}
              <div className="rounded-lg border border-zinc-800 bg-zinc-950/70 p-3.5 flex flex-col justify-between gap-3">
                <div className="space-y-1.5">
                  <div className="flex items-center justify-between">
                    <span className="rounded bg-emerald-500/15 px-1.5 py-0.5 font-mono text-[11px] font-bold text-emerald-400">
                      STEP 1
                    </span>
                    <span className="text-xs text-zinc-400">Prerequisites</span>
                  </div>
                  <h4 className="text-xs font-semibold text-zinc-200">Prepare VPS & Ports</h4>
                  <p className="text-xs text-zinc-400 leading-normal">
                    Run on your remote VPS as <strong className="text-zinc-200">root</strong>:
                  </p>
                </div>
                <div className="relative rounded-md border border-zinc-800 bg-zinc-900/90 p-2 font-mono text-[11px] text-zinc-300">
                  <pre className="overflow-x-auto whitespace-pre pr-7">
                    apt update && apt install -y curl ufw{'\n'}ufw allow 80/tcp{'\n'}ufw allow 443/tcp{'\n'}ufw allow 443/udp
                  </pre>
                  <button
                    type="button"
                    onClick={() => handleCopy(SHIELD_PREP_COMMAND, 'guide-step-1')}
                    className="absolute right-1.5 top-1.5 h-6 w-6 rounded flex items-center justify-center text-zinc-400 hover:text-white hover:bg-zinc-800 transition-colors cursor-pointer"
                    title="Copy commands"
                    aria-label="Copy firewall preparation commands"
                  >
                    {copied === 'guide-step-1' ? (
                      <Check className="h-3.5 w-3.5 text-emerald-400" />
                    ) : (
                      <Copy className="h-3.5 w-3.5" />
                    )}
                  </button>
                </div>
              </div>

              {/* Step 2 */}
              <div className="rounded-lg border border-zinc-800 bg-zinc-950/70 p-3.5 flex flex-col justify-between gap-3">
                <div className="space-y-1.5">
                  <div className="flex items-center justify-between">
                    <span className="rounded bg-cyan-500/15 px-1.5 py-0.5 font-mono text-[11px] font-bold text-cyan-400">
                      STEP 2
                    </span>
                    <span className="text-xs text-zinc-400">Panel</span>
                  </div>
                  <h4 className="text-xs font-semibold text-zinc-200">Add Node</h4>
                  <p className="text-xs text-zinc-400 leading-normal">
                    Click <strong className="text-zinc-200">+ Add Shield Node</strong> above to enter VPS IP and generate a Shield token.
                  </p>
                </div>
                <div className="rounded-md border border-zinc-800 bg-zinc-900/40 p-2 text-xs text-zinc-400 text-center">
                  Uses <code className="text-zinc-200">--token-stdin</code> (never URL query tokens)
                </div>
              </div>

              {/* Step 3 */}
              <div className="rounded-lg border border-zinc-800 bg-zinc-950/70 p-3.5 flex flex-col justify-between gap-3">
                <div className="space-y-1.5">
                  <div className="flex items-center justify-between">
                    <span className="rounded bg-amber-500/15 px-1.5 py-0.5 font-mono text-[11px] font-bold text-amber-400">
                      STEP 3
                    </span>
                    <span className="text-xs text-zinc-400">Install</span>
                  </div>
                  <h4 className="text-xs font-semibold text-zinc-200">Install nd edge</h4>
                  <p className="text-xs text-zinc-400 leading-normal">
                    Run the node&apos;s <code className="text-zinc-200">--token-stdin</code> install command as root:
                  </p>
                </div>
                <div className="relative rounded-md border border-zinc-800 bg-zinc-900/90 p-2 font-mono text-[11px] text-zinc-300">
                  <pre className="overflow-x-auto whitespace-pre pr-8 text-amber-300">
                    {buildInstallCommand(panelOrigin, 'YOUR_TOKEN')}
                  </pre>
                  <button
                    type="button"
                    onClick={() => handleCopy(buildInstallCommand(panelOrigin, 'YOUR_TOKEN'), 'guide-step-3')}
                    className="absolute right-1.5 top-1.5 h-6 w-6 rounded flex items-center justify-center text-zinc-400 hover:text-white hover:bg-zinc-800 transition-colors cursor-pointer"
                    title="Copy command template"
                  >
                    {copied === 'guide-step-3' ? (
                      <Check className="h-3.5 w-3.5 text-emerald-400" />
                    ) : (
                      <Copy className="h-3.5 w-3.5" />
                    )}
                  </button>
                </div>
              </div>

              {/* Step 4 */}
              <div className="rounded-lg border border-zinc-800 bg-zinc-950/70 p-3.5 flex flex-col justify-between gap-3">
                <div className="space-y-1.5">
                  <div className="flex items-center justify-between">
                    <span className="rounded bg-emerald-500/15 px-1.5 py-0.5 font-mono text-[11px] font-bold text-emerald-400">
                      STEP 4
                    </span>
                    <span className="text-xs text-zinc-400">Verify</span>
                  </div>
                  <h4 className="text-xs font-semibold text-zinc-200">Verify & DNS</h4>
                  <p className="text-xs text-zinc-400 leading-normal">
                    Node will show <span className="text-emerald-400 font-medium">Sync OK</span>. Point domain DNS A-Record to the shield IP:
                  </p>
                </div>
                <div className="relative rounded-md border border-zinc-800 bg-zinc-900/90 p-2 font-mono text-[11px] text-zinc-300">
                  <pre className="overflow-x-auto whitespace-pre pr-7">
                    nd edge doctor
                  </pre>
                  <button
                    type="button"
                    onClick={() => handleCopy('nd edge doctor', 'guide-step-4')}
                    className="absolute right-1.5 top-1.5 h-6 w-6 rounded flex items-center justify-center text-zinc-400 hover:text-white hover:bg-zinc-800 transition-colors cursor-pointer"
                    title="Copy command"
                  >
                    {copied === 'guide-step-4' ? (
                      <Check className="h-3.5 w-3.5 text-emerald-400" />
                    ) : (
                      <Copy className="h-3.5 w-3.5" />
                    )}
                  </button>
                </div>
              </div>
            </div>
          </div>
        )}
      </div>

      {/* Nodes List Container */}
      <div className="rounded-xl border border-zinc-800/80 bg-zinc-900/40 backdrop-blur-sm overflow-hidden">
        <div className="border-b border-zinc-800 px-4 py-3 flex items-center justify-between gap-3">
          <h2 className="text-xs font-semibold uppercase tracking-wider text-zinc-300">
            Connected Shield VPS
          </h2>
          {panelId && (
            <span className="font-mono text-[11px] text-zinc-400 tabular-nums" title={`Panel ID: ${panelId}`}>
              Panel: {panelId.slice(0, 8)}
            </span>
          )}
        </div>

        {loading ? (
          <div className="py-12 text-center text-xs text-zinc-400">
            Loading shield nodes...
          </div>
        ) : nodes.length === 0 ? (
          <div className="py-14 text-center px-4">
            <Shield className="mx-auto h-9 w-9 text-zinc-500 mb-2" />
            <h3 className="text-sm font-medium text-zinc-200">No Shield Nodes Configured</h3>
            <p className="mt-1 text-xs text-zinc-400 max-w-sm mx-auto">
              Add your first remote VPS to reverse-proxy application traffic and mask your server origin IP.
            </p>
            <button
              type="button"
              onClick={() => setShowAddModal(true)}
              className="mt-4 inline-flex items-center gap-1.5 h-9 px-4 rounded-lg bg-emerald-600 text-xs font-medium text-white hover:bg-emerald-500 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-emerald-400 transition-colors cursor-pointer"
            >
              <Plus className="h-3.5 w-3.5" />
              <span>Add Shield Node</span>
            </button>
          </div>
        ) : (
          <>
            {/* Desktop Table View */}
            <div className="hidden md:block overflow-x-auto">
              <table className="w-full text-left text-xs">
                <thead className="border-b border-zinc-800 bg-zinc-950/40 text-xs font-medium text-zinc-400">
                  <tr>
                    <th className="px-4 py-3 font-medium">Shield Node</th>
                    <th className="px-4 py-3 font-medium">Public IP</th>
                    <th className="px-4 py-3 font-medium">Status & Sync</th>
                    <th className="px-4 py-3 font-medium">Versions</th>
                    <th className="px-4 py-3 font-medium">Domains & Conflicts</th>
                    <th className="px-4 py-3 font-medium">Last Sync</th>
                    <th className="px-4 py-3 font-medium text-right">Actions</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-zinc-800/60">
                  {nodes.map((node) => {
                    const syncTime = formatSyncTime(node.last_sync_at || node.last_heartbeat_at);
                    const isConnected = node.status === 'connected';
                    const isPending = node.status === 'pending';

                    return (
                      <tr key={node.id} className="hover:bg-zinc-800/25 transition-colors">
                        <td className="px-4 py-3.5 align-middle">
                          <div className="flex items-center gap-2.5">
                            <div
                              className={`flex h-8 w-8 shrink-0 items-center justify-center rounded-lg ${
                                isConnected
                                  ? 'bg-emerald-500/10 text-emerald-400 ring-1 ring-emerald-500/20'
                                  : isPending
                                  ? 'bg-amber-500/10 text-amber-400 ring-1 ring-amber-500/20'
                                  : 'bg-rose-500/10 text-rose-400 ring-1 ring-rose-500/20'
                              }`}
                            >
                              <Shield className="h-4 w-4" />
                            </div>
                            <div className="min-w-0">
                              <span className="font-semibold text-zinc-100 block truncate">{node.name}</span>
                              <div className="text-[11px] text-zinc-400 font-mono tabular-nums">
                                ID: #{node.id}{node.edge_id ? ` · ${node.edge_id.slice(0, 8)}` : ''}
                              </div>
                            </div>
                          </div>
                        </td>

                        <td className="px-4 py-3.5 align-middle">
                          <div className="inline-flex items-center gap-1.5">
                            <span className="font-mono text-xs tabular-nums text-zinc-200 font-medium">{node.ip}</span>
                            <button
                              type="button"
                              onClick={() => handleCopy(node.ip, `ip-${node.id}`)}
                              aria-label={`Copy public IP ${node.ip}`}
                              title="Copy IP for DNS A-Record"
                              className="h-7 w-7 rounded-md flex items-center justify-center text-zinc-400 hover:bg-zinc-800 hover:text-zinc-100 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-emerald-500/60 transition-colors cursor-pointer"
                            >
                              {copied === `ip-${node.id}` ? (
                                <Check className="h-3.5 w-3.5 text-emerald-400" />
                              ) : (
                                <Copy className="h-3.5 w-3.5" />
                              )}
                            </button>
                          </div>
                        </td>

                        <td className="px-4 py-3.5 align-middle">
                          <NodeStatusReadout node={node} />
                        </td>

                        <td className="px-4 py-3.5 align-middle font-mono tabular-nums text-xs">
                          <div className="space-y-0.5">
                            <div className="text-zinc-200">edge: {node.edge_version || <span className="text-zinc-500">—</span>}</div>
                            <div className="text-[11px] text-zinc-400">caddy: {node.caddy_version || <span className="text-zinc-500">—</span>}</div>
                          </div>
                        </td>

                        <td className="px-4 py-3.5 align-middle">
                          <NodeDomainsCell node={node} />
                        </td>

                        <td className="px-4 py-3.5 align-middle text-xs tabular-nums text-zinc-400 whitespace-nowrap">
                          {syncTime ? (
                            <span title={syncTime.full}>{syncTime.short}</span>
                          ) : (
                            <span className="text-zinc-600">Awaiting sync</span>
                          )}
                        </td>

                        <td className="px-4 py-3.5 align-middle text-right">
                          <div className="flex items-center justify-end gap-1.5">
                            {node.assigned_domains > 0 && (
                              <button
                                type="button"
                                onClick={() => {
                                  setReassignModalNode(node);
                                  setTargetShieldId(null);
                                }}
                                className="inline-flex items-center gap-1.5 h-8 px-2.5 rounded-md border border-cyan-800/60 bg-cyan-950/40 text-xs font-medium text-cyan-300 hover:bg-cyan-900/60 hover:border-cyan-700 transition-colors cursor-pointer"
                                title="Failover / Reassign domains"
                              >
                                <ArrowLeftRight className="h-3.5 w-3.5 text-cyan-400" />
                                <span>Reassign</span>
                              </button>
                            )}
                            <button
                              type="button"
                              onClick={() => setSelectedScriptNode(node)}
                              className="inline-flex items-center gap-1.5 h-8 px-2.5 rounded-md border border-emerald-500/30 bg-emerald-500/10 text-xs font-medium text-emerald-300 hover:bg-emerald-500/20 hover:border-emerald-500/50 transition-colors cursor-pointer"
                              title="Setup, upgrade, and uninstall commands"
                            >
                              <Terminal className="h-3.5 w-3.5 text-emerald-400" />
                              <span>Commands</span>
                            </button>
                            <button
                              type="button"
                              onClick={() => openRotateModal(node)}
                              className="inline-flex items-center gap-1.5 h-8 px-2.5 rounded-md border border-amber-500/30 bg-amber-500/10 text-xs font-medium text-amber-300 hover:bg-amber-500/20 hover:border-amber-500/50 transition-colors cursor-pointer"
                              title="Rotate Shield authentication token"
                            >
                              <KeyRound className="h-3.5 w-3.5 text-amber-400" />
                              <span>Rotate</span>
                            </button>
                            <button
                              type="button"
                              onClick={() => handleTest(node)}
                              disabled={testingId === node.id}
                              aria-label={`Test connectivity for ${node.name}`}
                              className="inline-flex items-center gap-1.5 h-8 px-2.5 rounded-md border border-sky-500/30 bg-sky-500/10 text-xs font-medium text-sky-300 hover:bg-sky-500/20 hover:border-sky-500/50 disabled:opacity-50 transition-colors cursor-pointer"
                              title="Test connectivity"
                            >
                              <Activity className={`h-3.5 w-3.5 ${testingId === node.id ? 'animate-spin text-sky-300' : 'text-sky-400'}`} />
                              <span>{testingId === node.id ? 'Testing...' : 'Test'}</span>
                            </button>
                            <button
                              type="button"
                              onClick={() => setDeleteModalNode(node)}
                              className="h-8 w-8 rounded-md border border-zinc-800 flex items-center justify-center text-zinc-400 hover:border-rose-500/40 hover:bg-rose-500/10 hover:text-rose-400 transition-colors cursor-pointer"
                              title="Delete Shield Node"
                            >
                              <Trash2 className="h-3.5 w-3.5" />
                            </button>
                          </div>
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>

            {/* Mobile Cards View */}
            <div className="block md:hidden divide-y divide-zinc-800/60">
              {nodes.map((node) => {
                const syncTime = formatSyncTime(node.last_sync_at || node.last_heartbeat_at);
                const isConnected = node.status === 'connected';
                const isPending = node.status === 'pending';

                return (
                  <div key={node.id} className="p-4 space-y-3">
                    <div className="flex items-start justify-between gap-3">
                      <div className="flex items-center gap-2.5 min-w-0">
                        <div
                          className={`flex h-8 w-8 shrink-0 items-center justify-center rounded-lg ${
                            isConnected
                              ? 'bg-emerald-500/10 text-emerald-400 ring-1 ring-emerald-500/20'
                              : isPending
                              ? 'bg-amber-500/10 text-amber-400 ring-1 ring-amber-500/20'
                              : 'bg-rose-500/10 text-rose-400 ring-1 ring-rose-500/20'
                          }`}
                        >
                          <Shield className="h-4 w-4" />
                        </div>
                        <div className="min-w-0">
                          <span className="font-semibold text-zinc-100 text-sm block truncate">
                            {node.name}
                          </span>
                          <span className="text-[11px] text-zinc-400 font-mono tabular-nums">
                            #{node.id}{node.edge_id ? ` · ${node.edge_id.slice(0, 8)}` : ''}
                          </span>
                        </div>
                      </div>
                      <div className="shrink-0 text-right">
                        <NodeStatusReadout node={node} />
                      </div>
                    </div>

                    <div className="flex flex-wrap items-center justify-between gap-2 text-xs bg-zinc-950/60 px-3 py-2 rounded-lg border border-zinc-800/60">
                      <div className="flex items-center gap-1.5">
                        <code className="font-mono text-zinc-200 tabular-nums">{node.ip}</code>
                        <button
                          type="button"
                          onClick={() => handleCopy(node.ip, `ip-mobile-${node.id}`)}
                          aria-label={`Copy public IP ${node.ip}`}
                          className="h-7 w-7 rounded flex items-center justify-center text-zinc-400 hover:text-white focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-emerald-500/60 cursor-pointer"
                        >
                          {copied === `ip-mobile-${node.id}` ? (
                            <Check className="h-3.5 w-3.5 text-emerald-400" />
                          ) : (
                            <Copy className="h-3.5 w-3.5" />
                          )}
                        </button>
                      </div>
                      <NodeDomainsCell node={node} />
                    </div>

                    <div className="flex flex-wrap items-center justify-between gap-2 text-[11px] text-zinc-400 font-mono tabular-nums">
                      <span>
                        {node.edge_version ? `edge ${node.edge_version}` : 'edge -'}
                        {node.caddy_version ? ` · caddy ${node.caddy_version}` : ''}
                      </span>
                      <span className="font-sans" title={syncTime?.full}>
                        {syncTime ? `Synced ${syncTime.short}` : 'Awaiting sync'}
                      </span>
                    </div>

                    <div className="flex flex-wrap items-center gap-1.5 pt-1">
                      {node.assigned_domains > 0 && (
                        <button
                          type="button"
                          onClick={() => {
                            setReassignModalNode(node);
                            setTargetShieldId(null);
                          }}
                          className="inline-flex items-center justify-center gap-1 h-9 px-2.5 rounded-md border border-cyan-800/60 bg-cyan-950/40 text-xs font-medium text-cyan-300 hover:bg-cyan-900/60 transition-colors cursor-pointer"
                        >
                          <ArrowLeftRight className="h-3.5 w-3.5 text-cyan-400" />
                          <span>Reassign</span>
                        </button>
                      )}
                      <button
                        type="button"
                        onClick={() => setSelectedScriptNode(node)}
                        className="flex-1 inline-flex items-center justify-center gap-1.5 h-9 rounded-md border border-emerald-500/30 bg-emerald-500/10 text-xs font-medium text-emerald-300 hover:bg-emerald-500/20 transition-colors cursor-pointer"
                      >
                        <Terminal className="h-3.5 w-3.5 text-emerald-400" />
                        <span>Commands</span>
                      </button>
                      <button
                        type="button"
                        onClick={() => openRotateModal(node)}
                        className="inline-flex items-center justify-center gap-1 h-9 px-2.5 rounded-md border border-amber-500/30 bg-amber-500/10 text-xs font-medium text-amber-300 hover:bg-amber-500/20 transition-colors cursor-pointer"
                      >
                        <KeyRound className="h-3.5 w-3.5 text-amber-400" />
                        <span>Rotate</span>
                      </button>
                      <button
                        type="button"
                        onClick={() => handleTest(node)}
                        disabled={testingId === node.id}
                        aria-label={`Test connectivity for ${node.name}`}
                        className="flex-1 inline-flex items-center justify-center gap-1.5 h-9 rounded-md border border-sky-500/30 bg-sky-500/10 text-xs font-medium text-sky-300 hover:bg-sky-500/20 disabled:opacity-50 transition-colors cursor-pointer"
                      >
                        <Activity className={`h-3.5 w-3.5 ${testingId === node.id ? 'animate-spin text-sky-300' : 'text-sky-400'}`} />
                        <span>{testingId === node.id ? 'Testing...' : 'Test'}</span>
                      </button>
                      <button
                        type="button"
                        onClick={() => setDeleteModalNode(node)}
                        className="h-9 w-9 shrink-0 rounded-md border border-zinc-800 flex items-center justify-center text-zinc-400 hover:border-rose-500/40 hover:bg-rose-500/10 hover:text-rose-400 transition-colors cursor-pointer"
                        title="Delete Shield Node"
                      >
                        <Trash2 className="h-3.5 w-3.5" />
                      </button>
                    </div>
                  </div>
                );
              })}
            </div>
          </>
        )}
      </div>

      {/* Add Shield Node Modal */}
      <ShadcnDialog
        open={showAddModal}
        onClose={() => setShowAddModal(false)}
        title="Add New Shield VPS Node"
        description="Register a remote VPS to serve as an edge reverse-proxy and mask your origin IP."
        icon={<Shield className="h-5 w-5 text-emerald-400" />}
        maxWidth="md"
        footer={
          <div className="flex items-center justify-end gap-2 w-full">
            <button
              type="button"
              onClick={() => setShowAddModal(false)}
              className="h-9 px-3.5 rounded-md border border-zinc-800 bg-zinc-900 text-xs font-medium text-zinc-300 hover:bg-zinc-800 hover:text-white transition-colors cursor-pointer"
            >
              Cancel
            </button>
            <button
              type="submit"
              form="add-shield-form"
              disabled={submitting}
              className="h-9 px-4 rounded-md bg-emerald-600 text-xs font-medium text-white hover:bg-emerald-500 disabled:opacity-50 transition-colors cursor-pointer"
            >
              {submitting ? 'Registering...' : 'Add & Generate Installer'}
            </button>
          </div>
        }
      >
        <form id="add-shield-form" onSubmit={handleCreate} className="space-y-4">
          <div>
            <label htmlFor="shield-node-name" className="block text-xs font-medium text-zinc-300 mb-1.5">
              Node Label / Name
            </label>
            <input
              id="shield-node-name"
              type="text"
              placeholder="e.g. Frankfurt Shield 01"
              value={name}
              onChange={(e) => setName(e.target.value)}
              className="w-full h-9 rounded-md border border-zinc-800 bg-zinc-950 px-3 text-xs text-white placeholder-zinc-500 focus:border-emerald-500 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-emerald-500/40"
              required
            />
          </div>

          <div>
            <label htmlFor="shield-node-ip" className="block text-xs font-medium text-zinc-300 mb-1.5">
              Shield VPS Public IPv4 Address
            </label>
            <input
              id="shield-node-ip"
              type="text"
              placeholder="e.g. 198.51.100.25"
              value={ip}
              onChange={(e) => setIP(e.target.value)}
              className="w-full h-9 rounded-md border border-zinc-800 bg-zinc-950 px-3 text-xs text-white placeholder-zinc-500 font-mono focus:border-emerald-500 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-emerald-500/40"
              required
            />
          </div>
        </form>
      </ShadcnDialog>

      {/* Setup / Maintenance / Removal Commands Modal */}
      <ManageShieldModal
        node={selectedScriptNode}
        panelId={panelId}
        panelOrigin={panelOrigin}
        onClose={() => setSelectedScriptNode(null)}
        onRotateToken={openRotateModal}
        onDone={fetchNodes}
      />

      {/* Rotate Token Modal */}
      <ShadcnDialog
        open={Boolean(rotateModalNode)}
        onClose={() => !rotating && setRotateModalNode(null)}
        title={rotateModalNode ? `Rotate Token: ${rotateModalNode.name}` : 'Rotate Shield Token'}
        description="Issue a new Shield Bearer token with a 10-minute overlap grace period."
        icon={<KeyRound className="h-5 w-5 text-amber-400" />}
        maxWidth="md"
        footer={
          rotateResult ? (
            <button
              type="button"
              onClick={() => setRotateModalNode(null)}
              className="h-9 px-4 rounded-md bg-zinc-800 text-xs font-medium text-white hover:bg-zinc-700 transition-colors cursor-pointer"
            >
              Done
            </button>
          ) : (
            <div className="flex items-center justify-end gap-2 w-full">
              <button
                type="button"
                onClick={() => setRotateModalNode(null)}
                disabled={rotating}
                className="h-9 px-3.5 rounded-md border border-zinc-800 bg-zinc-900 text-xs font-medium text-zinc-300 hover:bg-zinc-800 hover:text-white transition-colors cursor-pointer"
              >
                Cancel
              </button>
              <button
                type="button"
                onClick={() => void handleRotateToken()}
                disabled={rotating}
                className="h-9 px-4 rounded-md bg-amber-600 text-xs font-medium text-white hover:bg-amber-500 disabled:opacity-50 transition-colors cursor-pointer"
              >
                {rotating ? 'Rotating...' : 'Rotate Token'}
              </button>
            </div>
          )
        }
      >
        {rotateModalNode && !rotateResult && (
          <div className="space-y-3 text-xs text-zinc-300">
            <div className="rounded-lg border border-amber-500/25 bg-amber-500/10 p-3 text-amber-200 leading-relaxed">
              Rotating generates a new <code className="font-mono text-amber-100">shld_</code> token immediately. The previous token stays valid for <strong>10 minutes</strong> so you can run <code className="font-mono">nd edge set-token --token-stdin</code> on the Shield VPS without dropping sync.
            </div>
            <p className="text-zinc-400">
              After 10 minutes, any Shield still using the old token will enter <span className="text-rose-300 font-medium">Auth Failed</span> state (existing Caddy routes keep serving).
            </p>
          </div>
        )}

        {rotateModalNode && rotateResult && (
          <div className="space-y-4">
            <div className="rounded-lg border border-emerald-500/25 bg-emerald-500/10 p-3 text-xs text-emerald-200">
              New token issued. Previous token expires at{' '}
              <strong className="font-mono text-white">
                {rotateResult.token_prev_expires_at
                  ? new Date(rotateResult.token_prev_expires_at).toLocaleTimeString()
                  : '10 minutes from now'}
              </strong>.
            </div>

            <div className="space-y-1.5">
              <span className="text-xs font-medium text-zinc-300 block">New Shield Token</span>
              <div className="flex items-center gap-1.5">
                <code className="min-w-0 flex-1 truncate rounded-lg border border-zinc-800 bg-zinc-950 px-3 py-2 font-mono text-xs text-zinc-200">
                  {showRotatedToken ? rotateResult.token : '•'.repeat(28)}
                </code>
                <button
                  type="button"
                  onClick={() => setShowRotatedToken((v) => !v)}
                  className="h-8 px-2.5 rounded-md border border-zinc-800 bg-zinc-900 text-xs text-zinc-300 hover:text-white cursor-pointer inline-flex items-center gap-1"
                >
                  {showRotatedToken ? <EyeOff className="h-3.5 w-3.5" /> : <Eye className="h-3.5 w-3.5" />}
                  <span>{showRotatedToken ? 'Hide' : 'Show'}</span>
                </button>
                <button
                  type="button"
                  onClick={() => handleCopy(rotateResult.token || '', 'rotated-token')}
                  className="h-8 px-2.5 rounded-md border border-zinc-800 bg-zinc-900 text-xs text-zinc-300 hover:text-white cursor-pointer inline-flex items-center gap-1"
                >
                  {copied === 'rotated-token' ? <Check className="h-3.5 w-3.5 text-emerald-400" /> : <Copy className="h-3.5 w-3.5" />}
                </button>
              </div>
            </div>

            <div className="space-y-1.5">
              <span className="text-xs font-medium text-zinc-300 block">
                Update Token on Shield VPS (<code className="text-amber-300">--token-stdin</code>):
              </span>
              <div className="relative rounded-lg border border-amber-500/30 bg-zinc-950 p-3 font-mono text-xs">
                <pre className="overflow-x-auto whitespace-pre-wrap break-all pr-8 text-amber-200">
                  {buildSetTokenCommand(panelId, rotateResult.token || '')}
                </pre>
                <button
                  type="button"
                  onClick={() => handleCopy(buildSetTokenCommand(panelId, rotateResult.token || ''), 'rotated-set-token-cmd')}
                  className="absolute right-2 top-2 h-7 w-7 rounded-md border border-amber-500/40 bg-amber-500/10 flex items-center justify-center text-amber-300 hover:bg-amber-500/20 cursor-pointer transition-colors"
                  title="Copy set-token command"
                >
                  {copied === 'rotated-set-token-cmd' ? (
                    <Check className="h-3.5 w-3.5 text-emerald-400" />
                  ) : (
                    <Copy className="h-3.5 w-3.5" />
                  )}
                </button>
              </div>
            </div>
          </div>
        )}
      </ShadcnDialog>

      {/* Failover / Reassign Domains Modal */}
      <ShadcnDialog
        open={Boolean(reassignModalNode)}
        onClose={() => setReassignModalNode(null)}
        title="Reassign Domains"
        description="Switch all routed domains to another shield node or direct origin."
        icon={<ArrowLeftRight className="h-5 w-5 text-cyan-400" />}
        maxWidth="md"
        footer={
          <div className="flex items-center justify-end gap-2 w-full">
            <button
              type="button"
              onClick={() => setReassignModalNode(null)}
              className="h-8 px-3 rounded-lg border border-white/[0.08] bg-zinc-900 text-xs font-medium text-zinc-300 hover:bg-zinc-800 transition-colors cursor-pointer"
            >
              Cancel
            </button>
            <button
              type="submit"
              form="reassign-shield-form"
              disabled={reassigning}
              className="h-8 px-3.5 rounded-lg bg-cyan-600 text-xs font-semibold text-white hover:bg-cyan-500 disabled:opacity-50 transition-colors cursor-pointer flex items-center gap-1.5"
            >
              <ArrowLeftRight className="h-3.5 w-3.5" />
              <span>{reassigning ? 'Reassigning...' : 'Reassign'}</span>
            </button>
          </div>
        }
      >
        {reassignModalNode && (
          <form id="reassign-shield-form" onSubmit={handleReassign} className="space-y-4">
            <div className="rounded-lg border border-white/[0.08] bg-zinc-950 p-3 flex items-center justify-between">
              <div>
                <div className="text-xs font-semibold text-zinc-100">{reassignModalNode.name}</div>
                <div className="font-mono text-[11px] text-zinc-400">{reassignModalNode.ip}</div>
              </div>
              <span className="inline-flex items-center gap-1 rounded-md bg-zinc-900 border border-white/[0.08] px-2 py-0.5 font-mono text-xs text-zinc-300">
                {reassignModalNode.assigned_domains || 0} domain{reassignModalNode.assigned_domains === 1 ? '' : 's'}
              </span>
            </div>

            <div className="space-y-1.5">
              <label htmlFor="reassign-target-shield" className="block text-xs font-medium text-zinc-300">
                New Destination
              </label>
              <select
                id="reassign-target-shield"
                value={targetShieldId === null ? 'direct' : targetShieldId}
                onChange={(e) => {
                  const val = e.target.value;
                  setTargetShieldId(val === 'direct' ? null : Number(val));
                }}
                className="w-full h-9 rounded-lg border border-white/[0.08] bg-zinc-950 px-3 text-xs text-zinc-200 focus:border-cyan-500 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-cyan-500/40 cursor-pointer"
              >
                <option value="direct">Direct Origin (Server IP)</option>
                {nodes
                  .filter((n) => n.id !== reassignModalNode.id)
                  .map((n) => (
                    <option key={n.id} value={n.id} disabled={n.status !== 'connected'}>
                      {n.name} ({n.ip}){n.status === 'connected' ? '' : ` • ${n.status}`}
                    </option>
                  ))}
              </select>
            </div>
          </form>
        )}
      </ShadcnDialog>

      {/* Delete Shield Node & Clean Up VPS Uninstaller Modal */}
      <ShadcnDialog
        open={Boolean(deleteModalNode)}
        onClose={() => setDeleteModalNode(null)}
        title="Delete Shield Node"
        description={`Remove '${deleteModalNode?.name}' from panel and clean up your remote VPS.`}
        icon={<Trash2 className="h-5 w-5 text-rose-400" />}
        maxWidth="md"
      >
        {deleteModalNode && (
          <div className="space-y-4">
            <div className="rounded-lg bg-amber-500/10 border border-amber-500/20 p-3 text-xs text-amber-300">
              Any domains assigned to this shield node will immediately revert back to <strong>Direct Origin</strong>.
            </div>

            <div className="space-y-2">
              <span className="font-semibold text-zinc-200 text-xs block">
                1. Remove Panel / Uninstall on your VPS (Optional, keeps other panels intact unless <code className="text-zinc-100">--all</code>):
              </span>
              <div className="relative rounded-lg border border-white/[0.08] bg-zinc-950 p-2.5 font-mono text-xs">
                <pre className="overflow-x-auto whitespace-pre-wrap break-all pr-8 text-zinc-300">
                  {`${buildUninstallCommand(panelId)}\n# Or full removal:\nnd edge uninstall --all`}
                </pre>
                <button
                  type="button"
                  onClick={() => handleCopy(buildUninstallCommand(panelId), 'del-uninstall-cmd')}
                  className="absolute right-2 top-2 h-7 w-7 rounded-md border border-white/[0.08] bg-zinc-900 flex items-center justify-center text-zinc-400 hover:bg-zinc-800 hover:text-white cursor-pointer transition-colors"
                  title="Copy uninstall command"
                >
                  {copied === 'del-uninstall-cmd' ? (
                    <Check className="h-3.5 w-3.5 text-emerald-400" />
                  ) : (
                    <Copy className="h-3.5 w-3.5" />
                  )}
                </button>
              </div>
              <p className="text-[11px] text-zinc-400">
                Removes this panel&apos;s Caddy routes and origin CA from the Shield VPS without exposing tokens in shell history or URLs.
              </p>
            </div>

            <div className="flex items-center justify-end gap-2 pt-2 border-t border-white/[0.06]">
              <button
                type="button"
                onClick={() => setDeleteModalNode(null)}
                className="h-8 px-3 rounded-lg border border-white/[0.08] bg-zinc-900 text-xs font-medium text-zinc-300 hover:bg-zinc-800 transition-colors cursor-pointer"
              >
                Cancel
              </button>
              <button
                type="button"
                onClick={async () => {
                  const node = deleteModalNode;
                  setDeleteModalNode(null);
                  try {
                    const res = await backendApi.deleteShieldNode(node.id);
                    if (res.ok) {
                      toast.success(`Shield node '${node.name}' deleted`);
                      fetchNodes();
                    } else {
                      toast.error(res.error || 'Failed to delete');
                    }
                  } catch (err: any) {
                    toast.error(err.message || 'Failed to delete');
                  }
                }}
                className="h-8 px-3.5 rounded-lg bg-rose-500 text-xs font-semibold text-white hover:bg-rose-600 transition-colors cursor-pointer"
              >
                Confirm Delete
              </button>
            </div>
          </div>
        )}
      </ShadcnDialog>

      {/* How Shield Nodes Work (Architecture Docs) Modal */}
      <ShadcnDialog
        open={showDocsModal}
        onClose={() => setShowDocsModal(false)}
        title="Shield Node Architecture & Guide"
        description="Understanding multi-panel edge reverse-proxying, origin masking, and explicit-host TLS."
        icon={<BookOpen className="h-5 w-5 text-emerald-400" />}
        maxWidth="lg"
        footer={
          <button
            type="button"
            onClick={() => setShowDocsModal(false)}
            className="h-9 px-4 rounded-md bg-zinc-800 text-xs font-medium text-white hover:bg-zinc-700 transition-colors cursor-pointer"
          >
            Close Guide
          </button>
        }
      >
        <div className="space-y-4 text-xs text-zinc-300">
          <div className="rounded-lg border border-zinc-800 bg-zinc-950 p-3.5 space-y-1.5">
            <h4 className="font-semibold text-white flex items-center gap-1.5">
              <ShieldCheck className="h-4 w-4 text-emerald-400" />
              1. Origin IP Masking & Multi-Panel Isolation
            </h4>
            <p className="text-zinc-400 leading-relaxed">
              When a shield node is assigned to a domain, public DNS records point exclusively to the shield node&apos;s public IP. One Shield VPS can front multiple NextDeploy panels with isolated per-panel route files and conflict protection.
            </p>
          </div>

          <div className="rounded-lg border border-zinc-800 bg-zinc-950 p-3.5 space-y-1.5">
            <h4 className="font-semibold text-white flex items-center gap-1.5">
              <Lock className="h-4 w-4 text-cyan-400" />
              2. Explicit-Host Automatic HTTPS
            </h4>
            <p className="text-zinc-400 leading-relaxed">
              <code className="text-zinc-200">nd edge</code> polls <code className="text-zinc-200">/api/v1/edge/routes</code> over HTTPS Bearer auth, validates DNS readiness, and writes explicit per-host Caddy blocks. Caddy issues and renews Let&apos;s Encrypt certificates in the background and continues serving even if the panel is offline or rebooting.
            </p>
          </div>

          <div className="rounded-lg border border-zinc-800 bg-zinc-950 p-3.5 space-y-1.5">
            <h4 className="font-semibold text-white flex items-center gap-1.5">
              <Terminal className="h-4 w-4 text-amber-400" />
              3. Safe Token Handling & Upgrades
            </h4>
            <p className="text-zinc-400 leading-relaxed">
              Install and token rotation commands use <code className="text-zinc-200">--token-stdin</code> instead of query parameters so tokens never appear in access logs. Upgrades run via <code className="text-zinc-200">nd edge upgrade</code> with SHA-256 verification.
            </p>
          </div>

          <div className="rounded-lg border border-zinc-800 bg-zinc-950 p-3.5 space-y-1.5">
            <h4 className="font-semibold text-white flex items-center gap-1.5">
              <Globe className="h-4 w-4 text-emerald-400" />
              4. DNS & Cloudflare Configuration
            </h4>
            <p className="text-zinc-400 leading-relaxed">
              Point your domain&apos;s <strong>A Record</strong> to the <strong>Shield Public IP</strong>. Behind Cloudflare (orange cloud), use <strong>Full</strong> or <strong>Full (strict)</strong> SSL encryption mode.
            </p>
          </div>

          <div className="rounded-lg border border-zinc-800 bg-zinc-950 p-3.5 space-y-1.5">
            <h4 className="font-semibold text-white flex items-center gap-1.5">
              <ArrowLeftRight className="h-4 w-4 text-cyan-400" />
              5. 1-Click Standby Failover
            </h4>
            <p className="text-zinc-400 leading-relaxed">
              If an edge VPS experiences an outage, open the <strong>Reassign</strong> dialog to migrate all assigned domains to another standby shield node or revert to direct origin routing.
            </p>
          </div>
        </div>
      </ShadcnDialog>
    </div>
  );
}
