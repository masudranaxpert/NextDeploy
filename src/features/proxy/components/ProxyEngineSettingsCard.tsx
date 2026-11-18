import React, { useEffect, useState } from "react";
import {
  AlertTriangle,
  ArrowLeftRight,
  Check,
  CheckCircle2,
  ChevronDown,
  Cpu,
  Loader2,
  Minus,
  RefreshCw,
  Undo2,
} from "lucide-react";
import { ProxyEngineBadge } from "../../../components/proxy/ProxyEngineBadge";
import type { ProxyEngineName } from "../../../lib/api/proxyApi";
import { useProxyEngine } from "../hooks/useProxyEngine";
import { ProxySwitchModal } from "./ProxySwitchModal";

interface EngineFeature {
  label: string;
  ok: boolean;
  note?: string;
}

interface EngineCatalogItem {
  id: ProxyEngineName;
  label: string;
  tagline: string;
  description: string;
  tag?: { text: string; tone: "cyan" | "amber" | "zinc" };
  accent: { ring: string; glow: string; dot: string; text: string };
  features: EngineFeature[];
}

const ENGINE_CATALOG: EngineCatalogItem[] = [
  {
    id: "caddy",
    label: "Caddy",
    tagline: "Go · Docker labels",
    description: "Default. Highest RAM under many idle connections.",
    tag: { text: "default", tone: "zinc" },
    accent: {
      ring: "ring-emerald-500/40",
      glow: "from-emerald-500/[0.08]",
      dot: "bg-emerald-400",
      text: "text-emerald-300",
    },
    features: [
      { label: "Automatic HTTPS", ok: true },
      { label: "Reload without dropping", ok: true },
      { label: "Idle HTTP/2 timeout", ok: true },
      { label: "PHP, .htaccess, static files", ok: true },
    ],
  },
  {
    id: "angie",
    label: "Angie",
    tagline: "nginx core",
    description: "Lowest RAM. Best for 1 GB VPS.",
    tag: { text: "recommended", tone: "cyan" },
    accent: {
      ring: "ring-cyan-500/40",
      glow: "from-cyan-500/[0.08]",
      dot: "bg-cyan-400",
      text: "text-cyan-300",
    },
    features: [
      { label: "Automatic HTTPS", ok: true },
      { label: "Reload without dropping", ok: true },
      { label: "Idle HTTP/2 timeout", ok: true },
      { label: "PHP, .htaccess, static files", ok: true },
    ],
  },
  {
    id: "ferron",
    label: "Ferron",
    tagline: "Rust · experimental",
    description: "Fastest, low RAM. Still a release candidate.",
    tag: { text: "experimental", tone: "amber" },
    accent: {
      ring: "ring-amber-500/40",
      glow: "from-amber-500/[0.08]",
      dot: "bg-amber-300",
      text: "text-amber-200",
    },
    features: [
      { label: "Automatic HTTPS", ok: true },
      { label: "Reload without dropping", ok: true },
      { label: "Idle HTTP/2 timeout", ok: false },
      { label: "PHP, .htaccess, static files", ok: true },
    ],
  },
];

const TAG_TONE: Record<"cyan" | "amber" | "zinc", string> = {
  cyan: "bg-cyan-500/10 text-cyan-200 ring-1 ring-cyan-500/25",
  amber: "bg-amber-500/10 text-amber-200 ring-1 ring-amber-500/25",
  zinc: "bg-zinc-800 text-zinc-300 ring-1 ring-white/[0.08]",
};

interface ProxyEngineSettingsCardProps {
  /** Called after a successful switch so the rest of the proxy page can reload. */
  onSwitched?: () => void;
}

export const ProxyEngineSettingsCard: React.FC<
  ProxyEngineSettingsCardProps
> = ({ onSwitched }) => {
  const engine = useProxyEngine();
  const {
    status,
    activeEngine,
    loading,
    error,
    refresh,
    runPreflight,
    executeSwitch,
    resetSwitch,
    switching,
  } = engine;

  const [targetEngine, setTargetEngine] = useState<ProxyEngineName | null>(
    null,
  );
  // Frozen when the dialog opens: the live engine changes during the switch.
  const [fromEngine, setFromEngine] = useState<ProxyEngineName>(activeEngine);

  const openSwitch = (target: ProxyEngineName) => {
    resetSwitch();
    setFromEngine(activeEngine);
    setTargetEngine(target);
    void runPreflight(target);
  };

  const confirmSwitch = async (experimentalAck: boolean) => {
    if (!targetEngine) return;
    const ok = await executeSwitch(targetEngine, experimentalAck);
    if (ok) onSwitched?.();
  };

  const closeModal = () => {
    if (switching) return;
    setTargetEngine(null);
    resetSwitch();
  };

  const lastSwitch = status?.last_switch;
  const lastStatus = (lastSwitch?.status || "").toLowerCase();
  const lastFailed = lastStatus === "failed" || lastStatus === "rolled_back";
  const lastRunning = lastStatus === "running";
  const lastWhen = lastSwitch?.updated_at || lastSwitch?.timestamp;

  // Collapsed by default; opens on its own while a switch runs or after one failed.
  const [open, setOpen] = useState(false);
  const needsAttention = lastRunning || lastFailed || switching;
  useEffect(() => {
    if (needsAttention) setOpen(true);
  }, [needsAttention]);

  return (
    <section className="rounded-xl border border-white/[0.08] bg-[#111115] p-3 sm:p-4 space-y-3">
      <header className="flex items-center justify-between gap-3">
        <button
          type="button"
          onClick={() => setOpen((v) => !v)}
          aria-expanded={open}
          className="flex min-w-0 flex-1 items-center gap-3 text-left cursor-pointer min-h-[44px] sm:min-h-0"
        >
          <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg border border-white/[0.08] bg-zinc-900">
            <Cpu className="h-4 w-4 text-zinc-300" />
          </div>
          <h2 className="text-sm font-semibold text-zinc-100">Proxy engine</h2>
          {status && (
            <ProxyEngineBadge
              engine={activeEngine}
              version={status.version}
              running={status.running ?? status.up}
              experimental={status.experimental}
              size="xs"
            />
          )}
          <ChevronDown
            className={`ml-auto h-4 w-4 shrink-0 text-zinc-500 transition-transform ${open ? "rotate-180" : ""}`}
          />
        </button>

        {open && (
          <button
            type="button"
            onClick={() => void refresh()}
            disabled={loading}
            aria-label="Refresh"
            className="inline-flex h-11 w-11 sm:h-8 sm:w-8 shrink-0 items-center justify-center rounded-lg border border-white/[0.08] bg-zinc-900 text-zinc-400 hover:bg-zinc-800 hover:text-zinc-100 disabled:opacity-50 transition-colors cursor-pointer"
          >
            <RefreshCw
              className={`h-3.5 w-3.5 ${loading ? "animate-spin" : ""}`}
            />
          </button>
        )}
      </header>

      {open && (
        <div className="space-y-3">
          {error && (
            <div className="rounded-lg border border-rose-500/30 bg-rose-500/10 p-3 flex flex-wrap items-center justify-between gap-2 text-xs text-rose-200">
              <span>{error}</span>
              <button
                type="button"
                onClick={() => void refresh()}
                className="min-h-[36px] px-2.5 rounded-md border border-rose-500/30 bg-zinc-950 text-xs font-medium text-rose-200 hover:bg-zinc-900 cursor-pointer"
              >
                Retry
              </button>
            </div>
          )}

          {lastSwitch?.to && (
            <div
              className={`rounded-lg border px-3 py-2.5 text-xs flex items-start gap-2.5 ${
                lastRunning
                  ? "border-cyan-500/25 bg-cyan-500/[0.07] text-cyan-100"
                  : lastFailed
                    ? "border-rose-500/30 bg-rose-500/10 text-rose-200"
                    : "border-white/[0.06] bg-zinc-900/60 text-zinc-300"
              }`}
            >
              {lastRunning ? (
                <Loader2 className="h-4 w-4 shrink-0 mt-0.5 animate-spin text-cyan-300" />
              ) : lastFailed ? (
                lastSwitch.rolled_back ? (
                  <Undo2 className="h-4 w-4 shrink-0 mt-0.5 text-rose-300" />
                ) : (
                  <AlertTriangle className="h-4 w-4 shrink-0 mt-0.5 text-rose-300" />
                )
              ) : (
                <CheckCircle2 className="h-4 w-4 shrink-0 mt-0.5 text-emerald-300" />
              )}
              <div className="min-w-0 flex-1">
                <span className="font-medium">
                  {lastRunning
                    ? `Switching from ${lastSwitch.from || "?"} to ${lastSwitch.to}…`
                    : lastFailed
                      ? `Last switch from ${lastSwitch.from || "?"} to ${lastSwitch.to} ${
                          lastSwitch.rolled_back
                            ? "failed and was rolled back"
                            : "failed"
                        }`
                      : `Last switch: ${lastSwitch.from || "?"} → ${lastSwitch.to}`}
                </span>
                {lastFailed && lastSwitch.error && (
                  <div className="mt-0.5 text-[11px] opacity-90 break-words">
                    {lastSwitch.error}
                  </div>
                )}
              </div>
              {lastWhen && (
                <span className="shrink-0 font-mono text-[11px] opacity-70">
                  {new Date(lastWhen).toLocaleString()}
                </span>
              )}
            </div>
          )}

          {loading && !status ? (
            <div
              className="grid grid-cols-1 gap-3 md:grid-cols-3"
              aria-busy="true"
            >
              {[0, 1, 2].map((i) => (
                <div
                  key={i}
                  className="h-44 rounded-xl bg-zinc-900/70 animate-pulse"
                />
              ))}
            </div>
          ) : (
            <div className="grid grid-cols-1 gap-3 md:grid-cols-3">
              {ENGINE_CATALOG.map((item) => {
                const isCurrent = item.id === activeEngine;
                return (
                  <article
                    key={item.id}
                    className={`relative overflow-hidden rounded-xl border bg-[#0b0b0e] p-3.5 flex flex-col gap-3 transition-colors ${
                      isCurrent
                        ? `border-transparent ring-1 ${item.accent.ring}`
                        : "border-white/[0.07] hover:border-white/[0.14]"
                    }`}
                  >
                    {isCurrent && (
                      <div
                        aria-hidden
                        className={`pointer-events-none absolute inset-0 bg-gradient-to-b ${item.accent.glow} to-transparent`}
                      />
                    )}

                    <div className="relative space-y-1">
                      <div className="flex items-center justify-between gap-2">
                        <div className="flex items-center gap-2">
                          <span
                            className={`h-2 w-2 rounded-full ${item.accent.dot}`}
                          />
                          <h3 className="text-sm font-semibold text-zinc-100">
                            {item.label}
                          </h3>
                        </div>
                        {isCurrent ? (
                          <span className="inline-flex items-center gap-1 rounded-full bg-emerald-500/15 px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wide text-emerald-300">
                            <span className="h-1.5 w-1.5 rounded-full bg-emerald-400 animate-pulse" />
                            Active
                          </span>
                        ) : item.tag ? (
                          <span
                            className={`rounded-full px-2 py-0.5 text-[10px] font-medium ${TAG_TONE[item.tag.tone]}`}
                          >
                            {item.tag.text}
                          </span>
                        ) : null}
                      </div>
                      <div
                        className={`font-mono text-[11px] ${item.accent.text}`}
                      >
                        {item.tagline}
                      </div>
                    </div>

                    <p className="relative text-[12px] text-zinc-400">
                      {item.description}
                    </p>

                    <ul className="relative space-y-1">
                      {item.features.map((f) => (
                        <li
                          key={f.label}
                          className="flex items-start gap-2 text-[12px]"
                        >
                          {f.ok ? (
                            <Check className="h-3.5 w-3.5 mt-0.5 shrink-0 text-emerald-400" />
                          ) : (
                            <Minus className="h-3.5 w-3.5 mt-0.5 shrink-0 text-amber-300" />
                          )}
                          <span
                            className={
                              f.ok ? "text-zinc-300" : "text-amber-200"
                            }
                          >
                            {f.label}
                            {f.note && (
                              <span className="block text-[11px] text-zinc-500">
                                {f.note}
                              </span>
                            )}
                          </span>
                        </li>
                      ))}
                    </ul>

                    <div className="relative mt-auto pt-1">
                      <button
                        type="button"
                        disabled={
                          isCurrent || loading || switching || lastRunning
                        }
                        onClick={() => openSwitch(item.id)}
                        className={`w-full inline-flex items-center justify-center gap-1.5 min-h-[44px] sm:min-h-0 sm:h-9 px-3 rounded-lg text-xs font-semibold transition-colors ${
                          isCurrent
                            ? "border border-white/[0.08] bg-zinc-900/80 text-zinc-500 cursor-default"
                            : "bg-zinc-100 text-zinc-950 hover:bg-white disabled:opacity-45 disabled:cursor-not-allowed cursor-pointer"
                        }`}
                      >
                        {isCurrent ? (
                          <>
                            <CheckCircle2 className="h-3.5 w-3.5" />
                            In use
                          </>
                        ) : (
                          <>
                            <ArrowLeftRight className="h-3.5 w-3.5" />
                            Switch to {item.label}
                          </>
                        )}
                      </button>
                    </div>
                  </article>
                );
              })}
            </div>
          )}
        </div>
      )}

      <ProxySwitchModal
        open={Boolean(targetEngine)}
        currentEngine={fromEngine}
        targetEngine={targetEngine || "caddy"}
        state={engine}
        onRetryPreflight={() => {
          if (!targetEngine) return;
          resetSwitch();
          void runPreflight(targetEngine);
        }}
        onConfirm={confirmSwitch}
        onClose={closeModal}
      />
    </section>
  );
};
