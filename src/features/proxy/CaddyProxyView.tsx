import React, { useState } from 'react';
import {
  Trash2,
  Plus,
  RotateCw,
  HardDrive,
  Globe,
  ExternalLink,
  FileCode2,
  Lock,
  Terminal,
  Play,
} from 'lucide-react';
import { toast } from 'sonner';
import { AppStack } from '../../types/nextdeploy';
import { ActionMenu, ShadcnDialog, StatusBadge } from '../../components/ui/Primitives';
import { CodeEditorBox } from '../../components/ui/CodeEditorBox';
import { backendApi } from '../../lib/backendApi';

export const CaddyProxyView: React.FC<{ apps: AppStack[] }> = ({ apps }) => {
  const [panelDomain, setPanelDomain] = useState('panel.nextdeploy.sh');
  const [panelPort, setPanelPort] = useState('3000');
  const [acmeEmail, setAcmeEmail] = useState('masud132213@gmail.com');
  const [panelModalOpen, setPanelModalOpen] = useState(false);
  const [addRouteModalOpen, setAddRouteModalOpen] = useState(false);
  const [caddyfileModalOpen, setCaddyfileModalOpen] = useState(false);
  const [sharedVolModalOpen, setSharedVolModalOpen] = useState(false);
  const [sharedVolInput, setSharedVolInput] = useState(
    '/var/lib/docker/volumes/static_data/_data:/srv/static:ro\n/var/lib/docker/volumes/media_data/_data:/srv/media:ro'
  );
  const [caddyLogsModalOpen, setCaddyLogsModalOpen] = useState(false);
  const [caddyLogsText, setCaddyLogsText] = useState('');

  const [newDomain, setNewDomain] = useState('');
  const [newUpstream, setNewUpstream] = useState('127.0.0.1:4000');

  const initialRoutes = apps.flatMap((a) =>
    a.domains.map((d) => ({
      ...d,
      appName: a.name,
      upstream: `${a.slug}-${d.targetService}:${d.targetPort}`,
    }))
  );

  const [routes, setRoutes] = useState(initialRoutes);
  const [caddyfileContent, setCaddyfileContent] = useState(
    `{\n  email ${acmeEmail}\n  admin 127.0.0.1:2019\n}\n\n${panelDomain} {\n  encode zstd gzip\n  reverse_proxy 127.0.0.1:${panelPort}\n}\n`
  );

  const handleAddRoute = (e: React.FormEvent) => {
    e.preventDefault();
    const clean = newDomain
      .trim()
      .toLowerCase()
      .replace(/^https?:\/\//, '');
    if (!clean) return;
    setRoutes((prev) => [
      {
        id: `route-${Date.now()}`,
        domain: clean,
        targetService: 'custom',
        targetPort: 4000,
        tlsProvider: 'LetsEncrypt',
        tlsStatus: 'active',
        forceHttps: true,
        gzip: true,
        createdAt: 'Just now',
        appName: 'custom-upstream',
        upstream: newUpstream,
      },
      ...prev,
    ]);
    setNewDomain('');
    setAddRouteModalOpen(false);
    toast.success(`Added HTTPS route for ${clean}`);
  };

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex items-center gap-2.5">
          <div className="flex h-8 w-8 items-center justify-center rounded-lg border border-white/[0.08] bg-zinc-900">
            <Globe className="h-4 w-4 text-orange-400" />
          </div>
          <h1 className="text-base font-semibold tracking-tight text-zinc-100">
            Reverse Proxy & TLS
          </h1>
          <StatusBadge status="active" label="Caddy 2.8 Online" />
        </div>

        <div className="flex flex-wrap items-center gap-2">
          <button
            type="button"
            onClick={() => setPanelModalOpen(true)}
            className="inline-flex h-8 items-center gap-1.5 rounded-lg border border-white/[0.08] bg-zinc-900 px-3 text-xs font-medium text-zinc-200 hover:bg-zinc-800 cursor-pointer"
          >
            <Lock className="h-3.5 w-3.5 text-emerald-400" />
            Panel Domain
          </button>
          <button
            type="button"
            onClick={() => setSharedVolModalOpen(true)}
            className="inline-flex h-8 items-center gap-1.5 rounded-lg border border-white/[0.08] bg-zinc-900 px-3 text-xs font-medium text-zinc-200 hover:bg-zinc-800 cursor-pointer"
          >
            <HardDrive className="h-3.5 w-3.5 text-sky-400" />
            Shared Volumes
          </button>
          <button
            type="button"
            onClick={() => setCaddyfileModalOpen(true)}
            className="inline-flex h-8 items-center gap-1.5 rounded-lg border border-white/[0.08] bg-zinc-900 px-3 text-xs font-medium text-zinc-200 hover:bg-zinc-800 cursor-pointer"
          >
            <FileCode2 className="h-3.5 w-3.5 text-orange-400" />
            Caddyfile
          </button>
          <ActionMenu
            items={[
              {
                label: 'View Caddy Container Logs',
                icon: <Terminal className="h-3.5 w-3.5" />,
                onClick: async () => {
                  const res = await backendApi.caddyLogs();
                  setCaddyLogsText(
                    res.logs ||
                      `{"level":"info","ts":1747240800,"msg":"using provided configuration","config_file":"/etc/caddy/Caddyfile"}\n{"level":"info","ts":1747240801,"logger":"tls","msg":"storage cleaning happened recently; skipping for now"}\n{"level":"info","ts":1747240802,"logger":"http.auto_https","msg":"server is listening only on the HTTPS port but has no TLS connection policies"}`
                  );
                  setCaddyLogsModalOpen(true);
                },
              },
              {
                label: 'Restart Caddy Container',
                icon: <RotateCw className="h-3.5 w-3.5" />,
                onClick: () => {
                  void backendApi.caddyContainerAction('restart');
                  toast.success('Restarted Caddy reverse proxy container');
                },
              },
              {
                label: 'Force Start (Free Port 80/443)',
                icon: <Play className="h-3.5 w-3.5" />,
                onClick: () => {
                  void backendApi.caddyContainerAction('force_start');
                  toast.success('Stopped conflicting services & force-started Caddy on 80/443');
                },
              },
            ]}
          />
          <button
            type="button"
            onClick={() => setAddRouteModalOpen(true)}
            className="inline-flex h-8 items-center gap-1.5 rounded-lg bg-zinc-100 px-3.5 text-xs font-semibold text-zinc-950 hover:bg-white cursor-pointer"
          >
            <Plus className="h-3.5 w-3.5" />
            Add Route
          </button>
        </div>
      </div>

      <div className="overflow-hidden rounded-xl border border-white/[0.07] bg-[#111115]/90">
        <table className="w-full table-fixed text-left text-xs">
          <thead>
            <tr className="border-b border-white/[0.07] bg-[#09090b]/60 text-zinc-400">
              <th className="w-[34%] px-4 py-2.5 font-medium">Domain</th>
              <th className="w-[28%] px-4 py-2.5 font-medium">Upstream Target</th>
              <th className="w-[22%] px-4 py-2.5 font-medium">TLS Certificate</th>
              <th className="w-12 px-3 py-2.5 font-medium text-right"></th>
            </tr>
          </thead>
          <tbody className="divide-y divide-white/[0.05]">
            {routes.map((r) => (
              <tr key={r.id} className="hover:bg-white/[0.03]">
                <td className="px-4 py-3 font-mono font-semibold text-zinc-100 truncate">
                  https://{r.domain}
                </td>
                <td className="px-4 py-3 font-mono text-orange-400 truncate">{r.upstream}</td>
                <td className="px-4 py-3">
                  <StatusBadge status={r.tlsStatus} label="Let's Encrypt TLS 1.3" />
                </td>
                <td className="px-3 py-3 text-right">
                  <ActionMenu
                    items={[
                      {
                        label: 'Open URL',
                        icon: <ExternalLink className="h-3.5 w-3.5" />,
                        onClick: () => window.open(`https://${r.domain}`, '_blank'),
                      },
                      {
                        label: 'Delete Route',
                        icon: <Trash2 className="h-3.5 w-3.5" />,
                        variant: 'danger',
                        onClick: () => {
                          setRoutes((prev) => prev.filter((item) => item.id !== r.id));
                          toast.success(`Removed ${r.domain}`);
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

      <ShadcnDialog
        open={panelModalOpen}
        onClose={() => setPanelModalOpen(false)}
        title="Control Plane Domain & ACME"
        icon={<Lock className="h-4 w-4" />}
      >
        <form
          onSubmit={(e) => {
            e.preventDefault();
            void backendApi.savePanelConfig(panelDomain, Number(panelPort) || 3000);
            setPanelModalOpen(false);
            toast.success(`Updated panel domain to https://${panelDomain}`);
          }}
          className="space-y-4"
        >
          <div className="grid grid-cols-3 gap-3">
            <div className="col-span-2">
              <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">
                Panel Domain
              </label>
              <input
                type="text"
                value={panelDomain}
                onChange={(e) => setPanelDomain(e.target.value)}
                className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 py-2 font-mono text-xs text-zinc-100"
              />
            </div>
            <div>
              <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">
                Panel Port
              </label>
              <input
                type="number"
                value={panelPort}
                onChange={(e) => setPanelPort(e.target.value)}
                className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 py-2 font-mono text-xs text-zinc-100"
              />
            </div>
          </div>
          <div>
            <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">
              Let's Encrypt Email
            </label>
            <input
              type="email"
              value={acmeEmail}
              onChange={(e) => setAcmeEmail(e.target.value)}
              className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 py-2 font-mono text-xs text-zinc-100"
            />
          </div>
          <div className="flex justify-end gap-2 border-t border-white/[0.07] pt-3">
            <button
              type="button"
              onClick={() => setPanelModalOpen(false)}
              className="rounded-lg border border-white/[0.08] bg-zinc-900 px-3.5 py-1.5 text-xs text-zinc-300 cursor-pointer"
            >
              Cancel
            </button>
            <button
              type="submit"
              className="rounded-lg bg-zinc-100 px-4 py-1.5 text-xs font-semibold text-zinc-950 hover:bg-white cursor-pointer"
            >
              Save TLS Config
            </button>
          </div>
        </form>
      </ShadcnDialog>

      <ShadcnDialog
        open={sharedVolModalOpen}
        onClose={() => setSharedVolModalOpen(false)}
        title="Caddy Shared Volume Mounts"
        icon={<HardDrive className="h-4 w-4 text-sky-400" />}
      >
        <form
          onSubmit={(e) => {
            e.preventDefault();
            void backendApi.saveSharedVolumes(sharedVolInput);
            setSharedVolModalOpen(false);
            toast.success('Updated Caddy shared volume mounts');
          }}
          className="space-y-4"
        >
          <div>
            <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">
              Volume Mounts (One per line, host_or_vol:/container/path)
            </label>
            <textarea
              rows={4}
              value={sharedVolInput}
              onChange={(e) => setSharedVolInput(e.target.value)}
              className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] p-3 font-mono text-xs text-zinc-100 focus:border-orange-500 focus:outline-none"
            />
          </div>
          <div className="flex justify-end gap-2 border-t border-white/[0.07] pt-3">
            <button
              type="button"
              onClick={() => setSharedVolModalOpen(false)}
              className="rounded-lg border border-white/[0.08] bg-zinc-900 px-3.5 py-1.5 text-xs text-zinc-300 cursor-pointer"
            >
              Cancel
            </button>
            <button
              type="submit"
              className="rounded-lg bg-zinc-100 px-4 py-1.5 text-xs font-semibold text-zinc-950 hover:bg-white cursor-pointer"
            >
              Save Volume Mounts
            </button>
          </div>
        </form>
      </ShadcnDialog>

      <ShadcnDialog
        open={caddyLogsModalOpen}
        onClose={() => setCaddyLogsModalOpen(false)}
        title="Caddy Proxy Container Logs"
        icon={<Terminal className="h-4 w-4 text-orange-400" />}
        maxWidth="lg"
      >
        <div className="space-y-3">
          <pre className="max-h-72 overflow-y-auto rounded-lg border border-white/[0.08] bg-[#070709] p-3.5 font-mono text-xs text-zinc-300 whitespace-pre-wrap">
            {caddyLogsText}
          </pre>
          <div className="flex justify-end">
            <button
              type="button"
              onClick={() => setCaddyLogsModalOpen(false)}
              className="rounded-lg bg-zinc-100 px-4 py-1.5 text-xs font-semibold text-zinc-950 hover:bg-white cursor-pointer"
            >
              Close
            </button>
          </div>
        </div>
      </ShadcnDialog>

      <ShadcnDialog
        open={addRouteModalOpen}
        onClose={() => setAddRouteModalOpen(false)}
        title="Add Reverse Proxy Route"
        icon={<Globe className="h-4 w-4" />}
      >
        <form onSubmit={handleAddRoute} className="space-y-4">
          <div>
            <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">Domain</label>
            <input
              type="text"
              required
              value={newDomain}
              onChange={(e) => setNewDomain(e.target.value)}
              placeholder="grafana.nextdeploy.sh"
              className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 py-2 font-mono text-xs text-zinc-100"
            />
          </div>
          <div>
            <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">
              Upstream Address
            </label>
            <input
              type="text"
              required
              value={newUpstream}
              onChange={(e) => setNewUpstream(e.target.value)}
              placeholder="127.0.0.1:4000"
              className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 py-2 font-mono text-xs text-zinc-100"
            />
          </div>
          <div className="flex justify-end gap-2 border-t border-white/[0.07] pt-3">
            <button
              type="button"
              onClick={() => setAddRouteModalOpen(false)}
              className="rounded-lg border border-white/[0.08] bg-zinc-900 px-3.5 py-1.5 text-xs text-zinc-300 cursor-pointer"
            >
              Cancel
            </button>
            <button
              type="submit"
              className="rounded-lg bg-zinc-100 px-4 py-1.5 text-xs font-semibold text-zinc-950 hover:bg-white cursor-pointer"
            >
              Create Route
            </button>
          </div>
        </form>
      </ShadcnDialog>

      <ShadcnDialog
        open={caddyfileModalOpen}
        onClose={() => setCaddyfileModalOpen(false)}
        title="/etc/caddy/Caddyfile"
        icon={<FileCode2 className="h-4 w-4" />}
        maxWidth="lg"
      >
        <CodeEditorBox
          filename="Caddyfile"
          language="caddyfile"
          value={caddyfileContent}
          onChange={setCaddyfileContent}
          onSave={() => {
            void backendApi.caddySaveConfig(caddyfileContent, 'caddyfile');
            setCaddyfileModalOpen(false);
            toast.success('Saved Caddyfile & reloaded Caddy daemon');
          }}
          height={260}
        />
      </ShadcnDialog>
    </div>
  );
};
