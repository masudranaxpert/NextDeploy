import React, { useEffect, useRef, useState } from 'react';
import {
  Search,
  Terminal,
  Plus,
  Layers,
  Globe,
  FolderGit2,
  X,
  Activity,
  Box,
  HardDrive,
  Archive,
  Cable,
  Settings,
  RefreshCw,
  Trash2,
  Database,
  CornerDownLeft,
} from 'lucide-react';
import {
  AppStack,
  NavigationSection,
  DockerImageItem,
  DockerVolumeItem,
  GitProviderConnection,
} from '../../types/nextdeploy';
import { backendApi } from '../../lib/backendApi';
import { toast } from 'sonner';

export interface CommandPaletteProps {
  open: boolean;
  onClose: () => void;
  apps: AppStack[];
  images?: DockerImageItem[];
  volumes?: DockerVolumeItem[];
  gitProviders?: GitProviderConnection[];
  /** Non-admins do not get host-level actions and pages in the palette. */
  isAdmin?: boolean;
  onSelectNav: (nav: NavigationSection, tab?: string) => void;
  onSelectApp: (appId: string, tab?: string) => void;
  onOpenNewApp: () => void;
}

interface PaletteItem {
  id: string;
  category: 'Actions' | 'Projects' | 'Containers' | 'Storage' | 'Navigation';
  title: string;
  subtitle?: string;
  keywords?: string[];
  icon: React.ReactNode;
  badge?: {
    text: string;
    variant?: 'emerald' | 'orange' | 'zinc' | 'blue' | 'violet';
  };
  onSelect: () => void;
}

export const CommandPaletteModal: React.FC<CommandPaletteProps> = ({
  open,
  onClose,
  apps,
  images = [],
  volumes = [],
  gitProviders = [],
  isAdmin = true,
  onSelectNav,
  onSelectApp,
  onOpenNewApp,
}) => {
  const [query, setQuery] = useState('');
  const [selectedCategory, setSelectedCategory] = useState<string>('all');
  const [selectedIndex, setSelectedIndex] = useState(0);
  const inputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    if (open) {
      setQuery('');
      setSelectedCategory('all');
      setSelectedIndex(0);
      setTimeout(() => inputRef.current?.focus(), 50);
    }
  }, [open]);

  // Build searchable catalog
  const allItems: PaletteItem[] = React.useMemo(() => {
    const list: PaletteItem[] = [];

    // 1. Quick Actions & Commands
    list.push(
      {
        id: 'act-deploy',
        category: 'Actions',
        title: 'Deploy New Application',
        subtitle: 'Create a new service from Git, Dockerfile, or Compose',
        keywords: ['deploy', 'create', 'new', 'add', 'service', 'project', 'app'],
        icon: <Plus className="h-4 w-4 text-orange-400" />,
        badge: { text: 'Create', variant: 'orange' },
        onSelect: () => {
          onClose();
          onOpenNewApp();
        },
      },
      {
        id: 'act-terminal',
        category: 'Actions',
        title: 'Open VPS Host Terminal',
        subtitle: 'Launch full interactive Web PTY shell on the server',
        keywords: ['terminal', 'shell', 'bash', 'ssh', 'console', 'vps'],
        icon: <Terminal className="h-4 w-4 text-emerald-400" />,
        badge: { text: 'PTY', variant: 'emerald' },
        onSelect: () => {
          onClose();
          onSelectNav('terminal');
        },
      },
      {
        id: 'act-prune-images',
        category: 'Actions',
        title: 'Prune Unused Docker Images',
        subtitle: 'Safely reclaim disk space by removing untagged & dangling images',
        keywords: ['prune', 'clean', 'images', 'disk', 'docker', 'unused', 'free'],
        icon: <Trash2 className="h-4 w-4 text-rose-400" />,
        badge: { text: 'Disk', variant: 'zinc' },
        onSelect: async () => {
          onClose();
          try {
            const res = await backendApi.globalImagesPrune();
            if (res.ok) {
              toast.success('Successfully pruned unused Docker images');
            }
          } catch {
            // ignore
          }
        },
      },
      {
        id: 'act-prune-containers',
        category: 'Actions',
        title: 'Prune Stopped Containers',
        subtitle: 'Remove exited and dead containers from Docker runtime',
        keywords: ['prune', 'clean', 'containers', 'stopped', 'exited'],
        icon: <Trash2 className="h-4 w-4 text-rose-400" />,
        badge: { text: 'Docker', variant: 'zinc' },
        onSelect: async () => {
          onClose();
          try {
            const res = await backendApi.globalContainersPrune();
            if (res.ok) {
              toast.success('Successfully pruned stopped containers');
            }
          } catch {
            // ignore
          }
        },
      },
      {
        id: 'act-restart-caddy',
        category: 'Actions',
        title: 'Restart Caddy Proxy & TLS',
        subtitle: 'Gracefully restart Caddy reverse proxy to reload routes & certificates',
        keywords: ['caddy', 'proxy', 'restart', 'reload', 'ssl', 'tls', 'cert'],
        icon: <RefreshCw className="h-4 w-4 text-blue-400" />,
        badge: { text: 'Caddy', variant: 'blue' },
        onSelect: async () => {
          onClose();
          try {
            await backendApi.caddyContainerAction('restart');
            toast.success('Restarted Caddy reverse proxy');
          } catch {
            // ignore
          }
        },
      },
      {
        id: 'act-backup',
        category: 'Actions',
        title: 'Create System Backup Snapshot',
        subtitle: 'Export SQLite databases, environments, and Caddy rules',
        keywords: ['backup', 'export', 'snapshot', 'disaster', 'restore'],
        icon: <Archive className="h-4 w-4 text-amber-400" />,
        badge: { text: 'Backup', variant: 'zinc' },
        onSelect: () => {
          onClose();
          onSelectNav('backup');
        },
      },
      {
        id: 'act-migrate',
        category: 'Actions',
        title: 'Server Migration & Restore',
        subtitle: 'Full server migration between VPS instances',
        keywords: ['migrate', 'migration', 'transfer', 'clone', 'move'],
        icon: <Database className="h-4 w-4 text-violet-400" />,
        badge: { text: 'Migrate', variant: 'violet' },
        onSelect: () => {
          onClose();
          onSelectNav('migrate');
        },
      }
    );

    // 2. Projects (Apps) + Direct Subtabs
    apps.forEach((app) => {
      list.push({
        id: `app-${app.id}`,
        category: 'Projects',
        title: app.name,
        subtitle: app.primaryDomain || app.slug || 'Standalone Service',
        keywords: [
          app.name,
          app.slug,
          app.primaryDomain || '',
          app.status,
          app.sourceType || '',
          app.gitRepo || '',
          app.gitBranch || '',
          'project',
          'application',
          'service',
        ],
        icon: <Layers className="h-4 w-4 text-orange-400" />,
        badge: {
          text: app.status,
          variant: app.status === 'running' ? 'emerald' : 'zinc',
        },
        onSelect: () => {
          onClose();
          onSelectApp(app.id);
        },
      });

      // Include deep subtab jumps when searching
      const subtabs = [
        { tab: 'logs', label: 'Logs', kw: ['logs', 'tail', 'output'] },
        { tab: 'env', label: 'Environment', kw: ['env', 'secret', 'variable'] },
        { tab: 'files', label: 'Files', kw: ['files', 'code', 'upload'] },
        { tab: 'deploy', label: 'Deployments', kw: ['deploy', 'rollback', 'history'] },
        { tab: 'git', label: 'Git Settings', kw: ['git', 'repo', 'branch'] },
        { tab: 'domains', label: 'Domains & SSL', kw: ['domain', 'ssl', 'caddy'] },
        { tab: 'terminal', label: 'Container Shell', kw: ['terminal', 'exec', 'sh'] },
      ];

      subtabs.forEach((st) => {
        list.push({
          id: `app-${app.id}-${st.tab}`,
          category: 'Projects',
          title: `${app.name} · ${st.label}`,
          subtitle: `Jump directly to ${st.label.toLowerCase()} in ${app.name}`,
          keywords: [app.name, app.slug, st.tab, ...st.kw],
          icon: <Layers className="h-4 w-4 text-zinc-400" />,
          badge: { text: st.label, variant: 'zinc' },
          onSelect: () => {
            onClose();
            onSelectApp(app.id, st.tab);
          },
        });
      });
    });

    // 3. Docker Containers
    apps.forEach((app) => {
      (app.services || []).forEach((svc) => {
        list.push({
          id: `cont-${svc.id || svc.name}`,
          category: 'Containers',
          title: svc.name,
          subtitle: `${svc.image || 'Image'} · ${app.name} (${svc.state || 'running'})`,
          keywords: [
            svc.name,
            svc.image || '',
            app.name,
            svc.state || '',
            svc.ports ? String(svc.ports) : '',
            'container',
            'docker',
          ],
          icon: <Box className="h-4 w-4 text-blue-400" />,
          badge: {
            text: svc.state || 'running',
            variant: svc.state === 'running' ? 'emerald' : 'zinc',
          },
          onSelect: () => {
            onClose();
            onSelectNav('containers');
          },
        });
      });
    });

    // 4. Docker Images & Volumes
    images.forEach((img) => {
      const displayTag = img.repository ? `${img.repository}:${img.tag}` : img.id.slice(0, 16);
      list.push({
        id: `img-${img.id}`,
        category: 'Storage',
        title: displayTag,
        subtitle: `Docker Image · Size: ${img.sizeMB ? `${img.sizeMB}MB` : '—'} · ID: ${img.id.slice(0, 12)}`,
        keywords: [img.repository || '', img.tag || '', img.id, 'image', 'docker'],
        icon: <Layers className="h-4 w-4 text-violet-400" />,
        badge: { text: 'Image', variant: 'violet' },
        onSelect: () => {
          onClose();
          onSelectNav('images');
        },
      });
    });

    volumes.forEach((vol) => {
      list.push({
        id: `vol-${vol.name}`,
        category: 'Storage',
        title: vol.name,
        subtitle: `Persistent Volume · Driver: ${vol.driver || 'local'}`,
        keywords: [vol.name, vol.driver || '', 'volume', 'storage', 'data'],
        icon: <HardDrive className="h-4 w-4 text-emerald-400" />,
        badge: { text: 'Volume', variant: 'emerald' },
        onSelect: () => {
          onClose();
          onSelectNav('volumes');
        },
      });
    });

    // 5. Git Providers
    gitProviders.forEach((gp) => {
      list.push({
        id: `gp-${gp.id}`,
        category: 'Storage',
        title: `${gp.provider} · ${gp.accountName}`,
        subtitle: gp.authMethod || 'Git Integration',
        keywords: [gp.provider, gp.accountName || '', 'git', 'provider'],
        icon: <FolderGit2 className="h-4 w-4 text-orange-400" />,
        badge: { text: gp.provider, variant: 'orange' },
        onSelect: () => {
          onClose();
          onSelectNav('git');
        },
      });
    });

    // 6. Navigation (Control Plane Pages)
    const pages: Array<{ id: NavigationSection; label: string; desc: string; icon: React.ReactNode; kw: string[] }> = [
      { id: 'overview', label: 'Overview Dashboard', desc: 'Realtime telemetry, CPU/RAM charts, and host status', icon: <Activity className="h-4 w-4 text-orange-400" />, kw: ['metrics', 'cpu', 'memory', 'dashboard'] },
      { id: 'apps', label: 'Projects & Applications', desc: 'Manage Docker Compose services and git apps', icon: <Layers className="h-4 w-4 text-zinc-300" />, kw: ['projects', 'stacks', 'compose'] },
      { id: 'containers', label: 'Docker Containers', desc: 'Inspect live runtime containers, logs, and restart states', icon: <Box className="h-4 w-4 text-blue-400" />, kw: ['docker', 'runtime', 'inspect'] },
      { id: 'images', label: 'Docker Images', desc: 'Manage downloaded container images, tags, and prune space', icon: <Layers className="h-4 w-4 text-violet-400" />, kw: ['images', 'tags', 'prune'] },
      { id: 'volumes', label: 'Docker Volumes', desc: 'Inspect local storage volumes and mount paths', icon: <HardDrive className="h-4 w-4 text-emerald-400" />, kw: ['storage', 'mounts', 'persist'] },
      { id: 'terminal', label: 'VPS Host Terminal', desc: 'Full interactive root PTY terminal with direct shell access', icon: <Terminal className="h-4 w-4 text-emerald-400" />, kw: ['shell', 'bash', 'pty', 'exec'] },
      { id: 'git', label: 'Git Providers & Webhooks', desc: 'Connect GitHub and GitLab for push-to-deploy automations', icon: <FolderGit2 className="h-4 w-4 text-orange-400" />, kw: ['github', 'gitlab', 'tokens', 'webhooks'] },
      { id: 'registries', label: 'Container Registries', desc: 'Configure Docker Hub, GHCR, and private registry credentials', icon: <Database className="h-4 w-4 text-blue-400" />, kw: ['ghcr', 'dockerhub', 'auth', 'pull'] },
      { id: 'proxy', label: 'Caddy Proxy & TLS', desc: 'Manage automated HTTPS/SSL reverse proxy and custom domains', icon: <Globe className="h-4 w-4 text-emerald-400" />, kw: ['caddy', 'proxy', 'ssl', 'tls', 'https'] },
      { id: 'proxy-options', label: 'Caddy Server Options', desc: 'Timeouts, header limits and the real visitor IP behind a CDN', icon: <Globe className="h-4 w-4 text-emerald-400" />, kw: ['caddy', 'timeouts', 'cloudflare', 'trusted', 'real ip'] },
      { id: 'proxy-routes', label: 'Routes & Redirects', desc: 'Reverse proxy a domain to another address, or redirect it', icon: <Globe className="h-4 w-4 text-emerald-400" />, kw: ['reverse proxy', 'redirect', 'domain', 'upstream'] },
      { id: 'backup', label: 'Backups & Snapshots', desc: 'Automated backups and disaster recovery', icon: <Archive className="h-4 w-4 text-amber-400" />, kw: ['backups', 'snapshots', 's3', 'restore'] },
      { id: 'migrate', label: 'Server Migration', desc: 'Full server clone and export to another NextDeploy instance', icon: <Database className="h-4 w-4 text-violet-400" />, kw: ['migrate', 'transfer', 'clone'] },
      { id: 'mcp', label: 'MCP Server', desc: 'Model Context Protocol endpoints for Cursor, Claude & Windsurf', icon: <Cable className="h-4 w-4 text-purple-400" />, kw: ['ai', 'mcp', 'agent', 'cursor', 'claude', 'tools'] },
      { id: 'settings', label: 'System Settings', desc: 'Admin users, security credentials, and panel configuration', icon: <Settings className="h-4 w-4 text-zinc-400" />, kw: ['settings', 'users', 'config', 'admin', 'password'] },
    ];

    pages.forEach((p) => {
      list.push({
        id: `nav-${p.id}`,
        category: 'Navigation',
        title: p.label,
        subtitle: p.desc,
        keywords: [p.id, p.label, ...p.kw, 'page', 'navigate', 'goto'],
        icon: p.icon,
        badge: { text: 'Page', variant: 'zinc' },
        onSelect: () => {
          onClose();
          onSelectNav(p.id);
        },
      });
    });

    if (isAdmin) return list;
    const adminOnly = new Set([
      'act-terminal',
      'act-prune-images',
      'act-prune-containers',
      'act-restart-caddy',
      'act-migrate',
      'nav-terminal',
      'nav-proxy',
      'nav-migrate',
      'nav-settings',
    ]);
    return list.filter((it) => !adminOnly.has(it.id));
  }, [apps, images, volumes, gitProviders, isAdmin, onClose, onOpenNewApp, onSelectApp, onSelectNav]);

  // Multi-term fuzzy filter
  const filteredItems = React.useMemo(() => {
    const terms = query.trim().toLowerCase().split(/\s+/).filter(Boolean);

    let base = allItems;
    if (selectedCategory !== 'all') {
      base = base.filter((item) => item.category.toLowerCase() === selectedCategory.toLowerCase());
    }

    if (terms.length === 0) {
      // Prioritize: Actions, then Projects, then Navigation
      return base.filter((item) => {
        // Exclude deep subtabs in blank query to keep it clean
        if (item.id.includes('-logs') || item.id.includes('-env') || item.id.includes('-files')) {
          return false;
        }
        return true;
      });
    }

    return base.filter((item) => {
      const haystack = `${item.title} ${item.subtitle || ''} ${(item.keywords || []).join(' ')} ${item.category}`.toLowerCase();
      return terms.every((term) => haystack.includes(term));
    });
  }, [allItems, query, selectedCategory]);

  // Keyboard navigation
  useEffect(() => {
    setSelectedIndex(0);
  }, [query, selectedCategory]);

  useEffect(() => {
    if (!open) return;
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'ArrowDown') {
        e.preventDefault();
        setSelectedIndex((prev) => (prev + 1 < filteredItems.length ? prev + 1 : 0));
      } else if (e.key === 'ArrowUp') {
        e.preventDefault();
        setSelectedIndex((prev) => (prev - 1 >= 0 ? prev - 1 : Math.max(0, filteredItems.length - 1)));
      } else if (e.key === 'Enter') {
        e.preventDefault();
        if (filteredItems[selectedIndex]) {
          filteredItems[selectedIndex].onSelect();
        }
      } else if (e.key === 'Escape') {
        e.preventDefault();
        onClose();
      }
    };
    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [open, filteredItems, selectedIndex, onClose]);

  // Keep active item in view
  useEffect(() => {
    const el = document.getElementById(`cmdk-item-${selectedIndex}`);
    if (el) {
      el.scrollIntoView({ block: 'nearest' });
    }
  }, [selectedIndex]);

  if (!open) return null;

  const categories = [
    { id: 'all', label: 'All' },
    { id: 'projects', label: 'Projects' },
    { id: 'containers', label: 'Containers' },
    { id: 'storage', label: 'Storage' },
    { id: 'actions', label: 'Actions' },
    { id: 'navigation', label: 'Pages' },
  ];

  return (
    <div
      className="fixed inset-0 z-[999] flex items-start justify-center bg-black/80 pt-16 sm:pt-20 p-3 sm:p-4 backdrop-blur-md animate-in fade-in duration-150"
      onClick={onClose}
    >
      <div
        className="w-full max-w-2xl overflow-hidden rounded-2xl border border-white/[0.12] bg-[#0c0c0f] shadow-[0_24px_80px_rgba(0,0,0,0.9)] flex flex-col max-h-[82vh]"
        onClick={(e) => e.stopPropagation()}
      >
        {/* Search Input Bar */}
        <div className="flex items-center gap-3 border-b border-white/[0.08] px-4 py-3.5 shrink-0 bg-[#111115]/60">
          <Search className="h-4 w-4 text-orange-400 shrink-0" />
          <input
            ref={inputRef}
            type="text"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Search projects, containers, images, ports, commands..."
            className="w-full bg-transparent text-xs sm:text-sm text-zinc-100 placeholder-zinc-500 focus:outline-none font-sans"
          />
          {query && (
            <button
              type="button"
              onClick={() => {
                setQuery('');
                inputRef.current?.focus();
              }}
              className="text-zinc-500 hover:text-zinc-300 p-1 cursor-pointer"
            >
              <X className="h-3.5 w-3.5" />
            </button>
          )}
          <button
            type="button"
            onClick={onClose}
            aria-label="Close command palette"
            className="rounded-md border border-white/[0.08] bg-zinc-900 px-1.5 py-0.5 font-mono text-[10px] text-zinc-400 hover:text-white cursor-pointer shrink-0"
          >
            ESC
          </button>
        </div>

        {/* Category Pills */}
        <div className="flex items-center gap-1.5 border-b border-white/[0.06] px-4 py-2 bg-[#09090b] overflow-x-auto no-scrollbar shrink-0">
          {categories.map((c) => (
            <button
              key={c.id}
              type="button"
              onClick={() => setSelectedCategory(c.id)}
              className={`rounded-lg px-2.5 py-1 text-[11px] font-medium transition-all cursor-pointer whitespace-nowrap ${
                selectedCategory === c.id
                  ? 'border border-orange-500/40 bg-orange-500/10 text-orange-300 shadow-sm'
                  : 'border border-transparent bg-white/[0.03] text-zinc-400 hover:bg-white/[0.06] hover:text-zinc-200'
              }`}
            >
              {c.label}
            </button>
          ))}
        </div>

        {/* Results List */}
        <div className="flex-1 min-h-0 overflow-y-auto sidebar-scrollbar p-2 space-y-1">
          {filteredItems.length === 0 ? (
            <div className="py-14 text-center">
              <div className="mx-auto mb-3 flex h-11 w-11 items-center justify-center rounded-2xl border border-white/[0.08] bg-white/[0.02] text-zinc-500">
                <Search className="h-5 w-5" />
              </div>
              <p className="text-xs font-medium text-zinc-300">
                No results found for <span className="text-orange-400 font-mono">"{query}"</span>
              </p>
              <p className="mt-1 text-[11px] text-zinc-500 max-w-sm mx-auto">
                Try searching by service name, container ID, custom domain, port, or command like "deploy", "terminal", or "prune".
              </p>
            </div>
          ) : (
            filteredItems.map((item, idx) => {
              const isSelected = idx === selectedIndex;
              return (
                <div
                  key={item.id}
                  id={`cmdk-item-${idx}`}
                  onClick={item.onSelect}
                  onMouseEnter={() => setSelectedIndex(idx)}
                  className={`relative flex items-center justify-between gap-3 rounded-xl px-3 py-2 text-xs transition-all cursor-pointer select-none ${
                    isSelected
                      ? 'border border-orange-500/30 bg-white/[0.07] text-white shadow-sm'
                      : 'border border-transparent text-zinc-300 hover:bg-white/[0.03]'
                  }`}
                >
                  {isSelected && (
                    <span className="absolute left-0 top-2 bottom-2 w-1 rounded-r bg-orange-500" />
                  )}

                  <div className="flex items-center gap-3 min-w-0">
                    <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg border border-white/[0.08] bg-black/50">
                      {item.icon}
                    </div>
                    <div className="min-w-0">
                      <div className="flex items-center gap-2">
                        <span className="font-medium text-zinc-100 truncate">{item.title}</span>
                      </div>
                      {item.subtitle && (
                        <p className="text-[11px] font-mono text-zinc-500 truncate mt-0.5">
                          {item.subtitle}
                        </p>
                      )}
                    </div>
                  </div>

                  <div className="flex items-center gap-2 shrink-0">
                    {item.badge && (
                      <span
                        className={`rounded-md px-2 py-0.5 font-mono text-[10px] font-medium border ${
                          item.badge.variant === 'emerald'
                            ? 'border-emerald-500/25 bg-emerald-500/10 text-emerald-400'
                            : item.badge.variant === 'orange'
                              ? 'border-orange-500/25 bg-orange-500/10 text-orange-400'
                              : item.badge.variant === 'blue'
                                ? 'border-blue-500/25 bg-blue-500/10 text-blue-400'
                                : item.badge.variant === 'violet'
                                  ? 'border-violet-500/25 bg-violet-500/10 text-violet-400'
                                  : 'border-white/[0.08] bg-white/[0.04] text-zinc-400'
                        }`}
                      >
                        {item.badge.text}
                      </span>
                    )}
                    <span
                      className={`rounded px-1.5 py-0.5 text-[10px] font-mono text-zinc-500 transition-opacity ${
                        isSelected ? 'opacity-100 text-zinc-300' : 'opacity-0'
                      }`}
                    >
                      <CornerDownLeft className="h-3 w-3" />
                    </span>
                  </div>
                </div>
              );
            })
          )}
        </div>

        {/* Footer Bar */}
        <div className="flex items-center justify-between border-t border-white/[0.07] bg-[#09090b] px-4 py-2.5 text-[11px] text-zinc-500 shrink-0">
          <div className="flex items-center gap-3">
            <span className="flex items-center gap-1">
              <kbd className="rounded border border-white/[0.08] bg-zinc-900 px-1 py-0.5 font-mono text-[9px] text-zinc-400">
                ↑
              </kbd>
              <kbd className="rounded border border-white/[0.08] bg-zinc-900 px-1 py-0.5 font-mono text-[9px] text-zinc-400">
                ↓
              </kbd>
              <span>to navigate</span>
            </span>
            <span className="flex items-center gap-1">
              <kbd className="rounded border border-white/[0.08] bg-zinc-900 px-1.5 py-0.5 font-mono text-[9px] text-zinc-400">
                ↵
              </kbd>
              <span>to select</span>
            </span>
            <span className="flex items-center gap-1">
              <kbd className="rounded border border-white/[0.08] bg-zinc-900 px-1.5 py-0.5 font-mono text-[9px] text-zinc-400">
                esc
              </kbd>
              <span>to close</span>
            </span>
          </div>
          <div className="font-mono text-[10px] text-zinc-400">
            {filteredItems.length} {filteredItems.length === 1 ? 'item' : 'items'}
          </div>
        </div>
      </div>
    </div>
  );
};
