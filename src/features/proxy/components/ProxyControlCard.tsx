import React from 'react';
import { Loader2, Network, Play, Power, RefreshCw, RotateCcw, Settings2 } from 'lucide-react';
import { ShadcnSelect } from '../../../components/ui/ShadcnSelect';
import { normalizeProxyEngine } from '../../../lib/api/proxyApi';

export type ProxyContainerActionType =
  | 'start'
  | 'force_start'
  | 'restart'
  | 'stop'
  | 'reconcile_network';

interface ProxyControlCardProps {
  engine: string;
  supportsAdminApi: boolean;
  running: boolean;
  acmeEmail: string;
  setAcmeEmail: (v: string) => void;
  proxyImage: string;
  setProxyImage: (v: string) => void;
  adminApi: string;
  setAdminApi: (v: string) => void;
  savingSettings: boolean;
  actionPending: ProxyContainerActionType | null;
  onSaveSettings: (e: React.FormEvent) => void;
  onLifecycleAction: (action: ProxyContainerActionType) => void;
}

export const ProxyControlCard: React.FC<ProxyControlCardProps> = ({
  engine,
  supportsAdminApi,
  running,
  acmeEmail,
  setAcmeEmail,
  proxyImage,
  setProxyImage,
  adminApi,
  setAdminApi,
  savingSettings,
  actionPending,
  onSaveSettings,
  onLifecycleAction,
}) => {
  const normalized = normalizeProxyEngine(engine);
  const showAdminApiField = supportsAdminApi && normalized === 'caddy';
  const showImageField = normalized === 'caddy';
  const busy = actionPending !== null;
  const runConfirmed = (action: ProxyContainerActionType, question: string) => {
    if (window.confirm(question)) onLifecycleAction(action);
  };

  return (
    <div className="rounded-xl border border-white/[0.08] bg-[#111115] p-4 sm:p-5 space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="space-y-0.5">
          <div className="flex items-center gap-2">
            <Settings2 className="h-4 w-4 text-orange-300" />
            <h2 className="text-sm font-semibold text-zinc-100">Runtime Settings &amp; Control</h2>
          </div>
          <p className="text-xs text-zinc-400">
            Configure ACME contact email and control the active reverse proxy lifecycle.
          </p>
        </div>

        <span
          className={`inline-flex items-center gap-1.5 rounded-md px-2 py-0.5 font-mono text-[11px] font-medium ring-1 ${
            running
              ? 'bg-emerald-500/10 text-emerald-300 ring-emerald-500/20'
              : 'bg-rose-500/10 text-rose-200 ring-rose-500/25'
          }`}
        >
          <span className={`h-1.5 w-1.5 rounded-full ${running ? 'bg-emerald-400' : 'bg-rose-400'}`} />
          {running ? 'Running' : 'Stopped'}
        </span>
      </div>

      <form onSubmit={onSaveSettings} className="space-y-3">
        <div className={`grid grid-cols-1 gap-3 ${showImageField || showAdminApiField ? 'md:grid-cols-2' : ''}`}>
          <div className="space-y-1.5">
            <label htmlFor="proxy-acme-email" className="block text-xs font-medium text-zinc-300">
              ACME Certificate Email
            </label>
            <input
              id="proxy-acme-email"
              type="email"
              value={acmeEmail}
              onChange={(e) => setAcmeEmail(e.target.value)}
              placeholder="admin@example.com"
              className="h-9 w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 font-mono text-xs text-zinc-100 placeholder:text-zinc-400 focus:border-orange-500/50 focus:outline-none"
            />
            <p className="text-[11px] text-zinc-500">
              Optional. Certificates are issued and renewed without it; the CA only uses it to contact you about your account.
            </p>
          </div>

          {showImageField && (
            <div className="space-y-1.5">
              <label htmlFor="proxy-container-image" className="block text-xs font-medium text-zinc-300">
                Caddy Docker Image
              </label>
              <input
                id="proxy-container-image"
                type="text"
                value={proxyImage}
                onChange={(e) => setProxyImage(e.target.value)}
                placeholder="lucaslorentz/caddy-docker-proxy:2.9-alpine"
                className="h-9 w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 font-mono text-xs text-zinc-100 placeholder:text-zinc-400 focus:border-orange-500/50 focus:outline-none"
              />
            </div>
          )}
        </div>

        {showAdminApiField ? (
          <div className="space-y-1.5">
            <label htmlFor="caddy-admin-api" className="block text-xs font-medium text-zinc-300">
              Caddy Admin API Exposure
            </label>
            <ShadcnSelect
              id="caddy-admin-api"
              value={adminApi}
              onChange={(val) => setAdminApi(String(val))}
              options={[
                { value: 'off', label: 'Disabled (off - recommended)' },
                { value: 'local', label: 'Localhost only (127.0.0.1:2019)' },
              ]}
            />
          </div>
        ) : (
          <div className="rounded-lg border border-white/[0.06] bg-[#09090b] px-3 py-2 text-[11px] text-zinc-400">
            {normalized === 'angie'
              ? 'Angie needs no admin API. Each change is written to a new config, checked with angie -t, and applied with a graceful reload, so open connections are not dropped. A config that fails the check is never applied.'
              : 'Ferron needs no admin API. Each change is written to a new config, checked, and applied; if the proxy does not come back healthy, the previous config is restored automatically.'}
          </div>
        )}

        <div className="flex flex-wrap items-center justify-between gap-3 pt-1">
          <div className="flex flex-wrap items-center gap-2">
            <button
              type="button"
              disabled={busy || running}
              title={running ? 'The proxy is already running' : 'Start the proxy container'}
              onClick={() => onLifecycleAction('start')}
              className="inline-flex items-center gap-1.5 min-h-[40px] sm:min-h-0 sm:h-8 px-3 rounded-lg border border-emerald-500/25 bg-emerald-500/10 text-xs font-medium text-emerald-300 hover:bg-emerald-500/20 disabled:opacity-50 disabled:cursor-not-allowed cursor-pointer"
            >
              {actionPending === 'start' ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Play className="h-3.5 w-3.5" />}
              Start
            </button>
            <button
              type="button"
              disabled={busy || !running}
              title={running ? 'Restart the proxy container (sites are unreachable for a few seconds)' : 'The proxy is stopped; use Start'}
              onClick={() =>
                runConfirmed('restart', 'Restart the reverse proxy? All sites are unreachable for a few seconds.')
              }
              className="inline-flex items-center gap-1.5 min-h-[40px] sm:min-h-0 sm:h-8 px-3 rounded-lg border border-white/[0.09] bg-zinc-900 text-xs font-medium text-zinc-200 hover:bg-zinc-800 disabled:opacity-50 disabled:cursor-not-allowed cursor-pointer"
            >
              <RefreshCw className={`h-3.5 w-3.5 ${actionPending === 'restart' ? 'animate-spin' : ''}`} />
              Restart
            </button>
            <button
              type="button"
              disabled={busy || !running}
              title={running ? 'Stop the proxy container' : 'The proxy is already stopped'}
              onClick={() =>
                runConfirmed('stop', 'Stop the reverse proxy? Every site and the panel domain stop answering until it is started again.')
              }
              className="inline-flex items-center gap-1.5 min-h-[40px] sm:min-h-0 sm:h-8 px-3 rounded-lg border border-rose-500/25 bg-rose-500/10 text-xs font-medium text-rose-200 hover:bg-rose-500/20 disabled:opacity-50 disabled:cursor-not-allowed cursor-pointer"
            >
              {actionPending === 'stop' ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Power className="h-3.5 w-3.5" />}
              Stop
            </button>
            <button
              type="button"
              disabled={busy}
              title="Free ports 80/443 on the host, then start the proxy"
              onClick={() =>
                runConfirmed(
                  'force_start',
                  'Force start frees ports 80 and 443 on the host: it stops and disables host nginx, apache, caddy, lighttpd and traefik and kills any other process on those ports, then starts the proxy. Continue?'
                )
              }
              className="inline-flex items-center gap-1.5 min-h-[40px] sm:min-h-0 sm:h-8 px-3 rounded-lg border border-white/[0.09] bg-zinc-900 text-xs font-medium text-zinc-300 hover:bg-zinc-800 disabled:opacity-50 disabled:cursor-not-allowed cursor-pointer"
            >
              {actionPending === 'force_start' ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <RotateCcw className="h-3.5 w-3.5" />}
              Force start
            </button>
            <button
              type="button"
              disabled={busy}
              title="Reconnect the proxy container to the panel network"
              onClick={() => onLifecycleAction('reconcile_network')}
              className="inline-flex items-center gap-1.5 min-h-[40px] sm:min-h-0 sm:h-8 px-3 rounded-lg border border-white/[0.09] bg-zinc-900 text-xs font-medium text-zinc-300 hover:bg-zinc-800 disabled:opacity-50 disabled:cursor-not-allowed cursor-pointer"
            >
              {actionPending === 'reconcile_network' ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Network className="h-3.5 w-3.5" />}
              Reconcile Network
            </button>
          </div>

          <button
            type="submit"
            disabled={savingSettings}
            className="inline-flex items-center justify-center gap-1.5 min-h-[44px] sm:min-h-0 sm:h-9 px-4 rounded-lg bg-zinc-100 text-xs font-semibold text-zinc-950 hover:bg-zinc-200 disabled:opacity-50 transition-colors cursor-pointer"
          >
            {savingSettings && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
            {savingSettings ? 'Saving...' : 'Save Runtime Settings'}
          </button>
        </div>
      </form>
    </div>
  );
};
