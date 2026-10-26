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
  AlertCircle,
  ExternalLink,
  Lock,
  ArrowLeftRight,
} from 'lucide-react';
import { toast } from 'sonner';
import { backendApi } from '../../lib/backendApi';
import { ShieldNode } from '../../types/shield';
import { copyToClipboard } from '../../lib/clipboard';

export function ShieldProxiesView() {
  const [nodes, setNodes] = useState<ShieldNode[]>([]);
  const [originIP, setOriginIP] = useState<string>('');
  const [loading, setLoading] = useState(true);
  const [showAddModal, setShowAddModal] = useState(false);
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

  const handleCopy = (text: string, id: string) => {
    copyToClipboard(text);
    setCopied(id);
    toast.success('Copied to clipboard');
    setTimeout(() => setCopied(null), 2000);
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
    <div className="space-y-6 p-4 md:p-8 max-w-7xl mx-auto">
      {/* Header */}
      <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <div className="flex items-center gap-2.5">
            <div className="flex h-9 w-9 items-center justify-center rounded-lg bg-emerald-500/10 text-emerald-400 ring-1 ring-emerald-500/20">
              <Shield className="h-5 w-5" />
            </div>
            <div>
              <h1 className="text-xl font-bold tracking-tight text-white flex items-center gap-2">
                Shield Proxies
                <span className="rounded-full bg-emerald-500/10 px-2 py-0.5 text-[11px] font-medium text-emerald-400 ring-1 ring-emerald-500/20">
                  Offshore & DMCA Shield
                </span>
              </h1>
              <p className="text-xs text-zinc-400">
                Route traffic through external offshore/DDoS shield VPS nodes to mask your real origin IP address.
              </p>
            </div>
          </div>
        </div>

        <div className="flex items-center gap-2.5">
          <button
            onClick={fetchNodes}
            className="flex items-center gap-1.5 rounded-lg border border-zinc-800 bg-zinc-900/80 px-3 py-1.5 text-xs font-medium text-zinc-300 hover:bg-zinc-800 hover:text-white transition-colors"
          >
            <RefreshCw className="h-3.5 w-3.5" />
            Refresh
          </button>
          <button
            onClick={() => setShowAddModal(true)}
            className="flex items-center gap-1.5 rounded-lg bg-emerald-600 px-3.5 py-1.5 text-xs font-medium text-white shadow-sm hover:bg-emerald-500 transition-colors"
          >
            <Plus className="h-4 w-4" />
            Add Shield Node
          </button>
        </div>
      </div>

      {/* Metrics Cards */}
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-3">
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
            <span className="rounded bg-zinc-800 px-1.5 py-0.5 text-[10px] font-medium text-zinc-400">
              Direct Host
            </span>
          </div>
        </div>
      </div>

      {/* Nodes List */}
      <div className="rounded-xl border border-zinc-800/80 bg-zinc-900/40 backdrop-blur-sm overflow-hidden">
        <div className="border-b border-zinc-800 px-4 py-3 flex items-center justify-between">
          <h2 className="text-xs font-semibold uppercase tracking-wider text-zinc-300">
            Connected Shield VPS Fleet
          </h2>
          <span className="text-[11px] text-zinc-500">
            Automatic On-Demand SSL & Zero-Touch Caddy Routing
          </span>
        </div>

        {loading ? (
          <div className="py-12 text-center text-xs text-zinc-500">
            Loading shield nodes...
          </div>
        ) : nodes.length === 0 ? (
          <div className="py-14 text-center">
            <Shield className="mx-auto h-9 w-9 text-zinc-600 mb-2" />
            <h3 className="text-sm font-medium text-zinc-300">No Shield Nodes Configured</h3>
            <p className="mt-1 text-xs text-zinc-500 max-w-sm mx-auto">
              Add your first offshore VPS to automatically reverse-proxy customer applications and mask your origin IP address.
            </p>
            <button
              onClick={() => setShowAddModal(true)}
              className="mt-4 inline-flex items-center gap-1.5 rounded-lg bg-emerald-600 px-3.5 py-1.5 text-xs font-medium text-white hover:bg-emerald-500"
            >
              <Plus className="h-3.5 w-3.5" />
              Add Shield Node
            </button>
          </div>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-left text-xs">
              <thead className="border-b border-zinc-800 bg-zinc-950/40 text-[11px] font-medium text-zinc-400">
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
                            className={`flex h-7 w-7 items-center justify-center rounded-md ${
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
                            <div className="text-[10px] text-zinc-500 font-mono">
                              ID: #{node.id}
                            </div>
                          </div>
                        </div>
                      </td>

                      <td className="px-4 py-3.5">
                        <div className="flex items-center gap-1.5">
                          <span className="font-mono text-xs text-zinc-300 font-medium">{node.ip}</span>
                          <button
                            onClick={() => handleCopy(node.ip, `ip-${node.id}`)}
                            title="Copy IP for DNS A-Record"
                            className="rounded p-1 text-zinc-500 hover:bg-zinc-800 hover:text-zinc-200"
                          >
                            {copied === `ip-${node.id}` ? (
                              <Check className="h-3 w-3 text-emerald-400" />
                            ) : (
                              <Copy className="h-3 w-3" />
                            )}
                          </button>
                        </div>
                      </td>

                      <td className="px-4 py-3.5">
                        {isConnected ? (
                          <span className="inline-flex items-center gap-1.5 rounded-full bg-emerald-500/10 px-2 py-0.5 text-[11px] font-medium text-emerald-400 ring-1 ring-emerald-500/20">
                            <span className="h-1.5 w-1.5 rounded-full bg-emerald-400 animate-pulse" />
                            Connected
                          </span>
                        ) : isPending ? (
                          <span className="inline-flex items-center gap-1.5 rounded-full bg-amber-500/10 px-2 py-0.5 text-[11px] font-medium text-amber-400 ring-1 ring-amber-500/20">
                            <span className="h-1.5 w-1.5 rounded-full bg-amber-400" />
                            Pending Setup
                          </span>
                        ) : (
                          <span className="inline-flex items-center gap-1.5 rounded-full bg-red-500/10 px-2 py-0.5 text-[11px] font-medium text-red-400 ring-1 ring-red-500/20">
                            <span className="h-1.5 w-1.5 rounded-full bg-red-400" />
                            Offline
                          </span>
                        )}
                      </td>

                      <td className="px-4 py-3.5">
                        <span className="inline-flex items-center gap-1 rounded bg-zinc-800/80 px-2 py-0.5 text-[11px] font-medium text-zinc-300">
                          <Globe className="h-3 w-3 text-zinc-400" />
                          {node.assigned_domains || 0} domains
                        </span>
                      </td>

                      <td className="px-4 py-3.5 text-zinc-400 text-[11px]">
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
                              onClick={() => {
                                setReassignModalNode(node);
                                setTargetShieldId(null);
                              }}
                              className="flex items-center gap-1 rounded-md border border-cyan-800/60 bg-cyan-950/40 px-2.5 py-1 text-[11px] font-medium text-cyan-300 hover:bg-cyan-900/60"
                              title="Failover / Reassign all domains to another shield or direct"
                            >
                              <ArrowLeftRight className="h-3 w-3" />
                              Reassign
                            </button>
                          )}
                          <button
                            onClick={() => setSelectedScriptNode({ node, command: setupCmd })}
                            className="flex items-center gap-1 rounded-md border border-zinc-700 bg-zinc-800/80 px-2.5 py-1 text-[11px] font-medium text-zinc-200 hover:bg-zinc-700 hover:text-white"
                          >
                            <Terminal className="h-3 w-3 text-emerald-400" />
                            Install Script
                          </button>
                          <button
                            onClick={() => handleTest(node)}
                            disabled={testingId === node.id}
                            className="flex items-center gap-1 rounded-md border border-zinc-800 bg-zinc-900 px-2.5 py-1 text-[11px] font-medium text-zinc-300 hover:bg-zinc-800 hover:text-white disabled:opacity-50"
                          >
                            <Activity className={`h-3 w-3 ${testingId === node.id ? 'animate-spin' : 'text-cyan-400'}`} />
                            Test
                          </button>
                          <button
                            onClick={() => handleDelete(node.id, node.name)}
                            className="rounded-md border border-zinc-800 p-1 text-zinc-500 hover:border-red-500/30 hover:bg-red-500/10 hover:text-red-400 transition-colors"
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
        )}
      </div>

      {/* Add Shield Node Modal */}
      {showAddModal && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/70 p-4 backdrop-blur-sm">
          <div className="w-full max-w-md rounded-xl border border-zinc-800 bg-zinc-900 p-5 shadow-2xl">
            <div className="flex items-center justify-between pb-3 border-b border-zinc-800">
              <h3 className="text-sm font-semibold text-white flex items-center gap-2">
                <Shield className="h-4 w-4 text-emerald-400" />
                Add New Shield VPS Node
              </h3>
              <button
                onClick={() => setShowAddModal(false)}
                className="text-zinc-500 hover:text-zinc-300 text-xs"
              >
                ✕
              </button>
            </div>

            <form onSubmit={handleCreate} className="mt-4 space-y-4">
              <div>
                <label className="block text-xs font-medium text-zinc-300 mb-1">
                  Node Label / Name
                </label>
                <input
                  type="text"
                  placeholder="e.g. Moldova DMCA Shield"
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                  className="w-full rounded-lg border border-zinc-800 bg-zinc-950 px-3 py-2 text-xs text-white placeholder-zinc-600 focus:border-emerald-500 focus:outline-none"
                  required
                />
              </div>

              <div>
                <label className="block text-xs font-medium text-zinc-300 mb-1">
                  Shield VPS Public IPv4 Address
                </label>
                <input
                  type="text"
                  placeholder="e.g. 185.220.101.5"
                  value={ip}
                  onChange={(e) => setIP(e.target.value)}
                  className="w-full rounded-lg border border-zinc-800 bg-zinc-950 px-3 py-2 text-xs text-white placeholder-zinc-600 font-mono focus:border-emerald-500 focus:outline-none"
                  required
                />
                <p className="mt-1 text-[11px] text-zinc-500">
                  This is the public IP customer DNS A-Records will point to. Your origin server IP will stay hidden.
                </p>
              </div>

              <div className="flex items-center justify-end gap-2 pt-2 border-t border-zinc-800">
                <button
                  type="button"
                  onClick={() => setShowAddModal(false)}
                  className="rounded-lg border border-zinc-800 px-3 py-1.5 text-xs font-medium text-zinc-400 hover:bg-zinc-800"
                >
                  Cancel
                </button>
                <button
                  type="submit"
                  disabled={submitting}
                  className="rounded-lg bg-emerald-600 px-4 py-1.5 text-xs font-medium text-white hover:bg-emerald-500 disabled:opacity-50"
                >
                  {submitting ? 'Adding...' : 'Add & Generate Installer'}
                </button>
              </div>
            </form>
          </div>
        </div>
      )}

      {/* Setup Script Modal */}
      {selectedScriptNode && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/75 p-4 backdrop-blur-sm">
          <div className="w-full max-w-xl rounded-xl border border-zinc-800 bg-zinc-900 p-5 shadow-2xl">
            <div className="flex items-center justify-between pb-3 border-b border-zinc-800">
              <div className="flex items-center gap-2">
                <Terminal className="h-4 w-4 text-emerald-400" />
                <h3 className="text-sm font-semibold text-white">
                  Connect Shield VPS: {selectedScriptNode.node.name}
                </h3>
              </div>
              <button
                onClick={() => setSelectedScriptNode(null)}
                className="text-zinc-500 hover:text-zinc-300 text-xs"
              >
                ✕
              </button>
            </div>

            <div className="mt-4 space-y-3">
              <p className="text-xs text-zinc-300">
                Log into your remote Shield VPS (<span className="font-mono text-emerald-400">{selectedScriptNode.node.ip}</span>) via SSH as <strong className="text-white">root</strong> and run this single command:
              </p>

              <div className="relative rounded-lg border border-zinc-800 bg-zinc-950 p-3 font-mono text-xs text-zinc-200">
                <pre className="overflow-x-auto whitespace-pre-wrap break-all pr-10 text-emerald-300">
                  {selectedScriptNode.command}
                </pre>
                <button
                  onClick={() => handleCopy(selectedScriptNode.command, 'script-cmd')}
                  className="absolute right-2 top-2 rounded-md border border-zinc-800 bg-zinc-900 p-1.5 text-zinc-400 hover:bg-zinc-800 hover:text-white"
                  title="Copy command"
                >
                  {copied === 'script-cmd' ? (
                    <Check className="h-4 w-4 text-emerald-400" />
                  ) : (
                    <Copy className="h-4 w-4" />
                  )}
                </button>
              </div>

              <div className="rounded-lg bg-emerald-500/10 border border-emerald-500/20 p-3 text-xs text-emerald-300 space-y-1">
                <div className="font-semibold flex items-center gap-1.5">
                  <Lock className="h-3.5 w-3.5" />
                  What happens next?
                </div>
                <ul className="list-disc list-inside text-[11px] text-zinc-300 space-y-0.5">
                  <li>Caddy is automatically installed and configured with On-Demand TLS.</li>
                  <li>Whenever a domain assigned to this shield is requested, Caddy auto-issues Let's Encrypt SSL and proxies to your NextDeploy origin.</li>
                  <li>You <strong>never need to SSH into this Shield VPS again</strong>.</li>
                </ul>
              </div>

              <div className="flex justify-end pt-2 border-t border-zinc-800">
                <button
                  onClick={() => {
                    setSelectedScriptNode(null);
                    fetchNodes();
                  }}
                  className="rounded-lg bg-zinc-800 px-4 py-1.5 text-xs font-medium text-white hover:bg-zinc-700"
                >
                  Done
                </button>
              </div>
            </div>
          </div>
        </div>
      )}

      {/* Failover / Reassign Domains Modal */}
      {reassignModalNode && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/75 p-4 backdrop-blur-sm">
          <div className="w-full max-w-md rounded-xl border border-zinc-800 bg-zinc-900 p-5 shadow-2xl">
            <div className="flex items-center justify-between pb-3 border-b border-zinc-800">
              <div className="flex items-center gap-2">
                <ArrowLeftRight className="h-4 w-4 text-cyan-400" />
                <h3 className="text-sm font-semibold text-white">
                  Failover / Reassign Domains
                </h3>
              </div>
              <button
                onClick={() => setReassignModalNode(null)}
                className="text-zinc-500 hover:text-zinc-300 text-xs"
              >
                ✕
              </button>
            </div>

            <form onSubmit={handleReassign} className="mt-4 space-y-4">
              <div className="rounded-lg bg-zinc-950 border border-zinc-800 p-3 text-xs space-y-1">
                <div className="text-zinc-400">Source Shield VPS:</div>
                <div className="font-semibold text-white flex items-center justify-between">
                  <span>{reassignModalNode.name}</span>
                  <span className="font-mono text-zinc-400">{reassignModalNode.ip}</span>
                </div>
                <div className="text-[11px] text-amber-400 pt-1">
                  ⚠️ Currently routing <strong className="text-white">{reassignModalNode.assigned_domains || 0}</strong> domain(s).
                </div>
              </div>

              <div>
                <label className="block text-xs font-medium text-zinc-300 mb-1">
                  Migrate All Domains To:
                </label>
                <select
                  value={targetShieldId === null ? 'direct' : targetShieldId}
                  onChange={(e) => {
                    const val = e.target.value;
                    setTargetShieldId(val === 'direct' ? null : Number(val));
                  }}
                  className="w-full rounded-lg border border-zinc-800 bg-zinc-950 px-3 py-2 text-xs text-white focus:border-cyan-500 focus:outline-none"
                >
                  <option value="direct">🌐 Direct Origin (Server Public IP - No Shield)</option>
                  {nodes
                    .filter((n) => n.id !== reassignModalNode.id)
                    .map((n) => (
                      <option key={n.id} value={n.id}>
                        🛡️ {n.name} ({n.ip}) - {n.status}
                      </option>
                    ))}
                </select>
                <p className="mt-1.5 text-[11px] text-zinc-500">
                  Select a standby Shield VPS node, or revert all domains to direct origin routing without proxy.
                </p>
              </div>

              <div className="flex items-center justify-end gap-2 pt-2 border-t border-zinc-800">
                <button
                  type="button"
                  onClick={() => setReassignModalNode(null)}
                  className="rounded-lg border border-zinc-800 px-3 py-1.5 text-xs font-medium text-zinc-400 hover:bg-zinc-800"
                >
                  Cancel
                </button>
                <button
                  type="submit"
                  disabled={reassigning}
                  className="rounded-lg bg-cyan-600 px-4 py-1.5 text-xs font-medium text-white hover:bg-cyan-500 disabled:opacity-50 flex items-center gap-1.5"
                >
                  <ArrowLeftRight className="h-3.5 w-3.5" />
                  {reassigning ? 'Migrating...' : 'Confirm Migration'}
                </button>
              </div>
            </form>
          </div>
        </div>
      )}
    </div>
  );
}
