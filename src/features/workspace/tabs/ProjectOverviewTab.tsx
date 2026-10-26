import React, { useEffect, useState } from 'react';
import {
  GitBranch,
  Folder,
  FolderOpen,
  Pause,
  Play,
  RefreshCw,
  Copy,
  Check,
  Layers,
  FileCode2,
  Sliders,
  Trash2,
  ArrowUpRight,
  ShieldCheck,
  Server,
} from 'lucide-react';
import { toast } from 'sonner';
import { AppStack } from '../../../types/nextdeploy';
import { ShieldNode } from '../../../types/shield';
import { backendApi, getServerPublicIp } from '../../../lib/backendApi';
import { copyToClipboard } from '../../../lib/clipboard';
import { ShadcnDialog, ShadcnSwitch } from '../../../components/ui/Primitives';

interface ProjectOverviewTabProps {
  app: AppStack;
  onUpdateApp: (updated: AppStack) => void;
  hasGitConnection: boolean;
  isGitSyncActive: boolean;
  detectedBranches: string[];
  limitsInfo: any;
  productionUrl: string;
  previewUrl: string;
  previewHost: string;
  standbySlotName: string;
  onOpenLimitsModal: () => void;
  onOpenDeleteModal: () => void;
  onOpenGitModal: () => void;
  onOpenFilesTab: () => void;
  onOpenGitTab: () => void;
  onViewCaddyCompose: () => void;
}

export const ProjectOverviewTab: React.FC<ProjectOverviewTabProps> = ({
  app,
  onUpdateApp,
  hasGitConnection,
  isGitSyncActive,
  detectedBranches,
  limitsInfo,
  productionUrl,
  previewUrl,
  previewHost,
  standbySlotName,
  onOpenLimitsModal,
  onOpenDeleteModal,
  onOpenGitModal,
  onOpenFilesTab,
  onOpenGitTab,
  onViewCaddyCompose,
}) => {
  const [switchSourceModalOpen, setSwitchSourceModalOpen] = useState(false);
  const [gitSyncModalOpen, setGitSyncModalOpen] = useState(false);
  const [forceGitSyncResume, setForceGitSyncResume] = useState(false);
  const [gitRepoInput, setGitRepoInput] = useState(app.gitRepo || '');
  const [gitBranchInput, setGitBranchInput] = useState(app.gitBranch || 'main');
  const [shieldNodes, setShieldNodes] = useState<ShieldNode[]>([]);
  const [copiedKey, setCopiedKey] = useState<string | null>(null);

  const handleCopy = async (key: string, text: string) => {
    const ok = await copyToClipboard(text);
    if (ok) {
      setCopiedKey(key);
      setTimeout(() => {
        setCopiedKey((curr) => (curr === key ? null : curr));
      }, 2000);
    }
  };

  useEffect(() => {
    backendApi.listShieldNodes().then((res) => {
      if (res.ok && res.nodes) setShieldNodes(res.nodes);
    }).catch(() => {});
  }, []);

  return (
    <div className="space-y-5">
      {/* Deploy Source Bar */}
      <div className="rounded-xl border border-white/[0.07] bg-[#111115]/90 px-4 py-3.5 flex flex-wrap items-center justify-between gap-3">
        <div className="flex flex-wrap items-center gap-2.5 min-w-0">
          {isGitSyncActive ? (
            <span className="inline-flex items-center gap-1.5 rounded-md border border-emerald-500/30 bg-emerald-500/10 px-2.5 py-1 text-xs font-medium text-emerald-300">
              <GitBranch className="h-3.5 w-3.5" />
              Git Sync ON
            </span>
          ) : (
            <span className="inline-flex items-center gap-1.5 rounded-md border border-orange-500/30 bg-orange-500/10 px-2.5 py-1 text-xs font-medium text-orange-300">
              <Folder className="h-3.5 w-3.5" />
              {hasGitConnection ? 'Manual Files (Git Sync OFF)' : 'Manual Workspace'}
            </span>
          )}

          {hasGitConnection && !isGitSyncActive && (
            <span className="inline-flex items-center gap-1.5 rounded-md border border-amber-500/30 bg-amber-500/10 px-2.5 py-1 font-mono text-xs font-medium text-amber-300">
              Git connected · {app.gitBranch || 'main'} (manual sync available)
            </span>
          )}

          {isGitSyncActive && app.gitRepo && (
            <span className="font-mono text-xs text-zinc-400 break-all">
              {app.gitRepo} ({app.gitBranch || 'main'})
            </span>
          )}
        </div>

        <div className="flex flex-wrap items-center gap-2">
          {hasGitConnection && (
            <button
              type="button"
              onClick={() => {
                setForceGitSyncResume(false);
                setGitSyncModalOpen(true);
              }}
              className={`inline-flex h-9 items-center gap-1.5 rounded-lg border px-3 text-xs font-medium cursor-pointer ${
                isGitSyncActive
                  ? 'border-white/[0.08] bg-zinc-900 text-zinc-200 hover:bg-zinc-800'
                  : 'border-emerald-500/30 bg-emerald-500/10 text-emerald-300 hover:bg-emerald-500/20'
              }`}
            >
              {isGitSyncActive ? (
                <>
                  <Pause className="h-3.5 w-3.5 text-amber-400" />
                  Git Sync OFF
                </>
              ) : (
                <>
                  <Play className="h-3.5 w-3.5 text-emerald-400" />
                  Git Sync ON
                </>
              )}
            </button>
          )}

          <button
            type="button"
            onClick={() => setSwitchSourceModalOpen(true)}
            className="inline-flex h-9 items-center gap-1.5 rounded-lg border border-white/[0.08] bg-zinc-900 px-3 text-xs font-medium text-zinc-200 hover:bg-zinc-800 cursor-pointer"
          >
            <RefreshCw className="h-3.5 w-3.5 text-orange-400" />
            {hasGitConnection ? 'Switch to Manual Mode' : 'Connect Git'}
          </button>
        </div>
      </div>

      {/* Row 2: Source Pipeline + Container Resource Limits & Styled Delete Button */}
      <div className="grid grid-cols-1 gap-4 lg:grid-cols-3">
        <div className="lg:col-span-2 rounded-xl border border-white/[0.07] bg-[#111115]/90 p-5 flex flex-col justify-between gap-4">
          <div className="flex items-center justify-between gap-3">
            <div className="flex min-w-0 items-center gap-2.5">
              <span className="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg border border-orange-500/30 bg-orange-500/10">
                {hasGitConnection ? (
                  <GitBranch className="h-4 w-4 text-orange-400" />
                ) : (
                  <Folder className="h-4 w-4 text-orange-400" />
                )}
              </span>
              <h3 className="truncate font-mono text-[13px] font-semibold uppercase tracking-wide text-zinc-100">
                {hasGitConnection ? 'Git Source & Environments' : 'Workspace Directory'}
              </h3>
            </div>
            {hasGitConnection ? (
              <button
                type="button"
                onClick={onOpenGitModal}
                className="inline-flex h-9 shrink-0 items-center gap-1.5 whitespace-nowrap rounded-lg border border-white/[0.08] bg-zinc-900 px-3 text-xs font-medium text-zinc-200 hover:bg-zinc-800 cursor-pointer"
              >
                <GitBranch className="h-3.5 w-3.5 text-orange-400" />
                Change Branch / Repo
              </button>
            ) : (
              <button
                type="button"
                onClick={onOpenFilesTab}
                className="inline-flex h-9 shrink-0 items-center gap-1.5 whitespace-nowrap rounded-lg border border-white/[0.08] bg-zinc-900 px-3 text-xs font-medium text-zinc-200 hover:bg-zinc-800 cursor-pointer"
              >
                <FolderOpen className="h-3.5 w-3.5 text-orange-400" />
                Manage Files
              </button>
            )}
          </div>

          {hasGitConnection ? (
            <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
              <div className="rounded-lg border border-white/[0.06] bg-[#09090b] p-3">
                <span className="text-xs font-medium uppercase tracking-wider text-zinc-500">
                  Repository
                </span>
                <div className="mt-1 truncate font-mono text-xs text-zinc-200">
                  {app.gitRepo
                    ? app.gitRepo.replace('https://github.com/', '')
                    : 'Local Compose Stack'}
                </div>
              </div>
              <div className="rounded-lg border border-white/[0.06] bg-[#09090b] p-3">
                <span className="text-xs font-medium uppercase tracking-wider text-zinc-500">
                  Active Branch ({detectedBranches.length} detected)
                </span>
                <div className="mt-1 truncate font-mono text-xs text-orange-400">
                  {app.gitBranch || 'main'} · {app.composeFilePath}
                </div>
              </div>
              <div className="rounded-lg border border-white/[0.06] bg-[#09090b] p-3 flex items-center justify-between">
                <div>
                  <span className="text-xs font-medium uppercase tracking-wider text-zinc-500 block">
                    Git Sync
                  </span>
                  <span className="mt-1 font-mono text-xs text-zinc-200 block">
                    {isGitSyncActive ? 'ON (Auto-Pull)' : 'OFF (Manual)'}
                  </span>
                </div>
                <ShadcnSwitch
                  checked={isGitSyncActive}
                  onChange={(next) => {
                    void backendApi.setGitSync(app.id, next, false);
                    onUpdateApp({
                      ...app,
                      sourceType: next ? 'git' : 'compose',
                      gitSync: next,
                    });
                    toast.success(
                      next
                        ? 'Git Sync ON — deployments pull from repository'
                        : 'Git Sync OFF — deployments use manual workspace files'
                    );
                  }}
                />
              </div>
            </div>
          ) : (
            <div className="grid grid-cols-2 gap-2.5">
              <div className="rounded-lg border border-white/[0.06] bg-[#09090b] p-3">
                <span className="text-xs font-medium uppercase tracking-wider text-zinc-500">
                  Deploy Mode
                </span>
                <div className="mt-1 font-medium text-xs text-orange-400 whitespace-nowrap">
                  Manual Files Only
                </div>
              </div>
              <div className="rounded-lg border border-white/[0.06] bg-[#09090b] p-3 min-w-0">
                <span className="text-xs font-medium uppercase tracking-wider text-zinc-500">
                  Compose Entry
                </span>
                <div
                  className="mt-1 truncate font-mono text-xs text-emerald-400"
                  title={app.composeFilePath || 'docker-compose.yml'}
                >
                  {app.composeFilePath || 'docker-compose.yml'}
                </div>
              </div>
              <div className="col-span-2 rounded-lg border border-white/[0.06] bg-[#09090b] p-3">
                <span className="text-[10px] font-medium uppercase tracking-wider text-zinc-500">
                  Workspace Directory
                </span>
                <div className="mt-1 flex items-center justify-between gap-2">
                  <span className="font-mono text-xs text-zinc-100 break-all">
                    /data/apps/{app.slug}
                  </span>
                  <button
                    type="button"
                    onClick={() => handleCopy('workspace-dir', `/data/apps/${app.slug}`)}
                    title="Copy workspace directory"
                    className="shrink-0 rounded-md border border-white/[0.08] bg-zinc-900 p-1.5 text-zinc-400 hover:text-zinc-100 cursor-pointer transition-colors"
                  >
                    {copiedKey === 'workspace-dir' ? (
                      <Check className="h-3 w-3 text-emerald-400" />
                    ) : (
                      <Copy className="h-3 w-3" />
                    )}
                  </button>
                </div>
              </div>
            </div>
          )}

          {/* Production & Preview URLs + Quick Compose & File Manager Actions */}
          <div className="space-y-2 border-t border-white/[0.06] pt-3 text-xs">
            {productionUrl && (
              <a
                href={productionUrl}
                target="_blank"
                rel="noreferrer"
                className="flex items-center gap-2 rounded-lg border border-emerald-500/25 bg-emerald-500/[0.06] px-3 py-2 font-mono text-[11px] text-emerald-300 transition-colors hover:bg-emerald-500/10"
              >
                <span
                  aria-hidden="true"
                  className="h-1.5 w-1.5 shrink-0 rounded-full bg-emerald-400"
                />
                <span className="min-w-0 flex-1 truncate">
                  {productionUrl.replace('https://', '')}
                </span>
                <ArrowUpRight className="h-3.5 w-3.5 shrink-0" />
              </a>
            )}
            {previewUrl && (
              <a
                href={previewUrl}
                target="_blank"
                rel="noreferrer"
                className="flex items-center gap-2 rounded-lg border border-violet-500/25 bg-violet-500/[0.06] px-3 py-2 font-mono text-[11px] text-violet-300 transition-colors hover:bg-violet-500/10"
              >
                <span
                  aria-hidden="true"
                  className="h-1.5 w-1.5 shrink-0 rounded-full bg-violet-400"
                />
                <span className="min-w-0 flex-1 truncate">
                  Standby ({standbySlotName}): {previewHost}
                </span>
                <ArrowUpRight className="h-3.5 w-3.5 shrink-0" />
              </a>
            )}

            {(() => {
              const assignedShieldId = app.domains.find((d) => d.shield_node_id)?.shield_node_id;
              const matchedShield = assignedShieldId ? shieldNodes.find((s) => s.id === assignedShieldId) : null;
              const directIP = getServerPublicIp();

              if (matchedShield) {
                return (
                  <div className="flex items-center justify-between rounded-lg border border-cyan-500/25 bg-cyan-500/[0.05] px-3 py-2 text-[11px]">
                    <div className="flex items-center gap-1.5 text-cyan-300 font-medium min-w-0">
                      <ShieldCheck className="h-3.5 w-3.5 shrink-0 text-cyan-400" />
                      <span className="truncate">DNS A-Record:</span>
                      <code className="font-mono text-white font-bold bg-zinc-900/80 px-1.5 py-0.5 rounded border border-cyan-500/30">
                        {matchedShield.ip}
                      </code>
                      <span className="text-[10px] text-zinc-400 truncate">({matchedShield.name})</span>
                    </div>
                    <button
                      type="button"
                      onClick={() => handleCopy('shield-ip', matchedShield.ip)}
                      className="rounded p-1 text-zinc-400 hover:text-white shrink-0 cursor-pointer transition-colors"
                      title="Copy Shield IP"
                    >
                      {copiedKey === 'shield-ip' ? (
                        <Check className="h-3.5 w-3.5 text-emerald-400" />
                      ) : (
                        <Copy className="h-3.5 w-3.5" />
                      )}
                    </button>
                  </div>
                );
              }

              if (directIP) {
                return (
                  <div className="flex items-center justify-between rounded-lg border border-zinc-800 bg-zinc-950 px-3 py-2 text-[11px]">
                    <div className="flex items-center gap-1.5 text-zinc-400 font-medium min-w-0">
                      <Server className="h-3.5 w-3.5 shrink-0 text-zinc-500" />
                      <span className="truncate">Server IP (DNS):</span>
                      <code className="font-mono text-zinc-200 font-bold bg-zinc-900 px-1.5 py-0.5 rounded border border-zinc-700">
                        {directIP}
                      </code>
                    </div>
                    <button
                      type="button"
                      onClick={() => handleCopy('server-ip', directIP)}
                      className="rounded p-1 text-zinc-400 hover:text-white shrink-0 cursor-pointer transition-colors"
                      title="Copy Server IP"
                    >
                      {copiedKey === 'server-ip' ? (
                        <Check className="h-3.5 w-3.5 text-emerald-400" />
                      ) : (
                        <Copy className="h-3.5 w-3.5" />
                      )}
                    </button>
                  </div>
                );
              }

              return null;
            })()}

            <div className="grid grid-cols-2 gap-2">
              <button
                type="button"
                onClick={onViewCaddyCompose}
                className="inline-flex h-9 items-center justify-center gap-1.5 rounded-lg border border-emerald-500/30 bg-emerald-500/[0.07] text-xs font-medium text-emerald-300 hover:bg-emerald-500/15 cursor-pointer"
              >
                <Layers className="h-3.5 w-3.5" />
                Caddy Compose
              </button>
              <button
                type="button"
                onClick={onOpenFilesTab}
                className="inline-flex h-9 items-center justify-center gap-1.5 rounded-lg border border-orange-500/30 bg-orange-500/[0.07] text-xs font-medium text-orange-300 hover:bg-orange-500/15 cursor-pointer"
              >
                <FileCode2 className="h-3.5 w-3.5" />
                Open File Manager
              </button>
            </div>
          </div>
        </div>

        {/* Right Card: Container Resource Limits & Styled Delete Project Button */}
        <div className="rounded-xl border border-white/[0.07] bg-[#111115]/90 p-5 flex flex-col justify-between gap-4">
          <div>
            <div className="flex items-center justify-between">
              <div className="flex items-center gap-2.5">
                <span className="flex h-8 w-8 items-center justify-center rounded-lg border border-orange-500/30 bg-orange-500/10">
                  <Sliders className="h-3.5 w-3.5 text-orange-400" />
                </span>
                <span className="text-xs font-semibold uppercase tracking-wider text-zinc-300">
                  Resource Limits
                </span>
              </div>
              <button
                type="button"
                onClick={onOpenLimitsModal}
                className="rounded-md border border-white/[0.08] bg-zinc-900 px-2 py-0.5 text-[11px] font-medium text-orange-400 hover:bg-zinc-800 cursor-pointer"
              >
                Adjust
              </button>
            </div>

            <div className="mt-3 grid grid-cols-2 gap-2.5 font-mono text-xs">
              <div className="rounded-lg border border-white/[0.06] bg-[#09090b] p-2.5">
                <span className="text-[10px] text-zinc-500 block">Max CPU</span>
                <div className="mt-0.5 flex items-baseline justify-between">
                  <span className="text-zinc-100 font-semibold">
                    {limitsInfo?.limits.cpus
                      ? `${limitsInfo.limits.cpus} vCPU`
                      : limitsInfo
                      ? 'Shared'
                      : '…'}
                  </span>
                  <span className="text-[10px] text-emerald-400">
                    {limitsInfo
                      ? `${(limitsInfo.usage.cpu_cores * 100).toFixed(2)}% in use`
                      : ''}
                  </span>
                </div>
              </div>
              <div className="rounded-lg border border-white/[0.06] bg-[#09090b] p-2.5">
                <span className="text-[10px] text-zinc-500 block">Max Memory</span>
                <div className="mt-0.5 flex items-baseline justify-between">
                  <span className="text-zinc-100 font-semibold">
                    {limitsInfo?.limits.memory_mb
                      ? `${limitsInfo.limits.memory_mb} MB`
                      : limitsInfo
                      ? 'Shared'
                      : '…'}
                  </span>
                  <span className="text-[10px] text-sky-400">
                    {limitsInfo ? `${limitsInfo.usage.memory_mb} MB in use` : ''}
                  </span>
                </div>
              </div>
            </div>
          </div>

          {/* Clean Footer with Copyable ID + Modern Destructive Delete Button */}
          <div className="flex items-center justify-between gap-2 border-t border-white/[0.06] pt-3.5">
            <button
              type="button"
              onClick={() => handleCopy('app-id', app.id)}
              className="inline-flex items-center gap-1.5 rounded-lg border border-white/[0.06] bg-[#09090b] px-2.5 py-1.5 font-mono text-[11px] text-zinc-400 hover:border-white/[0.14] hover:text-zinc-200 transition-colors cursor-pointer"
              title="Copy Project ID"
            >
              {copiedKey === 'app-id' ? (
                <Check className="h-3 w-3 text-emerald-400" />
              ) : (
                <Copy className="h-3 w-3 text-zinc-500" />
              )}
              <span>{app.id}</span>
            </button>

            <button
              type="button"
              onClick={onOpenDeleteModal}
              className="inline-flex items-center gap-1.5 rounded-lg border border-rose-500/25 bg-rose-500/10 px-3 py-1.5 text-xs font-medium text-rose-400 hover:border-rose-500/45 hover:bg-rose-500/20 hover:text-rose-300 transition-all cursor-pointer"
            >
              <Trash2 className="h-3.5 w-3.5" />
              <span>Delete Project</span>
            </button>
          </div>
        </div>
      </div>

      {/* Switch Deploy Source Modal */}
      <ShadcnDialog
        open={switchSourceModalOpen}
        onClose={() => setSwitchSourceModalOpen(false)}
        title={
          hasGitConnection
            ? 'Switch to Pure Manual Workspace Directory'
            : 'Connect Git Repository Source'
        }
        icon={<RefreshCw className="h-4 w-4 text-orange-400" />}
      >
        <div className="space-y-4">
          {hasGitConnection ? (
            <div className="rounded-lg border border-amber-500/30 bg-amber-500/10 p-3 text-xs text-amber-200 leading-relaxed">
              Switching to <strong>Manual Workspace Files</strong> completely detaches Git from this
              project and hides the Git tab. (If you want to keep Git connected while editing files
              manually, use <strong>Pause Git sync</strong> instead.)
            </div>
          ) : (
            <div className="space-y-3">
              <div className="rounded-lg border border-orange-500/30 bg-orange-500/10 p-3 text-xs text-orange-200 leading-relaxed">
                Connecting a <strong>Git repository</strong> enables the Git tab and sets the
                repository checkout as the deploy source.
              </div>
              <div>
                <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">
                  Git Repository URL
                </label>
                <input
                  type="text"
                  value={gitRepoInput}
                  onChange={(e) => setGitRepoInput(e.target.value)}
                  placeholder="https://github.com/org/repo"
                  className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 py-2 font-mono text-xs text-zinc-100"
                />
              </div>
              <div>
                <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">Branch</label>
                <input
                  type="text"
                  value={gitBranchInput}
                  onChange={(e) => setGitBranchInput(e.target.value)}
                  placeholder="main"
                  className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 py-2 font-mono text-xs text-zinc-100"
                />
              </div>
            </div>
          )}

          <div className="flex justify-end gap-2 border-t border-white/[0.07] pt-3">
            <button
              type="button"
              onClick={() => setSwitchSourceModalOpen(false)}
              className="rounded-lg border border-white/[0.08] bg-zinc-900 px-3.5 py-1.5 text-xs text-zinc-300 cursor-pointer"
            >
              Cancel
            </button>
            <button
              type="button"
              onClick={async () => {
                const targetSource = hasGitConnection ? 'files' : 'git';
                if (targetSource === 'git' && !gitRepoInput.trim()) {
                  toast.error('Please enter a Git repository URL');
                  return;
                }
                const res = await backendApi.switchAppSource(app.id, targetSource);
                if (!res.ok && !res.offlineFallback) {
                  toast.error(res.error || 'Failed to switch deploy source');
                  return;
                }
                if (targetSource === 'git') {
                  const nextRepo = gitRepoInput.trim();
                  const nextBranch = gitBranchInput.trim() || 'main';
                  onUpdateApp({
                    ...app,
                    gitRepo: nextRepo,
                    gitBranch: nextBranch,
                    sourceType: 'git',
                    gitSync: true,
                  });
                  setSwitchSourceModalOpen(false);
                  onOpenGitTab();
                  toast.success('Connected Git repository source');
                } else {
                  onUpdateApp({
                    ...app,
                    gitRepo: '',
                    sourceType: 'compose',
                    gitSync: false,
                  });
                  setSwitchSourceModalOpen(false);
                  onOpenFilesTab();
                  toast.success('Switched to pure Manual Workspace Directory (Git detached)');
                }
              }}
              className="rounded-lg bg-zinc-100 px-4 py-1.5 text-xs font-semibold text-zinc-950 hover:bg-white cursor-pointer"
            >
              {hasGitConnection ? 'Detach Git & Use Manual Files' : 'Connect Git Source'}
            </button>
          </div>
        </div>
      </ShadcnDialog>

      {/* Pause / Resume Git Sync Modal */}
      <ShadcnDialog
        open={gitSyncModalOpen}
        onClose={() => setGitSyncModalOpen(false)}
        title={app.sourceType === 'git' ? 'Pause Git Sync' : 'Resume Git Sync'}
        icon={<GitBranch className="h-4 w-4 text-orange-400" />}
      >
        <div className="space-y-4">
          {app.sourceType === 'git' ? (
            <p className="text-xs text-zinc-300 leading-relaxed">
              Copies the current Git checkout into the workspace (newer workspace files are
              preserved) and pauses automatic Git pulls so you can edit files directly in File
              Manager or push via <code className="font-mono text-orange-300">nd push</code>.
            </p>
          ) : (
            <div className="space-y-3">
              <p className="text-xs text-zinc-300 leading-relaxed">
                Resumes deploying from{' '}
                <code className="font-mono text-emerald-300">{app.gitRepo}</code> (
                <code className="font-mono text-orange-300">{app.gitBranch || 'main'}</code>
                ). Workspace files stay on disk, but the next deploy pulls from Git.
              </p>
              <label className="flex items-start gap-2.5 rounded-lg border border-white/[0.08] bg-[#09090b] p-3 text-xs text-zinc-300 cursor-pointer">
                <input
                  type="checkbox"
                  checked={forceGitSyncResume}
                  onChange={(e) => setForceGitSyncResume(e.target.checked)}
                  className="mt-0.5 rounded border-zinc-700 accent-orange-500"
                />
                <span>
                  Discard uncommitted local edits in the Git checkout if present (
                  <code className="font-mono text-[11px]">force=1</code>)
                </span>
              </label>
            </div>
          )}

          <div className="flex justify-end gap-2 border-t border-white/[0.07] pt-3">
            <button
              type="button"
              onClick={() => setGitSyncModalOpen(false)}
              className="rounded-lg border border-white/[0.08] bg-zinc-900 px-3.5 py-1.5 text-xs text-zinc-300 cursor-pointer"
            >
              Cancel
            </button>
            <button
              type="button"
              onClick={async () => {
                const nextEnabled = app.sourceType !== 'git';
                const res = await backendApi.setGitSync(
                  app.id,
                  nextEnabled,
                  nextEnabled ? forceGitSyncResume : false
                );
                if (!res.ok && !res.offlineFallback) {
                  toast.error(res.error || 'Failed to update Git sync');
                  return;
                }
                onUpdateApp({
                  ...app,
                  sourceType: nextEnabled ? 'git' : 'compose',
                  gitSync: nextEnabled,
                });
                setGitSyncModalOpen(false);
                toast.success(
                  nextEnabled
                    ? 'Git sync resumed — deployments pull from repository'
                    : 'Git sync paused — workspace files are now active'
                );
              }}
              className="rounded-lg bg-zinc-100 px-4 py-1.5 text-xs font-semibold text-zinc-950 hover:bg-white cursor-pointer"
            >
              {app.sourceType === 'git' ? 'Pause Git Sync' : 'Resume Git Sync'}
            </button>
          </div>
        </div>
      </ShadcnDialog>
    </div>
  );
};
