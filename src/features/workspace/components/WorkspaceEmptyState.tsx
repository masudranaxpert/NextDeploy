import React from 'react';

interface WorkspaceEmptyStateProps {
  icon?: React.ReactNode;
  title: string;
  description?: string;
  action?: React.ReactNode;
  className?: string;
}

export const WorkspaceEmptyState: React.FC<WorkspaceEmptyStateProps> = ({
  icon,
  title,
  description,
  action,
  className = '',
}) => {
  return (
    <div
      className={`flex flex-col items-center justify-center p-8 text-center space-y-2 rounded-xl border border-white/[0.05] bg-[#09090b]/40 ${className}`}
    >
      {icon && <div className="text-zinc-500 mb-1">{icon}</div>}
      <p className="text-xs font-semibold text-zinc-300">{title}</p>
      {description && (
        <p className="text-[11px] text-zinc-500 max-w-sm">{description}</p>
      )}
      {action && <div className="pt-2">{action}</div>}
    </div>
  );
};
