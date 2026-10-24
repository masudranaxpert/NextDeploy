import React, { useEffect, useMemo, useState } from 'react';
import {
  Copy,
  Plus,
  Download,
  ArrowLeftRight,
  BookOpen,
  Terminal,
  Trash2,
  FileText,
  HardDrive,
  ShieldCheck,
  CheckCircle2,
  Clock,
  Layers,
} from 'lucide-react';
import { toast } from 'sonner';
import { AppStack, MigrationExportBundle } from '../../types/nextdeploy';
import { ActionMenu, ShadcnDialog, StatusBadge } from '../../components/ui/Primitives';
import { backendApi } from '../../lib/backendApi';

interface MigrationViewProps {
  apps: AppStack[];
  bundles: MigrationExportBundle[];
  onExportBundle: (selectedSlugs: string[]) => void | Promise<void>;
  onDeleteBundle?: (id: string) => void;
}

function formatBytesHuman(bytes: number): string {
  if (bytes >= 1024 * 1024 * 1024) {
    return `${(bytes / (1024 * 1024 * 1024)).toFixed(2)} GB`;
  }
  if (bytes >= 1024 * 1024) {
    return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
  }
  if (bytes >= 1024) {
    return `${(bytes / 1024).toFixed(1)} KB`;
  }
  return `${bytes} B`;
}

export const MigrationView: React.FC<MigrationViewProps> = ({
  apps,
  bundles,
  onExportBundle,
  onDeleteBundle,
}) => {
  const [exportModalOpen, setExportModalOpen] = useState(false);
  const [docsModalOpen, setDocsModalOpen] = useState(false);
  const [docsTab, setDocsTab] = useState<'overview' | 'contents' | 'cli-flags' | 'security'>(
    'overview'
  );
  const [selectedBundleForCmd, setSelectedBundleForCmd] = useState<MigrationExportBundle | null>(
    bundles[0] || null
  );
  const [logModalBundle, setLogModalBundle] = useState<MigrationExportBundle | null>(null);
  const [selectedSlugs, setSelectedSlugs] = useState<string[]>(apps.map((a) => a.slug));
  const [liveEstimate, setLiveEstimate] = useState<{
    workspaceBytes: number;
    volumeBytes: number;
    totalBytes: number;
    totalHuman: string;
    appCount: number;
    volumeCount: number;
  } | null>(null);

  useEffect(() => {
    if (bundles.length > 0 && !selectedBundleForCmd) {
      setSelectedBundleForCmd(bundles[0]);
    }
  }, [bundles, selectedBundleForCmd]);

  // Compute local fallback + query Go backend /migrate/estimate?apps=...
  const computedEstimate = useMemo(() => {
    const chosenApps = apps.filter((a) => selectedSlugs.includes(a.slug));
    const workspaceBytes = chosenApps.reduce((acc, a) => acc + Math.max(12, a.files.length * 4) * 1024 * 1024, 0);
    const volumeBytes = chosenApps.reduce(
      (acc, a) =>
        acc +
        a.volumes.reduce((vAcc, v) => vAcc + v.sizeMB * 1024 * 1024, 0) +
        a.diskMB * 1024 * 1024 * 0.4,
      0
    );
    const totalBytes = workspaceBytes + volumeBytes;
    const volumeCount = chosenApps.reduce((acc, a) => acc + Math.max(1, a.volumes.length), 0);
    return {
      workspaceBytes,
      volumeBytes,
      totalBytes,
      totalHuman: formatBytesHuman(totalBytes),
      appCount: chosenApps.length,
      volumeCount,
    };
  }, [apps, selectedSlugs]);

  useEffect(() => {
    const selectedIds = apps.filter((a) => selectedSlugs.includes(a.slug)).map((a) => a.id);
    if (selectedIds.length === 0) {
      setLiveEstimate({
        workspaceBytes: 0,
        volumeBytes: 0,
        totalBytes: 0,
        totalHuman: '0 B',
        appCount: 0,
        volumeCount: 0,
      });
      return;
    }
    void backendApi.migrateEstimate(selectedIds).then((res) => {
      if (res.ok && res.data && typeof res.data.total_bytes === 'number') {
        setLiveEstimate({
          workspaceBytes: res.data.workspace_bytes,
          volumeBytes: res.data.volume_bytes,
          totalBytes: res.data.total_bytes,
          totalHuman: res.data.total_human || formatBytesHuman(res.data.total_bytes),
          appCount: res.data.app_count,
          volumeCount: res.data.volume_count,
        });
      } else {
        setLiveEstimate(null);
      }
    });
  }, [apps, selectedSlugs]);

  const activeEstimate = liveEstimate || computedEstimate;
  const activeBundle = selectedBundleForCmd || bundles[0];

  const handleStartExport = async () => {
    if (selectedSlugs.length === 0) {
      toast.error('Select at least one project to export');
      return;
    }
    const selectedIds = apps.filter((a) => selectedSlugs.includes(a.slug)).map((a) => a.id);
    const res = await backendApi.migrateExportStart(selectedIds);
    if (!res.ok) {
      toast.error(res.error || 'Could not start the export');
      return;
    }
    toast.success('Export started');
    onExportBundle(selectedSlugs);
    setExportModalOpen(false);
  };

  return (
    <div className="space-y-4">
      {/* Header */}
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex items-center gap-2.5">
          <div className="flex h-8 w-8 items-center justify-center rounded-lg border border-white/[0.08] bg-zinc-900">
            <ArrowLeftRight className="h-4 w-4 text-orange-400" />
          </div>
          <div className="flex items-center gap-2">
            <h1 className="text-base font-semibold tracking-tight text-zinc-100">
              Server Migration
            </h1>
            <span className="rounded-md bg-white/[0.06] px-2 py-0.5 font-mono text-[11px] text-zinc-400">
              .nd-migrate v1
            </span>
          </div>
        </div>

        <div className="flex flex-wrap items-center gap-2">
          <button
            type="button"
            onClick={() => setDocsModalOpen(true)}
            className="inline-flex h-8 items-center gap-1.5 rounded-lg border border-white/[0.08] bg-zinc-900 px-3 text-xs font-medium text-zinc-200 hover:bg-zinc-800 cursor-pointer"
          >
            <BookOpen className="h-3.5 w-3.5 text-orange-400" />
            Migration Docs & Guide
          </button>
          <button
            type="button"
            onClick={() => setExportModalOpen(true)}
            className="inline-flex h-8 items-center gap-1.5 rounded-lg bg-zinc-100 px-3.5 text-xs font-semibold text-zinc-950 hover:bg-white cursor-pointer"
          >
            <Plus className="h-3.5 w-3.5" />
            Create Export Bundle
          </button>
        </div>
      </div>

      {/* Live Target VPS One-Liner Card (Matches Go backend migrate_cmd) */}
      {activeBundle && (
        <div className="rounded-xl border border-white/[0.07] bg-[#111115]/90 p-4 space-y-3">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <div className="flex items-center gap-2">
              <Terminal className="h-4 w-4 text-emerald-400" />
              <span className="text-xs font-semibold text-zinc-100">
                Target VPS One-Command Import ({activeBundle.id})
              </span>
              <span className="rounded bg-emerald-500/10 px-1.5 py-0.5 font-mono text-[10px] text-emerald-400">
                Single-Use Token · Expires in {activeBundle.expiresIn}
              </span>
            </div>
            <div className="flex items-center gap-2">
              <button
                type="button"
                onClick={() => {
                  const url =
                    activeBundle.downloadUrl ||
                    `https://panel.nextdeploy.sh/migrate/download/${activeBundle.token}`;
                  navigator.clipboard?.writeText(url);
                  toast.success('Copied one-time download URL');
                }}
                className="rounded-lg border border-white/[0.08] bg-zinc-900 px-2.5 py-1 font-mono text-[11px] text-zinc-300 hover:text-white cursor-pointer"
              >
                Copy Download URL
              </button>
              <button
                type="button"
                onClick={() => {
                  const cmd =
                    activeBundle.migrateCmd ||
                    `curl -fsSL https://raw.githubusercontent.com/masudranaxpert/NextDeploy/main/migrate.sh | sudo bash -s -- --url "${
                      activeBundle.downloadUrl ||
                      `https://panel.nextdeploy.sh/migrate/download/${activeBundle.token}`
                    }"`;
                  navigator.clipboard?.writeText(cmd);
                  toast.success('Copied target VPS migrate.sh command');
                }}
                className="inline-flex items-center gap-1 rounded-lg bg-orange-500/15 border border-orange-500/30 px-2.5 py-1 font-mono text-[11px] font-medium text-orange-300 hover:bg-orange-500/25 cursor-pointer"
              >
                <Copy className="h-3 w-3" />
                Copy Command
              </button>
            </div>
          </div>

          <div className="rounded-lg border border-white/[0.07] bg-[#09090b] p-3 font-mono text-[11px] text-zinc-200 overflow-x-auto whitespace-nowrap">
            {activeBundle.migrateCmd ||
              `curl -fsSL https://raw.githubusercontent.com/masudranaxpert/NextDeploy/main/migrate.sh | sudo bash -s -- --url "${
                activeBundle.downloadUrl ||
                `https://panel.nextdeploy.sh/migrate/download/${activeBundle.token}`
              }"`}
          </div>
        </div>
      )}

      {/* Export Bundles Table */}
      <div className="overflow-hidden rounded-xl border border-white/[0.07] bg-[#111115]/90">
        <div className="overflow-x-auto">
          <table className="w-full min-w-[680px] table-fixed text-left text-xs">
            <thead>
              <tr className="border-b border-white/[0.07] bg-[#09090b]/60 text-zinc-400">
                <th className="w-[18%] px-4 py-2.5 font-medium">Bundle ID</th>
                <th className="w-[34%] px-4 py-2.5 font-medium">Projects & Volumes</th>
                <th className="w-[16%] px-4 py-2.5 font-medium">Status</th>
                <th className="w-[12%] px-4 py-2.5 font-medium">Expires</th>
                <th className="w-[14%] px-4 py-2.5 font-medium text-right">Archive Size</th>
                <th className="w-12 px-3 py-2.5 font-medium text-right"></th>
              </tr>
            </thead>
            <tbody className="divide-y divide-white/[0.05]">
              {bundles.map((b) => (
                <tr
                  key={b.id}
                  onClick={() => setSelectedBundleForCmd(b)}
                  className={`hover:bg-white/[0.03] cursor-pointer transition-colors ${
                    activeBundle?.id === b.id ? 'bg-white/[0.02]' : ''
                  }`}
                >
                  <td className="px-4 py-3">
                    <div className="font-mono font-semibold text-zinc-100">{b.id}</div>
                    <div className="font-mono text-[10px] text-zinc-500">{b.createdAt}</div>
                  </td>
                  <td className="px-4 py-3">
                    <div className="font-mono text-orange-400 truncate">
                      {b.selectedApps.join(', ')}
                    </div>
                    <div className="mt-0.5 font-mono text-[10px] text-zinc-500">
                      {b.selectedApps.length} project(s) ·{' '}
                      {b.workspaceBytes
                        ? `workspace ${formatBytesHuman(b.workspaceBytes)} + volumes ${formatBytesHuman(b.volumeBytes || 0)}`
                        : 'full state + volumes'}
                    </div>
                  </td>
                  <td className="px-4 py-3">
                    <StatusBadge status={b.status} />
                  </td>
                  <td className="px-4 py-3 font-mono text-[11px] text-zinc-400">
                    <span className="inline-flex items-center gap-1">
                      <Clock className="h-3 w-3 text-zinc-500" />
                      {b.expiresIn}
                    </span>
                  </td>
                  <td className="px-4 py-3 text-right font-mono tabular-nums text-zinc-200">
                    {(b.actualSizeMB / 1024).toFixed(2)} GB
                  </td>
                  <td className="px-3 py-3 text-right">
                    <ActionMenu
                      items={[
                        {
                          label: 'Copy Target VPS Command',
                          icon: <Terminal className="h-3.5 w-3.5" />,
                          onClick: () => {
                            const cmd =
                              b.migrateCmd ||
                              `curl -fsSL https://raw.githubusercontent.com/masudranaxpert/NextDeploy/main/migrate.sh | sudo bash -s -- --url "${
                                b.downloadUrl ||
                                `https://panel.nextdeploy.sh/migrate/download/${b.token}`
                              }"`;
                            navigator.clipboard?.writeText(cmd);
                            toast.success('Copied target VPS migrate.sh command');
                          },
                        },
                        {
                          label: 'Copy One-Time Token',
                          icon: <Copy className="h-3.5 w-3.5" />,
                          onClick: () => {
                            navigator.clipboard?.writeText(b.token);
                            toast.success('Copied migration token');
                          },
                        },
                        {
                          label: 'View Export Log',
                          icon: <FileText className="h-3.5 w-3.5" />,
                          onClick: () => setLogModalBundle(b),
                        },
                        {
                          label: 'Download .nd-migrate',
                          icon: <Download className="h-3.5 w-3.5" />,
                          onClick: () =>
                            toast.success(`Downloading nextdeploy-${b.id}.nd-migrate`),
                        },
                        {
                          label: 'Delete Bundle',
                          icon: <Trash2 className="h-3.5 w-3.5" />,
                          variant: 'danger',
                          onClick: () => {
                            void backendApi.migrateExportDelete(b.id);
                            if (onDeleteBundle) onDeleteBundle(b.id);
                            toast.success(`Deleted export bundle ${b.id}`);
                          },
                        },
                      ]}
                    />
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </div>

      {/* MODAL 1: Create Migration Export Bundle (with Live Size Estimate) */}
      <ShadcnDialog
        open={exportModalOpen}
        onClose={() => setExportModalOpen(false)}
        title="Create Migration Export Bundle (.nd-migrate)"
        icon={<ArrowLeftRight className="h-4 w-4" />}
        maxWidth="lg"
      >
        <div className="space-y-4">
          {/* Live Size Estimate Banner */}
          <div className="grid grid-cols-3 gap-2.5 rounded-xl border border-white/[0.08] bg-[#09090b] p-3 font-mono text-xs">
            <div>
              <span className="font-sans text-[10px] uppercase tracking-wider text-zinc-500 block">
                Workspaces
              </span>
              <span className="text-zinc-200 font-semibold mt-0.5 block">
                {formatBytesHuman(activeEstimate.workspaceBytes)} ({activeEstimate.appCount} apps)
              </span>
            </div>
            <div>
              <span className="font-sans text-[10px] uppercase tracking-wider text-zinc-500 block">
                Docker Volumes
              </span>
              <span className="text-orange-400 font-semibold mt-0.5 block">
                {formatBytesHuman(activeEstimate.volumeBytes)} ({activeEstimate.volumeCount} vols)
              </span>
            </div>
            <div>
              <span className="font-sans text-[10px] uppercase tracking-wider text-zinc-500 block">
                Estimated Bundle
              </span>
              <span className="text-emerald-400 font-semibold mt-0.5 block">
                ~{activeEstimate.totalHuman}
              </span>
            </div>
          </div>

          <div className="flex items-center justify-between text-xs">
            <span className="font-medium text-zinc-300">
              Select Projects to Include ({selectedSlugs.length} of {apps.length})
            </span>
            <div className="flex items-center gap-2">
              <button
                type="button"
                onClick={() => setSelectedSlugs(apps.map((a) => a.slug))}
                className="text-[11px] text-orange-400 hover:underline cursor-pointer"
              >
                Select All
              </button>
              <span className="text-zinc-600">·</span>
              <button
                type="button"
                onClick={() => setSelectedSlugs([])}
                className="text-[11px] text-zinc-400 hover:underline cursor-pointer"
              >
                Clear
              </button>
            </div>
          </div>

          <div className="space-y-2 max-h-60 overflow-y-auto pr-1">
            {apps.map((a) => {
              const checked = selectedSlugs.includes(a.slug);
              return (
                <label
                  key={a.id}
                  className={`flex items-center justify-between rounded-lg border px-3 py-2.5 cursor-pointer transition-colors ${
                    checked
                      ? 'border-orange-500/40 bg-orange-500/[0.06]'
                      : 'border-white/[0.08] bg-[#09090b]'
                  }`}
                >
                  <div className="min-w-0">
                    <div className="flex items-center gap-2">
                      <span className="font-mono text-xs font-semibold text-zinc-100">
                        {a.name}
                      </span>
                      <StatusBadge status={a.status} />
                    </div>
                    <div className="mt-0.5 font-mono text-[10px] text-zinc-400">
                      {a.services.length} services · {a.volumes.length} volume(s) · {a.diskMB} MB
                    </div>
                  </div>
                  <input
                    type="checkbox"
                    checked={checked}
                    onChange={() =>
                      setSelectedSlugs((prev) =>
                        checked ? prev.filter((s) => s !== a.slug) : [...prev, a.slug]
                      )
                    }
                    className="h-4 w-4 accent-orange-500 cursor-pointer"
                  />
                </label>
              );
            })}
          </div>

          <div className="rounded-lg border border-amber-500/20 bg-amber-500/[0.05] px-3 py-2 text-[11px] text-amber-200/90">
            Selected apps are briefly stopped during volume snapshot to guarantee database
            consistency, then automatically restarted on this server.
          </div>

          <div className="flex justify-end gap-2 border-t border-white/[0.07] pt-3">
            <button
              type="button"
              onClick={() => setExportModalOpen(false)}
              className="rounded-lg border border-white/[0.08] bg-zinc-900 px-3.5 py-1.5 text-xs text-zinc-300 cursor-pointer"
            >
              Cancel
            </button>
            <button
              type="button"
              onClick={handleStartExport}
              className="rounded-lg bg-zinc-100 px-4 py-1.5 text-xs font-semibold text-zinc-950 hover:bg-white cursor-pointer"
            >
              Generate .nd-migrate Bundle
            </button>
          </div>
        </div>
      </ShadcnDialog>

      {/* MODAL 2: Export Progress Log Viewer */}
      <ShadcnDialog
        open={Boolean(logModalBundle)}
        onClose={() => setLogModalBundle(null)}
        title={`Migration Export Log · ${logModalBundle?.id || ''}`}
        icon={<FileText className="h-4 w-4 text-orange-400" />}
        maxWidth="lg"
      >
        {logModalBundle && (
          <div className="space-y-3">
            <pre className="max-h-72 overflow-y-auto rounded-lg border border-white/[0.08] bg-[#070709] p-3.5 font-mono text-xs text-zinc-300 whitespace-pre-wrap">
              {logModalBundle.progressLog.join('\n')}
            </pre>
            <div className="flex justify-end">
              <button
                type="button"
                onClick={() => setLogModalBundle(null)}
                className="rounded-lg bg-zinc-100 px-4 py-1.5 text-xs font-semibold text-zinc-950 hover:bg-white cursor-pointer"
              >
                Close
              </button>
            </div>
          </div>
        )}
      </ShadcnDialog>

      {/* MODAL 3: Migration Docs & Architecture Guide */}
      <ShadcnDialog
        open={docsModalOpen}
        onClose={() => setDocsModalOpen(false)}
        title="NextDeploy Server Migration Documentation"
        description="Full-stack VPS-to-VPS transfer via signed .nd-migrate bundles & migrate.sh"
        icon={<BookOpen className="h-4 w-4 text-orange-400" />}
        maxWidth="2xl"
      >
        <div className="space-y-4">
          {/* Docs Navigation Tabs */}
          <div className="flex flex-wrap items-center gap-1 rounded-lg border border-white/[0.07] bg-[#09090b] p-1">
            {(
              [
                { id: 'overview', label: '1. Quick Start Flow' },
                { id: 'contents', label: '2. Bundle Manifest' },
                { id: 'cli-flags', label: '3. migrate.sh Flags' },
                { id: 'security', label: '4. Consistency & Security' },
              ] as const
            ).map((t) => (
              <button
                key={t.id}
                type="button"
                onClick={() => setDocsTab(t.id)}
                className={`rounded-md px-3 py-1.5 text-xs font-medium transition-colors cursor-pointer ${
                  docsTab === t.id
                    ? 'bg-zinc-800 text-zinc-100'
                    : 'text-zinc-400 hover:text-zinc-200'
                }`}
              >
                {t.label}
              </button>
            ))}
          </div>

          {docsTab === 'overview' && (
            <div className="space-y-3 text-xs leading-relaxed text-zinc-300">
              <div className="rounded-xl border border-white/[0.07] bg-[#09090b] p-3.5 space-y-2">
                <div className="font-semibold text-zinc-100 flex items-center gap-2">
                  <CheckCircle2 className="h-4 w-4 text-emerald-400" />
                  Step 1 — Generate an Export Bundle on Source VPS
                </div>
                <p className="text-zinc-400">
                  Click <strong className="text-zinc-200">Create Export Bundle</strong> and choose
                  which projects to migrate. NextDeploy estimates workspace + Docker volume sizes
                  via <code className="text-orange-300">GET /migrate/estimate</code> and packages
                  everything into a single <code className="text-orange-300">.nd-migrate</code>{' '}
                  archive.
                </p>
              </div>

              <div className="rounded-xl border border-white/[0.07] bg-[#09090b] p-3.5 space-y-2">
                <div className="font-semibold text-zinc-100 flex items-center gap-2">
                  <Terminal className="h-4 w-4 text-orange-400" />
                  Step 2 — Run the One-Line Import on Target VPS
                </div>
                <p className="text-zinc-400">
                  SSH into your new Ubuntu/Debian server. If NextDeploy is not installed yet,{' '}
                  <code className="text-orange-300">migrate.sh</code> automatically runs{' '}
                  <code className="text-orange-300">install.sh</code> first, streams the bundle over
                  HTTPS using your one-time token, restores volumes and workspaces, and brings up
                  all Compose stacks:
                </p>
                <pre className="rounded-lg border border-white/[0.08] bg-[#060608] p-3 font-mono text-[11px] text-emerald-400 overflow-x-auto">
                  {`curl -fsSL https://raw.githubusercontent.com/masudranaxpert/NextDeploy/main/migrate.sh | sudo bash -s -- \\\n  --url "https://panel.yourdomain.com/migrate/download/<one-time-token>"`}
                </pre>
              </div>

              <div className="rounded-xl border border-white/[0.07] bg-[#09090b] p-3.5 space-y-2">
                <div className="font-semibold text-zinc-100 flex items-center gap-2">
                  <HardDrive className="h-4 w-4 text-sky-400" />
                  Step 3 — Point DNS A/AAAA Records to New VPS IP
                </div>
                <p className="text-zinc-400">
                  Once <code className="text-orange-300">migrate.sh</code> completes, update your
                  domains&apos; DNS records to the new server IP. Caddy automatically provisions
                  fresh TLS 1.3 certificates for every restored domain route.
                </p>
              </div>
            </div>
          )}

          {docsTab === 'contents' && (
            <div className="space-y-3 text-xs">
              <p className="text-zinc-400">
                Every <code className="text-orange-300">.nd-migrate</code> archive (schema version{' '}
                <code className="text-zinc-200">v1</code>) contains a structured{' '}
                <code className="text-orange-300">manifest.json</code> plus compressed workspace and
                Docker volume tarballs:
              </p>
              <div className="grid grid-cols-1 gap-2.5 sm:grid-cols-2">
                {[
                  {
                    title: 'workspaces/<app_id>.tar.gz',
                    desc: 'Complete /data/apps/<id> directory including docker-compose.yml, .env secrets, Dockerfiles, and uploaded assets.',
                  },
                  {
                    title: 'volumes/<vol_name>.tar.gz',
                    desc: 'Crash-consistent tar.gz snapshots of every named Docker volume referenced by selected apps plus Caddy shared volumes.',
                  },
                  {
                    title: 'Users & Collaborators',
                    desc: 'All panel users (bcrypt password hashes, roles, status, CPU/RAM/disk quotas, cgroup limits) and per-app collaborator grants.',
                  },
                  {
                    title: 'Domains & Caddy Config',
                    desc: 'All domain routes, target ports, HTTPS/www flags, static/media file server mounts, and panel .env settings.',
                  },
                  {
                    title: 'Git Providers & Webhooks',
                    desc: 'Connected GitHub App private keys/installations and GitLab OAuth 2.0 credentials for seamless push-to-deploy continuity.',
                  },
                  {
                    title: 'Container Registries',
                    desc: 'Global and personal registry credentials (GHCR, Docker Hub, GitLab, AWS ECR, Custom) so private images pull immediately.',
                  },
                ].map((item) => (
                  <div
                    key={item.title}
                    className="rounded-xl border border-white/[0.07] bg-[#09090b] p-3 space-y-1"
                  >
                    <div className="font-mono text-xs font-semibold text-orange-400 flex items-center gap-1.5">
                      <Layers className="h-3.5 w-3.5 shrink-0" />
                      {item.title}
                    </div>
                    <p className="text-[11px] text-zinc-400 leading-relaxed">{item.desc}</p>
                  </div>
                ))}
              </div>
            </div>
          )}

          {docsTab === 'cli-flags' && (
            <div className="space-y-3 text-xs">
              <div className="overflow-hidden rounded-xl border border-white/[0.07] bg-[#09090b]">
                <table className="w-full text-left text-xs">
                  <thead>
                    <tr className="border-b border-white/[0.07] bg-white/[0.03] text-zinc-400">
                      <th className="px-3.5 py-2 font-medium">Flag / Option</th>
                      <th className="px-3.5 py-2 font-medium">Description</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-white/[0.05] font-mono text-[11px]">
                    <tr>
                      <td className="px-3.5 py-2.5 text-orange-400 whitespace-nowrap">
                        --url &lt;download_url&gt;
                      </td>
                      <td className="px-3.5 py-2.5 font-sans text-zinc-300">
                        Streams the bundle directly from the source NextDeploy panel&apos;s one-time
                        endpoint (<code className="font-mono">/migrate/download/:token</code>).
                      </td>
                    </tr>
                    <tr>
                      <td className="px-3.5 py-2.5 text-orange-400 whitespace-nowrap">
                        --file &lt;path.nd-migrate&gt;
                      </td>
                      <td className="px-3.5 py-2.5 font-sans text-zinc-300">
                        Imports a local bundle file that you downloaded from the browser or copied
                        via <code className="font-mono">scp</code> / <code className="font-mono">rsync</code>.
                      </td>
                    </tr>
                    <tr>
                      <td className="px-3.5 py-2.5 text-orange-400 whitespace-nowrap">
                        --no-deploy
                      </td>
                      <td className="px-3.5 py-2.5 font-sans text-zinc-300">
                        Restores all database records, workspaces, and Docker volumes without
                        running <code className="font-mono">docker compose up -d</code> yet.
                      </td>
                    </tr>
                    <tr>
                      <td className="px-3.5 py-2.5 text-orange-400 whitespace-nowrap">
                        --data-dir /data
                      </td>
                      <td className="px-3.5 py-2.5 font-sans text-zinc-300">
                        Host data directory where <code className="font-mono">panel.db</code> and{' '}
                        <code className="font-mono">apps/</code> live (defaults to{' '}
                        <code className="font-mono">/data</code>).
                      </td>
                    </tr>
                    <tr>
                      <td className="px-3.5 py-2.5 text-orange-400 whitespace-nowrap">
                        --container nextdeploy
                      </td>
                      <td className="px-3.5 py-2.5 font-sans text-zinc-300">
                        Name of the running NextDeploy control-plane container when running in
                        Docker mode.
                      </td>
                    </tr>
                  </tbody>
                </table>
              </div>

              <div className="rounded-xl border border-white/[0.07] bg-[#09090b] p-3 space-y-1.5">
                <div className="font-semibold text-zinc-200">
                  Direct Go Binary CLI Subcommand (Alternative)
                </div>
                <pre className="rounded-lg bg-[#060608] p-2.5 font-mono text-[11px] text-zinc-300 overflow-x-auto">
                  {`nextdeploy import --file /tmp/bundle.nd-migrate --data-dir /data`}
                </pre>
              </div>
            </div>
          )}

          {docsTab === 'security' && (
            <div className="space-y-3 text-xs text-zinc-300 leading-relaxed">
              <div className="rounded-xl border border-white/[0.07] bg-[#09090b] p-3.5 space-y-1.5">
                <div className="font-semibold text-zinc-100 flex items-center gap-2">
                  <ShieldCheck className="h-4 w-4 text-emerald-400" />
                  Single-Use SHA-256 Token & 3-Hour Expiry
                </div>
                <p className="text-zinc-400">
                  When an export finishes, NextDeploy generates a 32-byte cryptographically random
                  token and stores only its <strong className="text-zinc-200">SHA-256 hash</strong>{' '}
                  in SQLite. The download link expires after <strong className="text-zinc-200">3 hours</strong>{' '}
                  and is invalidated the moment it is downloaded once (<code className="text-orange-300">consumed_at</code>).
                </p>
              </div>

              <div className="rounded-xl border border-white/[0.07] bg-[#09090b] p-3.5 space-y-1.5">
                <div className="font-semibold text-zinc-100 flex items-center gap-2">
                  <ArrowLeftRight className="h-4 w-4 text-orange-400" />
                  Crash-Consistent Database Volume Quiescing
                </div>
                <p className="text-zinc-400">
                  Before archiving Docker volumes, the exporter stops each selected app&apos;s
                  containers so PostgreSQL, MySQL, Redis, and SQLite flush all WAL pages cleanly to
                  disk. As soon as the volume tarballs are written, the source containers are
                  automatically restarted (`docker compose up -d`).
                </p>
              </div>

              <div className="rounded-xl border border-white/[0.07] bg-[#09090b] p-3.5 space-y-1.5">
                <div className="font-semibold text-zinc-100 flex items-center gap-2">
                  <Trash2 className="h-4 w-4 text-rose-400" />
                  Automatic Superseding & Disk Cleanup
                </div>
                <p className="text-zinc-400">
                  Starting a new export automatically revokes previous unconsumed tokens and deletes
                  older <code className="text-orange-300">/data/migrations/nextdeploy-export-*.nd-migrate</code>{' '}
                  files so temporary migration bundles never fill up your disk.
                </p>
              </div>
            </div>
          )}

          <div className="flex justify-end border-t border-white/[0.07] pt-3">
            <button
              type="button"
              onClick={() => setDocsModalOpen(false)}
              className="rounded-lg bg-zinc-100 px-4 py-1.5 text-xs font-semibold text-zinc-950 hover:bg-white cursor-pointer"
            >
              Got It
            </button>
          </div>
        </div>
      </ShadcnDialog>
    </div>
  );
};
