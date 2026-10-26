import React, { useState, useMemo } from 'react';
import {
  Search,
  Eye,
  EyeOff,
  FileCode2,
  Plus,
  Save,
  Trash2,
  Copy,
} from 'lucide-react';
import { toast } from 'sonner';
import { AppStack } from '../../../types/nextdeploy';
import { backendApi } from '../../../lib/backendApi';
import { copyToClipboard } from '../../../lib/clipboard';
import { CodeEditorBox } from '../../../components/ui/CodeEditorBox';

interface ProjectEnvTabProps {
  app: AppStack;
  onUpdateApp: (updated: AppStack) => void;
}

export const ProjectEnvTab: React.FC<ProjectEnvTabProps> = ({
  app,
  onUpdateApp,
}) => {
  const [envRawMode, setEnvRawMode] = useState(false);
  const [revealAllEnv, setRevealAllEnv] = useState(false);
  const [revealedEnvIds, setRevealedEnvIds] = useState<Record<string, boolean>>({});
  const [envRawText, setEnvRawText] = useState(
    app.envVars.map((e) => `${e.key}=${e.value}`).join('\n')
  );
  const [envSearch, setEnvSearch] = useState('');
  const [isEnvDirty, setIsEnvDirty] = useState(false);

  const emptyEnvCount = useMemo(
    () => app.envVars.filter((e) => !e.value || e.value.trim().length === 0).length,
    [app.envVars]
  );

  const filteredEnvVars = useMemo(
    () =>
      app.envVars.filter(
        (ev) =>
          ev.key.toLowerCase().includes(envSearch.toLowerCase()) ||
          ev.value.toLowerCase().includes(envSearch.toLowerCase())
      ),
    [app.envVars, envSearch]
  );

  const handleEnvChange = (id: string, field: 'key' | 'value', val: string) => {
    setIsEnvDirty(true);
    const nextVars = app.envVars.map((ev) =>
      ev.id === id
        ? {
            ...ev,
            [field]: field === 'key' ? val.toUpperCase().replace(/\s+/g, '_') : val,
          }
        : ev
    );
    onUpdateApp({ ...app, envVars: nextVars });
  };

  const handleAddBlankEnvRow = () => {
    setIsEnvDirty(true);
    const newId = `ev-${Date.now()}`;
    const nextVars = [
      ...app.envVars,
      { id: newId, key: '', value: '', isSecret: true },
    ];
    onUpdateApp({ ...app, envVars: nextVars });
  };

  const handlePasteMultiEnv = (
    e: React.ClipboardEvent<HTMLInputElement>,
    targetId: string
  ) => {
    const text = e.clipboardData.getData('text');
    if (!text || !text.includes('\n')) return;

    const lines = text
      .split('\n')
      .map((l) => l.trim())
      .filter((l) => l && !l.startsWith('#') && l.includes('='));

    if (lines.length === 0) return;

    e.preventDefault();
    setIsEnvDirty(true);
    const parsed = lines.map((line, i) => {
      const idx = line.indexOf('=');
      const k = line
        .slice(0, idx)
        .trim()
        .toUpperCase()
        .replace(/\s+/g, '_');
      const v = line.slice(idx + 1).trim();
      return {
        id: `ev-paste-${Date.now()}-${i}`,
        key: k,
        value: v,
        isSecret: true,
      };
    });

    const targetIndex = app.envVars.findIndex((ev) => ev.id === targetId);
    let nextVars = [...app.envVars];
    if (targetIndex !== -1) {
      nextVars.splice(targetIndex, 1, ...parsed);
    } else {
      nextVars = [...nextVars, ...parsed];
    }
    onUpdateApp({ ...app, envVars: nextVars });
    toast.success(`Pasted ${parsed.length} environment variables`);
  };

  const handleDeleteEnv = (id: string) => {
    setIsEnvDirty(true);
    onUpdateApp({
      ...app,
      envVars: app.envVars.filter((e) => e.id !== id),
    });
  };

  return (
    <div className="rounded-2xl border border-white/[0.07] bg-[#111115]/90 p-4 sm:p-5 space-y-4">
      {/* Header with Title, Count & Test Empty Badge */}
      <div className="flex items-center justify-between gap-3">
        <div className="flex items-center gap-2">
          <h2 className="text-sm font-semibold text-zinc-100">Environment Variables</h2>
          <span className="rounded-md bg-white/[0.06] px-2 py-0.5 font-mono text-[11px] text-zinc-400">
            {app.envVars.length}
          </span>
        </div>
        <span className="rounded-md border border-amber-500/30 bg-amber-500/10 px-2.5 py-0.5 font-mono text-[11px] font-medium text-amber-300">
          Test Empty ({emptyEnvCount})
        </span>
      </div>

      {!envRawMode && (
        <div className="space-y-2.5">
          <div className="relative w-full">
            <Search className="pointer-events-none absolute left-3 top-2.5 h-3.5 w-3.5 text-zinc-500" />
            <input
              type="text"
              value={envSearch}
              onChange={(e) => setEnvSearch(e.target.value)}
              placeholder="Filter keys..."
              className="h-9 w-full rounded-lg border border-white/[0.08] bg-[#09090b] pl-9 pr-3 text-xs text-zinc-100 placeholder-zinc-500 focus:border-orange-500 focus:outline-none"
            />
          </div>

          <div className="grid grid-cols-2 gap-2 sm:flex sm:items-center sm:justify-end">
            <button
              type="button"
              onClick={() => setRevealAllEnv((prev) => !prev)}
              className="inline-flex h-9 sm:h-7 items-center justify-center gap-1.5 rounded-lg border border-white/[0.08] bg-zinc-900 px-3 text-xs font-medium text-zinc-300 hover:bg-zinc-800 hover:text-white cursor-pointer"
            >
              {revealAllEnv ? (
                <EyeOff className="h-3.5 w-3.5 text-orange-400" />
              ) : (
                <Eye className="h-3.5 w-3.5 text-zinc-400" />
              )}
              {revealAllEnv ? 'Mask All' : 'Reveal All'}
            </button>

            <button
              type="button"
              onClick={() => {
                if (!envRawMode) {
                  setEnvRawText(app.envVars.map((e) => `${e.key}=${e.value}`).join('\n'));
                }
                setEnvRawMode((prev) => !prev);
              }}
              className="inline-flex h-9 sm:h-7 items-center justify-center gap-1.5 rounded-lg border border-white/[0.08] bg-zinc-900 px-3 text-xs font-medium text-zinc-300 hover:bg-zinc-800 hover:text-white cursor-pointer"
            >
              <FileCode2 className="h-3.5 w-3.5 text-orange-400" />
              {envRawMode ? 'Key-Value Rows' : 'Raw .env Editor'}
            </button>
          </div>
        </div>
      )}

      {envRawMode ? (
        <div className="space-y-3">
          <CodeEditorBox
            filename=".env"
            language="env"
            value={envRawText}
            onChange={setEnvRawText}
            height={300}
          />
          <div className="flex justify-end gap-2">
            <button
              type="button"
              onClick={async () => {
                const parsed = envRawText
                  .split('\n')
                  .map((l) => l.trim())
                  .filter((l) => l && !l.startsWith('#') && l.includes('='))
                  .map((line, idx) => {
                    const eq = line.indexOf('=');
                    return {
                      id: `ev-raw-${Date.now()}-${idx}`,
                      key: line.slice(0, eq).trim().toUpperCase(),
                      value: line.slice(eq + 1).trim(),
                      isSecret: true,
                    };
                  });
                const res = await backendApi.saveAppEnv(app.id, envRawText);
                if (!res.ok) {
                  toast.error(res.error || 'Failed to save .env file');
                  return;
                }
                setIsEnvDirty(false);
                onUpdateApp({ ...app, envVars: parsed });
                setEnvRawMode(false);
                toast.success(`Saved ${parsed.length} variables to .env`);
              }}
              className="inline-flex h-8 items-center gap-1.5 rounded-lg bg-zinc-100 px-4 text-xs font-semibold text-zinc-950 hover:bg-white cursor-pointer"
            >
              Apply .env Changes
            </button>
          </div>
        </div>
      ) : (
        <div className="space-y-3">
          {/* Subheaders */}
          <div className="flex items-center justify-between px-1 text-[10px] font-semibold uppercase tracking-wider text-zinc-500 sm:hidden">
            <span>KEY / VARIABLEVALUE</span>
            <span>ACTIONS</span>
          </div>
          <div className="hidden sm:grid sm:grid-cols-12 gap-2.5 px-1 text-[10px] font-semibold uppercase tracking-wider text-zinc-500">
            <div className="sm:col-span-5">Key</div>
            <div className="sm:col-span-7">Value</div>
          </div>

          {/* Mobile Card Rows */}
          <div className="block sm:hidden space-y-3">
            {filteredEnvVars.map((ev) => {
              const isRevealed = revealAllEnv || Boolean(revealedEnvIds[ev.id]);
              return (
                <div
                  key={ev.id}
                  className="rounded-xl border border-white/[0.07] bg-[#09090b]/60 p-3 space-y-2.5"
                >
                  {/* Line 1: KEY badge + Key input + 3 action buttons */}
                  <div className="flex items-center gap-2">
                    <span className="rounded border border-orange-500/30 bg-orange-500/10 px-2 py-1 font-mono text-[10px] font-semibold text-orange-400 shrink-0">
                      KEY
                    </span>
                    <input
                      type="text"
                      value={ev.key}
                      onPaste={(e) => handlePasteMultiEnv(e, ev.id)}
                      onChange={(e) => handleEnvChange(ev.id, 'key', e.target.value)}
                      placeholder="VARIABLE_NAME"
                      className="flex-1 h-8 rounded-lg border border-white/[0.08] bg-zinc-900/80 px-2.5 font-mono text-xs font-medium text-zinc-100 placeholder-zinc-600 focus:border-orange-500/70 focus:outline-none min-w-0"
                    />
                    <div className="flex items-center gap-1 shrink-0">
                      <button
                        type="button"
                        onClick={() =>
                          setRevealedEnvIds((prev) => ({
                            ...prev,
                            [ev.id]: !prev[ev.id],
                          }))
                        }
                        title={isRevealed ? 'Mask value' : 'Reveal value'}
                        className="inline-flex h-8 w-8 items-center justify-center rounded-lg border border-white/[0.08] bg-zinc-900/80 text-zinc-400 hover:text-zinc-200 transition-colors cursor-pointer"
                      >
                        {isRevealed ? <EyeOff className="h-3.5 w-3.5" /> : <Eye className="h-3.5 w-3.5" />}
                      </button>
                      <button
                        type="button"
                        onClick={() => {
                          void copyToClipboard(`${ev.key}=${ev.value}`);
                          toast.success(`Copied ${ev.key}`);
                        }}
                        title="Copy variable"
                        className="inline-flex h-8 w-8 items-center justify-center rounded-lg border border-white/[0.08] bg-zinc-900/80 text-zinc-400 hover:text-zinc-200 transition-colors cursor-pointer"
                      >
                        <Copy className="h-3.5 w-3.5" />
                      </button>
                      <button
                        type="button"
                        onClick={() => handleDeleteEnv(ev.id)}
                        title="Delete variable"
                        className="inline-flex h-8 w-8 items-center justify-center rounded-lg border border-rose-500/30 bg-rose-500/10 text-rose-400 hover:bg-rose-500/20 transition-colors cursor-pointer"
                      >
                        <Trash2 className="h-3.5 w-3.5" />
                      </button>
                    </div>
                  </div>

                  {/* Line 2: VAL badge + Value input */}
                  <div className="flex items-center gap-2">
                    <span className="rounded border border-white/[0.08] bg-zinc-800/80 px-2 py-1 font-mono text-[10px] font-semibold text-zinc-400 shrink-0">
                      VAL
                    </span>
                    <input
                      type={isRevealed ? 'text' : 'password'}
                      value={ev.value}
                      onPaste={(e) => handlePasteMultiEnv(e, ev.id)}
                      onChange={(e) => handleEnvChange(ev.id, 'value', e.target.value)}
                      placeholder="Value..."
                      className="flex-1 h-8 rounded-lg border border-white/[0.08] bg-zinc-900/80 px-2.5 font-mono text-xs text-zinc-200 placeholder-zinc-600 focus:border-orange-500/70 focus:outline-none min-w-0"
                    />
                  </div>
                </div>
              );
            })}
          </div>

          {/* Desktop Rows */}
          <div className="hidden sm:block space-y-2">
            {filteredEnvVars.map((ev) => {
              const isRevealed = revealAllEnv || Boolean(revealedEnvIds[ev.id]);
              return (
                <div
                  key={ev.id}
                  className="grid grid-cols-1 sm:grid-cols-12 gap-2.5 items-center"
                >
                  <div className="sm:col-span-5">
                    <input
                      type="text"
                      value={ev.key}
                      onPaste={(e) => handlePasteMultiEnv(e, ev.id)}
                      onChange={(e) => handleEnvChange(ev.id, 'key', e.target.value)}
                      placeholder="VARIABLE_NAME"
                      className="h-9 w-full rounded-lg border border-white/[0.08] bg-[#09090b] px-3 font-mono text-xs font-medium text-zinc-100 placeholder-zinc-600 focus:border-orange-500/70 focus:outline-none transition-colors"
                    />
                  </div>

                  <div className="sm:col-span-7 relative flex items-center">
                    <input
                      type={isRevealed ? 'text' : 'password'}
                      value={ev.value}
                      onPaste={(e) => handlePasteMultiEnv(e, ev.id)}
                      onChange={(e) => handleEnvChange(ev.id, 'value', e.target.value)}
                      placeholder="Value..."
                      className="h-9 w-full rounded-lg border border-white/[0.08] bg-[#09090b] pl-3 pr-24 font-mono text-xs text-zinc-200 placeholder-zinc-600 focus:border-orange-500/70 focus:outline-none transition-colors"
                    />
                    <div className="absolute right-1.5 flex items-center gap-0.5">
                      <button
                        type="button"
                        onClick={() =>
                          setRevealedEnvIds((prev) => ({
                            ...prev,
                            [ev.id]: !prev[ev.id],
                          }))
                        }
                        className="rounded-md p-1.5 text-zinc-500 hover:bg-white/[0.06] hover:text-zinc-200 transition-colors cursor-pointer"
                      >
                        {isRevealed ? (
                          <EyeOff className="h-3.5 w-3.5" />
                        ) : (
                          <Eye className="h-3.5 w-3.5" />
                        )}
                      </button>
                      <button
                        type="button"
                        onClick={() => {
                          void copyToClipboard(`${ev.key}=${ev.value}`);
                          toast.success(`Copied ${ev.key}`);
                        }}
                        className="rounded-md p-1.5 text-zinc-500 hover:bg-white/[0.06] hover:text-zinc-200 transition-colors cursor-pointer"
                      >
                        <Copy className="h-3.5 w-3.5" />
                      </button>
                      <button
                        type="button"
                        onClick={() => handleDeleteEnv(ev.id)}
                        className="rounded-md p-1.5 text-zinc-500 hover:bg-rose-500/15 hover:text-rose-400 transition-colors cursor-pointer"
                      >
                        <Trash2 className="h-3.5 w-3.5" />
                      </button>
                    </div>
                  </div>
                </div>
              );
            })}
          </div>

          {/* Bottom Actions */}
          <div className="grid grid-cols-2 gap-2 pt-3 border-t border-white/[0.06] sm:flex sm:items-center sm:justify-between">
            <button
              type="button"
              onClick={handleAddBlankEnvRow}
              className="inline-flex h-9 sm:h-8 items-center justify-center gap-1.5 rounded-lg border border-white/[0.09] bg-zinc-900 px-3 text-xs font-medium text-zinc-200 hover:bg-zinc-800 transition-colors cursor-pointer"
            >
              <Plus className="h-3.5 w-3.5 text-orange-400" />
              Add Variable
            </button>

            <button
              type="button"
              onClick={async () => {
                const rawEnv = app.envVars
                  .filter((e) => e.key.trim().length > 0)
                  .map((e) => `${e.key}=${e.value}`)
                  .join('\n');
                const res = await backendApi.saveAppEnv(app.id, rawEnv);
                if (!res.ok) {
                  toast.error(res.error || 'Failed to save .env variables');
                  return;
                }
                setIsEnvDirty(false);
                toast.success(`Saved ${app.envVars.length} variables to .env`);
              }}
              className="inline-flex h-9 sm:h-8 items-center justify-center gap-1.5 rounded-lg bg-zinc-100 px-4 text-xs font-semibold text-zinc-950 hover:bg-white shadow-xs transition-all cursor-pointer"
            >
              <Save className="h-3.5 w-3.5" />
              Save .env {isEnvDirty ? '*' : ''}
            </button>
          </div>
        </div>
      )}
    </div>
  );
};
