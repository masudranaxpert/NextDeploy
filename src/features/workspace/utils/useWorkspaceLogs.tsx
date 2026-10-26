import React, { useState, useMemo, useEffect } from 'react';
import type { AppStack, AppWorkspaceTab, LogEntry } from '../../../types/nextdeploy';

export function useWorkspaceLogs(app: AppStack, activeTab: AppWorkspaceTab) {
  const [logContainerFilter, setLogContainerFilter] = useState<string>('all');
  const [terminalService, setTerminalService] = useState<string>(app.services[0]?.service || '');
  const [logLevelFilter, setLogLevelFilter] = useState<'all' | 'info' | 'warn' | 'error' | 'debug'>('all');
  const [logTimeRange, setLogTimeRange] = useState<'live' | '15m' | '1h' | '24h'>('live');
  const [logSearch, setLogSearch] = useState('');
  const [logLiveTail, setLogLiveTail] = useState(true);
  const [liveStreamLogs, setLiveStreamLogs] = useState<LogEntry[]>([]);

  useEffect(() => {
    if (activeTab !== 'logs' || !logLiveTail) return;
    setLiveStreamLogs([]);
    const targetSvc =
      logContainerFilter !== 'all'
        ? app.services.find((s) => s.service === logContainerFilter || s.name === logContainerFilter)
        : app.services[0];
    const containerName =
      logContainerFilter !== 'all'
        ? targetSvc?.service || targetSvc?.name || logContainerFilter
        : targetSvc?.service || '';
    const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const sinceParam = logTimeRange !== 'live' ? `&since=${encodeURIComponent(logTimeRange)}` : '';
    const wsUrl = `${proto}//${window.location.host}/apps/${encodeURIComponent(
      app.id
    )}/ws/logs?tail=200${sinceParam}${containerName ? `&container=${encodeURIComponent(containerName)}` : ''}`;
    let ws: WebSocket | null = null;
    try {
      ws = new WebSocket(wsUrl);
      ws.onmessage = (ev) => {
        const raw = typeof ev.data === 'string' ? ev.data : '';
        if (!raw) return;
        const lines = raw.split(/\r?\n/).filter((l) => l.trim().length > 0);
        const nowIso = new Date().toISOString();
        const newEntries: LogEntry[] = lines.map((line, i) => {
          let logTime = nowIso;
          let logMsg = line;
          const tsMatch = line.match(/^(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?Z?)\s+(.*)$/);
          if (tsMatch) {
            logTime = tsMatch[1];
            logMsg = tsMatch[2];
          }
          const lower = logMsg.toLowerCase();
          const level: LogEntry['level'] =
            lower.includes('error') || lower.includes('fatal') || lower.includes('panic')
              ? 'error'
              : lower.includes('warn')
              ? 'warn'
              : lower.includes('debug')
              ? 'debug'
              : 'info';
          return {
            id: `ws-log-${Date.now()}-${i}-${Math.random().toString(36).slice(2, 6)}`,
            timestamp: logTime,
            container: targetSvc?.service || containerName || 'app',
            level,
            message: logMsg,
          };
        });
        setLiveStreamLogs((prev) => [...prev.slice(-600), ...newEntries]);
      };
      ws.onerror = (err) => {
        console.warn('Logs WebSocket error:', err);
      };
    } catch (err) {
      console.warn('Failed to connect to logs WebSocket:', err);
    }
    return () => {
      if (ws) {
        try {
          ws.close();
        } catch {
          // ignore
        }
      }
    };
  }, [activeTab, logLiveTail, logContainerFilter, app.id, app.services, logTimeRange]);

  const syntheticLogs: LogEntry[] = useMemo(() => {
    return [...(app.logs || []), ...liveStreamLogs];
  }, [app.logs, liveStreamLogs]);

  const filteredLogs = useMemo(() => {
    return syntheticLogs.filter((l) => {
      if (logContainerFilter !== 'all' && l.container !== logContainerFilter) return false;
      if (logLevelFilter !== 'all' && l.level !== logLevelFilter) return false;
      if (
        logSearch &&
        !l.message.toLowerCase().includes(logSearch.toLowerCase()) &&
        !l.container.toLowerCase().includes(logSearch.toLowerCase())
      ) {
        return false;
      }
      return true;
    });
  }, [syntheticLogs, logContainerFilter, logLevelFilter, logSearch]);

  const logCounts = useMemo(() => {
    return {
      all: syntheticLogs.length,
      info: syntheticLogs.filter((l) => l.level === 'info').length,
      warn: syntheticLogs.filter((l) => l.level === 'warn').length,
      error: syntheticLogs.filter((l) => l.level === 'error').length,
      debug: syntheticLogs.filter((l) => l.level === 'debug').length,
    };
  }, [syntheticLogs]);

  const renderLogMessage = (msg: string) => {
    if (!logSearch.trim()) return msg;
    const idx = msg.toLowerCase().indexOf(logSearch.toLowerCase());
    if (idx === -1) return msg;
    const before = msg.slice(0, idx);
    const match = msg.slice(idx, idx + logSearch.length);
    const after = msg.slice(idx + logSearch.length);
    return (
      <>
        {before}
        <mark className="rounded bg-orange-500/30 px-1 text-orange-200">{match}</mark>
        {after}
      </>
    );
  };

  return {
    logContainerFilter,
    setLogContainerFilter,
    terminalService,
    setTerminalService,
    logLevelFilter,
    setLogLevelFilter,
    logTimeRange,
    setLogTimeRange,
    logSearch,
    setLogSearch,
    logLiveTail,
    setLogLiveTail,
    filteredLogs,
    logCounts,
    renderLogMessage,
  };
}
