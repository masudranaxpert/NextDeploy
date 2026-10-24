import React from 'react';
import { Sliders, Check, User } from 'lucide-react';
import { Card, GhostButton, Pill, PrimaryButton, inputCls } from '../ui';
import type { PhpInstanceData } from '../../../lib/api/phpApi';

interface PhpLimitsCardProps {
  data: PhpInstanceData;
  minCron: number;
  limits: Record<number, { d: number; c: number }>;
  busy: string;
  onMinCronChange: (val: number) => void;
  onLimitsChange: (limits: Record<number, { d: number; c: number }>) => void;
  onSaveMinCron: () => Promise<void>;
  onSaveUserLimit: (userId: number, d: number, c: number) => Promise<void>;
}

export const PhpLimitsCard: React.FC<PhpLimitsCardProps> = ({
  data,
  minCron,
  limits,
  busy,
  onMinCronChange,
  onLimitsChange,
  onSaveMinCron,
  onSaveUserLimit,
}) => {
  const users = data.user_limits ?? [];

  return (
    <Card
      title={
        <div className="flex items-center gap-2">
          <Sliders className="h-4 w-4 text-orange-400" />
          <span>Tenant Quotas & Limits</span>
        </div>
      }
      description="Configure minimum cron intervals and maximum database/cron allowances per non-admin user."
    >
      {/* Global Cron Minimum Setting */}
      <div className="mb-5 flex flex-col gap-3 rounded-xl border border-white/[0.06] bg-[#09090b]/60 p-4 sm:flex-row sm:items-end sm:justify-between">
        <div className="max-w-md">
          <label className="block text-xs font-semibold text-zinc-200">
            Minimum Cron Job Frequency
          </label>
          <p className="mt-0.5 text-[11px] leading-relaxed text-zinc-400">
            Enforces the lowest allowed interval for non-admin scheduled tasks.
          </p>
        </div>

        <div className="flex items-center gap-2">
          <div className="flex items-center gap-1.5">
            <input
              type="number"
              min={1}
              max={1440}
              className={`${inputCls} !w-24 text-center font-mono`}
              value={minCron}
              onChange={(e) => onMinCronChange(Number(e.target.value))}
            />
            <span className="text-xs text-zinc-400 font-mono">min</span>
          </div>

          <GhostButton busy={busy === 'cron'} onClick={onSaveMinCron} className="h-8.5 px-3">
            <Check className="h-3.5 w-3.5 text-emerald-400" />
            <span>Save Interval</span>
          </GhostButton>
        </div>
      </div>

      {/* Users Limits Table */}
      <div className="overflow-x-auto -mx-4 sm:mx-0">
        <table className="w-full text-left text-xs min-w-[540px]">
          <thead>
            <tr className="border-b border-white/[0.06] text-[11px] font-medium text-zinc-400">
              <th className="py-2.5 px-4 font-medium">Username</th>
              <th className="py-2.5 px-4 font-medium">Role</th>
              <th className="py-2.5 px-4 font-medium">Max Databases</th>
              <th className="py-2.5 px-4 font-medium">Max Cron Jobs</th>
              <th className="py-2.5 px-4 text-right font-medium">Action</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-white/[0.04]">
            {users.map((u) => {
              const cur = limits[u.user_id] ?? { d: u.max_databases, c: u.max_cron_jobs };
              const dirty = cur.d !== u.max_databases || cur.c !== u.max_cron_jobs;
              const isAdmin = u.role === 'admin';

              return (
                <tr key={u.user_id} className="hover:bg-white/[0.02] transition-colors">
                  <td className="py-3 px-4">
                    <div className="flex items-center gap-2 font-medium text-zinc-200">
                      <User className="h-3.5 w-3.5 text-zinc-500" />
                      <span>{u.username}</span>
                    </div>
                  </td>
                  <td className="py-3 px-4">
                    {isAdmin ? (
                      <Pill tone="orange">Admin · Unlimited</Pill>
                    ) : (
                      <Pill tone="zinc">Member</Pill>
                    )}
                  </td>
                  <td className="py-3 px-4">
                    <input
                      type="number"
                      min={0}
                      disabled={isAdmin}
                      className={`${inputCls} !w-24 text-center font-mono`}
                      value={cur.d}
                      onChange={(e) =>
                        onLimitsChange({
                          ...limits,
                          [u.user_id]: { ...cur, d: Number(e.target.value) },
                        })
                      }
                    />
                  </td>
                  <td className="py-3 px-4">
                    <input
                      type="number"
                      min={0}
                      disabled={isAdmin}
                      className={`${inputCls} !w-24 text-center font-mono`}
                      value={cur.c}
                      onChange={(e) =>
                        onLimitsChange({
                          ...limits,
                          [u.user_id]: { ...cur, c: Number(e.target.value) },
                        })
                      }
                    />
                  </td>
                  <td className="py-3 px-4 text-right">
                    {dirty && !isAdmin ? (
                      <PrimaryButton
                        busy={busy === 'ul' + u.user_id}
                        onClick={() => onSaveUserLimit(u.user_id, cur.d, cur.c)}
                        className="h-7.5 px-3 text-[11px]"
                      >
                        Save
                      </PrimaryButton>
                    ) : (
                      <span className="text-[11px] text-zinc-600 font-mono">
                        {isAdmin ? 'System Default' : 'Saved'}
                      </span>
                    )}
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>

      <p className="mt-3 text-[11px] text-zinc-500 pt-2 border-t border-white/[0.06]">
        Value of <code className="font-mono text-zinc-300">0</code> indicates unlimited allocation.
      </p>
    </Card>
  );
};
