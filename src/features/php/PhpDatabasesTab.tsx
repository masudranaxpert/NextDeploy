import React, { useRef, useState } from 'react';
import { Database, Download, ExternalLink, KeyRound, Pencil, Plus, Trash2, Upload, UserRound } from 'lucide-react';
import { toast } from 'sonner';
import { AppStack } from '../../types/nextdeploy';
import { backendApi } from '../../lib/backendApi';
import { ShadcnDialog } from '../../components/ui/Primitives';
import type { PhpDbUserRow } from '../../lib/api/phpApi';
import {
  Card,
  CopyField,
  ErrorBlock,
  Field,
  GhostButton,
  LoadingBlock,
  Pill,
  PrimaryButton,
  formatBytes,
  inputCls,
  selectCls,
  usePhpSite,
} from './ui';

type Access = 'all' | 'read';
interface Creds {
  title: string;
  host: string;
  port: number;
  user: string;
  password: string;
}

const accessLabel = (a: Access) => (a === 'all' ? 'read & write' : 'read only');

export const PhpDatabasesTab: React.FC<{ app: AppStack }> = ({ app }) => {
  const { detail, error, loading, reload } = usePhpSite(app.id);
  const [dbOpen, setDbOpen] = useState(false);
  const [dbName, setDbName] = useState('');
  const [userOpen, setUserOpen] = useState(false);
  const [userName, setUserName] = useState('');
  const [userPass, setUserPass] = useState('');
  const [picked, setPicked] = useState<Record<number, Access>>({});
  const [editing, setEditing] = useState<PhpDbUserRow | null>(null);
  const [busy, setBusy] = useState('');
  const [creds, setCreds] = useState<Creds | null>(null);
  const importTarget = useRef<number | null>(null);
  const fileRef = useRef<HTMLInputElement>(null);

  if (loading) return <LoadingBlock />;
  if (error || !detail) return <ErrorBlock message={error || 'Site not found'} onRetry={reload} />;

  const { mysql, limits } = detail;
  const databases = detail.databases ?? [];
  const users = detail.db_users ?? [];
  const limitReached = !limits.unlimited && limits.max_databases > 0 && limits.used_databases >= limits.max_databases;
  const dbById = new Map(databases.map((d) => [d.id, d]));

  const openPma = async () => {
    setBusy('pma');
    const res = await backendApi.phpPmaOpen(app.id);
    setBusy('');
    if (!res.ok || !res.data?.url) {
      toast.error(res.error || 'Could not open phpMyAdmin');
      return;
    }
    window.open(res.data.url, '_blank', 'noopener');
  };

  const createDb = async () => {
    setBusy('createdb');
    const res = await backendApi.phpDbCreate(app.id, { name: dbName.trim() });
    setBusy('');
    if (!res.ok) {
      toast.error(res.error || 'Could not create the database');
      return;
    }
    toast.success(`${res.data?.db_name ?? 'Database'} created. Now add a user that can use it`);
    setDbOpen(false);
    setDbName('');
    await reload();
  };

  const removeDb = async (id: number, name: string) => {
    if (!window.confirm(`Delete database ${name}? All data in it is lost. Its users are kept but lose access.`)) return;
    setBusy('deldb' + id);
    const res = await backendApi.phpDbDelete(app.id, id);
    setBusy('');
    if (!res.ok) toast.error(res.error || 'Could not delete the database');
    else toast.success(`${name} deleted`);
    await reload();
  };

  const openCreateUser = () => {
    setEditing(null);
    setUserName('');
    setUserPass('');
    setPicked(databases.length === 1 ? { [databases[0].id]: 'all' } : {});
    setUserOpen(true);
  };

  const openEditAccess = (u: PhpDbUserRow) => {
    setEditing(u);
    setPicked(Object.fromEntries(u.grants.map((g) => [g.db_id, g.access])));
    setUserOpen(true);
  };

  const togglePick = (id: number) =>
    setPicked((p) => {
      const n = { ...p };
      if (n[id]) delete n[id];
      else n[id] = 'all';
      return n;
    });

  const grantsList = () => Object.entries(picked).map(([id, access]) => ({ db_id: Number(id), access }));

  const saveUser = async () => {
    setBusy('saveuser');
    if (editing) {
      const res = await backendApi.phpDbUserGrants(app.id, editing.id, grantsList());
      setBusy('');
      if (!res.ok) {
        toast.error(res.error || 'Could not update access');
        return;
      }
      toast.success('Access updated');
      setUserOpen(false);
      await reload();
      return;
    }
    const res = await backendApi.phpDbUserCreate(app.id, {
      name: userName.trim(),
      password: userPass.trim() || undefined,
      grants: grantsList(),
    });
    setBusy('');
    if (!res.ok || !res.data) {
      toast.error(res.error || 'Could not create the user');
      return;
    }
    setUserOpen(false);
    setCreds({ title: 'Database user created', host: res.data.host, port: res.data.port, user: res.data.user, password: res.data.password });
    await reload();
  };

  const resetPassword = async (u: PhpDbUserRow) => {
    if (!window.confirm(`Generate a new password for ${u.user_name}? Applications using the old one will stop connecting.`)) return;
    setBusy('pw' + u.id);
    const res = await backendApi.phpDbUserPassword(app.id, u.id);
    setBusy('');
    if (!res.ok || !res.data) {
      toast.error(res.error || 'Could not change the password');
      return;
    }
    setCreds({ title: 'New password', host: mysql.host, port: mysql.port, user: u.user_name, password: res.data.password });
  };

  const removeUser = async (u: PhpDbUserRow) => {
    if (!window.confirm(`Delete database user ${u.user_name}? Applications using it will stop connecting.`)) return;
    setBusy('deluser' + u.id);
    const res = await backendApi.phpDbUserDelete(app.id, u.id);
    setBusy('');
    if (!res.ok) toast.error(res.error || 'Could not delete the user');
    else toast.success(`${u.user_name} deleted`);
    await reload();
  };

  const onFile = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    const id = importTarget.current;
    e.target.value = '';
    if (!file || id === null) return;
    if (!window.confirm(`Import ${file.name}? Existing tables with the same names may be overwritten.`)) return;
    setBusy('imp' + id);
    const res = await backendApi.phpDbImport(app.id, id, file);
    setBusy('');
    if (!res.ok) toast.error(res.error || 'Import failed');
    else toast.success('Import finished');
    await reload();
  };

  return (
    <div className="space-y-5">
      <Card
        title="Databases"
        description={`Connection host: ${mysql.host}:${mysql.port}`}
        actions={
          <div className="flex flex-wrap items-center gap-2">
            {!limits.unlimited && (
              <Pill tone={limitReached ? 'amber' : 'zinc'}>
                {limits.used_databases} / {limits.max_databases || '∞'} Quota
              </Pill>
            )}
            {detail.pma?.enabled && databases.length > 0 && (
              <GhostButton
                disabled={!mysql.ready}
                busy={busy === 'pma'}
                onClick={() => void openPma()}
                className="h-8 text-orange-300"
              >
                <ExternalLink className="h-3.5 w-3.5 text-orange-400" />
                <span>phpMyAdmin</span>
              </GhostButton>
            )}
            <PrimaryButton
              disabled={!mysql.ready || limitReached}
              onClick={() => setDbOpen(true)}
              className="h-8"
            >
              <Plus className="h-3.5 w-3.5" />
              <span>New Database</span>
            </PrimaryButton>
          </div>
        }
      >
        {!mysql.ready && (
          <div className="mb-4 rounded-lg border border-amber-500/25 bg-amber-500/10 px-3.5 py-2.5 text-xs text-amber-200">
            Database server container is offline. Enable it in PHP Hosting → Instance Setup.
          </div>
        )}
        {databases.length === 0 ? (
          <div className="flex flex-col items-center gap-2 py-8 text-center">
            <Database className="h-6 w-6 text-zinc-600" />
            <p className="text-xs text-zinc-400">No databases created yet. Create a database to get started.</p>
          </div>
        ) : (
          <div className="overflow-x-auto -mx-4 sm:mx-0">
            <table className="w-full text-left text-xs min-w-[500px]">
              <thead className="border-b border-white/[0.06] text-[11px] font-medium text-zinc-400">
                <tr>
                  <th className="py-2.5 px-4 font-medium">Database Name</th>
                  <th className="py-2.5 px-4 font-medium">Storage Size</th>
                  <th className="py-2.5 px-4 font-medium">Authorized Users</th>
                  <th className="py-2.5 px-4 text-right font-medium">Actions</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-white/[0.04]">
                {databases.map((d) => (
                  <tr key={d.id} className="hover:bg-white/[0.02] transition-colors">
                    <td className="py-3 px-4 font-mono font-semibold text-zinc-100">{d.db_name}</td>
                    <td className="py-3 px-4 font-mono text-zinc-400">{formatBytes(d.size_bytes)}</td>
                    <td className="py-3 px-4">
                      {(d.users ?? []).length === 0 ? (
                        <span className="text-zinc-500">None assigned</span>
                      ) : (
                        <div className="flex flex-wrap gap-1">
                          {d.users.map((u) => (
                            <Pill key={u.user_id} tone={u.access === 'all' ? 'green' : 'zinc'}>
                              {u.user} · {accessLabel(u.access)}
                            </Pill>
                          ))}
                        </div>
                      )}
                    </td>
                    <td className="py-3 px-4 text-right">
                      <div className="flex items-center justify-end gap-1.5">
                        <a
                          href={backendApi.phpDbExportUrl(app.id, d.id)}
                          className="inline-flex h-7.5 items-center gap-1.5 rounded-lg border border-white/[0.08] bg-zinc-900 px-2.5 text-xs font-medium text-zinc-200 hover:bg-zinc-800 transition-colors"
                          title="Export database dump (.sql.gz)"
                        >
                          <Download className="h-3 w-3" />
                          <span className="hidden sm:inline text-[11px]">Dump</span>
                        </a>
                        <GhostButton
                          busy={busy === 'imp' + d.id}
                          title="Import database dump"
                          onClick={() => {
                            importTarget.current = d.id;
                            fileRef.current?.click();
                          }}
                          className="h-7.5 px-2.5"
                        >
                          <Upload className="h-3 w-3" />
                          <span className="hidden sm:inline text-[11px]">Import</span>
                        </GhostButton>
                        <GhostButton
                          tone="danger"
                          busy={busy === 'deldb' + d.id}
                          onClick={() => removeDb(d.id, d.db_name)}
                          title="Delete database"
                          className="h-7.5 px-2.5"
                        >
                          <Trash2 className="h-3 w-3" />
                        </GhostButton>
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
        <input ref={fileRef} type="file" accept=".sql,.gz" className="hidden" onChange={onFile} />
      </Card>

      <Card
        title="Database Users"
        description="Database accounts and permissions"
        actions={
          <PrimaryButton disabled={!mysql.ready} onClick={openCreateUser} className="h-8">
            <Plus className="h-3.5 w-3.5" />
            <span>New User</span>
          </PrimaryButton>
        }
      >
        {users.length === 0 ? (
          <div className="flex flex-col items-center gap-2 py-8 text-center">
            <UserRound className="h-6 w-6 text-zinc-600" />
            <p className="text-xs text-zinc-400">No database users yet.</p>
          </div>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-left text-xs">
              <thead className="text-[11px] text-zinc-500">
                <tr>
                  <th className="py-2 pr-4 font-medium">User</th>
                  <th className="py-2 pr-4 font-medium">Access to</th>
                  <th className="py-2 text-right font-medium">Actions</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-white/[0.05]">
                {users.map((u) => (
                  <tr key={u.id}>
                    <td className="py-2.5 pr-4 font-mono text-zinc-100">{u.user_name}</td>
                    <td className="py-2.5 pr-4">
                      {u.grants.length === 0 ? (
                        <span className="text-zinc-500">no database</span>
                      ) : (
                        <div className="flex flex-wrap gap-1">
                          {u.grants.map((g) => (
                            <Pill key={g.db_id} tone={g.access === 'all' ? 'green' : 'zinc'}>
                              {dbById.get(g.db_id)?.db_name ?? `#${g.db_id}`} · {accessLabel(g.access)}
                            </Pill>
                          ))}
                        </div>
                      )}
                    </td>
                    <td className="py-2.5">
                      <div className="flex flex-wrap items-center justify-end gap-1.5">
                        <GhostButton onClick={() => openEditAccess(u)} title="Change access">
                          <Pencil className="h-3.5 w-3.5" />
                        </GhostButton>
                        <GhostButton busy={busy === 'pw' + u.id} onClick={() => resetPassword(u)} title="New password">
                          <KeyRound className="h-3.5 w-3.5" />
                        </GhostButton>
                        <GhostButton tone="danger" busy={busy === 'deluser' + u.id} onClick={() => removeUser(u)} title="Delete user">
                          <Trash2 className="h-3.5 w-3.5" />
                        </GhostButton>
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
        <p className="mt-3 text-[11px] text-zinc-500">Passwords are never stored. Reset one to get a new password, shown once.</p>
      </Card>

      <ShadcnDialog
        open={dbOpen}
        onClose={() => setDbOpen(false)}
        title="Create Database"
        description="Creates an isolated MariaDB database schema for this site."
        icon={<Database className="h-4 w-4 text-orange-400" />}
        footer={
          <div className="flex items-center justify-end gap-2">
            <GhostButton onClick={() => setDbOpen(false)}>Cancel</GhostButton>
            <PrimaryButton busy={busy === 'createdb'} onClick={createDb} disabled={!dbName.trim()}>
              Create Database
            </PrimaryButton>
          </div>
        }
      >
        <Field label="Database Name" hint="Lowercase alphanumeric and underscores (e.g. main_db)">
          <input className={inputCls} value={dbName} onChange={(e) => setDbName(e.target.value.toLowerCase())} placeholder="app_db" autoFocus />
        </Field>
      </ShadcnDialog>

      <ShadcnDialog
        open={userOpen}
        onClose={() => setUserOpen(false)}
        title={editing ? `Edit Privileges for ${editing.user_name}` : 'Create Database User'}
        description={editing ? 'Assign database schema access permissions.' : 'User credentials for connecting to databases.'}
        icon={<UserRound className="h-4 w-4 text-orange-400" />}
        footer={
          <div className="flex items-center justify-end gap-2">
            <GhostButton onClick={() => setUserOpen(false)}>Cancel</GhostButton>
            <PrimaryButton busy={busy === 'saveuser'} onClick={saveUser} disabled={!editing && !userName.trim()}>
              {editing ? 'Save Access' : 'Create User'}
            </PrimaryButton>
          </div>
        }
      >
        <div className="space-y-4">
          {!editing && (
            <div className="grid gap-3 sm:grid-cols-2">
              <Field label="User name" hint="Lowercase letters, numbers and underscores; a prefix is added.">
                <input className={inputCls} value={userName} onChange={(e) => setUserName(e.target.value.toLowerCase())} placeholder="shop_app" autoFocus />
              </Field>
              <Field label="Password (optional)" hint="Leave empty to generate a strong one.">
                <input className={inputCls} type="text" value={userPass} onChange={(e) => setUserPass(e.target.value)} placeholder="auto-generate" />
              </Field>
            </div>
          )}
          <div>
            <div className="mb-1.5 text-[11px] font-medium text-zinc-300">Databases</div>
            {databases.length === 0 ? (
              <p className="text-xs text-zinc-500">Create a database first.</p>
            ) : (
              <ul className="divide-y divide-white/[0.05] rounded-lg border border-white/[0.08]">
                {databases.map((d) => (
                  <li key={d.id} className="flex items-center justify-between gap-3 px-3 py-2">
                    <label className="flex min-w-0 cursor-pointer items-center gap-2">
                      <input type="checkbox" className="accent-orange-500" checked={picked[d.id] !== undefined} onChange={() => togglePick(d.id)} />
                      <span className="truncate font-mono text-xs text-zinc-100">{d.db_name}</span>
                    </label>
                    <select
                      className={`${selectCls} !w-36`}
                      disabled={picked[d.id] === undefined}
                      value={picked[d.id] ?? 'all'}
                      onChange={(e) => setPicked((p) => ({ ...p, [d.id]: e.target.value as Access }))}
                    >
                      <option value="all">Read &amp; write</option>
                      <option value="read">Read only</option>
                    </select>
                  </li>
                ))}
              </ul>
            )}
          </div>
        </div>
      </ShadcnDialog>

      <ShadcnDialog
        open={creds !== null}
        onClose={() => setCreds(null)}
        title={creds?.title || ''}
        description="Save the password now, it is shown only once."
        icon={<KeyRound className="h-4 w-4" />}
        footer={<PrimaryButton onClick={() => setCreds(null)}>I saved it</PrimaryButton>}
      >
        {creds && (
          <div className="space-y-3">
            <CopyField label="Host" value={creds.host} />
            <CopyField label="Port" value={String(creds.port)} />
            <CopyField label="Username" value={creds.user} />
            <CopyField label="Password" value={creds.password} secret />
          </div>
        )}
      </ShadcnDialog>
    </div>
  );
};
