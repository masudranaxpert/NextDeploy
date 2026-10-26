import React, { useEffect, useState } from 'react';
import {
  Shield,
  ShieldCheck,
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
  ArrowUpRight,
  ChevronDown,
  ChevronUp,
  CheckCircle2,
} from 'lucide-react';
import { toast } from 'sonner';
import { backendApi } from '../../lib/backendApi';
import { ShieldNode } from '../../types/shield';
import { copyToClipboard } from '../../lib/clipboard';
import { ShadcnDialog } from '../../components/ui/Primitives';

export function ShieldProxiesView() {
  const [nodes, setNodes] = useState<ShieldNode[]>([]);
  const [originIP, setOriginIP] = useState<string>('');
  const [loading, setLoading] = useState(true);
  const [showAddModal, setShowAddModal] = useState(false);
  const [showDocsModal, setShowDocsModal] = useState(false);
  const [showGuide, setShowGuide] = useState(false);
  const [selectedScriptNode, setSelectedScriptNode] = useState<{ node: ShieldNode; command: string } | null>(null);

  // Form states
  const [name, setName] = useState('');
  const [ip, setIP] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [testingId, setTestingId] = useState<number | null>(null);
  const [copied, setCopied] = useState<string | null>(null);

  // Failover / Reassign modal state
  const [reassignModalNode, setReassignModalNode] = useState<ShieldNode | null>(null);
  const [targetShieldId, setTargetShieldId] = useState<number | null>(null);
  const [reassigning, setReassigning] = useState(false);

  const fetchNodes = async () => {
    try {
      const res = await backendApi.listShieldNodes();
      if (res.ok) {
        setNodes(res.nodes || []);
        if (res.origin_ip) setOriginIP(res.origin_ip);
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
        setName('');
        setIP('');
        setShowAddModal(false);
        fetchNodes();
        if (res.setup_command) {
          setSelectedScriptNode({ node: res.node, command: res.setup_command });
        }
      } else {
        toast.error(res.error || 'Failed to create shield node');
      }
    } catch (err: any) {
      toast.error(err.message || 'Failed to create shield node');
    } finally {
      setSubmitting(false);
    }
  };

  const handleDelete = async (id: number, nodeName: string) => {
    if (!confirm(`Are you sure you want to delete shield node '${nodeName}'? Any domains assigned to this shield will revert to Direct Origin.`)) {
      return;
    }
    try {
      const res = await backendApi.deleteShieldNode(id);
      if (res.ok) {
        toast.success('Shield node deleted');
        fetchNodes();
      } else {
        toast.error(res.error || 'Failed to delete');
      }
    } catch (err: any) {
      toast.error(err.message || 'Failed to delete');
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

  const activeCount = nodes.filter((n) => n.status === 'connected').length;
  const totalProtectedDomains = nodes.reduce((acc, n) => acc + (n.assigned_domains || 0), 0);

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
        <div
          onClick={() => setShowGuide((prev) => !prev)}
          className="px-4 py-3 border-b border-zinc-800/80 flex items-center justify-between cursor-pointer hover:bg-zinc-800/30 transition-colors"
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
        </div>

        {showGuide && (
          <div className="p-4 sm:p-5 space-y-4">
            <p className="text-xs text-zinc-400 leading-relaxed">
              4 quick steps to connect an external Linux VPS as a reverse proxy.
            </p>

            <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-3">
              {/* Step 1 */}
              <div className="rounded-lg border border-zinc-800 bg-zinc-950/70 p-3.5 flex flex-col justify-between gap-3">
                <div className="space-y-1.5">
                  <div className="flex items-center justify-between">
                    <span className="rounded bg-emerald-500/15 px-1.5 py-0.5 font-mono text-[11px] font-bold text-emerald-400">
                      STEP 1
                    </span>
                    <span className="text-xs text-zinc-500">Prerequisites</span>
                  </div>
                  <h4 className="text-xs font-semibold text-zinc-200">Prepare VPS & Ports</h4>
                  <p className="text-xs text-zinc-400 leading-normal">
                    Run on your remote VPS as <strong className="text-zinc-200">root</strong>:
                  </p>
                </div>
                <div className="relative rounded-md border border-zinc-800 bg-zinc-900/90 p-2 font-mono text-[11px] text-zinc-300">
                  <pre className="overflow-x-auto whitespace-pre pr-7">
                    apt update && apt install -y curl ufw{'\n'}ufw allow 80/tcp{'\n'}ufw allow 443/tcp
                  </pre>
                  <button
                    type="button"
                    onClick={() => handleCopy('apt update && apt install -y curl ufw && ufw allow 80/tcp && ufw allow 443/tcp', 'guide-step-1')}
                    className="absolute right-1.5 top-1.5 h-6 w-6 rounded flex items-center justify-center text-zinc-400 hover:text-white hover:bg-zinc-800 transition-colors cursor-pointer"
                    title="Copy commands"
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
                    <span className="text-xs text-zinc-500">Panel</span>
                  </div>
                  <h4 className="text-xs font-semibold text-zinc-200">Add Node</h4>
                  <p className="text-xs text-zinc-400 leading-normal">
                    Click <strong className="text-zinc-200">+ Add Shield Node</strong> above to enter VPS IP and generate an install token.
                  </p>
                </div>
                <div className="rounded-md border border-zinc-800 bg-zinc-900/40 p-2 text-xs text-zinc-400 text-center">
                  Generates an authorized install command
                </div>
              </div>

              {/* Step 3 */}
              <div className="rounded-lg border border-zinc-800 bg-zinc-950/70 p-3.5 flex flex-col justify-between gap-3">
                <div className="space-y-1.5">
                  <div className="flex items-center justify-between">
                    <span className="rounded bg-amber-500/15 px-1.5 py-0.5 font-mono text-[11px] font-bold text-amber-400">
                      STEP 3
                    </span>
                    <span className="text-xs text-zinc-500">Install</span>
                  </div>
                  <h4 className="text-xs font-semibold text-zinc-200">Run Install Script</h4>
                  <p className="text-xs text-zinc-400 leading-normal">
                    Run the node&apos;s installer command as root on the VPS:
                  </p>
                </div>
                <div className="relative rounded-md border border-zinc-800 bg-zinc-900/90 p-2 font-mono text-[11px] text-zinc-300">
                  <pre className="overflow-x-auto whitespace-pre pr-8 text-amber-300">
                    {`curl -sSL "${typeof window !== 'undefined' ? `${window.location.protocol}//${window.location.host}` : ''}/api/v1/edge/setup.sh?token=YOUR_TOKEN" | bash`}
                  </pre>
                  <button
                    type="button"
                    onClick={() => handleCopy(`curl -sSL "${typeof window !== 'undefined' ? `${window.location.protocol}//${window.location.host}` : ''}/api/v1/edge/setup.sh?token=YOUR_TOKEN" | bash`, 'guide-step-3')}
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
                    <span className="text-xs text-zinc-500">Verify</span>
                  </div>
                  <h4 className="text-xs font-semibold text-zinc-200">Verify & DNS</h4>
                  <p className="text-xs text-zinc-400 leading-normal">
                    Node will show <span className="text-emerald-400 font-medium">Connected</span>. Point domain DNS A-Record to the shield IP:
                  </p>
                </div>
                <div className="relative rounded-md border border-zinc-800 bg-zinc-900/90 p-2 font-mono text-[11px] text-zinc-300">
                  <pre className="overflow-x-auto whitespace-pre pr-7">
                    systemctl status caddy
                  </pre>
                  <button
                    type="button"
                    onClick={() => handleCopy('systemctl status caddy', 'guide-step-4')}
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
        </div>

        {loading ? (
          <div className="py-12 text-center text-xs text-zinc-500">
            Loading shield nodes...
          </div>
        ) : nodes.length === 0 ? (
          <div className="py-14 text-center px-4">
            <Shield className="mx-auto h-9 w-9 text-zinc-600 mb-2" />
            <h3 className="text-sm font-medium text-zinc-300">No Shield Nodes Configured</h3>
            <p className="mt-1 text-xs text-zinc-500 max-w-sm mx-auto">
              Add your first remote VPS to reverse-proxy application traffic and mask your server origin IP.
            </p>
            <button
              type="button"
              onClick={() => setShowAddModal(true)}
              className="mt-4 inline-flex items-center gap-1.5 h-9 px-4 rounded-lg bg-emerald-600 text-xs font-medium text-white hover:bg-emerald-500 transition-colors"
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
                    <th className="px-4 py-3">Shield Node</th>
                    <th className="px-4 py-3">Public IP</th>
                    <th className="px-4 py-3">Status</th>
                    <th className="px-4 py-3">Assigned Domains</th>
                    <th className="px-4 py-3">Last Heartbeat</th>
                    <th className="px-4 py-3 text-right">Actions</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-zinc-800/60">
                  {nodes.map((node) => {
                    const isConnected = node.status === 'connected';
                    const isPending = node.status === 'pending';
                    const originScheme = window.location.protocol;
                    const originHost = window.location.host;
                    const setupCmd = `curl -sSL "${originScheme}//${originHost}/api/v1/edge/setup.sh?token=${node.token}" | bash`;

                    return (
                      <tr key={node.id} className="hover:bg-zinc-800/30 transition-colors">
                        <td className="px-4 py-3.5">
                          <div className="flex items-center gap-2.5">
                            <div
                              className={`flex h-8 w-8 items-center justify-center rounded-md ${
                                isConnected
                                  ? 'bg-emerald-500/10 text-emerald-400 ring-1 ring-emerald-500/20'
                                  : isPending
                                  ? 'bg-amber-500/10 text-amber-400 ring-1 ring-amber-500/20'
                                  : 'bg-red-500/10 text-red-400 ring-1 ring-red-500/20'
                              }`}
                            >
                              <Shield className="h-4 w-4" />
                            </div>
                            <div>
                              <span className="font-semibold text-zinc-100">{node.name}</span>
                              <div className="text-xs text-zinc-500 font-mono">
                                ID: #{node.id}
                              </div>
                            </div>
                          </div>
                        </td>

                        <td className="px-4 py-3.5">
                          <div className="flex items-center gap-1.5">
                            <span className="font-mono text-xs text-zinc-300 font-medium">{node.ip}</span>
                            <button
                              type="button"
                              onClick={() => handleCopy(node.ip, `ip-${node.id}`)}
                              title="Copy IP for DNS A-Record"
                              className="h-7 w-7 rounded flex items-center justify-center text-zinc-400 hover:bg-zinc-800 hover:text-zinc-200 transition-colors cursor-pointer"
                            >
                              {copied === `ip-${node.id}` ? (
                                <Check className="h-3.5 w-3.5 text-emerald-400" />
                              ) : (
                                <Copy className="h-3.5 w-3.5" />
                              )}
                            </button>
                          </div>
                        </td>

                        <td className="px-4 py-3.5">
                          {isConnected ? (
                            <span className="inline-flex items-center gap-1.5 rounded-md bg-emerald-500/10 px-2 py-0.5 text-xs font-medium text-emerald-400 ring-1 ring-emerald-500/20">
                              <span className="h-1.5 w-1.5 rounded-full bg-emerald-400 animate-pulse" />
                              Connected
                            </span>
                          ) : isPending ? (
                            <span className="inline-flex items-center gap-1.5 rounded-md bg-amber-500/10 px-2 py-0.5 text-xs font-medium text-amber-400 ring-1 ring-amber-500/20">
                              <span className="h-1.5 w-1.5 rounded-full bg-amber-400" />
                              Pending Setup
                            </span>
                          ) : (
                            <span className="inline-flex items-center gap-1.5 rounded-md bg-red-500/10 px-2 py-0.5 text-xs font-medium text-red-400 ring-1 ring-red-500/20">
                              <span className="h-1.5 w-1.5 rounded-full bg-red-400" />
                              Offline
                            </span>
                          )}
                        </td>

                        <td className="px-4 py-3.5">
                          <span className="inline-flex items-center gap-1 rounded-md bg-zinc-800/80 px-2 py-0.5 text-xs font-medium text-zinc-300">
                            <Globe className="h-3 w-3 text-zinc-400" />
                            {node.assigned_domains || 0} domains
                          </span>
                        </td>

                        <td className="px-4 py-3.5 text-zinc-400 text-xs">
                          {node.last_heartbeat_at ? (
                            <span>{new Date(node.last_heartbeat_at).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' })}</span>
                          ) : (
                            <span className="text-zinc-600">Awaiting ping</span>
                          )}
                        </td>

                        <td className="px-4 py-3.5 text-right">
                          <div className="flex items-center justify-end gap-1.5">
                            {node.assigned_domains > 0 && (
                              <button
                                type="button"
                                onClick={() => {
                                  setReassignModalNode(node);
                                  setTargetShieldId(null);
                                }}
                                className="inline-flex items-center gap-1.5 h-9 px-3 rounded-md border border-cyan-800/60 bg-cyan-950/40 text-xs font-medium text-cyan-300 hover:bg-cyan-900/60 transition-colors cursor-pointer"
                                title="Failover / Reassign domains"
                              >
                                <ArrowLeftRight className="h-3.5 w-3.5" />
                                <span>Reassign</span>
                              </button>
                            )}
                            <button
                              type="button"
                              onClick={() => setSelectedScriptNode({ node, command: setupCmd })}
                              className="inline-flex items-center gap-1.5 h-9 px-3 rounded-md border border-zinc-700 bg-zinc-800/80 text-xs font-medium text-zinc-200 hover:bg-zinc-700 hover:text-white transition-colors cursor-pointer"
                            >
                              <Terminal className="h-3.5 w-3.5 text-emerald-400" />
                              <span>Script</span>
                            </button>
                            <button
                              type="button"
                              onClick={() => handleTest(node)}
                              disabled={testingId === node.id}
                              className="inline-flex items-center gap-1.5 h-9 px-3 rounded-md border border-zinc-800 bg-zinc-900 text-xs font-medium text-zinc-300 hover:bg-zinc-800 hover:text-white disabled:opacity-50 transition-colors cursor-pointer"
                            >
                              <Activity className={`h-3.5 w-3.5 ${testingId === node.id ? 'animate-spin' : 'text-cyan-400'}`} />
                              <span>Test</span>
                            </button>
                            <button
                              type="button"
                              onClick={() => handleDelete(node.id, node.name)}
                              className="h-9 w-9 rounded-md border border-zinc-800 flex items-center justify-center text-zinc-400 hover:border-red-500/30 hover:bg-red-500/10 hover:text-red-400 transition-colors cursor-pointer"
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

            {/* Mobile Cards View (Responsive alternative to wide table) */}
            <div className="block md:hidden divide-y divide-zinc-800/60">
              {nodes.map((node) => {
                const isConnected = node.status === 'connected';
                const isPending = node.status === 'pending';
                const originScheme = window.location.protocol;
                const originHost = window.location.host;
                const setupCmd = `curl -sSL "${originScheme}//${originHost}/api/v1/edge/setup.sh?token=${node.token}" | bash`;

                return (
                  <div key={node.id} className="p-4 space-y-3">
                    {/* Header: Node Name + Status Badge */}
                    <div className="flex items-center justify-between gap-2">
                      <div className="flex items-center gap-2.5 min-w-0">
                        <div
                          className={`flex h-8 w-8 shrink-0 items-center justify-center rounded-md ${
                            isConnected
                              ? 'bg-emerald-500/10 text-emerald-400 ring-1 ring-emerald-500/20'
                              : isPending
                              ? 'bg-amber-500/10 text-amber-400 ring-1 ring-amber-500/20'
                              : 'bg-red-500/10 text-red-400 ring-1 ring-red-500/20'
                          }`}
                        >
                          <Shield className="h-4 w-4" />
                        </div>
                        <div className="min-w-0">
                          <span className="font-semibold text-zinc-100 text-sm block truncate">
                            {node.name}
                          </span>
                          <span className="text-xs text-zinc-500 font-mono">
                            ID: #{node.id}
                          </span>
                        </div>
                      </div>

                      {isConnected ? (
                        <span className="inline-flex items-center gap-1.5 rounded-md bg-emerald-500/10 px-2 py-0.5 text-xs font-medium text-emerald-400 ring-1 ring-emerald-500/20 shrink-0">
                          <span className="h-1.5 w-1.5 rounded-full bg-emerald-400 animate-pulse" />
                          Connected
                        </span>
                      ) : isPending ? (
                        <span className="inline-flex items-center gap-1.5 rounded-md bg-amber-500/10 px-2 py-0.5 text-xs font-medium text-amber-400 ring-1 ring-amber-500/20 shrink-0">
                          <span className="h-1.5 w-1.5 rounded-full bg-amber-400" />
                          Pending Setup
                        </span>
                      ) : (
                        <span className="inline-flex items-center gap-1.5 rounded-md bg-red-500/10 px-2 py-0.5 text-xs font-medium text-red-400 ring-1 ring-red-500/20 shrink-0">
                          <span className="h-1.5 w-1.5 rounded-full bg-red-400" />
                          Offline
                        </span>
                      )}
                    </div>

                    {/* Meta: IP & Domains */}
                    <div className="flex flex-wrap items-center justify-between gap-2 text-xs bg-zinc-950/60 p-2.5 rounded-lg border border-zinc-800/60">
                      <div className="flex items-center gap-2">
                        <span className="text-zinc-500">Public IP:</span>
                        <code className="font-mono text-zinc-200 font-semibold">{node.ip}</code>
                        <button
                          type="button"
                          onClick={() => handleCopy(node.ip, `ip-mobile-${node.id}`)}
                          className="h-6 w-6 rounded flex items-center justify-center text-zinc-400 hover:text-white cursor-pointer"
                        >
                          {copied === `ip-mobile-${node.id}` ? (
                            <Check className="h-3.5 w-3.5 text-emerald-400" />
                          ) : (
                            <Copy className="h-3.5 w-3.5" />
                          )}
                        </button>
                      </div>

                      <span className="inline-flex items-center gap-1 rounded-md bg-zinc-800 px-2 py-0.5 text-xs font-medium text-zinc-300">
                        <Globe className="h-3 w-3 text-zinc-400" />
                        {node.assigned_domains || 0} domains
                      </span>
                    </div>

                    {/* Last Heartbeat */}
                    <div className="flex items-center justify-between text-xs text-zinc-500 px-0.5">
                      <span>Heartbeat:</span>
                      <span>
                        {node.last_heartbeat_at
                          ? new Date(node.last_heartbeat_at).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' })
                          : 'Awaiting first ping'}
                      </span>
                    </div>

                    {/* Actions Grid */}
                    <div className="grid grid-cols-2 gap-2 pt-1">
                      <button
                        type="button"
                        onClick={() => setSelectedScriptNode({ node, command: setupCmd })}
                        className="inline-flex items-center justify-center gap-1.5 h-9 rounded-md border border-zinc-700 bg-zinc-800 text-xs font-medium text-zinc-200 hover:bg-zinc-700 hover:text-white transition-colors cursor-pointer"
                      >
                        <Terminal className="h-3.5 w-3.5 text-emerald-400" />
                        <span>Install Script</span>
                      </button>

                      <button
                        type="button"
                        onClick={() => handleTest(node)}
                        disabled={testingId === node.id}
                        className="inline-flex items-center justify-center gap-1.5 h-9 rounded-md border border-zinc-800 bg-zinc-900 text-xs font-medium text-zinc-300 hover:bg-zinc-800 hover:text-white disabled:opacity-50 transition-colors cursor-pointer"
                      >
                        <Activity className={`h-3.5 w-3.5 ${testingId === node.id ? 'animate-spin' : 'text-cyan-400'}`} />
                        <span>Test Ping</span>
                      </button>

                      {node.assigned_domains > 0 && (
                        <button
                          type="button"
                          onClick={() => {
                            setReassignModalNode(node);
                            setTargetShieldId(null);
                          }}
                          className="inline-flex items-center justify-center gap-1.5 h-9 rounded-md border border-cyan-800/60 bg-cyan-950/40 text-xs font-medium text-cyan-300 hover:bg-cyan-900/60 transition-colors cursor-pointer"
                        >
                          <ArrowLeftRight className="h-3.5 w-3.5" />
                          <span>Reassign</span>
                        </button>
                      )}

                      <button
                        type="button"
                        onClick={() => handleDelete(node.id, node.name)}
                        className={`inline-flex items-center justify-center gap-1.5 h-9 rounded-md border text-xs font-medium transition-colors cursor-pointer ${
                          node.assigned_domains > 0
                            ? 'border-red-500/20 bg-red-500/10 text-red-400 hover:bg-red-500/20'
                            : 'col-span-2 border-zinc-800 bg-zinc-900/60 text-zinc-400 hover:border-red-500/30 hover:bg-red-500/10 hover:text-red-400'
                        }`}
                      >
                        <Trash2 className="h-3.5 w-3.5" />
                        <span>Delete Node</span>
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
            <label className="block text-xs font-medium text-zinc-300 mb-1.5">
              Node Label / Name
            </label>
            <input
              type="text"
              placeholder="e.g. Frankfurt Shield 01"
              value={name}
              onChange={(e) => setName(e.target.value)}
              className="w-full h-9 rounded-md border border-zinc-800 bg-zinc-950 px-3 text-xs text-white placeholder-zinc-600 focus:border-emerald-500 focus:outline-none"
              required
            />
          </div>

          <div>
            <label className="block text-xs font-medium text-zinc-300 mb-1.5">
              Shield VPS Public IPv4 Address
            </label>
            <input
              type="text"
              placeholder="e.g. 198.51.100.25"
              value={ip}
              onChange={(e) => setIP(e.target.value)}
              className="w-full h-9 rounded-md border border-zinc-800 bg-zinc-950 px-3 text-xs text-white placeholder-zinc-600 font-mono focus:border-emerald-500 focus:outline-none"
              required
            />
          </div>
        </form>
      </ShadcnDialog>

      {/* Setup Script Modal */}
      <ShadcnDialog
        open={Boolean(selectedScriptNode)}
        onClose={() => setSelectedScriptNode(null)}
        title={selectedScriptNode ? `Connect Shield: ${selectedScriptNode.node.name}` : 'Connect Shield'}
        description="Run this one-line command via SSH as root on your shield VPS."
        icon={<Terminal className="h-5 w-5 text-emerald-400" />}
        maxWidth="lg"
        footer={
          <button
            type="button"
            onClick={() => {
              setSelectedScriptNode(null);
              fetchNodes();
            }}
            className="h-9 px-4 rounded-md bg-zinc-800 text-xs font-medium text-white hover:bg-zinc-700 transition-colors cursor-pointer"
          >
            Done
          </button>
        }
      >
        {selectedScriptNode && (
          <div className="space-y-4">
            <div className="rounded-lg border border-zinc-800 bg-zinc-950 p-3 text-xs flex items-center justify-between">
              <div>
                <span className="text-zinc-500 block">Target Shield VPS:</span>
                <span className="font-semibold text-white">{selectedScriptNode.node.name}</span>
              </div>
              <span className="font-mono text-emerald-400 font-semibold px-2 py-0.5 rounded bg-emerald-500/10 border border-emerald-500/20">
                {selectedScriptNode.node.ip}
              </span>
            </div>

            {/* Step 1: VPS Prep */}
            <div className="space-y-1.5">
              <div className="flex items-center justify-between text-xs">
                <span className="font-semibold text-zinc-300">1. Install Prerequisites (on VPS as root):</span>
                <span className="text-zinc-500 text-[11px]">Ubuntu / Debian</span>
              </div>
              <div className="relative rounded-lg border border-zinc-800 bg-zinc-950 p-2.5 font-mono text-xs text-zinc-300">
                <pre className="overflow-x-auto whitespace-pre pr-8">
                  apt update && apt install -y curl ufw && ufw allow 80/tcp && ufw allow 443/tcp
                </pre>
                <button
                  type="button"
                  onClick={() => handleCopy('apt update && apt install -y curl ufw && ufw allow 80/tcp && ufw allow 443/tcp', 'modal-prep-cmd')}
                  className="absolute right-2 top-2 h-7 w-7 rounded-md border border-zinc-800 bg-zinc-900 flex items-center justify-center text-zinc-400 hover:bg-zinc-800 hover:text-white cursor-pointer transition-colors"
                  title="Copy command"
                >
                  {copied === 'modal-prep-cmd' ? (
                    <Check className="h-3.5 w-3.5 text-emerald-400" />
                  ) : (
                    <Copy className="h-3.5 w-3.5" />
                  )}
                </button>
              </div>
            </div>

            {/* Step 2: Installer */}
            <div className="space-y-1.5">
              <span className="font-semibold text-zinc-300 text-xs block">2. Run Automated NextDeploy Shield Installer:</span>
              <div className="relative rounded-lg border border-emerald-500/30 bg-zinc-950 p-3 font-mono text-xs">
                <pre className="overflow-x-auto whitespace-pre-wrap break-all pr-8 text-emerald-300">
                  {selectedScriptNode.command}
                </pre>
                <button
                  type="button"
                  onClick={() => handleCopy(selectedScriptNode.command, 'script-cmd')}
                  className="absolute right-2 top-2 h-7 w-7 rounded-md border border-emerald-500/40 bg-emerald-500/10 flex items-center justify-center text-emerald-400 hover:bg-emerald-500/20 cursor-pointer transition-colors"
                  title="Copy installer command"
                >
                  {copied === 'script-cmd' ? (
                    <Check className="h-3.5 w-3.5 text-emerald-400" />
                  ) : (
                    <Copy className="h-3.5 w-3.5" />
                  )}
                </button>
              </div>
            </div>

            {/* Step 3: Verification */}
            <div className="space-y-1.5">
              <span className="font-semibold text-zinc-300 text-xs block">3. Verify Service Status:</span>
              <div className="relative rounded-lg border border-zinc-800 bg-zinc-950 p-2.5 font-mono text-xs text-zinc-300">
                <pre className="overflow-x-auto whitespace-pre pr-8">
                  systemctl status caddy
                </pre>
                <button
                  type="button"
                  onClick={() => handleCopy('systemctl status caddy', 'modal-verify-cmd')}
                  className="absolute right-2 top-2 h-7 w-7 rounded-md border border-zinc-800 bg-zinc-900 flex items-center justify-center text-zinc-400 hover:bg-zinc-800 hover:text-white cursor-pointer transition-colors"
                  title="Copy command"
                >
                  {copied === 'modal-verify-cmd' ? (
                    <Check className="h-3.5 w-3.5 text-emerald-400" />
                  ) : (
                    <Copy className="h-3.5 w-3.5" />
                  )}
                </button>
              </div>
            </div>

            {/* Step 4: Uninstaller & Cleanup */}
            <div className="space-y-1.5 pt-2 border-t border-white/[0.06]">
              <span className="font-semibold text-zinc-400 text-xs block">Uninstall & Restore Original Node (If unlinking):</span>
              <div className="relative rounded-lg border border-white/[0.08] bg-zinc-950 p-2.5 font-mono text-xs">
                <pre className="overflow-x-auto whitespace-pre-wrap break-all pr-8 text-zinc-400">
                  {`curl -sSL "${typeof window !== 'undefined' ? `${window.location.protocol}//${window.location.host}` : ''}/api/v1/edge/uninstall.sh?token=${selectedScriptNode.node.token}" | bash`}
                </pre>
                <button
                  type="button"
                  onClick={() => handleCopy(`curl -sSL "${typeof window !== 'undefined' ? `${window.location.protocol}//${window.location.host}` : ''}/api/v1/edge/uninstall.sh?token=${selectedScriptNode.node.token}" | bash`, 'modal-uninstall-cmd')}
                  className="absolute right-2 top-2 h-7 w-7 rounded-md border border-white/[0.08] bg-zinc-900 flex items-center justify-center text-zinc-400 hover:bg-zinc-800 hover:text-white cursor-pointer transition-colors"
                  title="Copy uninstall command"
                >
                  {copied === 'modal-uninstall-cmd' ? (
                    <Check className="h-3.5 w-3.5 text-emerald-400" />
                  ) : (
                    <Copy className="h-3.5 w-3.5" />
                  )}
                </button>
              </div>
            </div>

            <div className="rounded-lg bg-emerald-500/10 border border-emerald-500/20 p-3 text-xs text-emerald-300 space-y-1">
              <div className="font-semibold text-white flex items-center gap-1.5">
                <CheckCircle2 className="h-3.5 w-3.5 text-emerald-400" />
                Automatic Verification:
              </div>
              <p className="text-zinc-300 leading-relaxed text-xs">
                As soon as the script completes on your VPS, Caddy will ping NextDeploy origin. This node will automatically turn green <strong className="text-emerald-400">Connected</strong> within 15–30 seconds.
              </p>
            </div>
          </div>
        )}
      </ShadcnDialog>

      {/* Failover / Reassign Domains Modal */}
      <ShadcnDialog
        open={Boolean(reassignModalNode)}
        onClose={() => setReassignModalNode(null)}
        title="Failover & Domain Reassignment"
        description="Migrate all domains from this shield node to a standby node or direct origin."
        icon={<ArrowLeftRight className="h-5 w-5 text-cyan-400" />}
        maxWidth="md"
        footer={
          <div className="flex items-center justify-end gap-2 w-full">
            <button
              type="button"
              onClick={() => setReassignModalNode(null)}
              className="h-9 px-3.5 rounded-md border border-zinc-800 bg-zinc-900 text-xs font-medium text-zinc-300 hover:bg-zinc-800 hover:text-white transition-colors cursor-pointer"
            >
              Cancel
            </button>
            <button
              type="submit"
              form="reassign-shield-form"
              disabled={reassigning}
              className="h-9 px-4 rounded-md bg-cyan-600 text-xs font-medium text-white hover:bg-cyan-500 disabled:opacity-50 transition-colors cursor-pointer flex items-center gap-1.5"
            >
              <ArrowLeftRight className="h-3.5 w-3.5" />
              <span>{reassigning ? 'Migrating...' : 'Confirm Migration'}</span>
            </button>
          </div>
        }
      >
        {reassignModalNode && (
          <form id="reassign-shield-form" onSubmit={handleReassign} className="space-y-4">
            <div className="rounded-lg bg-zinc-950 border border-zinc-800 p-3 text-xs space-y-1.5">
              <div className="text-zinc-400">Current Active Source Node:</div>
              <div className="font-semibold text-white flex items-center justify-between">
                <span>{reassignModalNode.name}</span>
                <span className="font-mono text-zinc-400">{reassignModalNode.ip}</span>
              </div>
              <div className="text-xs text-amber-400 pt-0.5">
                Routes currently assigned: <strong className="text-white">{reassignModalNode.assigned_domains || 0}</strong> domain(s).
              </div>
            </div>

            <div>
              <label className="block text-xs font-medium text-zinc-300 mb-1.5">
                Migrate All Assigned Domains To:
              </label>
              <select
                value={targetShieldId === null ? 'direct' : targetShieldId}
                onChange={(e) => {
                  const val = e.target.value;
                  setTargetShieldId(val === 'direct' ? null : Number(val));
                }}
                className="w-full h-9 rounded-md border border-zinc-800 bg-zinc-950 px-3 text-xs text-white focus:border-cyan-500 focus:outline-none"
              >
                <option value="direct">Direct Origin (Server Public IP - Direct Routing)</option>
                {nodes
                  .filter((n) => n.id !== reassignModalNode.id)
                  .map((n) => (
                    <option key={n.id} value={n.id}>
                      {n.name} ({n.ip}) - {n.status}
                    </option>
                  ))}
              </select>
              <p className="mt-1.5 text-xs text-zinc-500">
                Select another active shield VPS, or route domains directly to the host server IP.
              </p>
            </div>
          </form>
        )}
      </ShadcnDialog>

      {/* How Shield Nodes Work (Architecture Docs) Modal */}
      <ShadcnDialog
        open={showDocsModal}
        onClose={() => setShowDocsModal(false)}
        title="Shield Node Architecture & Guide"
        description="Understanding edge reverse-proxying, origin masking, and on-demand TLS."
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
              1. Origin IP Masking & DDoS Protection
            </h4>
            <p className="text-zinc-400 leading-relaxed">
              When a shield node is assigned to a project domain, public DNS records point exclusively to the shield node&apos;s public IP. Your central NextDeploy server IP remains completely unadvertised, protecting it against targeted attacks, IP blacklisting, and direct port scans.
            </p>
          </div>

          <div className="rounded-lg border border-zinc-800 bg-zinc-950 p-3.5 space-y-1.5">
            <h4 className="font-semibold text-white flex items-center gap-1.5">
              <Lock className="h-4 w-4 text-cyan-400" />
              2. Automatic On-Demand TLS
            </h4>
            <p className="text-zinc-400 leading-relaxed">
              Each shield node runs an autonomous Caddy instance configured with On-Demand TLS. When an HTTPS request arrives for any customer domain mapped to that shield node, Caddy validates the domain against your NextDeploy origin via an internal ask endpoint (<code className="text-zinc-200">/api/v1/edge/ask</code>), issues a Let&apos;s Encrypt certificate on the fly, and terminates TLS at the edge.
            </p>
          </div>

          <div className="rounded-lg border border-zinc-800 bg-zinc-950 p-3.5 space-y-1.5">
            <h4 className="font-semibold text-white flex items-center gap-1.5">
              <Terminal className="h-4 w-4 text-amber-400" />
              3. Zero Maintenance Setup
            </h4>
            <p className="text-zinc-400 leading-relaxed">
              Provisioning a new shield requires only a single <code className="text-zinc-200">curl | bash</code> command on any fresh Debian or Ubuntu VPS. The installer sets up Caddy, provisions a systemd service, and establishes periodic heartbeat pings back to your NextDeploy dashboard.
            </p>
          </div>

          <div className="rounded-lg border border-zinc-800 bg-zinc-950 p-3.5 space-y-1.5">
            <h4 className="font-semibold text-white flex items-center gap-1.5">
              <Globe className="h-4 w-4 text-emerald-400" />
              4. DNS Configuration
            </h4>
            <p className="text-zinc-400 leading-relaxed">
              In your DNS provider (e.g. Cloudflare, Namecheap, Route53), configure an <strong>A Record</strong> pointing your domain (<code className="text-zinc-200">example.com</code>) to the <strong>Shield Public IP</strong>. If using Cloudflare, you can set proxy status to &quot;DNS Only&quot; (Grey Cloud) to let Caddy issue edge certificates directly.
            </p>
          </div>

          <div className="rounded-lg border border-zinc-800 bg-zinc-950 p-3.5 space-y-1.5">
            <h4 className="font-semibold text-white flex items-center gap-1.5">
              <ArrowLeftRight className="h-4 w-4 text-cyan-400" />
              5. 1-Click Standby Failover
            </h4>
            <p className="text-zinc-400 leading-relaxed">
              If an edge VPS experiences an upstream provider outage, open the <strong>Reassign</strong> dialog to immediately migrate all assigned domains to another standby shield node or revert to direct origin routing.
            </p>
          </div>
        </div>
      </ShadcnDialog>
    </div>
  );
}
