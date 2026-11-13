import React from 'react';

/** Helper pulse block for clean, consistent skeleton anatomy */
const SkelBlock: React.FC<{ className?: string }> = ({ className = 'h-4 w-full rounded bg-white/[0.05]' }) => (
  <div className={`animate-pulse ${className}`} />
);

/** 1. Overview Dashboard Skeleton */
export const DashboardSkeleton: React.FC = () => (
  <div className="space-y-5 nd-skel" aria-busy="true" aria-label="Loading Overview">
    {/* Live Telemetry Header */}
    <div className="flex items-center justify-between gap-3">
      <div className="flex items-center gap-2">
        <div className="h-2.5 w-2.5 rounded-full bg-emerald-500/40" />
        <div className="h-4 w-28 rounded bg-white/[0.08]" />
      </div>
      <div className="h-4 w-36 rounded bg-white/[0.04]" />
    </div>

    {/* Top 4 Stat Cards */}
    <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
      {[
        { label: 'Host Memory', val: '2.8 / 8 GB', pct: '37%' },
        { label: 'Storage (/)', val: '35.8 / 144 GB', pct: '25%' },
        { label: 'CPU Load (4 vCPU)', val: '10%', status: 'Healthy' },
        { label: 'Containers & TLS', val: '10 running', status: '7 projects' },
      ].map((_, i) => (
        <div key={i} className="h-24 rounded-2xl border border-white/[0.06] bg-[#111115]/80 p-4 flex flex-col justify-between">
          <div className="flex items-center justify-between">
            <div className="h-3 w-20 rounded bg-white/[0.05]" />
            <div className="h-4 w-10 rounded-md bg-white/[0.06]" />
          </div>
          <div className="h-6 w-28 rounded bg-white/[0.09]" />
          <div className="h-1.5 w-full rounded-full bg-white/[0.04]" />
        </div>
      ))}
    </div>

    {/* Edge Shield Ingress Banner */}
    <div className="rounded-2xl border border-white/[0.06] bg-[#111115]/80 p-4 flex items-center justify-between gap-4">
      <div className="flex items-center gap-3">
        <div className="h-8 w-8 rounded-xl bg-emerald-500/10 border border-emerald-500/20" />
        <div className="space-y-1.5">
          <div className="h-4 w-44 rounded bg-white/[0.08]" />
          <div className="h-3 w-64 rounded bg-white/[0.04]" />
        </div>
      </div>
      <div className="flex items-center gap-2">
        <div className="h-7 w-20 rounded-lg bg-white/[0.05]" />
        <div className="h-7 w-24 rounded-lg bg-white/[0.07]" />
      </div>
    </div>

    {/* Charts & System Row */}
    <div className="grid gap-4 lg:grid-cols-3">
      <div className="h-72 rounded-2xl border border-white/[0.06] bg-[#111115]/80 p-4 lg:col-span-2 space-y-3">
        <div className="flex items-center justify-between">
          <div className="h-4 w-36 rounded bg-white/[0.08]" />
          <div className="h-6 w-24 rounded-md bg-white/[0.04]" />
        </div>
        <div className="h-56 rounded-xl bg-white/[0.02] border border-white/[0.03]" />
      </div>

      <div className="h-72 rounded-2xl border border-white/[0.06] bg-[#111115]/80 p-4 space-y-3">
        <div className="flex items-center justify-between">
          <div className="h-4 w-32 rounded bg-white/[0.08]" />
          <div className="h-4 w-12 rounded bg-white/[0.04]" />
        </div>
        <div className="space-y-2.5 pt-1">
          {[1, 2, 3, 4].map((j) => (
            <div key={j} className="h-12 rounded-xl border border-white/[0.04] bg-white/[0.02] p-3 flex items-center justify-between">
              <div className="flex items-center gap-2.5">
                <div className="h-2 w-2 rounded-full bg-emerald-500/50" />
                <div className="h-3.5 w-24 rounded bg-white/[0.07]" />
              </div>
              <div className="h-4 w-14 rounded-md bg-white/[0.05]" />
            </div>
          ))}
        </div>
      </div>
    </div>
  </div>
);

/** 2. Monitoring View Skeleton */
export const MonitorSkeleton: React.FC = () => (
  <div className="space-y-5 nd-skel" aria-busy="true" aria-label="Loading Monitoring">
    {/* Header */}
    <div className="flex items-center justify-between gap-3">
      <div className="flex items-center gap-2.5">
        <div className="h-8 w-8 rounded-lg border border-white/[0.08] bg-zinc-900" />
        <div className="space-y-1">
          <div className="h-5 w-28 rounded bg-white/[0.08]" />
          <div className="h-3 w-48 rounded bg-white/[0.04]" />
        </div>
      </div>
      <div className="flex items-center gap-2">
        <div className="h-7 w-28 rounded-lg bg-emerald-500/10 border border-emerald-500/20" />
        <div className="h-7 w-8 rounded-lg bg-white/[0.06]" />
      </div>
    </div>

    {/* 3 Realtime Telemetry Charts */}
    <div className="grid gap-4 lg:grid-cols-3">
      {['CPU Load', 'Memory', 'Network & Site Requests'].map((title, i) => (
        <div key={i} className="rounded-2xl border border-white/[0.06] bg-[#111115]/80 p-4 space-y-3">
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-2">
              <span className="h-3.5 w-16 rounded bg-white/[0.07]" />
              <span className="h-4 w-10 rounded bg-white/[0.09]" />
            </div>
            <div className="h-5 w-5 rounded bg-white/[0.04]" />
          </div>
          <div className="h-36 rounded-xl bg-white/[0.02] border border-white/[0.03]" />
        </div>
      ))}
    </div>

    {/* Project Resource Usage Table */}
    <div className="rounded-2xl border border-white/[0.06] bg-[#111115]/80 overflow-hidden">
      <div className="flex items-center justify-between border-b border-white/[0.06] p-4 gap-3">
        <div className="flex items-center gap-2">
          <div className="h-4 w-40 rounded bg-white/[0.08]" />
          <div className="h-4 w-10 rounded-full bg-white/[0.04]" />
        </div>
        <div className="flex items-center gap-2">
          <div className="h-7 w-48 rounded-lg bg-white/[0.04]" />
          <div className="h-7 w-24 rounded-lg bg-white/[0.06]" />
        </div>
      </div>

      <div className="divide-y divide-white/[0.04]">
        {[1, 2, 3, 4, 5, 6].map((row) => (
          <div key={row} className="flex items-center justify-between p-4 gap-4">
            <div className="flex items-center gap-3 min-w-0 flex-1">
              <div className="h-4 w-4 rounded bg-white/[0.06]" />
              <div className="space-y-1">
                <div className="h-4 w-36 rounded bg-white/[0.08]" />
                <div className="h-3 w-28 rounded bg-white/[0.03]" />
              </div>
              <div className="h-4 w-14 rounded-md bg-white/[0.04]" />
            </div>
            <div className="h-5 w-16 rounded-full bg-emerald-500/10" />
            <div className="h-4 w-12 rounded bg-white/[0.06]" />
            <div className="h-4 w-16 rounded bg-white/[0.06]" />
            <div className="h-4 w-16 rounded bg-white/[0.06]" />
          </div>
        ))}
      </div>
    </div>
  </div>
);

/** 3. Projects List Skeleton */
export const ProjectsListSkeleton: React.FC = () => (
  <div className="space-y-5 nd-skel overflow-hidden" aria-busy="true" aria-label="Loading Projects">
    {/* Header */}
    <div className="flex flex-wrap items-center justify-between gap-3">
      <div className="flex items-center gap-2.5">
        <div className="h-8 w-8 rounded-lg border border-white/[0.08] bg-zinc-900" />
        <div className="flex items-center gap-2">
          <div className="h-5 w-24 rounded bg-white/[0.08]" />
          <div className="h-4 w-8 rounded-md bg-white/[0.06]" />
        </div>
      </div>
      <div className="h-9 w-32 rounded-lg bg-white/[0.09]" />
    </div>

    {/* Search & Filter Bar */}
    <div className="flex flex-wrap items-center justify-between gap-3">
      <div className="h-9 w-full max-w-64 rounded-lg bg-white/[0.04]" />
      <div className="flex items-center gap-2">
        <div className="h-8 w-20 rounded-lg bg-white/[0.04]" />
        <div className="h-8 w-20 rounded-lg bg-white/[0.04]" />
      </div>
    </div>

    {/* Project Cards Grid */}
    <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
      {[1, 2, 3, 4, 5, 6].map((i) => (
        <div key={i} className="rounded-2xl border border-white/[0.06] bg-[#111115]/80 p-4 space-y-4">
          <div className="flex items-start justify-between">
            <div className="flex items-center gap-3">
              <div className="h-9 w-9 rounded-xl bg-white/[0.06] border border-white/[0.08]" />
              <div className="space-y-1.5">
                <div className="h-4 w-32 rounded bg-white/[0.09]" />
                <div className="h-3 w-20 rounded bg-white/[0.04]" />
              </div>
            </div>
            <div className="h-5 w-16 rounded-full bg-emerald-500/10" />
          </div>
          <div className="h-3 w-48 rounded bg-white/[0.04]" />
          <div className="pt-2 border-t border-white/[0.04] flex items-center justify-between">
            <div className="flex items-center gap-2">
              <div className="h-3 w-12 rounded bg-white/[0.04]" />
              <div className="h-3 w-14 rounded bg-white/[0.04]" />
            </div>
            <div className="h-7 w-20 rounded-lg bg-white/[0.06]" />
          </div>
        </div>
      ))}
    </div>
  </div>
);

/** 4. Workspace Skeleton (Selected App) */
export const WorkspaceSkeleton: React.FC = () => (
  <div className="space-y-4 nd-skel overflow-hidden" aria-busy="true" aria-label="Loading Project Workspace">
    {/* Hero + Wrapped Tab Nav inside card */}
    <div className="rounded-2xl border border-white/[0.06] bg-[#111115]/80 p-5 space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="space-y-2">
          <div className="flex items-center gap-2.5">
            <div className="h-5 w-44 rounded bg-white/[0.09]" />
            <div className="h-5 w-16 rounded-full bg-emerald-500/10" />
          </div>
          <div className="h-3.5 w-52 max-w-full rounded bg-white/[0.04]" />
        </div>
        <div className="flex items-center gap-2">
          <div className="h-8 w-20 rounded-lg bg-white/[0.05]" />
          <div className="h-8 w-24 rounded-lg bg-emerald-500/20" />
        </div>
      </div>

      <div className="flex flex-wrap gap-2 pt-1">
        {['Overview', 'Services', 'Domains', 'Environment', 'Backups', 'Deploy Logs', 'Settings'].map((t, i) => (
          <div key={i} className="h-7 w-22 rounded-lg bg-white/[0.04]" />
        ))}
      </div>
    </div>

    {/* Workspace Body */}
    <div className="h-80 rounded-2xl border border-white/[0.06] bg-[#111115]/80 p-5 space-y-4">
      <div className="flex items-center justify-between">
        <div className="h-4 w-36 rounded bg-white/[0.07]" />
        <div className="h-6 w-20 rounded bg-white/[0.04]" />
      </div>
      <div className="h-56 rounded-xl bg-white/[0.02] border border-white/[0.03]" />
    </div>
  </div>
);

/** 5. Shield Nodes Skeleton */
export const ShieldProxiesSkeleton: React.FC = () => (
  <div className="space-y-5 nd-skel" aria-busy="true" aria-label="Loading Shield Nodes">
    {/* Header */}
    <div className="flex items-center justify-between gap-3">
      <div className="flex items-center gap-2.5">
        <div className="h-8 w-8 rounded-lg border border-white/[0.08] bg-zinc-900" />
        <div className="space-y-1">
          <div className="h-5 w-44 rounded bg-white/[0.09]" />
          <div className="h-3 w-56 rounded bg-white/[0.04]" />
        </div>
      </div>
      <div className="h-9 w-36 rounded-lg bg-emerald-500/20 border border-emerald-500/30" />
    </div>

    {/* 3 Metric Cards */}
    <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
      {[1, 2, 3].map((i) => (
        <div key={i} className="h-20 rounded-2xl border border-white/[0.06] bg-[#111115]/80 p-4 space-y-2">
          <div className="h-3 w-28 rounded bg-white/[0.05]" />
          <div className="h-6 w-36 rounded bg-white/[0.08]" />
        </div>
      ))}
    </div>

    {/* Setup Guide Box */}
    <div className="rounded-2xl border border-white/[0.06] bg-[#111115]/80 p-4 space-y-3">
      <div className="h-4 w-40 rounded bg-white/[0.08]" />
      <div className="grid grid-cols-1 sm:grid-cols-4 gap-3">
        {[1, 2, 3, 4].map((s) => (
          <div key={s} className="h-24 rounded-xl border border-white/[0.04] bg-white/[0.02] p-3 space-y-2">
            <div className="h-3 w-16 rounded bg-white/[0.06]" />
            <div className="h-10 rounded bg-white/[0.03]" />
          </div>
        ))}
      </div>
    </div>

    {/* Node Cards List */}
    <div className="rounded-2xl border border-white/[0.06] bg-[#111115]/80 p-4 space-y-3">
      <div className="flex items-center justify-between">
        <div className="h-4 w-36 rounded bg-white/[0.08]" />
        <div className="h-4 w-20 rounded bg-white/[0.04]" />
      </div>
      {[1, 2].map((n) => (
        <div key={n} className="rounded-xl border border-white/[0.05] bg-white/[0.02] p-4 flex items-center justify-between gap-4">
          <div className="flex items-center gap-3">
            <div className="h-3 w-3 rounded-full bg-emerald-500/40" />
            <div className="space-y-1">
              <div className="h-4 w-32 rounded bg-white/[0.08]" />
              <div className="h-3 w-48 rounded bg-white/[0.04]" />
            </div>
          </div>
          <div className="flex items-center gap-2">
            <div className="h-8 w-20 rounded-lg bg-white/[0.05]" />
            <div className="h-8 w-20 rounded-lg bg-white/[0.05]" />
          </div>
        </div>
      ))}
    </div>
  </div>
);

/** 6. Proxy Routes Skeleton */
export const ProxyRoutesSkeleton: React.FC = () => (
  <div className="space-y-5 nd-skel" aria-busy="true" aria-label="Loading Proxy Routes">
    <div className="flex items-center justify-between gap-3">
      <div className="flex items-center gap-2.5">
        <div className="h-8 w-8 rounded-lg border border-white/[0.08] bg-zinc-900" />
        <div className="space-y-1">
          <div className="h-5 w-36 rounded bg-white/[0.09]" />
          <div className="h-3 w-60 rounded bg-white/[0.04]" />
        </div>
      </div>
      <div className="h-9 w-32 rounded-lg bg-emerald-500/20" />
    </div>

    {/* SSRF & Shield Notice Banner */}
    <div className="h-16 rounded-2xl border border-emerald-500/20 bg-emerald-500/5 p-4 flex items-center gap-3">
      <div className="h-6 w-6 rounded-lg bg-emerald-500/20" />
      <div className="space-y-1">
        <div className="h-3.5 w-48 rounded bg-emerald-400/40" />
        <div className="h-2.5 w-80 rounded bg-emerald-400/20" />
      </div>
    </div>

    {/* Routes Table */}
    <div className="rounded-2xl border border-white/[0.06] bg-[#111115]/80 overflow-hidden">
      <div className="flex items-center justify-between border-b border-white/[0.06] p-4">
        <div className="h-8 w-56 rounded-lg bg-white/[0.04]" />
        <div className="flex gap-2">
          <div className="h-7 w-16 rounded-md bg-white/[0.05]" />
          <div className="h-7 w-16 rounded-md bg-white/[0.03]" />
        </div>
      </div>
      <div className="divide-y divide-white/[0.04]">
        {[1, 2, 3, 4, 5].map((r) => (
          <div key={r} className="p-4 flex items-center justify-between gap-4">
            <div className="space-y-1.5 flex-1">
              <div className="flex items-center gap-2">
                <div className="h-4 w-40 rounded bg-white/[0.09]" />
                <div className="h-4 w-14 rounded-md bg-emerald-500/10" />
              </div>
              <div className="h-3 w-64 rounded bg-white/[0.04]" />
            </div>
            <div className="h-5 w-24 rounded bg-white/[0.05]" />
            <div className="h-6 w-12 rounded-full bg-white/[0.08]" />
            <div className="flex gap-1.5">
              <div className="h-7 w-7 rounded bg-white/[0.04]" />
              <div className="h-7 w-7 rounded bg-white/[0.04]" />
            </div>
          </div>
        ))}
      </div>
    </div>
  </div>
);

/** 7. Caddy Options Skeleton */
export const CaddyOptionsSkeleton: React.FC = () => (
  <div className="space-y-5 nd-skel" aria-busy="true" aria-label="Loading Caddy Options">
    <div className="flex items-center justify-between">
      <div className="space-y-1">
        <div className="h-5 w-40 rounded bg-white/[0.09]" />
        <div className="h-3 w-56 rounded bg-white/[0.04]" />
      </div>
      <div className="h-9 w-28 rounded-lg bg-emerald-500/20" />
    </div>

    {/* 4 Profile Cards */}
    <div className="grid grid-cols-2 sm:grid-cols-4 gap-3">
      {['Default', 'Balanced', 'High traffic', 'Strict'].map((p, i) => (
        <div key={i} className="h-24 rounded-2xl border border-white/[0.06] bg-[#111115]/80 p-4 space-y-2">
          <div className="h-4 w-20 rounded bg-white/[0.08]" />
          <div className="h-3 w-full rounded bg-white/[0.04]" />
        </div>
      ))}
    </div>

    {/* Form Card */}
    <div className="rounded-2xl border border-white/[0.06] bg-[#111115]/80 p-5 space-y-4">
      <div className="h-4 w-36 rounded bg-white/[0.08]" />
      <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
        {[1, 2, 3, 4, 5, 6].map((f) => (
          <div key={f} className="space-y-1.5">
            <div className="h-3 w-24 rounded bg-white/[0.05]" />
            <div className="h-9 rounded-lg bg-white/[0.03] border border-white/[0.05]" />
          </div>
        ))}
      </div>
    </div>
  </div>
);

/** 8. PHP Sites Skeleton */
export const PhpSitesSkeleton: React.FC = () => (
  <div className="space-y-5 nd-skel" aria-busy="true" aria-label="Loading PHP Sites">
    <div className="flex items-center justify-between gap-3">
      <div className="flex items-center gap-2.5">
        <div className="h-8 w-8 rounded-lg border border-white/[0.08] bg-zinc-900" />
        <div className="space-y-1">
          <div className="h-5 w-28 rounded bg-white/[0.09]" />
          <div className="h-3 w-48 rounded bg-white/[0.04]" />
        </div>
      </div>
      <div className="h-9 w-32 rounded-lg bg-emerald-500/20" />
    </div>

    {/* Runtime Chips */}
    <div className="flex items-center gap-2">
      {['PHP 8.3', 'PHP 8.2', 'PHP 7.4'].map((v, i) => (
        <div key={i} className="h-7 w-24 rounded-lg border border-white/[0.06] bg-white/[0.03] px-3 py-1" />
      ))}
    </div>

    {/* Sites Table */}
    <div className="rounded-2xl border border-white/[0.06] bg-[#111115]/80 overflow-hidden">
      <div className="p-4 border-b border-white/[0.06] flex items-center justify-between">
        <div className="h-8 w-60 rounded-lg bg-white/[0.04]" />
        <div className="h-8 w-24 rounded-lg bg-white/[0.06]" />
      </div>
      <div className="divide-y divide-white/[0.04]">
        {[1, 2, 3, 4].map((s) => (
          <div key={s} className="p-4 flex items-center justify-between gap-4">
            <div className="flex items-center gap-3">
              <div className="h-8 w-8 rounded-lg bg-violet-500/10 border border-violet-500/20" />
              <div className="space-y-1">
                <div className="h-4 w-36 rounded bg-white/[0.09]" />
                <div className="h-3 w-48 rounded bg-white/[0.04]" />
              </div>
            </div>
            <div className="h-5 w-16 rounded-md bg-violet-500/10" />
            <div className="h-5 w-16 rounded-md bg-white/[0.04]" />
            <div className="h-5 w-16 rounded-full bg-emerald-500/10" />
            <div className="h-8 w-8 rounded-lg bg-white/[0.04]" />
          </div>
        ))}
      </div>
    </div>
  </div>
);

/** 9. Docker Containers Skeleton */
export const ContainersSkeleton: React.FC = () => (
  <div className="space-y-5 nd-skel" aria-busy="true" aria-label="Loading Containers">
    <div className="flex items-center justify-between gap-3">
      <div className="flex items-center gap-2">
        <div className="h-8 w-8 rounded-lg border border-white/[0.08] bg-zinc-900" />
        <div className="h-5 w-32 rounded bg-white/[0.09]" />
      </div>
      <div className="h-9 w-28 rounded-lg bg-rose-500/10 border border-rose-500/20" />
    </div>

    <div className="flex items-center justify-between gap-3">
      <div className="h-9 w-64 rounded-lg bg-white/[0.04]" />
      <div className="flex gap-1.5">
        <div className="h-8 w-16 rounded-lg bg-white/[0.06]" />
        <div className="h-8 w-16 rounded-lg bg-white/[0.03]" />
      </div>
    </div>

    <div className="rounded-2xl border border-white/[0.06] bg-[#111115]/80 overflow-hidden divide-y divide-white/[0.04]">
      {[1, 2, 3, 4, 5, 6].map((c) => (
        <div key={c} className="p-4 flex items-center justify-between gap-4">
          <div className="flex items-center gap-3">
            <div className="h-3 w-3 rounded-full bg-emerald-500/40" />
            <div className="space-y-1">
              <div className="h-4 w-40 rounded bg-white/[0.09]" />
              <div className="h-3 w-28 rounded bg-white/[0.04]" />
            </div>
          </div>
          <div className="h-4 w-28 rounded bg-white/[0.04]" />
          <div className="h-5 w-20 rounded-md bg-white/[0.04]" />
          <div className="h-8 w-20 rounded-lg bg-white/[0.04]" />
        </div>
      ))}
    </div>
  </div>
);

/** 10. Docker Images Skeleton */
export const ImagesSkeleton: React.FC = () => (
  <div className="space-y-5 nd-skel" aria-busy="true" aria-label="Loading Images">
    <div className="flex items-center justify-between gap-3">
      <div className="flex items-center gap-2">
        <div className="h-8 w-8 rounded-lg border border-white/[0.08] bg-zinc-900" />
        <div className="h-5 w-24 rounded bg-white/[0.09]" />
      </div>
      <div className="flex items-center gap-2">
        <div className="h-9 w-24 rounded-lg bg-white/[0.08]" />
        <div className="h-9 w-32 rounded-lg bg-rose-500/10 border border-rose-500/20" />
      </div>
    </div>

    <div className="flex items-center justify-between gap-3">
      <div className="h-9 w-64 rounded-lg bg-white/[0.04]" />
      <div className="flex gap-1.5">
        <div className="h-8 w-16 rounded-lg bg-white/[0.06]" />
        <div className="h-8 w-16 rounded-lg bg-white/[0.03]" />
      </div>
    </div>

    <div className="rounded-2xl border border-white/[0.06] bg-[#111115]/80 overflow-hidden divide-y divide-white/[0.04]">
      {[1, 2, 3, 4, 5].map((img) => (
        <div key={img} className="p-4 flex items-center justify-between gap-4">
          <div className="flex items-center gap-3">
            <div className="h-7 w-7 rounded-lg bg-white/[0.06]" />
            <div className="space-y-1">
              <div className="h-4 w-48 rounded bg-white/[0.09]" />
              <div className="h-3 w-24 rounded bg-white/[0.04]" />
            </div>
          </div>
          <div className="h-5 w-20 rounded-full bg-white/[0.05]" />
          <div className="h-4 w-20 rounded bg-white/[0.04]" />
          <div className="h-4 w-16 rounded bg-white/[0.08]" />
          <div className="h-7 w-7 rounded bg-white/[0.04]" />
        </div>
      ))}
    </div>
  </div>
);

/** 11. Docker Volumes Skeleton */
export const VolumesSkeleton: React.FC = () => (
  <div className="space-y-5 nd-skel" aria-busy="true" aria-label="Loading Volumes">
    <div className="flex items-center justify-between gap-3">
      <div className="flex items-center gap-2">
        <div className="h-8 w-8 rounded-lg border border-white/[0.08] bg-zinc-900" />
        <div className="h-5 w-28 rounded bg-white/[0.09]" />
      </div>
      <div className="h-9 w-28 rounded-lg bg-rose-500/10 border border-rose-500/20" />
    </div>

    {/* Volume Storage Progress Bar Card */}
    <div className="h-20 rounded-2xl border border-white/[0.06] bg-[#111115]/80 p-4 space-y-2">
      <div className="flex justify-between">
        <div className="h-3.5 w-32 rounded bg-white/[0.06]" />
        <div className="h-3.5 w-24 rounded bg-white/[0.06]" />
      </div>
      <div className="h-2 w-full rounded-full bg-white/[0.04]" />
    </div>

    <div className="rounded-2xl border border-white/[0.06] bg-[#111115]/80 overflow-hidden divide-y divide-white/[0.04]">
      {[1, 2, 3, 4, 5].map((v) => (
        <div key={v} className="p-4 flex items-center justify-between gap-4">
          <div className="space-y-1">
            <div className="h-4 w-44 rounded bg-white/[0.09]" />
            <div className="h-3 w-32 rounded bg-white/[0.04]" />
          </div>
          <div className="h-4 w-16 rounded bg-white/[0.08]" />
          <div className="h-7 w-7 rounded bg-white/[0.04]" />
        </div>
      ))}
    </div>
  </div>
);

/** 12. Backups View Skeleton */
export const BackupsSkeleton: React.FC = () => (
  <div className="space-y-5 nd-skel" aria-busy="true" aria-label="Loading Backups">
    <div className="flex items-center justify-between gap-3">
      <div className="flex items-center gap-2">
        <div className="h-8 w-8 rounded-lg border border-white/[0.08] bg-zinc-900" />
        <div className="h-5 w-32 rounded bg-white/[0.09]" />
      </div>
      <div className="h-9 w-36 rounded-lg bg-emerald-500/20" />
    </div>

    <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
      <div className="rounded-2xl border border-white/[0.06] bg-[#111115]/80 p-4 space-y-3">
        <div className="h-4 w-36 rounded bg-white/[0.08]" />
        <div className="h-20 rounded-xl bg-white/[0.02] border border-white/[0.04]" />
      </div>
      <div className="rounded-2xl border border-white/[0.06] bg-[#111115]/80 p-4 space-y-3">
        <div className="h-4 w-36 rounded bg-white/[0.08]" />
        <div className="h-20 rounded-xl bg-white/[0.02] border border-white/[0.04]" />
      </div>
    </div>

    <div className="rounded-2xl border border-white/[0.06] bg-[#111115]/80 p-4 space-y-3">
      <div className="h-4 w-36 rounded bg-white/[0.08]" />
      <div className="space-y-2">
        {[1, 2, 3].map((b) => (
          <div key={b} className="h-12 rounded-xl border border-white/[0.04] bg-white/[0.02] p-3 flex justify-between items-center">
            <div className="h-4 w-40 rounded bg-white/[0.07]" />
            <div className="h-4 w-20 rounded bg-white/[0.04]" />
          </div>
        ))}
      </div>
    </div>
  </div>
);

/** 13. Server Migration Skeleton */
export const MigrationSkeleton: React.FC = () => (
  <div className="space-y-5 nd-skel" aria-busy="true" aria-label="Loading Server Migration">
    <div className="flex items-center justify-between gap-3">
      <div className="flex items-center gap-2.5">
        <div className="h-8 w-8 rounded-lg border border-white/[0.08] bg-zinc-900" />
        <div className="space-y-1">
          <div className="h-5 w-40 rounded bg-white/[0.09]" />
          <div className="h-3 w-56 rounded bg-white/[0.04]" />
        </div>
      </div>
      <div className="flex gap-2">
        <div className="h-9 w-28 rounded-lg bg-white/[0.06]" />
        <div className="h-9 w-28 rounded-lg bg-emerald-500/20" />
      </div>
    </div>

    {/* Wizard Step Bar */}
    <div className="h-16 rounded-2xl border border-white/[0.06] bg-[#111115]/80 p-4 flex items-center justify-between">
      {[1, 2, 3].map((step) => (
        <div key={step} className="flex items-center gap-2">
          <div className="h-6 w-6 rounded-full bg-white/[0.07]" />
          <div className="h-3.5 w-24 rounded bg-white/[0.06]" />
        </div>
      ))}
    </div>

    {/* Migration Content */}
    <div className="h-72 rounded-2xl border border-white/[0.06] bg-[#111115]/80 p-5 space-y-4">
      <div className="h-4 w-44 rounded bg-white/[0.08]" />
      <div className="h-48 rounded-xl bg-white/[0.02] border border-white/[0.03]" />
    </div>
  </div>
);

/** 14. Git & Registries Skeleton */
export const GitAndRegistriesSkeleton: React.FC = () => (
  <div className="space-y-5 nd-skel" aria-busy="true" aria-label="Loading Git & Registries">
    <div className="flex items-center justify-between gap-3">
      <div className="flex items-center gap-2">
        <div className="h-8 w-8 rounded-lg border border-white/[0.08] bg-zinc-900" />
        <div className="h-5 w-48 rounded bg-white/[0.09]" />
      </div>
      <div className="h-9 w-32 rounded-lg bg-white/[0.08]" />
    </div>

    <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
      <div className="rounded-2xl border border-white/[0.06] bg-[#111115]/80 p-5 space-y-3">
        <div className="h-4 w-36 rounded bg-white/[0.08]" />
        <div className="space-y-2">
          {[1, 2, 3].map((g) => (
            <div key={g} className="h-14 rounded-xl border border-white/[0.04] bg-white/[0.02] p-3 flex justify-between items-center">
              <div className="h-4 w-32 rounded bg-white/[0.07]" />
              <div className="h-7 w-20 rounded bg-white/[0.05]" />
            </div>
          ))}
        </div>
      </div>

      <div className="rounded-2xl border border-white/[0.06] bg-[#111115]/80 p-5 space-y-3">
        <div className="h-4 w-36 rounded bg-white/[0.08]" />
        <div className="space-y-2">
          {[1, 2].map((r) => (
            <div key={r} className="h-14 rounded-xl border border-white/[0.04] bg-white/[0.02] p-3 flex justify-between items-center">
              <div className="h-4 w-32 rounded bg-white/[0.07]" />
              <div className="h-7 w-20 rounded bg-white/[0.05]" />
            </div>
          ))}
        </div>
      </div>
    </div>
  </div>
);

/** 15. CLI & MCP Skeleton */
export const McpAndCliSkeleton: React.FC = () => (
  <div className="space-y-5 nd-skel" aria-busy="true" aria-label="Loading CLI & MCP">
    <div className="flex items-center gap-2">
      <div className="h-8 w-8 rounded-lg border border-white/[0.08] bg-zinc-900" />
      <div className="h-5 w-44 rounded bg-white/[0.09]" />
    </div>

    {/* CLI Installation Box */}
    <div className="rounded-2xl border border-white/[0.06] bg-[#111115]/80 p-5 space-y-3">
      <div className="h-4 w-32 rounded bg-white/[0.08]" />
      <div className="h-12 rounded-xl bg-black border border-white/[0.06] p-3 flex items-center justify-between">
        <div className="h-3.5 w-80 rounded bg-white/[0.06]" />
        <div className="h-7 w-16 rounded bg-white/[0.08]" />
      </div>
    </div>

    {/* API Tokens */}
    <div className="rounded-2xl border border-white/[0.06] bg-[#111115]/80 p-5 space-y-3">
      <div className="flex justify-between items-center">
        <div className="h-4 w-36 rounded bg-white/[0.08]" />
        <div className="h-8 w-28 rounded-lg bg-white/[0.06]" />
      </div>
      <div className="space-y-2">
        {[1, 2].map((t) => (
          <div key={t} className="h-12 rounded-xl border border-white/[0.04] bg-white/[0.02] p-3 flex justify-between items-center">
            <div className="h-4 w-40 rounded bg-white/[0.07]" />
            <div className="h-6 w-16 rounded bg-white/[0.04]" />
          </div>
        ))}
      </div>
    </div>
  </div>
);

/** 16. Team & Users Skeleton */
export const UsersSkeleton: React.FC = () => (
  <div className="space-y-5 nd-skel" aria-busy="true" aria-label="Loading Team & Quotas">
    <div className="flex items-center justify-between gap-3">
      <div className="flex items-center gap-2">
        <div className="h-8 w-8 rounded-lg border border-white/[0.08] bg-zinc-900" />
        <div className="h-5 w-40 rounded bg-white/[0.09]" />
      </div>
      <div className="h-9 w-28 rounded-lg bg-emerald-500/20" />
    </div>

    {/* Resource Allocation Bars */}
    <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
      {['Memory Allocation', 'CPU Cores', 'Disk Storage Quota'].map((q, i) => (
        <div key={i} className="rounded-2xl border border-white/[0.06] bg-[#111115]/80 p-4 space-y-2">
          <div className="flex justify-between">
            <div className="h-3 w-28 rounded bg-white/[0.05]" />
            <div className="h-3 w-10 rounded bg-white/[0.05]" />
          </div>
          <div className="h-2 w-full rounded-full bg-white/[0.04]" />
        </div>
      ))}
    </div>

    {/* Users Table */}
    <div className="rounded-2xl border border-white/[0.06] bg-[#111115]/80 overflow-hidden divide-y divide-white/[0.04]">
      {[1, 2, 3].map((u) => (
        <div key={u} className="p-4 flex items-center justify-between gap-4">
          <div className="flex items-center gap-3">
            <div className="h-8 w-8 rounded-full bg-white/[0.06]" />
            <div className="space-y-1">
              <div className="h-4 w-32 rounded bg-white/[0.09]" />
              <div className="h-3 w-40 rounded bg-white/[0.04]" />
            </div>
          </div>
          <div className="h-5 w-16 rounded-md bg-white/[0.05]" />
          <div className="h-4 w-24 rounded bg-white/[0.04]" />
          <div className="h-8 w-20 rounded-lg bg-white/[0.04]" />
        </div>
      ))}
    </div>
  </div>
);

/** 17. System Settings Skeleton */
export const SettingsSkeleton: React.FC = () => (
  <div className="space-y-5 nd-skel" aria-busy="true" aria-label="Loading Settings">
    <div className="flex items-center justify-between">
      <div className="space-y-1">
        <div className="h-5 w-36 rounded bg-white/[0.09]" />
        <div className="h-3 w-48 rounded bg-white/[0.04]" />
      </div>
      <div className="h-9 w-28 rounded-lg bg-emerald-500/20" />
    </div>

    {[1, 2, 3].map((s) => (
      <div key={s} className="rounded-2xl border border-white/[0.06] bg-[#111115]/80 p-5 space-y-3">
        <div className="h-4 w-44 rounded bg-white/[0.08]" />
        <div className="h-3 w-72 rounded bg-white/[0.04]" />
        <div className="pt-2 flex justify-between items-center">
          <div className="h-9 w-40 rounded-lg bg-white/[0.04]" />
          <div className="h-6 w-12 rounded-full bg-white/[0.08]" />
        </div>
      </div>
    ))}
  </div>
);

/** 18. NextDeploy AI Copilot Skeleton */
export const NextDeployAiSkeleton: React.FC = () => (
  <div className="h-full flex flex-col justify-between p-6 nd-skel" aria-busy="true" aria-label="Loading ND AI">
    <div className="flex items-center justify-between border-b border-white/[0.06] pb-4">
      <div className="flex items-center gap-2.5">
        <div className="h-7 w-7 rounded-lg bg-violet-500/20" />
        <div className="h-4 w-32 rounded bg-white/[0.09]" />
      </div>
      <div className="h-7 w-20 rounded-lg bg-white/[0.04]" />
    </div>

    <div className="space-y-4 my-auto max-w-2xl mx-auto w-full">
      <div className="flex gap-3">
        <div className="h-8 w-8 rounded-full bg-violet-500/20 shrink-0" />
        <div className="h-20 w-80 rounded-2xl bg-white/[0.04] p-4" />
      </div>
      <div className="flex gap-3 justify-end">
        <div className="h-12 w-64 rounded-2xl bg-orange-500/10 p-3" />
      </div>
    </div>

    <div className="h-14 rounded-2xl border border-white/[0.08] bg-zinc-900/80 p-3 flex items-center justify-between">
      <div className="h-4 w-60 rounded bg-white/[0.05]" />
      <div className="h-8 w-8 rounded-lg bg-white/[0.08]" />
    </div>
  </div>
);

/** 19. Terminal Skeleton */
export const TerminalSkeleton: React.FC = () => (
  <div className="h-[calc(100vh-8rem)] rounded-2xl border border-white/[0.08] bg-black p-4 space-y-3 font-mono nd-skel" aria-busy="true" aria-label="Loading Terminal">
    <div className="flex items-center gap-2 border-b border-white/[0.06] pb-3">
      <div className="h-3 w-3 rounded-full bg-rose-500/40" />
      <div className="h-3 w-3 rounded-full bg-amber-500/40" />
      <div className="h-3 w-3 rounded-full bg-emerald-500/40" />
      <div className="h-3.5 w-36 rounded bg-white/[0.08] ml-2" />
    </div>
    <div className="space-y-2 pt-2">
      <div className="h-3.5 w-64 rounded bg-white/[0.05]" />
      <div className="h-3.5 w-96 rounded bg-white/[0.04]" />
      <div className="h-3.5 w-44 rounded bg-white/[0.03]" />
      <div className="flex items-center gap-2">
        <div className="h-3.5 w-32 rounded bg-emerald-500/30" />
        <div className="h-4 w-2 bg-emerald-400/80 animate-pulse" />
      </div>
    </div>
  </div>
);

/** Generic Table Skeleton fallback */
export const TableSkeleton: React.FC<{ title?: string }> = ({ title = 'Loading' }) => (
  <div className="space-y-4 nd-skel" aria-busy="true" aria-label={title}>
    <div className="flex items-center justify-between gap-3">
      <div className="h-8 w-44 rounded-xl bg-white/[0.06]" />
      <div className="flex items-center gap-2">
        <div className="h-8 w-24 rounded-lg bg-white/[0.04]" />
        <div className="h-8 w-28 rounded-lg bg-white/[0.08]" />
      </div>
    </div>
    <div className="rounded-2xl border border-white/[0.06] bg-[#111115]/80 p-4 space-y-3">
      <div className="h-9 rounded-lg bg-white/[0.04]" />
      {[1, 2, 3, 4, 5].map((row) => (
        <div key={row} className="flex items-center justify-between gap-4 py-2 border-b border-white/[0.03] last:border-0">
          <div className="h-5 w-48 rounded bg-white/[0.05]" />
          <div className="h-5 w-24 rounded bg-white/[0.03]" />
          <div className="h-5 w-16 rounded bg-white/[0.03]" />
          <div className="h-7 w-20 rounded-lg bg-white/[0.04]" />
        </div>
      ))}
    </div>
  </div>
);

/** Comprehensive Page Skeleton Switcher */
export const PageSkeleton: React.FC<{ nav: string }> = ({ nav }) => {
  switch (nav) {
    case 'overview':
      return <DashboardSkeleton />;
    case 'monitor':
      return <MonitorSkeleton />;
    case 'apps':
      return <ProjectsListSkeleton />;
    case 'workspace':
      return <WorkspaceSkeleton />;
    case 'shield-proxies':
      return <ShieldProxiesSkeleton />;
    case 'proxy-routes':
      return <ProxyRoutesSkeleton />;
    case 'proxy':
    case 'caddy-proxy':
      return <CaddyOptionsSkeleton />;
    case 'proxy-options':
      return <CaddyOptionsSkeleton />;
    case 'php-sites':
      return <PhpSitesSkeleton />;
    case 'php-instance':
      return <PhpSitesSkeleton />;
    case 'containers':
      return <ContainersSkeleton />;
    case 'images':
      return <ImagesSkeleton />;
    case 'volumes':
      return <VolumesSkeleton />;
    case 'backups':
      return <BackupsSkeleton />;
    case 'migrate':
      return <MigrationSkeleton />;
    case 'git':
      return <GitAndRegistriesSkeleton />;
    case 'mcp-cli':
    case 'cli-sessions':
      return <McpAndCliSkeleton />;
    case 'users':
      return <UsersSkeleton />;
    case 'settings':
      return <SettingsSkeleton />;
    case 'nextdeploy-ai':
      return <NextDeployAiSkeleton />;
    case 'terminal':
      return <TerminalSkeleton />;
    default:
      return <TableSkeleton title={`Loading ${nav}`} />;
  }
};
