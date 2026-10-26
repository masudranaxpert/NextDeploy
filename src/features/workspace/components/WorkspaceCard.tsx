import React from 'react';

interface WorkspaceCardProps {
  title?: string;
  count?: number | string;
  subtitle?: string;
  headerBadge?: React.ReactNode;
  headerActions?: React.ReactNode;
  children: React.ReactNode;
  className?: string;
}

export const WorkspaceCard: React.FC<WorkspaceCardProps> = ({
  title,
  count,
  subtitle,
  headerBadge,
  headerActions,
  children,
  className = '',
}) => {
  const hasHeader = Boolean(title || headerBadge || headerActions);

  return (
    <div
      className={`rounded-2xl border border-white/[0.07] bg-[#111115]/90 p-4 sm:p-5 space-y-4 ${className}`}
    >
      {hasHeader && (
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div className="flex items-center gap-2 min-w-0">
            {title && (
              <h2 className="text-sm font-semibold text-zinc-100 truncate">
                {title}
              </h2>
            )}
            {count !== undefined && (
              <span className="rounded-md bg-white/[0.06] px-2 py-0.5 font-mono text-[11px] text-zinc-400">
                {count}
              </span>
            )}
            {headerBadge}
          </div>
          {headerActions && (
            <div className="flex items-center gap-2 shrink-0">
              {headerActions}
            </div>
          )}
        </div>
      )}
      {subtitle && (
        <p className="-mt-2 text-xs text-zinc-400">{subtitle}</p>
      )}
      {children}
    </div>
  );
};
