import React from 'react';

export const DashboardSkeleton: React.FC = () => (
  <div className="space-y-5 animate-pulse" aria-busy="true" aria-label="Loading Overview">
    {/* Top Stat Cards */}
    <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
      {[1, 2, 3, 4].map((i) => (
        <div key={i} className="h-24 rounded-2xl border border-white/[0.06] bg-[#111115]/80 p-4 space-y-2">
          <div className="h-3 w-16 rounded bg-white/[0.05]" />
          <div className="h-6 w-24 rounded bg-white/[0.08]" />
        </div>
      ))}
    </div>
    {/* Charts & System Row */}
    <div className="grid gap-4 lg:grid-cols-3">
      <div className="h-64 rounded-2xl border border-white/[0.06] bg-[#111115]/80 p-4 lg:col-span-2 space-y-3">
        <div className="h-4 w-32 rounded bg-white/[0.06]" />
        <div className="h-48 rounded-xl bg-white/[0.02]" />
      </div>
      <div className="h-64 rounded-2xl border border-white/[0.06] bg-[#111115]/80 p-4 space-y-3">
        <div className="h-4 w-28 rounded bg-white/[0.06]" />
        <div className="space-y-2 pt-2">
          {[1, 2, 3].map((j) => (
            <div key={j} className="h-10 rounded-lg bg-white/[0.03]" />
          ))}
        </div>
      </div>
    </div>
  </div>
);

export const TableSkeleton: React.FC<{ title?: string }> = ({ title = 'Loading' }) => (
  <div className="space-y-4 animate-pulse" aria-busy="true" aria-label={title}>
    {/* Action Bar */}
    <div className="flex items-center justify-between gap-3">
      <div className="h-8 w-44 rounded-xl bg-white/[0.06]" />
      <div className="flex items-center gap-2">
        <div className="h-8 w-24 rounded-lg bg-white/[0.04]" />
        <div className="h-8 w-28 rounded-lg bg-white/[0.08]" />
      </div>
    </div>
    {/* Table Card */}
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

export const WorkspaceSkeleton: React.FC = () => (
  <div className="space-y-4 animate-pulse" aria-busy="true" aria-label="Loading Project Workspace">
    {/* Workspace Header */}
    <div className="h-24 rounded-2xl border border-white/[0.06] bg-[#111115]/80 p-5 flex items-center justify-between">
      <div className="space-y-2">
        <div className="h-5 w-40 rounded bg-white/[0.08]" />
        <div className="h-3 w-64 rounded bg-white/[0.04]" />
      </div>
      <div className="flex gap-2">
        <div className="h-8 w-20 rounded-lg bg-white/[0.05]" />
        <div className="h-8 w-24 rounded-lg bg-emerald-500/10" />
      </div>
    </div>
    {/* Tab Nav Skeleton */}
    <div className="flex gap-2 border-b border-white/[0.06] pb-2">
      {[1, 2, 3, 4, 5].map((t) => (
        <div key={t} className="h-7 w-20 rounded-lg bg-white/[0.04]" />
      ))}
    </div>
    {/* Workspace Body */}
    <div className="h-72 rounded-2xl border border-white/[0.06] bg-[#111115]/80 p-5 space-y-3">
      <div className="h-4 w-32 rounded bg-white/[0.06]" />
      <div className="h-48 rounded-xl bg-white/[0.02]" />
    </div>
  </div>
);

export const PageSkeleton: React.FC<{ nav: string }> = ({ nav }) => {
  if (nav === 'overview') return <DashboardSkeleton />;
  if (nav === 'apps') return <WorkspaceSkeleton />;
  return <TableSkeleton title={`Loading ${nav}`} />;
};
