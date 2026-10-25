import React, { useCallback, useEffect, useRef, useState } from 'react';
import { RefreshCw } from 'lucide-react';
import { AppStack } from '../../types/nextdeploy';
import { backendApi } from '../../lib/backendApi';
import { Card, GhostButton, selectCls } from './ui';

export const PhpLogsTab: React.FC<{ app: AppStack }> = ({ app }) => {
  const [source, setSource] = useState<'php' | 'runtime'>('php');
  const [lines, setLines] = useState(200);
  const [live, setLive] = useState(false);
  const [text, setText] = useState('');
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const box = useRef<HTMLPreElement>(null);

  const load = useCallback(async () => {
    setBusy(true);
    const res = await backendApi.phpLogs(app.id, source, lines);
    setBusy(false);
    if (res.ok && res.data) {
      setText(res.data.text);
      setError('');
    } else setError(res.error || 'Could not load logs');
  }, [app.id, source, lines]);

  useEffect(() => {
    void load();
  }, [load]);

  useEffect(() => {
    if (!live) return;
    const t = setInterval(() => void load(), 3000);
    return () => clearInterval(t);
  }, [live, load]);

  useEffect(() => {
    if (box.current) box.current.scrollTop = box.current.scrollHeight;
  }, [text]);

  return (
    <Card
      title="Logs"
      actions={
        <>
          <select aria-label="Log source" className={`${selectCls} sm:!w-56`} value={source} onChange={(e) => setSource(e.target.value as 'php' | 'runtime')}>
            <option value="php">PHP errors</option>
            <option value="runtime">PHP-FPM output</option>
          </select>
          <select aria-label="Number of lines" className={`${selectCls} !w-28`} value={lines} onChange={(e) => setLines(Number(e.target.value))}>
            {[100, 200, 500, 1000, 2000].map((n) => <option key={n} value={n}>{n} lines</option>)}
          </select>
          <button
            type="button"
            onClick={() => setLive((p) => !p)}
            aria-pressed={live}
            title={live ? 'Live streaming is on — click to pause' : 'Stream new log lines as they are written'}
            className={`inline-flex h-8 items-center gap-1.5 whitespace-nowrap rounded-lg border px-3 text-xs font-medium transition-colors cursor-pointer ${
              live
                ? 'border-emerald-500/30 bg-emerald-500/10 text-emerald-300'
                : 'border-white/[0.08] bg-zinc-900 text-zinc-400 hover:text-zinc-200'
            }`}
          >
            <span
              aria-hidden="true"
              className={`h-1.5 w-1.5 shrink-0 rounded-full ${live ? 'bg-emerald-400 animate-pulse' : 'bg-zinc-600'}`}
            />
            Live
          </button>
          <GhostButton busy={busy} onClick={() => void load()}>
            <RefreshCw className="h-3.5 w-3.5" /> Refresh
          </GhostButton>
        </>
      }
    >
      {error && <div className="mb-2 text-xs text-red-300">{error}</div>}
      <pre
        ref={box}
        className="h-[28rem] overflow-auto whitespace-pre-wrap break-all rounded-lg bg-[#09090b] p-3 font-mono text-[11px] leading-relaxed text-zinc-300"
      >
        {text || (source === 'php' ? 'No PHP errors logged yet.' : 'No runtime output.')}
      </pre>
    </Card>
  );
};
