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
          <label className="inline-flex cursor-pointer items-center gap-1.5 whitespace-nowrap text-xs text-zinc-300">
            <input type="checkbox" checked={live} onChange={(e) => setLive(e.target.checked)} className="accent-orange-500" />
            Live
          </label>
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
