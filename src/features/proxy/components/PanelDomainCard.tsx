import React, { useState } from 'react';
import { Check, Copy, Globe, Loader2 } from 'lucide-react';
import { ShadcnSwitch } from '../../../components/ui/Primitives';
import { copyToClipboard } from '../../../lib/clipboard';
import { toast } from 'sonner';

interface PanelDomainCardProps {
  panelDomain: string;
  setPanelDomain: (v: string) => void;
  panelEnableHttps: boolean;
  setPanelEnableHttps: (v: boolean) => void;
  panelEnableWww: boolean;
  setPanelEnableWww: (v: boolean) => void;
  serverIp: string;
  saving: boolean;
  onSave: (e: React.FormEvent) => void;
}

export const PanelDomainCard: React.FC<PanelDomainCardProps> = ({
  panelDomain,
  setPanelDomain,
  panelEnableHttps,
  setPanelEnableHttps,
  panelEnableWww,
  setPanelEnableWww,
  serverIp,
  saving,
  onSave,
}) => {
  const [copiedIp, setCopiedIp] = useState(false);

  const handleCopyIp = async () => {
    if (!serverIp) return;
    const ok = await copyToClipboard(serverIp);
    if (ok) {
      setCopiedIp(true);
      toast.success(`Copied ${serverIp}`);
      window.setTimeout(() => setCopiedIp(false), 1800);
    }
  };

  return (
    <form
      onSubmit={onSave}
      className="rounded-xl border border-white/[0.08] bg-[#111115] p-4 sm:p-5 space-y-4"
    >
      <div className="flex items-start justify-between gap-3">
        <div className="space-y-0.5">
          <div className="flex items-center gap-2">
            <Globe className="h-4 w-4 text-emerald-300" />
            <h2 className="text-sm font-semibold text-zinc-100">Panel Control Plane Domain</h2>
          </div>
          <p className="text-xs text-zinc-400">
            Bind a custom domain with automated TLS for accessing this NextDeploy panel.
          </p>
        </div>

        {serverIp && (
          <button
            type="button"
            onClick={handleCopyIp}
            title="Copy server IP for DNS A record"
            className="inline-flex items-center gap-1.5 min-h-[36px] px-2.5 rounded-lg border border-white/[0.08] bg-[#09090b] font-mono text-[11px] text-zinc-300 hover:bg-zinc-900 cursor-pointer shrink-0"
          >
            <span>{serverIp}</span>
            {copiedIp ? (
              <Check className="h-3 w-3 text-emerald-300" />
            ) : (
              <Copy className="h-3 w-3 text-zinc-400" />
            )}
          </button>
        )}
      </div>

      <div className="space-y-1.5">
        <label htmlFor="panel-domain-input" className="block text-xs font-medium text-zinc-300">
          Panel Hostname
        </label>
        <input
          id="panel-domain-input"
          type="text"
          value={panelDomain}
          onChange={(e) => setPanelDomain(e.target.value)}
          placeholder="panel.example.com"
          className="h-9 w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 font-mono text-xs text-zinc-100 placeholder:text-zinc-400 focus:border-emerald-500/50 focus:outline-none"
        />
      </div>

      <div className="flex flex-wrap items-center gap-5 rounded-lg border border-white/[0.07] bg-[#09090b] px-3.5 py-2.5">
        <ShadcnSwitch
          checked={panelEnableHttps}
          onChange={setPanelEnableHttps}
          label="Automatic HTTPS (TLS)"
        />
        <ShadcnSwitch
          checked={panelEnableWww}
          onChange={setPanelEnableWww}
          label="Redirect www subdomain"
        />
      </div>

      <div className="flex justify-end">
        <button
          type="submit"
          disabled={saving}
          className="inline-flex items-center justify-center gap-1.5 min-h-[44px] sm:min-h-0 sm:h-9 px-4 rounded-lg bg-zinc-100 text-xs font-semibold text-zinc-950 hover:bg-zinc-200 disabled:opacity-50 transition-colors cursor-pointer"
        >
          {saving && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
          {saving ? 'Applying...' : 'Save & Apply Panel Domain'}
        </button>
      </div>
    </form>
  );
};
