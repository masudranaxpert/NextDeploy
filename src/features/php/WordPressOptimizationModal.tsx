import React, { useState } from 'react';
import { Check, CheckCircle2, Circle, FileCheck, Layers, RefreshCw, ShieldCheck, Trash2, Zap } from 'lucide-react';
import { ShadcnDialog } from '../../components/ui/Primitives';
import { WordPressIcon } from '../../components/ui/BrandIcons';

export interface WordPressCachePluginInfo {
  slug: string;
  name: string;
  installed: boolean;
  has_cache: boolean;
  cache_path: string;
}

interface WordPressOptimizationModalProps {
  open: boolean;
  onClose: () => void;
  siteName: string;
  plugins?: WordPressCachePluginInfo[];
  onRefresh?: () => void;
  onPurge?: () => Promise<void>;
}

type TabKey = 'cache' | 'object-cache' | 'assets';

const PLUGIN_DESCRIPTIONS: Record<string, { summary: string; sampleFile: string }> = {
  'wp-fastest-cache': {
    summary: 'Generates static HTML files and combines CSS/JS assets. Simple one-click setup.',
    sampleFile: '/wp-content/cache/all/.../index.html',
  },
  'wp-super-cache': {
    summary: 'Official Automattic static file generator designed for high-traffic WordPress sites.',
    sampleFile: '/wp-content/cache/supercache/{host}{uri}/index-https.html',
  },
  'cache-enabler': {
    summary: 'Lightweight disk cache engine with built-in precompressed Gzip (.gz) file output.',
    sampleFile: '/wp-content/cache/cache-enabler/{host}{uri}/https-index.html',
  },
  'wp-rocket': {
    summary: 'Premium caching and performance optimization suite for WordPress.',
    sampleFile: '/wp-content/cache/wp-rocket/{host}{uri}/index-https.html',
  },
};

export const WordPressOptimizationModal: React.FC<WordPressOptimizationModalProps> = ({
  open,
  onClose,
  siteName,
  plugins = [],
  onRefresh,
  onPurge,
}) => {
  const [activeTab, setActiveTab] = useState<TabKey>('cache');
  const [refreshing, setRefreshing] = useState(false);
  const [purging, setPurging] = useState(false);

  // Fallback list if props are not yet loaded
  const fallbackPlugins: WordPressCachePluginInfo[] = [
    { slug: 'wp-fastest-cache', name: 'WP Fastest Cache', installed: false, has_cache: false, cache_path: '/wp-content/cache/all' },
    { slug: 'wp-super-cache', name: 'WP Super Cache', installed: false, has_cache: false, cache_path: '/wp-content/cache/supercache' },
    { slug: 'cache-enabler', name: 'Cache Enabler', installed: false, has_cache: false, cache_path: '/wp-content/cache/cache-enabler' },
    { slug: 'wp-rocket', name: 'WP Rocket', installed: false, has_cache: false, cache_path: '/wp-content/cache/wp-rocket' },
  ];

  const safePlugins = Array.isArray(plugins) ? plugins : [];
  const displayPlugins = safePlugins.length > 0 ? safePlugins : fallbackPlugins;
  const activePlugin = displayPlugins.find((p) => p.installed && p.has_cache);
  const installedPlugin = displayPlugins.find((p) => p.installed);

  const handleRefresh = async () => {
    if (!onRefresh || refreshing) return;
    setRefreshing(true);
    try {
      await onRefresh();
    } finally {
      setTimeout(() => setRefreshing(false), 500);
    }
  };

  const handlePurge = async () => {
    if (!onPurge || purging) return;
    setPurging(true);
    try {
      await onPurge();
    } finally {
      setPurging(false);
    }
  };

  return (
    <ShadcnDialog
      open={open}
      onClose={onClose}
      title="WordPress Optimization & Performance"
      description={`Site: ${siteName} · Caddy zero-overhead disk caching & traffic guide`}
      icon={<WordPressIcon className="h-5 w-5 text-[#3858E9]" />}
      maxWidth="2xl"
      footer={
        <div className="flex w-full flex-col-reverse gap-2 sm:flex-row sm:items-center sm:justify-between">
          <div className="flex items-center gap-1.5 text-[11px] text-zinc-400">
            <ShieldCheck className="h-4 w-4 shrink-0 text-emerald-400" />
            <span>Dynamic sessions & cart items automatically bypass cache</span>
          </div>
          <button
            type="button"
            onClick={onClose}
            className="self-end sm:self-auto rounded-lg bg-zinc-800 px-3.5 py-1.5 text-xs font-medium text-zinc-200 transition hover:bg-zinc-700 cursor-pointer"
          >
            Close
          </button>
        </div>
      }
    >
      <div className="space-y-4 text-zinc-300">
        {/* Navigation Tabs Header */}
        <div className="flex flex-col gap-2.5 sm:flex-row sm:items-center sm:justify-between border-b border-white/[0.06] pb-3">
          <div className="flex items-center gap-1 rounded-lg border border-white/[0.07] bg-zinc-950/80 p-0.5 overflow-x-auto">
            <button
              type="button"
              onClick={() => setActiveTab('cache')}
              className={`flex items-center gap-1.5 shrink-0 rounded-md px-2.5 py-1 text-xs font-medium transition cursor-pointer ${
                activeTab === 'cache'
                  ? 'bg-zinc-800 text-white shadow-sm'
                  : 'text-zinc-400 hover:text-zinc-200'
              }`}
            >
              <Zap className="h-3.5 w-3.5 text-amber-400" />
              <span>Disk Cache</span>
              {activePlugin && (
                <span className="h-1.5 w-1.5 rounded-full bg-emerald-400 shadow-[0_0_6px_rgba(52,211,153,0.8)]" />
              )}
            </button>
            <button
              type="button"
              onClick={() => setActiveTab('object-cache')}
              className={`flex items-center gap-1.5 shrink-0 rounded-md px-2.5 py-1 text-xs font-medium transition cursor-pointer ${
                activeTab === 'object-cache'
                  ? 'bg-zinc-800 text-white shadow-sm'
                  : 'text-zinc-400 hover:text-zinc-200'
              }`}
            >
              <Layers className="h-3.5 w-3.5 text-cyan-400" />
              <span>Redis</span>
              <span className="rounded bg-white/[0.06] px-1 py-0.2 font-mono text-[9px] text-zinc-400">Soon</span>
            </button>
            <button
              type="button"
              onClick={() => setActiveTab('assets')}
              className={`flex items-center gap-1.5 shrink-0 rounded-md px-2.5 py-1 text-xs font-medium transition cursor-pointer ${
                activeTab === 'assets'
                  ? 'bg-zinc-800 text-white shadow-sm'
                  : 'text-zinc-400 hover:text-zinc-200'
              }`}
            >
              <FileCheck className="h-3.5 w-3.5 text-emerald-400" />
              <span>Assets</span>
              <span className="rounded bg-white/[0.06] px-1 py-0.2 font-mono text-[9px] text-zinc-400">Soon</span>
            </button>
          </div>

          <div className="flex items-center justify-end gap-1.5">
            {onPurge && (
              <button
                type="button"
                onClick={handlePurge}
                disabled={purging || refreshing}
                title="Purge all generated static cache files from disk"
                className="flex items-center gap-1.5 rounded-lg border border-red-500/20 bg-red-500/10 px-2.5 py-1 text-[11px] font-medium text-red-300 transition hover:bg-red-500/20 hover:text-red-200 disabled:opacity-50 cursor-pointer"
              >
                <Trash2 className="h-3 w-3" />
                <span>{purging ? 'Purging...' : 'Purge Cache'}</span>
              </button>
            )}
            {onRefresh && (
              <button
                type="button"
                onClick={handleRefresh}
                disabled={refreshing || purging}
                title="Rescan WordPress plugins on disk"
                className="flex items-center gap-1.5 rounded-lg border border-white/[0.08] bg-zinc-900/80 px-2.5 py-1 text-[11px] font-medium text-zinc-300 transition hover:bg-zinc-800 hover:text-white disabled:opacity-50 cursor-pointer"
              >
                <RefreshCw className={`h-3.5 w-3.5 text-zinc-400 ${refreshing ? 'animate-spin text-orange-400' : ''}`} />
                <span>Rescan</span>
              </button>
            )}
          </div>
        </div>

        {/* Tab 1: Static Disk Cache */}
        {activeTab === 'cache' && (
          <div className="space-y-3.5">
            {/* Live Detection Summary Banner */}
            {activePlugin ? (
              <div className="rounded-xl border border-emerald-500/25 bg-emerald-500/10 p-3.5">
                <div className="flex items-start gap-2.5">
                  <div className="flex h-7 w-7 shrink-0 items-center justify-center rounded-lg bg-emerald-500/20 text-emerald-400">
                    <CheckCircle2 className="h-4 w-4" />
                  </div>
                  <div className="text-xs leading-relaxed">
                    <p className="font-semibold text-emerald-200">
                      Caddy Static Disk Caching is Active
                    </p>
                    <p className="mt-0.5 text-emerald-300/80">
                      <span className="font-medium text-emerald-100">{activePlugin.name}</span> is installed and generating static HTML on disk. Caddy serves matching visitor requests directly in &lt; 0.5 ms without running PHP-FPM or MariaDB.
                    </p>
                  </div>
                </div>
              </div>
            ) : installedPlugin ? (
              <div className="rounded-xl border border-amber-500/25 bg-amber-500/10 p-3.5">
                <div className="flex items-start gap-2.5">
                  <div className="flex h-7 w-7 shrink-0 items-center justify-center rounded-lg bg-amber-500/20 text-amber-400">
                    <Zap className="h-4 w-4" />
                  </div>
                  <div className="text-xs leading-relaxed">
                    <p className="font-semibold text-amber-200">
                      {installedPlugin.name} Detected on Disk
                    </p>
                    <p className="mt-0.5 text-amber-300/80">
                      The plugin is installed, but no pre-rendered cache files exist in <code className="rounded bg-black/40 px-1 py-0.5 font-mono text-[10.5px] text-amber-100">{installedPlugin.cache_path}</code> yet. Enable caching in WP Admin settings to start instant static serving.
                    </p>
                  </div>
                </div>
              </div>
            ) : (
              <div className="rounded-xl border border-white/[0.08] bg-zinc-900/50 p-3.5">
                <div className="flex items-start gap-2.5">
                  <div className="flex h-7 w-7 shrink-0 items-center justify-center rounded-lg bg-amber-500/10 text-amber-400">
                    <Zap className="h-4 w-4" />
                  </div>
                  <div className="text-xs leading-relaxed">
                    <p className="font-medium text-zinc-100">
                      How Caddy Zero-Overhead Disk Caching Works
                    </p>
                    <p className="mt-0.5 text-zinc-400">
                      Install any one of the 4 supported caching plugins below in WP Admin. When the plugin writes static HTML files to disk, Caddy automatically serves them via its internal file server in 0.5 ms, bypassing PHP-FPM and MariaDB entirely.
                    </p>
                  </div>
                </div>
              </div>
            )}

            {/* Supported Plugins Grid with Live Status */}
            <div className="space-y-2">
              <div className="flex flex-wrap items-center justify-between gap-1 text-xs">
                <span className="font-medium text-zinc-300">Supported Plugins & Detection Status:</span>
                <span className="text-[11px] text-zinc-500">Choose any 1 plugin</span>
              </div>

              <div className="grid gap-2.5 sm:grid-cols-2">
                {displayPlugins.map((p) => {
                  const meta = PLUGIN_DESCRIPTIONS[p.slug] || { summary: '', sampleFile: p.cache_path };
                  const isServing = p.installed && p.has_cache;
                  const isInstalledOnly = p.installed && !p.has_cache;

                  return (
                    <div
                      key={p.slug}
                      className={`flex flex-col justify-between rounded-xl border p-3.5 transition ${
                        isServing
                          ? 'border-emerald-500/40 bg-emerald-950/20 shadow-sm'
                          : isInstalledOnly
                          ? 'border-amber-500/30 bg-amber-950/15'
                          : 'border-white/[0.06] bg-zinc-950/60 hover:border-white/[0.12]'
                      }`}
                    >
                      <div>
                        <div className="flex flex-wrap items-center justify-between gap-1.5">
                          <div className="flex items-center gap-1.5 font-semibold text-xs text-zinc-100 min-w-0">
                            {isServing ? (
                              <CheckCircle2 className="h-4 w-4 text-emerald-400 shrink-0" />
                            ) : isInstalledOnly ? (
                              <Check className="h-4 w-4 text-amber-400 shrink-0" />
                            ) : (
                              <Circle className="h-3.5 w-3.5 text-zinc-600 shrink-0" />
                            )}
                            <span className="truncate">{p.name}</span>
                          </div>

                          {/* Plugin Status Badge */}
                          {isServing ? (
                            <span className="inline-flex items-center gap-1 rounded-full border border-emerald-500/30 bg-emerald-500/15 px-2 py-0.5 font-medium text-[10px] text-emerald-300">
                              <span className="h-1.5 w-1.5 rounded-full bg-emerald-400 shadow-[0_0_5px_rgba(52,211,153,0.8)]" />
                              Active (Cached)
                            </span>
                          ) : isInstalledOnly ? (
                            <span className="inline-flex items-center gap-1 rounded-full border border-amber-500/30 bg-amber-500/15 px-2 py-0.5 font-medium text-[10px] text-amber-300">
                              Installed
                            </span>
                          ) : (
                            <span className="rounded-full border border-white/[0.06] bg-white/[0.03] px-2 py-0.5 text-[10px] text-zinc-500">
                              Not Installed
                            </span>
                          )}
                        </div>

                        <p className="mt-2 text-[11.5px] leading-relaxed text-zinc-400">
                          {meta.summary}
                        </p>
                      </div>

                      <div className="mt-3 flex items-center justify-between rounded-md bg-zinc-900/90 px-2 py-1 font-mono text-[10px] text-zinc-500">
                        <span className="truncate">{p.cache_path}</span>
                        {p.has_cache && (
                          <span className="ml-1 shrink-0 font-sans text-[10px] font-medium text-emerald-400">
                            Files ready
                          </span>
                        )}
                      </div>
                    </div>
                  );
                })}
              </div>
            </div>

            {/* Automatic Safety Guardrails */}
            <div className="rounded-xl border border-white/[0.08] bg-zinc-900/40 p-3.5">
              <div className="flex items-center gap-2 text-xs font-semibold text-zinc-200">
                <ShieldCheck className="h-4 w-4 text-emerald-400" />
                <span>Automatic Bypass Guardrails</span>
              </div>
              <ul className="mt-2.5 space-y-1.5 text-[11.5px] text-zinc-400">
                <li className="flex items-start gap-2">
                  <span className="text-emerald-400">✓</span>
                  <span>
                    Logged-in users and admins (<code className="text-zinc-300">wordpress_logged_in</code>) bypass cache to live PHP-FPM.
                  </span>
                </li>
                <li className="flex items-start gap-2">
                  <span className="text-emerald-400">✓</span>
                  <span>
                    WooCommerce cart sessions (<code className="text-zinc-300">woocommerce_items_in_cart</code>) always route live.
                  </span>
                </li>
                <li className="flex items-start gap-2">
                  <span className="text-emerald-400">✓</span>
                  <span>
                    POST requests, search queries (<code className="text-zinc-300">?s=...</code>), and admin endpoints (<code className="text-zinc-300">/wp-admin/</code>) are never cached.
                  </span>
                </li>
                <li className="flex items-start gap-2">
                  <span className="text-emerald-400">✓</span>
                  <span>
                    If no cache plugin is active, Caddy falls back to standard PHP-FPM routing without interruption.
                  </span>
                </li>
              </ul>
            </div>
          </div>
        )}

        {/* Tab 2: Object Cache */}
        {activeTab === 'object-cache' && (
          <div className="rounded-xl border border-white/[0.08] bg-zinc-900/50 p-6 text-center">
            <Layers className="mx-auto h-8 w-8 text-cyan-400/80" />
            <h4 className="mt-3 text-sm font-semibold text-zinc-200">
              Redis Object Cache Module
            </h4>
            <p className="mx-auto mt-1.5 max-w-md text-xs leading-relaxed text-zinc-400">
              Shared Redis container and persistent in-memory object cache wiring for WordPress database queries will be available in the upcoming release.
            </p>
          </div>
        )}

        {/* Tab 3: Assets */}
        {activeTab === 'assets' && (
          <div className="rounded-xl border border-white/[0.08] bg-zinc-900/50 p-6 text-center">
            <FileCheck className="mx-auto h-8 w-8 text-emerald-400/80" />
            <h4 className="mt-3 text-sm font-semibold text-zinc-200">
              Static Asset Minification & Precompression
            </h4>
            <p className="mx-auto mt-1.5 max-w-md text-xs leading-relaxed text-zinc-400">
              Automated CSS/JS asset pipeline rules and Gzip / Zstandard precompression directives will be documented here in the next update.
            </p>
          </div>
        )}
      </div>
    </ShadcnDialog>
  );
};
