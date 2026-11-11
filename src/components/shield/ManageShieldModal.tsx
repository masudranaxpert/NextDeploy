import React, { useEffect, useState } from 'react';
import { Check, Copy, Eye, EyeOff, KeyRound, Lock, Terminal } from 'lucide-react';
import type { ShieldNode } from '../../types/shield';
import { copyToClipboard } from '../../lib/clipboard';
import { ShadcnDialog } from '../ui/Primitives';
import {
  SHIELD_PREP_COMMAND,
  buildInstallCommand,
  buildSetTokenCommand,
  buildUninstallCommand,
  buildUpgradeCommand,
  getPanelOrigin,
} from './shieldCommands';

type ManageTab = 'setup' | 'maintenance' | 'remove';

const TABS: Array<{ id: ManageTab; label: string }> = [
  { id: 'setup', label: 'Setup & Connect' },
  { id: 'maintenance', label: 'Maintenance' },
  { id: 'remove', label: 'Remove' },
];

export interface ManageShieldModalProps {
  node: ShieldNode | null;
  panelId: string;
  panelOrigin?: string;
  onClose: () => void;
  onRotateToken: (node: ShieldNode) => void;
  onDone?: () => void;
}

interface CommandBlockProps {
  id: string;
  label: string;
  hint?: string;
  command: string;
  accent?: 'primary' | 'cyan' | 'amber' | 'danger' | 'default';
  copiedId: string | null;
  onCopy: (text: string, id: string) => void;
}

const CommandBlock: React.FC<CommandBlockProps> = ({
  id,
  label,
  hint,
  command,
  accent = 'default',
  copiedId,
  onCopy,
}) => {
  const isCopied = copiedId === id;
  const borderClass =
    accent === 'primary'
      ? 'border-emerald-500/40 bg-zinc-950/90'
      : accent === 'cyan'
      ? 'border-cyan-500/40 bg-zinc-950/90'
      : accent === 'amber'
      ? 'border-amber-500/40 bg-zinc-950/90'
      : accent === 'danger'
      ? 'border-rose-500/40 bg-zinc-950/90'
      : 'border-white/[0.08] bg-zinc-950';

  return (
    <div className="space-y-1.5">
      <div className="flex items-baseline justify-between gap-2">
        <span className="text-xs font-medium text-zinc-200">{label}</span>
        {hint && <span className="text-[11px] text-zinc-400 shrink-0">{hint}</span>}
      </div>
      <div className={`relative rounded-lg border ${borderClass}`}>
        <pre className="overflow-x-auto px-3 py-2.5 pr-11 font-mono text-xs leading-relaxed text-zinc-200 whitespace-pre">
          {command}
        </pre>
        <button
          type="button"
          onClick={() => onCopy(command, id)}
          aria-label={`Copy ${label}`}
          title="Copy command"
          className="absolute right-2 top-2 inline-flex h-7 w-7 items-center justify-center rounded-md border border-white/[0.08] bg-zinc-900 text-zinc-300 hover:bg-zinc-800 hover:text-white focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-emerald-500/60 transition-colors cursor-pointer"
        >
          {isCopied ? <Check className="h-3.5 w-3.5 text-emerald-400" /> : <Copy className="h-3.5 w-3.5" />}
        </button>
      </div>
    </div>
  );
};

export const ManageShieldModal: React.FC<ManageShieldModalProps> = ({
  node,
  panelId,
  panelOrigin,
  onClose,
  onRotateToken,
  onDone,
}) => {
  const [activeTab, setActiveTab] = useState<ManageTab>('setup');
  const [copiedId, setCopiedId] = useState<string | null>(null);
  const [showToken, setShowToken] = useState(false);

  useEffect(() => {
    if (node) {
      setActiveTab('setup');
      setShowToken(false);
      setCopiedId(null);
    }
  }, [node?.id]);

  const handleCopy = async (text: string, id: string) => {
    if (!text) return;
    const ok = await copyToClipboard(text);
    if (ok) {
      setCopiedId(id);
      setTimeout(() => {
        setCopiedId((curr) => (curr === id ? null : curr));
      }, 1800);
    }
  };

  const origin = panelOrigin || getPanelOrigin();
  const installCmd = node ? buildInstallCommand(origin, node.token) : '';
  const upgradeCmd = `${buildUpgradeCommand()} && nd edge doctor`;
  const setTokenCmd = node ? buildSetTokenCommand(panelId, node.token) : '';
  const unlinkCmd = buildUninstallCommand(panelId);

  const handleTabKeyDown = (e: React.KeyboardEvent<HTMLButtonElement>, index: number) => {
    if (e.key === 'ArrowRight') {
      e.preventDefault();
      const next = TABS[(index + 1) % TABS.length];
      setActiveTab(next.id);
      document.getElementById(`shield-tab-${next.id}`)?.focus();
    } else if (e.key === 'ArrowLeft') {
      e.preventDefault();
      const prev = TABS[(index - 1 + TABS.length) % TABS.length];
      setActiveTab(prev.id);
      document.getElementById(`shield-tab-${prev.id}`)?.focus();
    }
  };

  return (
    <ShadcnDialog
      open={Boolean(node)}
      onClose={onClose}
      title={node ? `Manage Shield: ${node.name}` : 'Manage Shield'}
      description="Run over SSH as root on your Shield VPS."
      icon={<Terminal className="h-4 w-4 text-emerald-400" />}
      maxWidth="lg"
      headerActions={
        node ? (
          <button
            type="button"
            onClick={() => handleCopy(node.ip, 'header-ip')}
            aria-label={`Copy IP ${node.ip}`}
            title="Copy Shield IP"
            className="inline-flex h-7 items-center gap-1.5 rounded-md border border-emerald-500/30 bg-emerald-500/10 px-2 font-mono text-xs tabular-nums text-emerald-300 hover:bg-emerald-500/20 hover:border-emerald-500/50 transition-colors cursor-pointer"
          >
            <span>{node.ip}</span>
            {copiedId === 'header-ip' ? (
              <Check className="h-3 w-3 text-emerald-400" />
            ) : (
              <Copy className="h-3 w-3 text-emerald-400/70" />
            )}
          </button>
        ) : null
      }
      footer={
        <div className="flex items-center justify-between w-full gap-2">
          {node ? (
            <button
              type="button"
              onClick={() => {
                onClose();
                onRotateToken(node);
              }}
              className="inline-flex items-center gap-1.5 h-8 px-3 rounded-md border border-amber-500/30 bg-amber-500/10 text-xs font-medium text-amber-300 hover:bg-amber-500/20 hover:border-amber-500/50 transition-colors cursor-pointer"
            >
              <KeyRound className="h-3.5 w-3.5 text-amber-400" />
              <span>Rotate Token</span>
            </button>
          ) : (
            <span />
          )}
          <button
            type="button"
            onClick={() => {
              onDone?.();
              onClose();
            }}
            className="h-8 px-4 rounded-md bg-emerald-600 hover:bg-emerald-500 text-xs font-medium text-white shadow-xs transition-colors cursor-pointer"
          >
            Done
          </button>
        </div>
      }
    >
      {node && (
        <div className="space-y-4">
          {/* Segmented Tab Navigation */}
          <div
            role="tablist"
            aria-label="Shield management sections"
            className="grid grid-cols-3 gap-1 rounded-lg border border-white/[0.07] bg-zinc-950 p-1"
          >
            {TABS.map((tab, idx) => {
              const selected = activeTab === tab.id;
              return (
                <button
                  key={tab.id}
                  id={`shield-tab-${tab.id}`}
                  type="button"
                  role="tab"
                  aria-selected={selected}
                  aria-controls={`shield-panel-${tab.id}`}
                  tabIndex={selected ? 0 : -1}
                  onClick={() => setActiveTab(tab.id)}
                  onKeyDown={(e) => handleTabKeyDown(e, idx)}
                  className={`h-8 rounded-md text-xs font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-emerald-500/60 cursor-pointer ${
                    selected
                      ? 'bg-zinc-800 text-white shadow-xs'
                      : 'text-zinc-400 hover:text-zinc-200'
                  }`}
                >
                  {tab.label}
                </button>
              );
            })}
          </div>

          {activeTab === 'setup' && (
            <div
              id="shield-panel-setup"
              role="tabpanel"
              aria-labelledby="shield-tab-setup"
              className="space-y-4"
            >
              <CommandBlock
                id="modal-prep-cmd"
                label="1. Open firewall ports"
                hint="Ubuntu / Debian"
                command={SHIELD_PREP_COMMAND}
                accent="cyan"
                copiedId={copiedId}
                onCopy={handleCopy}
              />

              <CommandBlock
                id="script-cmd"
                label="2. Install nd edge and link panel"
                hint="--token-stdin"
                command={installCmd}
                accent="primary"
                copiedId={copiedId}
                onCopy={handleCopy}
              />

              <div className="flex flex-wrap items-center justify-between gap-2 rounded-lg border border-amber-500/25 bg-amber-500/5 px-3 py-2">
                <div className="flex items-center gap-2 min-w-0">
                  <span className="text-xs text-amber-400 font-medium shrink-0">Token</span>
                  <code className="font-mono text-xs text-amber-200 truncate max-w-[14rem] sm:max-w-[20rem]">
                    {showToken ? node.token : '••••••••••••••••••••••••'}
                  </code>
                </div>
                <div className="flex items-center gap-1.5">
                  <button
                    type="button"
                    onClick={() => setShowToken((v) => !v)}
                    aria-label={showToken ? 'Hide raw token' : 'Show raw token'}
                    className="inline-flex h-7 items-center gap-1 rounded-md border border-amber-500/30 bg-amber-500/10 px-2 text-xs text-amber-300 hover:bg-amber-500/20 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-amber-500/60 cursor-pointer"
                  >
                    {showToken ? <EyeOff className="h-3 w-3" /> : <Eye className="h-3 w-3" />}
                    <span>{showToken ? 'Hide' : 'Show'}</span>
                  </button>
                  <button
                    type="button"
                    onClick={() => handleCopy(node.token, 'raw-token')}
                    aria-label="Copy raw token"
                    className="inline-flex h-7 items-center gap-1 rounded-md border border-amber-500/30 bg-amber-500/10 px-2 text-xs text-amber-300 hover:bg-amber-500/20 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-amber-500/60 cursor-pointer"
                  >
                    {copiedId === 'raw-token' ? (
                      <>
                        <Check className="h-3 w-3 text-emerald-400" />
                        <span className="text-emerald-400">Copied</span>
                      </>
                    ) : (
                      <>
                        <Copy className="h-3 w-3" />
                        <span>Copy</span>
                      </>
                    )}
                  </button>
                </div>
              </div>

              <p className="flex items-start gap-2 text-[11px] text-zinc-400 leading-relaxed">
                <Lock className="h-3.5 w-3.5 text-emerald-400/80 shrink-0 mt-0.5" />
                <span>
                  Tokens pass via <code className="font-mono text-emerald-300">--token-stdin</code> instead of URLs or CLI flags. Routes sync automatically within 30s after install.
                </span>
              </p>
            </div>
          )}

          {activeTab === 'maintenance' && (
            <div
              id="shield-panel-maintenance"
              role="tabpanel"
              aria-labelledby="shield-tab-maintenance"
              className="space-y-4"
            >
              <CommandBlock
                id="modal-upgrade-cmd"
                label="Upgrade binary & check health"
                hint="SHA-256 verified"
                command={upgradeCmd}
                accent="cyan"
                copiedId={copiedId}
                onCopy={handleCopy}
              />

              <CommandBlock
                id="modal-doctor-cmd"
                label="Run edge diagnostics"
                command="nd edge doctor"
                accent="primary"
                copiedId={copiedId}
                onCopy={handleCopy}
              />

              <CommandBlock
                id="modal-set-token-cmd"
                label="Re-apply token on VPS"
                hint="--token-stdin"
                command={setTokenCmd}
                accent="amber"
                copiedId={copiedId}
                onCopy={handleCopy}
              />

              <p className="text-[11px] text-zinc-400 leading-relaxed">
                Upgrades keep active Caddy routes serving without interruption.
              </p>
            </div>
          )}

          {activeTab === 'remove' && (
            <div
              id="shield-panel-remove"
              role="tabpanel"
              aria-labelledby="shield-tab-remove"
              className="space-y-4"
            >
              <CommandBlock
                id="modal-unlink-cmd"
                label="Unlink this panel only"
                hint="Keeps other panels"
                command={unlinkCmd}
                accent="amber"
                copiedId={copiedId}
                onCopy={handleCopy}
              />

              <CommandBlock
                id="modal-uninstall-all-cmd"
                label="Uninstall nd edge completely"
                hint="Removes daemon & routes"
                command="nd edge uninstall --all"
                accent="danger"
                copiedId={copiedId}
                onCopy={handleCopy}
              />

              <p className="text-[11px] text-zinc-400 leading-relaxed">
                Unlinking deletes this panel&apos;s Caddy routes and origin CA from the VPS without affecting other panels.
              </p>
            </div>
          )}
        </div>
      )}
    </ShadcnDialog>
  );
};
