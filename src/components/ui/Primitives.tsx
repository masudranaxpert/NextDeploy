import React, { useEffect, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import {
  Area,
  AreaChart,
  CartesianGrid,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts';
import {
  Search,
  Terminal,
  Plus,
  Layers,
  Globe,
  FolderGit2,
  X,
  ArrowRight,
  Activity,
  Box,
  HardDrive,
  Archive,
  Sparkles,
  Settings,
  MoreHorizontal,
  GitBranch,
  FileCode2,
  Upload,
} from 'lucide-react';
import { AppStack, NavigationSection, TelemetryPoint } from '../../types/nextdeploy';

// ----------------------------------------------------------------------------
// 1. shadcn/ui Dialog (Modal) Primitive — Dokploy / Linear 2026 Glass & Obsidian
// ----------------------------------------------------------------------------
interface ShadcnDialogProps {
  open: boolean;
  onClose: () => void;
  title: string;
  description?: string;
  icon?: React.ReactNode;
  maxWidth?: 'sm' | 'md' | 'lg' | 'xl' | '2xl';
  children: React.ReactNode;
  footer?: React.ReactNode;
}

export const ShadcnDialog: React.FC<ShadcnDialogProps> = ({
  open,
  onClose,
  title,
  description,
  icon,
  maxWidth = 'md',
  children,
  footer,
}) => {
  useEffect(() => {
    if (!open) return;
    const handleEsc = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose();
    };
    window.addEventListener('keydown', handleEsc);
    return () => window.removeEventListener('keydown', handleEsc);
  }, [open, onClose]);

  if (!open) return null;

  const widthClass = {
    sm: 'max-w-sm',
    md: 'max-w-md',
    lg: 'max-w-lg',
    xl: 'max-w-xl',
    '2xl': 'max-w-2xl',
  }[maxWidth];

  return createPortal(
    <div
      className="fixed inset-0 z-[999] flex items-center justify-center bg-black/75 p-4 backdrop-blur-md animate-in fade-in duration-150"
      onClick={onClose}
    >
      <div
        onClick={(e) => e.stopPropagation()}
        className={`relative w-full ${widthClass} overflow-hidden rounded-2xl border border-white/[0.09] bg-[#0e0e11] text-zinc-100 shadow-[0_24px_80px_rgba(0,0,0,0.85),inset_0_1px_0_0_rgba(255,255,255,0.06)]`}
      >
        {/* Header */}
        <div className="flex items-start justify-between gap-4 border-b border-white/[0.07] px-5 py-4">
          <div className="flex items-center gap-3 min-w-0">
            {icon && (
              <div className="flex h-9 w-9 shrink-0 items-center justify-center rounded-xl border border-white/[0.08] bg-zinc-900/90 text-orange-400 shadow-inner">
                {icon}
              </div>
            )}
            <div className="min-w-0">
              <h2 className="text-sm font-semibold tracking-tight text-zinc-100 truncate">
                {title}
              </h2>
              {description && (
                <p className="mt-0.5 text-xs text-zinc-400 truncate">{description}</p>
              )}
            </div>
          </div>
          <button
            type="button"
            onClick={onClose}
            className="rounded-lg p-1.5 text-zinc-400 hover:bg-white/[0.06] hover:text-zinc-100 transition-colors cursor-pointer"
          >
            <X className="h-4 w-4" />
          </button>
        </div>

        {/* Body */}
        <div className="max-h-[75vh] overflow-y-auto px-5 py-4 text-xs">{children}</div>

        {/* Footer */}
        {footer && (
          <div className="flex items-center justify-end gap-2 border-t border-white/[0.07] bg-[#09090b]/60 px-5 py-3">
            {footer}
          </div>
        )}
      </div>
    </div>,
    document.body
  );
};

// ----------------------------------------------------------------------------
// 2. shadcn/ui Switch Primitive
// ----------------------------------------------------------------------------
interface ShadcnSwitchProps {
  checked: boolean;
  onChange: (next: boolean) => void;
  label?: string;
}

export const ShadcnSwitch: React.FC<ShadcnSwitchProps> = ({ checked, onChange, label }) => {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      onClick={() => onChange(!checked)}
      className="inline-flex items-center gap-2 cursor-pointer select-none group"
    >
      <span
        className={`relative inline-flex h-5 w-9 shrink-0 items-center rounded-full border transition-colors ${
          checked
            ? 'border-orange-500/50 bg-orange-500'
            : 'border-white/[0.1] bg-zinc-900 group-hover:border-white/[0.2]'
        }`}
      >
        <span
          className={`inline-block h-3.5 w-3.5 transform rounded-full bg-white shadow-xs transition-transform ${
            checked ? 'translate-x-4' : 'translate-x-0.5'
          }`}
        />
      </span>
      {label && <span className="text-xs font-medium text-zinc-300">{label}</span>}
    </button>
  );
};

// ----------------------------------------------------------------------------
// 3. Semantic Status Badge — Ultra-Crisp Dokploy / Linear Pill
// ----------------------------------------------------------------------------
interface StatusBadgeProps {
  status:
    | 'running'
    | 'building'
    | 'stopped'
    | 'failed'
    | 'exited'
    | 'restarting'
    | 'ready'
    | 'active'
    | 'provisioning'
    | 'error'
    | 'connected'
    | 'completed'
    | 'in-use'
    | 'unused'
    | 'expired';
  label?: string;
  size?: 'sm' | 'md';
}

export const StatusBadge: React.FC<StatusBadgeProps> = ({ status, label, size = 'sm' }) => {
  const styles: Record<
    StatusBadgeProps['status'],
    { bg: string; text: string; dot: string; ring: string; defaultLabel: string; pulse?: boolean }
  > = {
    running: {
      bg: 'bg-emerald-500/10',
      text: 'text-emerald-400',
      dot: 'bg-emerald-400',
      ring: 'ring-emerald-500/20',
      defaultLabel: 'Running',
      pulse: true,
    },
    ready: {
      bg: 'bg-emerald-500/10',
      text: 'text-emerald-400',
      dot: 'bg-emerald-400',
      ring: 'ring-emerald-500/20',
      defaultLabel: 'Ready',
    },
    active: {
      bg: 'bg-emerald-500/10',
      text: 'text-emerald-400',
      dot: 'bg-emerald-400',
      ring: 'ring-emerald-500/20',
      defaultLabel: 'Active',
      pulse: true,
    },
    connected: {
      bg: 'bg-emerald-500/10',
      text: 'text-emerald-400',
      dot: 'bg-emerald-400',
      ring: 'ring-emerald-500/20',
      defaultLabel: 'Connected',
    },
    completed: {
      bg: 'bg-emerald-500/10',
      text: 'text-emerald-400',
      dot: 'bg-emerald-400',
      ring: 'ring-emerald-500/20',
      defaultLabel: 'Completed',
    },
    'in-use': {
      bg: 'bg-emerald-500/10',
      text: 'text-emerald-400',
      dot: 'bg-emerald-400',
      ring: 'ring-emerald-500/20',
      defaultLabel: 'In Use',
    },
    building: {
      bg: 'bg-amber-500/10',
      text: 'text-amber-300',
      dot: 'bg-amber-400',
      ring: 'ring-amber-500/25',
      defaultLabel: 'Building',
      pulse: true,
    },
    restarting: {
      bg: 'bg-amber-500/10',
      text: 'text-amber-300',
      dot: 'bg-amber-400',
      ring: 'ring-amber-500/25',
      defaultLabel: 'Restarting',
      pulse: true,
    },
    provisioning: {
      bg: 'bg-amber-500/10',
      text: 'text-amber-300',
      dot: 'bg-amber-400',
      ring: 'ring-amber-500/25',
      defaultLabel: 'Provisioning',
      pulse: true,
    },
    stopped: {
      bg: 'bg-zinc-500/10',
      text: 'text-zinc-400',
      dot: 'bg-zinc-500',
      ring: 'ring-white/[0.08]',
      defaultLabel: 'Stopped',
    },
    exited: {
      bg: 'bg-zinc-500/10',
      text: 'text-zinc-400',
      dot: 'bg-zinc-500',
      ring: 'ring-white/[0.08]',
      defaultLabel: 'Exited',
    },
    unused: {
      bg: 'bg-zinc-500/10',
      text: 'text-zinc-400',
      dot: 'bg-zinc-500',
      ring: 'ring-white/[0.08]',
      defaultLabel: 'Unused',
    },
    expired: {
      bg: 'bg-zinc-500/10',
      text: 'text-zinc-400',
      dot: 'bg-zinc-500',
      ring: 'ring-white/[0.08]',
      defaultLabel: 'Expired',
    },
    failed: {
      bg: 'bg-rose-500/10',
      text: 'text-rose-400',
      dot: 'bg-rose-400',
      ring: 'ring-rose-500/25',
      defaultLabel: 'Failed',
    },
    error: {
      bg: 'bg-rose-500/10',
      text: 'text-rose-400',
      dot: 'bg-rose-400',
      ring: 'ring-rose-500/25',
      defaultLabel: 'Error',
    },
  };

  const cfg = styles[status] || styles.stopped;
  const padding = size === 'sm' ? 'px-2 py-0.5 text-[11px]' : 'px-2.5 py-1 text-xs';

  return (
    <span
      className={`inline-flex items-center gap-1.5 rounded-full font-medium ring-1 ring-inset whitespace-nowrap ${cfg.bg} ${cfg.text} ${cfg.ring} ${padding}`}
    >
      <span className="relative flex h-1.5 w-1.5">
        {cfg.pulse && (
          <span
            className={`absolute inline-flex h-full w-full animate-ping rounded-full opacity-75 ${cfg.dot}`}
          />
        )}
        <span className={`relative inline-flex h-1.5 w-1.5 rounded-full ${cfg.dot}`} />
      </span>
      {label || cfg.defaultLabel}
    </span>
  );
};

// ----------------------------------------------------------------------------
// 4. 3-Dot Action Dropdown Menu (Portal-rendered so tables never clip)
// ----------------------------------------------------------------------------
export interface ActionMenuItem {
  label: string;
  icon?: React.ReactNode;
  onClick: () => void;
  variant?: 'default' | 'danger';
  disabled?: boolean;
}

interface ActionMenuProps {
  items: ActionMenuItem[];
}

export const ActionMenu: React.FC<ActionMenuProps> = ({ items }) => {
  const [open, setOpen] = useState(false);
  const [coords, setCoords] = useState<{ top: number; left: number }>({ top: 0, left: 0 });
  const btnRef = useRef<HTMLButtonElement>(null);
  const menuRef = useRef<HTMLDivElement>(null);

  const updateCoords = () => {
    if (!btnRef.current) return;
    const rect = btnRef.current.getBoundingClientRect();
    const menuWidth = 192;
    const estimatedHeight = items.length * 34 + 16;
    const spaceBelow = window.innerHeight - rect.bottom;
    const top =
      spaceBelow < estimatedHeight && rect.top > estimatedHeight
        ? rect.top - estimatedHeight - 6
        : rect.bottom + 6;
    const left = Math.max(12, Math.min(rect.right - menuWidth, window.innerWidth - menuWidth - 12));
    setCoords({ top, left });
  };

  useEffect(() => {
    if (!open) return;
    updateCoords();
    const handleOutside = (e: MouseEvent) => {
      const target = e.target as Node;
      if (
        menuRef.current &&
        !menuRef.current.contains(target) &&
        btnRef.current &&
        !btnRef.current.contains(target)
      ) {
        setOpen(false);
      }
    };
    const handleScrollOrResize = () => setOpen(false);

    document.addEventListener('mousedown', handleOutside);
    window.addEventListener('scroll', handleScrollOrResize, true);
    window.addEventListener('resize', handleScrollOrResize);
    return () => {
      document.removeEventListener('mousedown', handleOutside);
      window.removeEventListener('scroll', handleScrollOrResize, true);
      window.removeEventListener('resize', handleScrollOrResize);
    };
  }, [open]);

  return (
    <div className="inline-block text-left" onClick={(e) => e.stopPropagation()}>
      <button
        ref={btnRef}
        type="button"
        onClick={() => setOpen((prev) => !prev)}
        className={`flex h-7 w-7 items-center justify-center rounded-lg border transition-colors cursor-pointer ${
          open
            ? 'border-white/[0.18] bg-zinc-800 text-zinc-100'
            : 'border-transparent bg-transparent text-zinc-400 hover:border-white/[0.08] hover:bg-white/[0.05] hover:text-zinc-100'
        }`}
        aria-label="Actions"
      >
        <MoreHorizontal className="h-4 w-4" />
      </button>

      {open &&
        createPortal(
          <div
            ref={menuRef}
            style={{ top: coords.top, left: coords.left }}
            onClick={(e) => e.stopPropagation()}
            className="fixed z-[9999] w-48 origin-top-right rounded-xl border border-white/[0.09] bg-[#111115]/95 p-1 shadow-[0_16px_48px_rgba(0,0,0,0.85)] backdrop-blur-xl animate-in fade-in zoom-in-95 duration-100"
          >
            {items.map((item, idx) => (
              <button
                key={idx}
                type="button"
                disabled={item.disabled}
                onClick={() => {
                  setOpen(false);
                  item.onClick();
                }}
                className={`flex w-full items-center gap-2.5 rounded-lg px-2.5 py-1.5 text-left text-xs font-medium transition-colors cursor-pointer disabled:opacity-40 disabled:pointer-events-none ${
                  item.variant === 'danger'
                    ? 'text-rose-400 hover:bg-rose-500/12'
                    : 'text-zinc-200 hover:bg-white/[0.06] hover:text-white'
                }`}
              >
                {item.icon && <span className="shrink-0 opacity-80">{item.icon}</span>}
                <span className="truncate">{item.label}</span>
              </button>
            ))}
          </div>,
          document.body
        )}
    </div>
  );
};

// ----------------------------------------------------------------------------
// 5. EvilCharts-Inspired Glowing Area Chart
// ----------------------------------------------------------------------------
interface EvilAreaChartProps {
  data: TelemetryPoint[];
  dataKey: keyof TelemetryPoint;
  secondaryKey?: keyof TelemetryPoint;
  color?: string;
  secondaryColor?: string;
  unit?: string;
  height?: number;
  label?: string;
  secondaryLabel?: string;
}

export const EvilAreaChart: React.FC<EvilAreaChartProps> = ({
  data,
  dataKey,
  secondaryKey,
  color = '#f97316',
  secondaryColor = '#38bdf8',
  unit = '%',
  height = 190,
  label,
  secondaryLabel,
}) => {
  const gradId1 = `evil-grad-${String(dataKey)}`;
  const gradId2 = `evil-grad-${String(secondaryKey || 'sec')}`;

  return (
    <div style={{ height }} className="w-full select-none">
      <ResponsiveContainer width="100%" height="100%">
        <AreaChart data={data} margin={{ top: 8, right: 6, left: -22, bottom: 0 }}>
          <defs>
            <linearGradient id={gradId1} x1="0" y1="0" x2="0" y2="1">
              <stop offset="0%" stopColor={color} stopOpacity={0.32} />
              <stop offset="85%" stopColor={color} stopOpacity={0.0} />
            </linearGradient>
            {secondaryKey && (
              <linearGradient id={gradId2} x1="0" y1="0" x2="0" y2="1">
                <stop offset="0%" stopColor={secondaryColor} stopOpacity={0.24} />
                <stop offset="85%" stopColor={secondaryColor} stopOpacity={0.0} />
              </linearGradient>
            )}
          </defs>
          <CartesianGrid strokeDasharray="3 3" stroke="rgba(255,255,255,0.05)" vertical={false} />
          <XAxis
            dataKey="time"
            stroke="#71717a"
            fontSize={10}
            tickLine={false}
            axisLine={false}
            fontFamily="JetBrains Mono"
          />
          <YAxis
            stroke="#71717a"
            fontSize={10}
            tickLine={false}
            axisLine={false}
            fontFamily="JetBrains Mono"
          />
          <Tooltip
            contentStyle={{
              backgroundColor: '#0e0e12',
              borderColor: 'rgba(255,255,255,0.09)',
              borderRadius: '10px',
              fontSize: '11px',
              fontFamily: 'JetBrains Mono',
              color: '#fafafa',
              boxShadow: '0 12px 32px rgba(0,0,0,0.65)',
            }}
            formatter={(val: number, name: string) => [
              `${val}${unit}`,
              name === dataKey ? label || String(dataKey) : secondaryLabel || String(secondaryKey),
            ]}
          />
          <Area
            type="monotone"
            dataKey={dataKey}
            stroke={color}
            strokeWidth={2}
            fillOpacity={1}
            fill={`url(#${gradId1})`}
            isAnimationActive={false}
          />
          {secondaryKey && (
            <Area
              type="monotone"
              dataKey={secondaryKey}
              stroke={secondaryColor}
              strokeWidth={1.75}
              fillOpacity={1}
              fill={`url(#${gradId2})`}
              isAnimationActive={false}
            />
          )}
        </AreaChart>
      </ResponsiveContainer>
    </div>
  );
};

// ----------------------------------------------------------------------------
// 6. Kobra-Inspired OTP Verification Slots
// ----------------------------------------------------------------------------
interface KobraInputOTPProps {
  length?: number;
  value: string;
  onChange: (nextValue: string) => void;
}

export const KobraInputOTP: React.FC<KobraInputOTPProps> = ({ length = 6, value, onChange }) => {
  const inputsRef = useRef<(HTMLInputElement | null)[]>([]);
  const chars = value.padEnd(length, ' ').split('').slice(0, length);

  const handleKeyChange = (index: number, raw: string) => {
    const clean = raw.replace(/[^a-zA-Z0-9]/g, '').toUpperCase();
    if (!clean) return;
    const nextChar = clean.slice(-1);
    const updated = chars.map((c, idx) => (idx === index ? nextChar : c === ' ' ? '' : c)).join('');
    onChange(updated);
    if (index < length - 1) {
      inputsRef.current[index + 1]?.focus();
    }
  };

  const handleKeyDown = (index: number, e: React.KeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'Backspace') {
      e.preventDefault();
      const nextArr = [...chars];
      if (nextArr[index] && nextArr[index] !== ' ') {
        nextArr[index] = ' ';
        onChange(nextArr.map((c) => (c === ' ' ? '' : c)).join(''));
      } else if (index > 0) {
        nextArr[index - 1] = ' ';
        onChange(nextArr.map((c) => (c === ' ' ? '' : c)).join(''));
        inputsRef.current[index - 1]?.focus();
      }
    }
  };

  return (
    <div className="flex items-center gap-2">
      {chars.map((char, i) => {
        const isFilled = char !== ' ' && char !== '';
        return (
          <input
            key={i}
            ref={(el) => {
              inputsRef.current[i] = el;
            }}
            type="text"
            inputMode="text"
            maxLength={2}
            value={char === ' ' ? '' : char}
            onChange={(e) => handleKeyChange(i, e.target.value)}
            onKeyDown={(e) => handleKeyDown(i, e)}
            placeholder="·"
            className={`h-10 w-9 rounded-lg border text-center font-mono text-sm font-semibold transition-all focus:outline-none ${
              isFilled
                ? 'border-orange-500/60 bg-orange-500/10 text-orange-300'
                : 'border-white/[0.1] bg-[#09090b] text-zinc-100 focus:border-orange-500'
            }`}
          />
        );
      })}
    </div>
  );
};

// ----------------------------------------------------------------------------
// 7. Command Palette (Cmd+K) — Raycast / Linear / Dokploy Style
// ----------------------------------------------------------------------------
interface CommandPaletteProps {
  open: boolean;
  onClose: () => void;
  apps: AppStack[];
  onSelectNav: (nav: NavigationSection) => void;
  onSelectApp: (appId: string) => void;
  onOpenNewApp: () => void;
}

export const CommandPaletteModal: React.FC<CommandPaletteProps> = ({
  open,
  onClose,
  apps,
  onSelectNav,
  onSelectApp,
  onOpenNewApp,
}) => {
  const [query, setQuery] = useState('');

  if (!open) return null;

  const navItems: { id: NavigationSection; label: string; icon: React.ReactNode }[] = [
    { id: 'overview', label: 'Overview', icon: <Activity className="h-3.5 w-3.5" /> },
    { id: 'apps', label: 'Applications', icon: <Layers className="h-3.5 w-3.5" /> },
    { id: 'containers', label: 'Containers', icon: <Box className="h-3.5 w-3.5" /> },
    { id: 'images', label: 'Images', icon: <Layers className="h-3.5 w-3.5" /> },
    { id: 'volumes', label: 'Volumes', icon: <HardDrive className="h-3.5 w-3.5" /> },
    { id: 'terminal', label: 'Host Terminal', icon: <Terminal className="h-3.5 w-3.5" /> },
    { id: 'git', label: 'Git Providers', icon: <FolderGit2 className="h-3.5 w-3.5" /> },
    { id: 'proxy', label: 'Reverse Proxy & TLS', icon: <Globe className="h-3.5 w-3.5" /> },
    { id: 'backup', label: 'Backups & S3/R2', icon: <Archive className="h-3.5 w-3.5" /> },
    { id: 'mcp', label: 'MCP Server', icon: <Sparkles className="h-3.5 w-3.5" /> },
    { id: 'settings', label: 'System Settings', icon: <Settings className="h-3.5 w-3.5" /> },
  ];

  const filteredApps = apps.filter(
    (a) =>
      a.name.toLowerCase().includes(query.toLowerCase()) ||
      (a.primaryDomain && a.primaryDomain.toLowerCase().includes(query.toLowerCase()))
  );

  const filteredNav = navItems.filter((n) => n.label.toLowerCase().includes(query.toLowerCase()));

  return (
    <div
      className="fixed inset-0 z-[999] flex items-start justify-center bg-black/75 pt-20 p-4 backdrop-blur-md"
      onClick={onClose}
    >
      <div
        className="w-full max-w-lg overflow-hidden rounded-2xl border border-white/[0.1] bg-[#0e0e12] shadow-[0_24px_80px_rgba(0,0,0,0.85)]"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center gap-3 border-b border-white/[0.07] px-4 py-3">
          <Search className="h-4 w-4 text-orange-400" />
          <input
            type="text"
            autoFocus
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Jump to project, container, or command..."
            className="w-full bg-transparent text-xs text-zinc-100 placeholder-zinc-500 focus:outline-none"
          />
          <button
            onClick={onClose}
            className="rounded-md border border-white/[0.08] bg-zinc-900 px-1.5 py-0.5 font-mono text-[10px] text-zinc-400 hover:text-white cursor-pointer"
          >
            ESC
          </button>
        </div>

        <div className="max-h-80 overflow-y-auto p-2 space-y-3">
          <div>
            <div className="px-2.5 py-1 text-[10px] font-semibold uppercase tracking-wider text-zinc-500">
              Quick Actions
            </div>
            <button
              onClick={() => {
                onClose();
                onOpenNewApp();
              }}
              className="flex w-full items-center justify-between rounded-xl px-2.5 py-2 text-xs text-zinc-100 hover:bg-white/[0.06] transition-colors cursor-pointer"
            >
              <span className="flex items-center gap-2.5">
                <Plus className="h-3.5 w-3.5 text-orange-400" />
                Deploy New Application
              </span>
              <ArrowRight className="h-3 w-3 text-zinc-500" />
            </button>
          </div>

          {filteredApps.length > 0 && (
            <div>
              <div className="px-2.5 py-1 text-[10px] font-semibold uppercase tracking-wider text-zinc-500">
                Projects
              </div>
              {filteredApps.map((app) => (
                <button
                  key={app.id}
                  onClick={() => {
                    onSelectApp(app.id);
                    onClose();
                  }}
                  className="flex w-full items-center justify-between rounded-xl px-2.5 py-2 text-xs text-zinc-200 hover:bg-white/[0.06] transition-colors cursor-pointer"
                >
                  <div className="flex items-center gap-2.5">
                    <span className="h-2 w-2 rounded-full bg-emerald-400" />
                    <span className="font-mono font-medium text-zinc-100">{app.name}</span>
                    {app.primaryDomain && (
                      <span className="font-mono text-[11px] text-zinc-500">
                        {app.primaryDomain}
                      </span>
                    )}
                  </div>
                  <StatusBadge status={app.status} />
                </button>
              ))}
            </div>
          )}

          {filteredNav.length > 0 && (
            <div>
              <div className="px-2.5 py-1 text-[10px] font-semibold uppercase tracking-wider text-zinc-500">
                Control Plane
              </div>
              {filteredNav.map((item) => (
                <button
                  key={item.id}
                  onClick={() => {
                    onSelectNav(item.id);
                    onClose();
                  }}
                  className="flex w-full items-center justify-between rounded-xl px-2.5 py-2 text-xs text-zinc-300 hover:bg-white/[0.06] transition-colors cursor-pointer"
                >
                  <span className="flex items-center gap-2.5">
                    <span className="text-zinc-400">{item.icon}</span>
                    <span>{item.label}</span>
                  </span>
                  <ArrowRight className="h-3 w-3 text-zinc-600" />
                </button>
              ))}
            </div>
          )}
        </div>
      </div>
    </div>
  );
};

// ----------------------------------------------------------------------------
// 8. Deploy New App Modal — Dokploy / Vercel Service Creation Wizard
// ----------------------------------------------------------------------------
interface DeployNewAppModalProps {
  open: boolean;
  onClose: () => void;
  onCreateApp: (newApp: {
    name: string;
    description: string;
    sourceType: 'git' | 'compose' | 'zip';
    gitRepo?: string;
    gitBranch?: string;
    primaryDomain?: string;
    composeContent: string;
  }) => void;
}

export const DeployNewAppModal: React.FC<DeployNewAppModalProps> = ({
  open,
  onClose,
  onCreateApp,
}) => {
  const [name, setName] = useState('');
  const [sourceType, setSourceType] = useState<'git' | 'compose' | 'zip'>('git');
  const [gitRepo, setGitRepo] = useState('https://github.com/masudranaxpert/next-saas-starter');
  const [gitBranch, setGitBranch] = useState('main');
  const [primaryDomain, setPrimaryDomain] = useState('');
  const [composeContent, setComposeContent] = useState(
    `services:\n  app:\n    build: .\n    restart: unless-stopped\n    ports:\n      - "3000:3000"\n    environment:\n      - NODE_ENV=production`
  );

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    if (!name.trim()) return;
    onCreateApp({
      name: name.trim().toLowerCase().replace(/[^a-z0-9-]/g, '-'),
      description: '',
      sourceType,
      gitRepo: sourceType === 'git' ? gitRepo : undefined,
      gitBranch: sourceType === 'git' ? gitBranch : undefined,
      primaryDomain: primaryDomain.trim() || undefined,
      composeContent,
    });
    setName('');
    setPrimaryDomain('');
    onClose();
  };

  return (
    <ShadcnDialog
      open={open}
      onClose={onClose}
      title="Create Project"
      icon={<Layers className="h-4 w-4" />}
      maxWidth="lg"
    >
      <form onSubmit={handleSubmit} className="space-y-4">
        {/* Source Type Cards (Dokploy style) */}
        <div className="grid grid-cols-3 gap-2">
          {(
            [
              { id: 'git', label: 'Git Repository', sub: 'GitHub / GitLab', icon: <GitBranch className="h-4 w-4" /> },
              { id: 'compose', label: 'Docker Compose', sub: 'Raw YAML', icon: <FileCode2 className="h-4 w-4" /> },
              { id: 'zip', label: 'Archive Upload', sub: '.zip / .tar.gz', icon: <Upload className="h-4 w-4" /> },
            ] as const
          ).map((tab) => (
            <button
              key={tab.id}
              type="button"
              onClick={() => setSourceType(tab.id)}
              className={`flex flex-col items-start gap-1 rounded-xl border p-3 text-left transition-all cursor-pointer ${
                sourceType === tab.id
                  ? 'border-orange-500/50 bg-orange-500/10 text-zinc-100 ring-1 ring-orange-500/20'
                  : 'border-white/[0.07] bg-zinc-900/50 text-zinc-400 hover:border-white/[0.15] hover:text-zinc-200'
              }`}
            >
              <span className={sourceType === tab.id ? 'text-orange-400' : 'text-zinc-400'}>
                {tab.icon}
              </span>
              <span className="text-xs font-semibold">{tab.label}</span>
              <span className="text-[10px] text-zinc-500">{tab.sub}</span>
            </button>
          ))}
        </div>

        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
          <div>
            <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">
              Project Slug
            </label>
            <input
              type="text"
              required
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="billing-worker-api"
              className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 py-2 font-mono text-xs text-zinc-100 placeholder-zinc-600 focus:border-orange-500 focus:outline-none"
            />
          </div>
          <div>
            <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">
              Domain
            </label>
            <input
              type="text"
              value={primaryDomain}
              onChange={(e) => setPrimaryDomain(e.target.value)}
              placeholder="billing.nextdeploy.sh"
              className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 py-2 font-mono text-xs text-zinc-100 placeholder-zinc-600 focus:border-orange-500 focus:outline-none"
            />
          </div>
        </div>

        {sourceType === 'git' && (
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
            <div className="sm:col-span-2">
              <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">
                Git Repository URL
              </label>
              <input
                type="text"
                value={gitRepo}
                onChange={(e) => setGitRepo(e.target.value)}
                className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 py-2 font-mono text-xs text-zinc-100 focus:border-orange-500 focus:outline-none"
              />
            </div>
            <div>
              <div className="flex items-center justify-between mb-1.5">
                <label className="text-[11px] font-medium text-zinc-400">Branch</label>
                <span className="rounded bg-emerald-500/10 px-1.5 py-0.2 font-mono text-[9px] text-emerald-400">
                  Auto-detected
                </span>
              </div>
              <select
                value={gitBranch}
                onChange={(e) => setGitBranch(e.target.value)}
                className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 py-2 font-mono text-xs text-orange-400 font-semibold focus:border-orange-500 focus:outline-none"
              >
                <option value="main">main (@8c4f19a)</option>
                <option value="production">production (@f42a90c)</option>
                <option value="staging">staging (@4d19b8c)</option>
                <option value="develop">develop (@71e09b4)</option>
              </select>
            </div>
          </div>
        )}

        <div>
          <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">
            docker-compose.yml
          </label>
          <textarea
            rows={5}
            value={composeContent}
            onChange={(e) => setComposeContent(e.target.value)}
            className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] p-3 font-mono text-xs text-zinc-200 focus:border-orange-500 focus:outline-none"
          />
        </div>

        <div className="flex items-center justify-end gap-2 border-t border-white/[0.07] pt-3">
          <button
            type="button"
            onClick={onClose}
            className="rounded-lg border border-white/[0.08] bg-zinc-900 px-3.5 py-1.5 text-xs font-medium text-zinc-300 hover:bg-zinc-800 cursor-pointer"
          >
            Cancel
          </button>
          <button
            type="submit"
            className="inline-flex items-center gap-1.5 rounded-lg bg-zinc-100 px-4 py-1.5 text-xs font-semibold text-zinc-950 hover:bg-white shadow-xs transition-colors cursor-pointer"
          >
            <Plus className="h-3.5 w-3.5" />
            Create & Deploy
          </button>
        </div>
      </form>
    </ShadcnDialog>
  );
};
