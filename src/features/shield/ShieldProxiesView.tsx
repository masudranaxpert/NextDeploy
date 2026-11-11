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
  Search,
  Layers,
  ExternalLink,
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

interface CliCommandItem {
  id: string;
  name: string;
  syntax: string;
  category: 'lifecycle' | 'panel' | 'diagnostics' | 'maintenance';
  descEn: string;
  descBn: string;
  flags: {
    flag: string;
    required?: boolean;
    descEn: string;
    descBn: string;
  }[];
  example: string;
}

const EDGE_CLI_COMMANDS: CliCommandItem[] = [
  {
    id: 'install',
    name: 'nd edge install',
    syntax: 'nd edge install --panel-url <url> --token-stdin [flags]',
    category: 'lifecycle',
    descEn: 'Install Caddy and nd-edge daemon, and link this Shield VPS to NextDeploy.',
    descBn: 'Caddy ও nd-edge ডেমন ইনস্টল করে এই Shield VPS-টিকে NextDeploy প্যানেলে যুক্ত করে।',
    flags: [
      { flag: '--panel-url <url>', required: true, descEn: 'HTTPS URL of NextDeploy panel', descBn: 'প্যানেলের সম্পূর্ণ HTTPS URL' },
      { flag: '--token-stdin', required: true, descEn: 'Read bearer token securely from standard input (recommended)', descBn: 'stdin থেকে নিরাপদে সিক্রেট টোকেন গ্রহণ করে (পরামর্শিত)' },
      { flag: '--name <name>', descEn: 'Friendly alias name for this shield node', descBn: 'নোডের জন্য পরিচিত নাম/অ্যালিয়াস' },
      { flag: '--token <token>', descEn: 'Pass token directly via command-line argument', descBn: 'সরাসরি কমান্ড লাইনে টোকেন ইনপুট' },
      { flag: '--insecure-skip-tls-verify', descEn: 'Skip TLS certificate verification (testing only)', descBn: 'TLS ভেরিফিকেশন স্কিপ (শুধুমাত্র টেস্টের জন্য)' },
    ],
    example: 'printf \'%s\\n\' \'<TOKEN>\' | nd edge install --panel-url "https://panel.example.com" --token-stdin',
  },
  {
    id: 'add-panel',
    name: 'nd edge add-panel',
    syntax: 'nd edge add-panel --panel-url <url> --token-stdin [flags]',
    category: 'panel',
    descEn: 'Register an additional panel to this Shield VPS (enables multi-panel sharing).',
    descBn: 'এই Shield VPS-এ আরেকটি নতুন প্যানেল যুক্ত করে (মাল্টি-প্যানেল শেয়ারিং)।',
    flags: [
      { flag: '--panel-url <url>', required: true, descEn: 'HTTPS URL of the second panel', descBn: 'দ্বিতীয় প্যানেলের HTTPS URL' },
      { flag: '--token-stdin', required: true, descEn: 'Read bearer token from stdin', descBn: 'stdin থেকে প্যানেলের টোকেন ইনপুট' },
      { flag: '--name <name>', descEn: 'Display alias for the new panel', descBn: 'নতুন প্যানেলের নাম বা পরিচিতি' },
    ],
    example: 'printf \'%s\\n\' \'<TOKEN>\' | nd edge add-panel --panel-url "https://panel-2.example.com" --token-stdin',
  },
  {
    id: 'remove-panel',
    name: 'nd edge remove-panel',
    syntax: 'nd edge remove-panel <panel_id|name>',
    category: 'panel',
    descEn: 'Unregister a panel and clean up its dedicated Caddy route file.',
    descBn: 'একটি প্যানেল নোড থেকে আনরেজিস্টার করে এবং এর রাউটিং ফাইল মুছে ফেলে।',
    flags: [
      { flag: '<panel_id|name>', required: true, descEn: 'Target panel ID or registered name', descBn: 'টার্গেট প্যানেল আইডি বা নাম' },
    ],
    example: 'nd edge remove-panel panel_1b8c1a',
  },
  {
    id: 'list',
    name: 'nd edge list',
    syntax: 'nd edge list [--json]',
    category: 'panel',
    descEn: 'List all registered panels, sync states, route counts, and node ID.',
    descBn: 'নিবন্ধিত প্যানেলসমূহ, সিঙ্ক অবস্থা, ডোমেইন সংখ্যা ও নোড আইডি দেখায়।',
    flags: [
      { flag: '--json', descEn: 'Output results in JSON format', descBn: 'JSON ফরম্যাটে আউটপুট প্রদর্শন' },
    ],
    example: 'nd edge list',
  },
  {
    id: 'status',
    name: 'nd edge status',
    syntax: 'nd edge status [domain] [--json]',
    category: 'diagnostics',
    descEn: 'Inspect real-time TLS readiness, Let\'s Encrypt cert status, and upstream routes.',
    descBn: 'ডোমেইনের রিয়েল-টাইম TLS সার্টিফিকেট, Caddy রুট ও আপস্ট্রিম স্ট্যাটাস পরীক্ষা করে।',
    flags: [
      { flag: '[domain]', descEn: 'Specific domain to inspect (omit to inspect all hosts)', descBn: 'নির্দিষ্ট কোনো ডোমেইন চেক করার নাম' },
      { flag: '--json', descEn: 'Output routing and cert state as JSON', descBn: 'JSON ফরম্যাটে আউটপুট' },
    ],
    example: 'nd edge status app.example.com',
  },
  {
    id: 'sync',
    name: 'nd edge sync',
    syntax: 'nd edge sync [--panel <id>] [--force]',
    category: 'lifecycle',
    descEn: 'Trigger immediate reconciliation with panel(s) and reload Caddy zero-downtime.',
    descBn: 'প্যানেলের সাথে তাৎক্ষণিক সিঙ্ক করে এবং ডাউনটাইম ছাড়া Caddy রিলোড করে।',
    flags: [
      { flag: '--panel <id>', descEn: 'Reconcile only the specified panel ID', descBn: 'নির্দিষ্ট কোনো প্যানেলের রুট সিঙ্ক করতে' },
      { flag: '--force', descEn: 'Force full Caddyfile rewrite and immediate reload', descBn: 'সম্পূর্ণ Caddyfile পুনর্লিখন ও তাৎক্ষণিক রিলোড' },
    ],
    example: 'nd edge sync --force',
  },
  {
    id: 'doctor',
    name: 'nd edge doctor',
    syntax: 'nd edge doctor',
    category: 'diagnostics',
    descEn: 'Audit Shield health: firewall ports (80/443), systemd service, disk space, and DNS.',
    descBn: 'ফায়ারওয়াল পোর্ট (৮০, ৪৪৩), সার্ভিস হেলথ, ডিস্ক স্পেস ও নেটওয়ার্ক ডায়াগনস্টিক চালায়।',
    flags: [],
    example: 'nd edge doctor',
  },
  {
    id: 'set-token',
    name: 'nd edge set-token',
    syntax: 'nd edge set-token <panel_id|name> --token-stdin',
    category: 'panel',
    descEn: 'Update or rotate authentication bearer token for a registered panel.',
    descBn: 'নিবন্ধিত প্যানেলের জন্য নতুন সিক্রেট টোকেন নিরাপদে আপডেট বা রোটেট করে।',
    flags: [
      { flag: '<panel_id|name>', required: true, descEn: 'Target panel ID or registered name', descBn: 'টার্গেট প্যানেল আইডি বা নাম' },
      { flag: '--token-stdin', required: true, descEn: 'Read new token securely from standard input', descBn: 'stdin থেকে নতুন সিক্রেট টোকেন গ্রহণ করে' },
    ],
    example: 'printf \'%s\\n\' \'<NEW_TOKEN>\' | nd edge set-token panel_1b8c1a --token-stdin',
  },
  {
    id: 'set-url',
    name: 'nd edge set-url',
    syntax: 'nd edge set-url <panel_id|name> --panel-url <url>',
    category: 'panel',
    descEn: 'Update API endpoint URL for a panel (e.g. after domain or hostname migration).',
    descBn: 'প্যানেলের নতুন API ডোমেইন বা URL কনফিগার করে।',
    flags: [
      { flag: '<panel_id|name>', required: true, descEn: 'Target panel ID or registered name', descBn: 'টার্গেট প্যানেল আইডি বা নাম' },
      { flag: '--panel-url <url>', required: true, descEn: 'New HTTPS URL for the panel', descBn: 'প্যানেলের নতুন HTTPS URL' },
    ],
    example: 'nd edge set-url panel_1b8c1a --panel-url "https://newpanel.example.com"',
  },
  {
    id: 'upgrade',
    name: 'nd edge upgrade',
    syntax: 'nd edge upgrade [--url <url>] [--sha256 <hex>]',
    category: 'maintenance',
    descEn: 'Self-update nd binary in-place with SHA-256 verification and daemon restart.',
    descBn: 'SHA-256 ভেরিফিকেশন সহ nd বাইনারি নিরাপদভাবে আপগ্রেড ও অটোমেটিক রিস্টার্ট করে।',
    flags: [
      { flag: '--url <url>', descEn: 'Custom binary download URL', descBn: 'কাস্টম বাইনারি ডাউনলোডের লিংক' },
      { flag: '--sha256 <hex>', descEn: 'Expected SHA-256 hash for verification', descBn: 'ভেরিফিকেশনের জন্য প্রত্যাশিত SHA-256 হ্যাশ' },
    ],
    example: 'nd edge upgrade',
  },
  {
    id: 'uninstall',
    name: 'nd edge uninstall',
    syntax: 'nd edge uninstall [--all]',
    category: 'maintenance',
    descEn: 'Stop nd-edge service, clean up systemd daemon units, and restore Caddy configuration.',
    descBn: 'nd-edge সার্ভিস বন্ধ করে, সিস্টেম ইউনিট ও কনফিগ পরিষ্কার করে Caddy রিস্টোর করে।',
    flags: [
      { flag: '--all', descEn: 'Purge all panel configs and edge working directories', descBn: 'সকল প্যানেল কনফিগ ও এজ ডিরেক্টরি সম্পূর্ণ মুছে ফেলতে' },
    ],
    example: 'nd edge uninstall --all',
  },
];

export function ShieldProxiesView() {
  const [nodes, setNodes] = useState<ShieldNode[]>([]);
  const [originIP, setOriginIP] = useState<string>('');
  const [panelId, setPanelIdState] = useState<string>(() => getPanelId());
  const [loading, setLoading] = useState(true);
  const [showAddModal, setShowAddModal] = useState(false);
  const [showDocsModal, setShowDocsModal] = useState(false);
  const [docsTab, setDocsTab] = useState<'cli' | 'dns' | 'setup' | 'arch'>('cli');
  const [docsLang, setDocsLang] = useState<'en' | 'bn'>('en');
  const [cliSearch, setCliSearch] = useState('');
  const [cliCategory, setCliCategory] = useState<'all' | 'lifecycle' | 'panel' | 'diagnostics' | 'maintenance'>('all');
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
            onClick={() => setShowDocsModal(true)}
            className="flex-1 sm:flex-none inline-flex items-center justify-center gap-1.5 h-9 min-h-[36px] px-3.5 rounded-lg border border-emerald-500/30 bg-emerald-500/10 text-xs font-semibold text-emerald-400 hover:bg-emerald-500/20 hover:text-emerald-300 transition-colors cursor-pointer"
          >
            <BookOpen className="h-3.5 w-3.5" />
            <span>Docs & CLI</span>
          </button>
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

      {/* Shield Documentation & CLI Reference Modal */}
      <ShadcnDialog
        open={showDocsModal}
        onClose={() => setShowDocsModal(false)}
        title={docsLang === 'en' ? 'Shield VPS Docs & CLI Reference' : 'Shield VPS ডকস ও CLI নির্দেশিকা'}
        description={
          docsLang === 'en'
            ? 'Complete guide for nd edge commands, DNS configuration, and architecture.'
            : 'nd edge কমান্ড, DNS সেটআপ ও আর্কিটেকচারের পূর্ণাঙ্গ গাইড।'
        }
        icon={<BookOpen className="h-5 w-5 text-emerald-400" />}
        maxWidth="3xl"
        headerActions={
          <div className="inline-flex items-center p-0.5 rounded-lg bg-zinc-900 border border-zinc-800 text-[11px] font-medium mr-1">
            <button
              type="button"
              onClick={() => setDocsLang('en')}
              className={`px-2 py-0.5 rounded transition-colors cursor-pointer ${
                docsLang === 'en'
                  ? 'bg-zinc-800 text-white shadow-sm font-semibold'
                  : 'text-zinc-400 hover:text-zinc-200'
              }`}
            >
              EN
            </button>
            <button
              type="button"
              onClick={() => setDocsLang('bn')}
              className={`px-2 py-0.5 rounded transition-colors cursor-pointer ${
                docsLang === 'bn'
                  ? 'bg-zinc-800 text-emerald-400 shadow-sm font-semibold'
                  : 'text-zinc-400 hover:text-zinc-200'
              }`}
            >
              বাংলা
            </button>
          </div>
        }
        footer={
          <div className="flex flex-col sm:flex-row items-center justify-between gap-2.5 w-full">
            <div className="text-[11px] text-zinc-400 text-center sm:text-left">
              {docsLang === 'en' ? 'Run ' : 'যেকোনো সময় VPS-এ চালান: '}
              <code className="text-emerald-400 font-mono">nd edge doctor</code>
              {docsLang === 'en' ? ' to diagnose firewall and TLS health.' : ' (ফায়ারওয়াল ও TLS ডায়াগনস্টিক)।'}
            </div>
            <button
              type="button"
              onClick={() => setShowDocsModal(false)}
              className="w-full sm:w-auto h-8 px-4 rounded-lg bg-zinc-800 text-xs font-medium text-white hover:bg-zinc-700 transition-colors cursor-pointer"
            >
              {docsLang === 'en' ? 'Close' : 'বন্ধ করুন'}
            </button>
          </div>
        }
      >
        <div className="space-y-3.5 text-xs">
          {/* Responsive Tab Bar */}
          <div className="flex items-center gap-1.5 border-b border-zinc-800 pb-2.5 overflow-x-auto scrollbar-none">
            <button
              type="button"
              onClick={() => setDocsTab('cli')}
              className={`px-3 py-1.5 rounded-lg text-xs font-medium flex items-center gap-1.5 whitespace-nowrap transition-colors cursor-pointer ${
                docsTab === 'cli'
                  ? 'bg-emerald-500/15 text-emerald-400 border border-emerald-500/30'
                  : 'text-zinc-400 hover:text-zinc-200 hover:bg-zinc-800/40 border border-transparent'
              }`}
            >
              <Terminal className="h-3.5 w-3.5" />
              <span>{docsLang === 'en' ? 'nd edge CLI (11)' : 'কমান্ড রেফারেন্স (১১)'}</span>
            </button>
            <button
              type="button"
              onClick={() => setDocsTab('dns')}
              className={`px-3 py-1.5 rounded-lg text-xs font-medium flex items-center gap-1.5 whitespace-nowrap transition-colors cursor-pointer ${
                docsTab === 'dns'
                  ? 'bg-emerald-500/15 text-emerald-400 border border-emerald-500/30'
                  : 'text-zinc-400 hover:text-zinc-200 hover:bg-zinc-800/40 border border-transparent'
              }`}
            >
              <Globe className="h-3.5 w-3.5" />
              <span>{docsLang === 'en' ? 'DNS & Cloudflare' : 'DNS ও Cloudflare'}</span>
            </button>
            <button
              type="button"
              onClick={() => setDocsTab('setup')}
              className={`px-3 py-1.5 rounded-lg text-xs font-medium flex items-center gap-1.5 whitespace-nowrap transition-colors cursor-pointer ${
                docsTab === 'setup'
                  ? 'bg-emerald-500/15 text-emerald-400 border border-emerald-500/30'
                  : 'text-zinc-400 hover:text-zinc-200 hover:bg-zinc-800/40 border border-transparent'
              }`}
            >
              <Layers className="h-3.5 w-3.5" />
              <span>{docsLang === 'en' ? 'Quick Setup' : 'কুইক সেটআপ'}</span>
            </button>
            <button
              type="button"
              onClick={() => setDocsTab('arch')}
              className={`px-3 py-1.5 rounded-lg text-xs font-medium flex items-center gap-1.5 whitespace-nowrap transition-colors cursor-pointer ${
                docsTab === 'arch'
                  ? 'bg-emerald-500/15 text-emerald-400 border border-emerald-500/30'
                  : 'text-zinc-400 hover:text-zinc-200 hover:bg-zinc-800/40 border border-transparent'
              }`}
            >
              <ShieldCheck className="h-3.5 w-3.5" />
              <span>{docsLang === 'en' ? 'Architecture' : 'আর্কিটেকচার'}</span>
            </button>
          </div>

          {/* Tab Content Container */}
          <div className="max-h-[60vh] sm:max-h-[64vh] overflow-y-auto pr-1 space-y-3.5">
            {/* TAB 1: CLI REFERENCE */}
            {docsTab === 'cli' && (
              <div className="space-y-3">
                {/* Search & Category Filter */}
                <div className="flex flex-col sm:flex-row gap-2">
                  <div className="relative flex-1">
                    <Search className="pointer-events-none absolute left-2.5 top-2.5 h-3.5 w-3.5 text-zinc-500" />
                    <input
                      type="text"
                      value={cliSearch}
                      onChange={(e) => setCliSearch(e.target.value)}
                      placeholder={
                        docsLang === 'en'
                          ? 'Search commands, flags, or parameters...'
                          : 'কমান্ড বা প্যারামিটার খুঁজুন...'
                      }
                      className="w-full h-8 pl-8 pr-3 rounded-lg border border-zinc-800 bg-zinc-950 text-xs text-zinc-200 placeholder:text-zinc-500 focus:border-emerald-500/50 focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-emerald-500/50"
                    />
                  </div>
                  <div className="flex items-center gap-1 overflow-x-auto scrollbar-none pb-0.5">
                    {(
                      [
                        { id: 'all', en: 'All', bn: 'সব' },
                        { id: 'lifecycle', en: 'Core', bn: 'কোর' },
                        { id: 'panel', en: 'Panels', bn: 'প্যানেল' },
                        { id: 'diagnostics', en: 'Health', bn: 'হেলথ' },
                        { id: 'maintenance', en: 'Maintenance', bn: 'আপগ্রেড' },
                      ] as const
                    ).map((cat) => (
                      <button
                        key={cat.id}
                        type="button"
                        onClick={() => setCliCategory(cat.id)}
                        className={`h-8 px-2.5 rounded-lg text-[11px] font-medium whitespace-nowrap transition-colors cursor-pointer ${
                          cliCategory === cat.id
                            ? 'bg-zinc-800 text-white font-semibold'
                            : 'text-zinc-400 hover:text-zinc-200 hover:bg-zinc-900'
                        }`}
                      >
                        {docsLang === 'en' ? cat.en : cat.bn}
                      </button>
                    ))}
                  </div>
                </div>

                {/* Commands List */}
                <div className="space-y-2.5">
                  {EDGE_CLI_COMMANDS.filter((cmd) => {
                    const matchesCat = cliCategory === 'all' || cmd.category === cliCategory;
                    if (!matchesCat) return false;
                    if (!cliSearch.trim()) return true;
                    const q = cliSearch.toLowerCase().trim();
                    return (
                      cmd.name.toLowerCase().includes(q) ||
                      cmd.syntax.toLowerCase().includes(q) ||
                      cmd.descEn.toLowerCase().includes(q) ||
                      cmd.descBn.toLowerCase().includes(q) ||
                      cmd.flags.some(
                        (f) =>
                          f.flag.toLowerCase().includes(q) ||
                          f.descEn.toLowerCase().includes(q) ||
                          f.descBn.toLowerCase().includes(q)
                      )
                    );
                  }).map((cmd) => (
                    <div
                      key={cmd.id}
                      className="rounded-xl border border-zinc-800/90 bg-zinc-950/70 p-3 sm:p-3.5 space-y-2.5"
                    >
                      {/* Top Header */}
                      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-1.5">
                        <div className="flex items-center gap-2">
                          <code className="text-xs font-bold text-emerald-400 font-mono">
                            {cmd.name}
                          </code>
                          <span className="rounded bg-zinc-900 border border-zinc-800 px-1.5 py-0.2 text-[10px] uppercase text-zinc-400">
                            {cmd.category}
                          </span>
                        </div>
                        <p className="text-[11px] text-zinc-300">
                          {docsLang === 'en' ? cmd.descEn : cmd.descBn}
                        </p>
                      </div>

                      {/* Syntax Box */}
                      <div className="relative rounded-lg border border-zinc-800 bg-zinc-900/80 p-2 font-mono text-[11px] text-zinc-300">
                        <pre className="overflow-x-auto whitespace-pre pr-8 font-mono text-zinc-200">
                          {cmd.syntax}
                        </pre>
                        <button
                          type="button"
                          onClick={() => handleCopy(cmd.syntax, `cmd-syntax-${cmd.id}`)}
                          className="absolute right-1.5 top-1.5 h-6 w-6 rounded flex items-center justify-center text-zinc-400 hover:text-white hover:bg-zinc-800 transition-colors cursor-pointer"
                          title="Copy syntax"
                        >
                          {copied === `cmd-syntax-${cmd.id}` ? (
                            <Check className="h-3.5 w-3.5 text-emerald-400" />
                          ) : (
                            <Copy className="h-3.5 w-3.5" />
                          )}
                        </button>
                      </div>

                      {/* Flags / Parameters */}
                      {cmd.flags.length > 0 && (
                        <div className="rounded-lg border border-zinc-800/60 bg-zinc-900/30 p-2 space-y-1.5">
                          <span className="text-[10px] font-semibold uppercase tracking-wider text-zinc-400 block">
                            {docsLang === 'en' ? 'Parameters / Flags' : 'প্যারামিটার ও ফ্ল্যাগ'}
                          </span>
                          <div className="grid grid-cols-1 gap-1 text-[11px]">
                            {cmd.flags.map((flag, idx) => (
                              <div
                                key={idx}
                                className="flex flex-col sm:flex-row sm:items-center justify-between gap-1 py-0.5 border-b border-zinc-800/40 last:border-0"
                              >
                                <div className="flex items-center gap-1.5">
                                  <code className="text-cyan-300 font-mono text-[11px]">
                                    {flag.flag}
                                  </code>
                                  {flag.required && (
                                    <span className="rounded bg-rose-500/15 px-1 py-0.2 text-[9px] font-bold text-rose-400 uppercase">
                                      {docsLang === 'en' ? 'Required' : 'প্রয়োজনীয়'}
                                    </span>
                                  )}
                                </div>
                                <span className="text-zinc-400 text-[11px]">
                                  {docsLang === 'en' ? flag.descEn : flag.descBn}
                                </span>
                              </div>
                            ))}
                          </div>
                        </div>
                      )}

                      {/* Example Box */}
                      <div className="flex items-center justify-between gap-2 rounded-md bg-zinc-900/50 border border-zinc-800/60 px-2 py-1.5 font-mono text-[10px] text-zinc-400">
                        <span className="truncate">
                          <strong className="text-zinc-400 uppercase mr-1">
                            {docsLang === 'en' ? 'Example:' : 'উদাহরণ:'}
                          </strong>
                          <span className="text-amber-300/90">{cmd.example}</span>
                        </span>
                        <button
                          type="button"
                          onClick={() => handleCopy(cmd.example, `cmd-ex-${cmd.id}`)}
                          className="text-zinc-400 hover:text-emerald-400 shrink-0 cursor-pointer p-0.5"
                          title="Copy example"
                        >
                          {copied === `cmd-ex-${cmd.id}` ? (
                            <Check className="h-3 w-3 text-emerald-400" />
                          ) : (
                            <Copy className="h-3 w-3" />
                          )}
                        </button>
                      </div>
                    </div>
                  ))}
                </div>
              </div>
            )}

            {/* TAB 2: DNS & CLOUDFLARE */}
            {docsTab === 'dns' && (
              <div className="space-y-3">
                <div className="rounded-xl border border-zinc-800 bg-zinc-950/70 p-3.5 space-y-2">
                  <div className="flex items-center gap-2">
                    <Globe className="h-4 w-4 text-emerald-400" />
                    <h4 className="font-semibold text-zinc-100">
                      {docsLang === 'en' ? '1. Domain DNS A-Record' : '১. ডোমেইন DNS A-রেকর্ড'}
                    </h4>
                  </div>
                  <p className="text-zinc-400 leading-relaxed">
                    {docsLang === 'en'
                      ? 'Point your domain (or wildcard *) directly to the Shield Node Public IP instead of your origin server.'
                      : 'ডোমেইনের DNS প্রোভাইডারে (Namecheap, Cloudflare ইত্যাদি) A-রেকর্ড সরাসরি Shield Node Public IP-তে পয়েন্ট করুন।'}
                  </p>
                  <div className="rounded-lg border border-zinc-800 bg-zinc-900/80 p-2.5 font-mono text-[11px] grid grid-cols-3 gap-2 text-center text-zinc-300">
                    <div>
                      <span className="text-[10px] text-zinc-500 block uppercase">Type</span>
                      <strong className="text-emerald-400">A</strong>
                    </div>
                    <div>
                      <span className="text-[10px] text-zinc-500 block uppercase">Host / Name</span>
                      <span>@ or subdomain</span>
                    </div>
                    <div>
                      <span className="text-[10px] text-zinc-500 block uppercase">Points To</span>
                      <span className="text-cyan-300">&lt;Shield_VPS_IP&gt;</span>
                    </div>
                  </div>
                </div>

                <div className="rounded-xl border border-amber-500/25 bg-amber-500/5 p-3.5 space-y-2">
                  <div className="flex items-center gap-2">
                    <Lock className="h-4 w-4 text-amber-400" />
                    <h4 className="font-semibold text-amber-200">
                      {docsLang === 'en'
                        ? '2. Cloudflare SSL Mode: Full or Full (strict)'
                        : '২. Cloudflare SSL মোড: Full অথবা Full (strict)'}
                    </h4>
                  </div>
                  <p className="text-zinc-300 leading-relaxed">
                    {docsLang === 'en' ? (
                      <>
                        Behind Cloudflare (orange cloud proxy), always set SSL/TLS Encryption mode to{' '}
                        <strong className="text-white">Full</strong> or{' '}
                        <strong className="text-white">Full (strict)</strong>. Never use{' '}
                        <em>Flexible</em>, because Flexible connects over HTTP:80 to your Shield VPS,
                        triggering an infinite redirect loop (
                        <code className="text-amber-300">ERR_TOO_MANY_REDIRECTS</code>).
                      </>
                    ) : (
                      <>
                        Cloudflare প্রক্সি (কমলা ক্লাউড) ব্যবহার করলে SSL/TLS মোড সবসময়{' '}
                        <strong className="text-white">Full</strong> বা{' '}
                        <strong className="text-white">Full (strict)</strong> রাখবেন। ভুলেও{' '}
                        <em>Flexible</em> সিলেক্ট করবেন না, কারণ Flexible পোর্ট ৮০ দিয়ে রিকোয়েস্ট পাঠায়
                        যা রিডাইরেক্ট লুপ (<code className="text-amber-300">ERR_TOO_MANY_REDIRECTS</code>)
                        তৈরি করে।
                      </>
                    )}
                  </p>
                </div>

                <div className="rounded-xl border border-zinc-800 bg-zinc-950/70 p-3.5 space-y-2">
                  <div className="flex items-center gap-2">
                    <Terminal className="h-4 w-4 text-cyan-400" />
                    <h4 className="font-semibold text-zinc-100">
                      {docsLang === 'en' ? '3. Verification Commands' : '৩. ভেরিফিকেশন কমান্ডস'}
                    </h4>
                  </div>
                  <div className="space-y-2">
                    <div className="relative rounded-md border border-zinc-800 bg-zinc-900/90 p-2 font-mono text-[11px] text-zinc-300">
                      <pre className="overflow-x-auto whitespace-pre pr-8">
                        dig +short yourdomain.com{'\n'}curl -Iv https://yourdomain.com{'\n'}nd edge status yourdomain.com
                      </pre>
                      <button
                        type="button"
                        onClick={() =>
                          handleCopy(
                            'dig +short yourdomain.com\ncurl -Iv https://yourdomain.com\nnd edge status yourdomain.com',
                            'dns-verify-cmds'
                          )
                        }
                        className="absolute right-1.5 top-1.5 h-6 w-6 rounded flex items-center justify-center text-zinc-400 hover:text-white hover:bg-zinc-800 transition-colors cursor-pointer"
                        title="Copy commands"
                      >
                        {copied === 'dns-verify-cmds' ? (
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

            {/* TAB 3: QUICK SETUP */}
            {docsTab === 'setup' && (
              <div className="space-y-3">
                <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                  {/* Step 1 */}
                  <div className="rounded-xl border border-zinc-800 bg-zinc-950/70 p-3.5 flex flex-col justify-between gap-2.5">
                    <div className="space-y-1">
                      <div className="flex items-center justify-between">
                        <span className="rounded bg-emerald-500/15 px-1.5 py-0.5 font-mono text-[11px] font-bold text-emerald-400">
                          STEP 1
                        </span>
                        <span className="text-[11px] text-zinc-400">Prerequisites</span>
                      </div>
                      <h4 className="font-semibold text-zinc-200">
                        {docsLang === 'en' ? 'Prepare VPS & Firewall' : 'ফায়ারওয়াল ও পোর্ট ওপেন'}
                      </h4>
                      <p className="text-[11px] text-zinc-400">
                        {docsLang === 'en'
                          ? 'Run on your remote VPS as root to allow HTTP/HTTPS:'
                          : 'রিমোট VPS-এ রুট ইউজার হিসেবে পোর্ট ওপেন করুন:'}
                      </p>
                    </div>
                    <div className="relative rounded-md border border-zinc-800 bg-zinc-900/90 p-2 font-mono text-[10px] text-zinc-300">
                      <pre className="overflow-x-auto whitespace-pre pr-7">
                        apt update && apt install -y curl ufw{'\n'}ufw allow 80/tcp{'\n'}ufw allow 443/tcp{'\n'}ufw allow 443/udp
                      </pre>
                      <button
                        type="button"
                        onClick={() => handleCopy(SHIELD_PREP_COMMAND, 'setup-step-1')}
                        className="absolute right-1.5 top-1.5 h-6 w-6 rounded flex items-center justify-center text-zinc-400 hover:text-white hover:bg-zinc-800 transition-colors cursor-pointer"
                        title="Copy commands"
                      >
                        {copied === 'setup-step-1' ? (
                          <Check className="h-3.5 w-3.5 text-emerald-400" />
                        ) : (
                          <Copy className="h-3.5 w-3.5" />
                        )}
                      </button>
                    </div>
                  </div>

                  {/* Step 2 */}
                  <div className="rounded-xl border border-zinc-800 bg-zinc-950/70 p-3.5 flex flex-col justify-between gap-2.5">
                    <div className="space-y-1">
                      <div className="flex items-center justify-between">
                        <span className="rounded bg-cyan-500/15 px-1.5 py-0.5 font-mono text-[11px] font-bold text-cyan-400">
                          STEP 2
                        </span>
                        <span className="text-[11px] text-zinc-400">Panel</span>
                      </div>
                      <h4 className="font-semibold text-zinc-200">
                        {docsLang === 'en' ? 'Add Shield Node' : 'প্যানেলে নোড যোগ করুন'}
                      </h4>
                      <p className="text-[11px] text-zinc-400">
                        {docsLang === 'en'
                          ? 'Click "+ Add Shield Node" above to enter VPS IP and generate an install command.'
                          : 'উপরে "+ Add Shield Node" বাটনে ক্লিক করে VPS IP দিন ও ইনস্টল টোকেন নিন।'}
                      </p>
                    </div>
                    <div className="rounded-md border border-zinc-800 bg-zinc-900/40 p-2 text-[11px] text-zinc-400 text-center">
                      Uses <code className="text-zinc-200">--token-stdin</code> (safe token handling)
                    </div>
                  </div>

                  {/* Step 3 */}
                  <div className="rounded-xl border border-zinc-800 bg-zinc-950/70 p-3.5 flex flex-col justify-between gap-2.5">
                    <div className="space-y-1">
                      <div className="flex items-center justify-between">
                        <span className="rounded bg-amber-500/15 px-1.5 py-0.5 font-mono text-[11px] font-bold text-amber-400">
                          STEP 3
                        </span>
                        <span className="text-[11px] text-zinc-400">Install</span>
                      </div>
                      <h4 className="font-semibold text-zinc-200">
                        {docsLang === 'en' ? 'Run Install Command' : 'ইনস্টল কমান্ড চালান'}
                      </h4>
                      <p className="text-[11px] text-zinc-400">
                        {docsLang === 'en'
                          ? 'Paste and run the generated command on your VPS as root.'
                          : 'জেনারেট হওয়া ইনস্টল কমান্ডটি VPS টার্মিনালে রুট হিসেবে রান করুন।'}
                      </p>
                    </div>
                    <div className="relative rounded-md border border-zinc-800 bg-zinc-900/90 p-2 font-mono text-[10px] text-zinc-300">
                      <pre className="overflow-x-auto whitespace-pre pr-7 text-amber-300">
                        {buildInstallCommand(panelOrigin, 'YOUR_TOKEN')}
                      </pre>
                      <button
                        type="button"
                        onClick={() =>
                          handleCopy(buildInstallCommand(panelOrigin, 'YOUR_TOKEN'), 'setup-step-3')
                        }
                        className="absolute right-1.5 top-1.5 h-6 w-6 rounded flex items-center justify-center text-zinc-400 hover:text-white hover:bg-zinc-800 transition-colors cursor-pointer"
                        title="Copy template"
                      >
                        {copied === 'setup-step-3' ? (
                          <Check className="h-3.5 w-3.5 text-emerald-400" />
                        ) : (
                          <Copy className="h-3.5 w-3.5" />
                        )}
                      </button>
                    </div>
                  </div>

                  {/* Step 4 */}
                  <div className="rounded-xl border border-zinc-800 bg-zinc-950/70 p-3.5 flex flex-col justify-between gap-2.5">
                    <div className="space-y-1">
                      <div className="flex items-center justify-between">
                        <span className="rounded bg-emerald-500/15 px-1.5 py-0.5 font-mono text-[11px] font-bold text-emerald-400">
                          STEP 4
                        </span>
                        <span className="text-[11px] text-zinc-400">Verify</span>
                      </div>
                      <h4 className="font-semibold text-zinc-200">
                        {docsLang === 'en' ? 'Run nd edge doctor' : 'হেলথ চেক ও নিশ্চিতকরণ'}
                      </h4>
                      <p className="text-[11px] text-zinc-400">
                        {docsLang === 'en'
                          ? 'Check that the node shows Sync OK, then point domain DNS.'
                          : 'ডায়াগনস্টিক কমান্ড চালিয়ে নিশ্চিত হোন এবং ডোমেইন DNS পরিবর্তন করুন।'}
                      </p>
                    </div>
                    <div className="relative rounded-md border border-zinc-800 bg-zinc-900/90 p-2 font-mono text-[11px] text-zinc-300">
                      <pre className="overflow-x-auto whitespace-pre pr-7">nd edge doctor</pre>
                      <button
                        type="button"
                        onClick={() => handleCopy('nd edge doctor', 'setup-step-4')}
                        className="absolute right-1.5 top-1.5 h-6 w-6 rounded flex items-center justify-center text-zinc-400 hover:text-white hover:bg-zinc-800 transition-colors cursor-pointer"
                        title="Copy command"
                      >
                        {copied === 'setup-step-4' ? (
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

            {/* TAB 4: ARCHITECTURE */}
            {docsTab === 'arch' && (
              <div className="space-y-2.5">
                <div className="rounded-xl border border-zinc-800 bg-zinc-950/70 p-3.5 space-y-1.5">
                  <h4 className="font-semibold text-zinc-100 flex items-center gap-1.5">
                    <ShieldCheck className="h-4 w-4 text-emerald-400" />
                    <span>
                      {docsLang === 'en'
                        ? '1. Origin IP Masking & DDoS Protection'
                        : '১. অরিজিন IP মাস্কিং ও সিকিউরিটি'}
                    </span>
                  </h4>
                  <p className="text-zinc-400 leading-relaxed text-[11px]">
                    {docsLang === 'en'
                      ? 'Visitors, bots, and crawlers only interact with the Shield Node IP. Your origin server and internal containers stay private and protected from direct network probing.'
                      : 'ভিজিটর বা বট ট্রাফিক শুধুমাত্র Shield Node IP দেখতে পায়। আপনার মেইন সার্ভার IP সম্পূর্ণ গোপন ও নিরাপদ থাকে।'}
                  </p>
                </div>

                <div className="rounded-xl border border-zinc-800 bg-zinc-950/70 p-3.5 space-y-1.5">
                  <h4 className="font-semibold text-zinc-100 flex items-center gap-1.5">
                    <Layers className="h-4 w-4 text-cyan-400" />
                    <span>
                      {docsLang === 'en'
                        ? '2. Multi-Panel Isolation & Route Separation'
                        : '২. মাল্টি-প্যানেল আইসোলেশন ও রুট ফাইল'}
                    </span>
                  </h4>
                  <p className="text-zinc-400 leading-relaxed text-[11px]">
                    {docsLang === 'en'
                      ? 'One Shield VPS can front multiple independent NextDeploy panels simultaneously. Each panel maintains its own isolated route configuration in /etc/caddy/nd-edge/panels/ with domain conflict protection.'
                      : 'একটিমাত্র Shield VPS একই সাথে একাধিক NextDeploy প্যানেলের প্রক্সি হিসেবে কাজ করতে পারে। প্রতিটি প্যানেলের জন্য আলাদা রুট ফাইল তৈরি হয় এবং ডোমেইন কনফ্লিক্ট সুরক্ষা বজায় থাকে।'}
                  </p>
                </div>

                <div className="rounded-xl border border-zinc-800 bg-zinc-950/70 p-3.5 space-y-1.5">
                  <h4 className="font-semibold text-zinc-100 flex items-center gap-1.5">
                    <Lock className="h-4 w-4 text-amber-400" />
                    <span>
                      {docsLang === 'en'
                        ? '3. Autonomous Caddy TLS & Zero Downtime'
                        : '৩. স্বয়ংক্রিয় TLS ও জিরো ডাউনটাইম'}
                    </span>
                  </h4>
                  <p className="text-zinc-400 leading-relaxed text-[11px]">
                    {docsLang === 'en'
                      ? 'Caddy issues and renews Let’s Encrypt / ZeroSSL certificates independently. Even if the NextDeploy panel is restarted or undergoes maintenance, the Shield VPS continues serving existing traffic seamlessly.'
                      : 'Caddy স্বয়ংক্রিয়ভাবে Let’s Encrypt ও ZeroSSL সার্টিফিকেট ইস্যু ও রিনিউ করে। প্যানেল রিবুট বা আপগ্রেড চলাকালীনও ট্রাফিকে কোনো ব্যাঘাত বা ডাউনটাইম ঘটে না।'}
                  </p>
                </div>

                <div className="rounded-xl border border-zinc-800 bg-zinc-950/70 p-3.5 space-y-1.5">
                  <h4 className="font-semibold text-zinc-100 flex items-center gap-1.5">
                    <ArrowLeftRight className="h-4 w-4 text-emerald-400" />
                    <span>
                      {docsLang === 'en'
                        ? '4. 1-Click Standby Failover'
                        : '৪. ১-ক্লিক স্ট্যান্ডবাই ফেইলওভার'}
                    </span>
                  </h4>
                  <p className="text-zinc-400 leading-relaxed text-[11px]">
                    {docsLang === 'en'
                      ? 'If a shield node ever degrades, click "Reassign" on any node to migrate all domains to another standby shield node or revert to direct origin host.'
                      : 'যদি কোনো এজ নোডে সমস্যা দেখা দেয়, "Reassign" বাটনে ক্লিক করে সাথে সাথে সমস্ত ডোমেইন স্ট্যান্ডবাই নোডে বা সরাসরি মেইন সার্ভারে সুইচ করতে পারবেন।'}
                  </p>
                </div>
              </div>
            )}
          </div>
        </div>
      </ShadcnDialog>
    </div>
  );
}
