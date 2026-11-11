import React, { useState } from 'react';
import {
  Terminal,
  Globe,
  Layers,
  ShieldCheck,
  Search,
  Copy,
  Check,
  Lock,
  BookOpen,
  ArrowLeftRight,
} from 'lucide-react';
import { ShadcnDialog } from '../ui/Primitives';
import { copyToClipboard } from '../../lib/clipboard';
import { SHIELD_PREP_COMMAND, buildInstallCommand, getPanelOrigin } from './shieldCommands';

export interface CliCommandItem {
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

export const EDGE_CLI_COMMANDS: CliCommandItem[] = [
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

interface ShieldDocsModalProps {
  open: boolean;
  onClose: () => void;
  panelOrigin?: string;
}

export const ShieldDocsModal: React.FC<ShieldDocsModalProps> = ({
  open,
  onClose,
  panelOrigin = getPanelOrigin(),
}) => {
  const [docsTab, setDocsTab] = useState<'cli' | 'dns' | 'setup' | 'arch'>('cli');
  const [docsLang, setDocsLang] = useState<'en' | 'bn'>('en');
  const [cliSearch, setCliSearch] = useState('');
  const [cliCategory, setCliCategory] = useState<'all' | 'lifecycle' | 'panel' | 'diagnostics' | 'maintenance'>('all');
  const [copied, setCopied] = useState<string | null>(null);

  const handleCopy = async (text: string, id: string) => {
    const ok = await copyToClipboard(text);
    if (ok) {
      setCopied(id);
      setTimeout(() => setCopied((curr) => (curr === id ? null : curr)), 1800);
    }
  };

  const filteredCommands = EDGE_CLI_COMMANDS.filter((cmd) => {
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
  });

  return (
    <ShadcnDialog
      open={open}
      onClose={onClose}
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
            onClick={onClose}
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
                {filteredCommands.map((cmd) => (
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
  );
};
