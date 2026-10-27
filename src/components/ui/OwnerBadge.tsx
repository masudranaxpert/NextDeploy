import React, { createContext, useContext } from 'react';
import { Server, ShieldCheck, User } from 'lucide-react';

/** Who is looking at the panel; the badge hides itself when it would only say "you". */
export const OwnerContext = createContext<{ viewerId: number; isAdmin: boolean }>({ viewerId: 0, isAdmin: false });

export interface OwnerLike {
  id?: number;
  name?: string;
  role?: string;
  system?: boolean;
}

const TONES = {
  admin: 'border-amber-500/30 bg-amber-500/10 text-amber-300',
  user: 'border-sky-500/30 bg-sky-500/10 text-sky-300',
  system: 'border-purple-500/30 bg-purple-500/10 text-purple-300',
} as const;

/**
 * Shows the owner's own name, coloured by what they are: an administrator, a regular user,
 * or the system itself. Admins see it everywhere; users only see it on projects shared with them.
 */
export const OwnerBadge: React.FC<OwnerLike & { className?: string }> = ({ id, name, role, system, className = '' }) => {
  const { viewerId, isAdmin } = useContext(OwnerContext);
  const isSystem =
    Boolean(system) ||
    role === 'system' ||
    name?.toLowerCase() === 'system' ||
    id === 0;

  const kind = isSystem
    ? 'system'
    : role === 'admin'
    ? 'admin'
    : 'user';

  if (!name) return null;
  if (!isAdmin && id !== undefined && id === viewerId) return null;
  const Icon = kind === 'system' ? Server : kind === 'admin' ? ShieldCheck : User;
  const label = kind === 'system' ? 'System' : kind === 'admin' ? 'Administrator' : 'User';
  return (
    <span
      title={`Owner: ${name} (${label})`}
      className={`inline-flex max-w-[9rem] shrink-0 items-center gap-1.5 rounded-md border px-2 py-0.5 text-xs font-medium leading-none ${TONES[kind]} ${className}`}
    >
      <Icon className="h-3 w-3 shrink-0" />
      <span className="truncate">{name}</span>
    </span>
  );
};
