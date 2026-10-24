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
      title="Site Logs"
      actions={
        <div className="flex flex-wrap items-center gap-2">
          <select
            className={`${selectCls} !w-44 h-8`}
            value={source}
            onChange={(e) => setSource(e.target.value as 'php' | 'runtime')}
          >
            <option value="php">PHP errors (php-error.log)</option>
            <option value="runtime">PHP-FPM runtime</option>
          </select>
          <select
            className={`${selectCls} !w-24 h-8`}
            value={lines}
            onChange={(e) => setLines(Number(e.target.value))}
          >
            {[100, 200, 500, 1000, 2000].map((n) => (
              <option key={n} value={n}>
                {n} lines
              </option>
            ))}
          </select>
          <label className="inline-flex cursor-pointer items-center gap-1.5 text-xs text-zinc-300 px-1">
            <input
              type="checkbox"
              checked={live}
              onChange={(e) => setLive(e.target.checked)}
              className="rounded border-zinc-700 bg-zinc-900 accent-orange-500"
            />
            <span>Live Tail</span>
          </label>
          <GhostButton busy={busy} onClick={() => void load()} className="h-8">
            <RefreshCw className="h-3 w-3 text-zinc-400" />
            <span>Refresh</span>
          </GhostButton>
        </div>
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
