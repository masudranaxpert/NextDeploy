import React, { useState, useMemo, useEffect, useRef } from 'react';
import type { AppStack, AppWorkspaceTab, LogEntry } from '../../../types/nextdeploy';

interface ParsedLine extends LogEntry {
  hasExplicitTimestamp: boolean;
}

function parseLogLines(
  lines: string[],
  fallbackContainer: string,
  nextId: () => string
): ParsedLine[] {
  const nowIso = new Date().toISOString();
  const entries: ParsedLine[] = [];
  for (let i = 0; i < lines.length; i++) {
    const rawLine = lines[i];
    if (!rawLine || !rawLine.trim()) continue;
    let logTime = nowIso;
    let hasExplicitTimestamp = false;
    let logMsg = rawLine;
    const tsMatch = rawLine.match(/^(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?Z?)\s+(.*)$/);
    if (tsMatch) {
      logTime = tsMatch[1];
      logMsg = tsMatch[2];
      hasExplicitTimestamp = true;
    }
    let container = fallbackContainer || 'app';
    const composeMatch = logMsg.match(/^([a-zA-Z0-9_-]+)(?:-\d+)?\s*\|\s*(.*)$/);
    if (composeMatch) {
      container = composeMatch[1];
      logMsg = composeMatch[2];
    } else {
      const bracketMatch = logMsg.match(/^\[([a-zA-Z0-9_-]+)\]\s*(.*)$/);
      if (bracketMatch) {
        container = bracketMatch[1];
        logMsg = bracketMatch[2];
      }
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
    entries.push({
      id: nextId(),
      timestamp: logTime,
      container,
      level,
      message: logMsg,
      hasExplicitTimestamp,
    });
  }
  return entries;
}

function logSignature(entry: LogEntry, hasExplicitTimestamp = true): string {
  return hasExplicitTimestamp
    ? `${entry.timestamp}|${entry.container}|${entry.message}`
    : `__raw__|${entry.container}|${entry.message}`;
}

export function useWorkspaceLogs(app: AppStack, activeTab: AppWorkspaceTab) {
  const [logContainerFilter, setLogContainerFilter] = useState<string>('all');
  const [terminalService, setTerminalService] = useState<string>(app.services?.[0]?.service || '');
  const [logLevelFilter, setLogLevelFilter] = useState<'all' | 'info' | 'warn' | 'error' | 'debug'>('all');
  const [logTimeRange, setLogTimeRange] = useState<'live' | '15m' | '1h' | '24h'>('live');
  const [logSearch, setLogSearch] = useState('');
  const [logLiveTail, setLogLiveTail] = useState(true);
  const [liveStreamLogs, setLiveStreamLogs] = useState<LogEntry[]>([]);
  const [isLogsLoading, setIsLogsLoading] = useState(false);

  const lastStreamKeyRef = useRef<string>('');
  const hasVisibleLogsRef = useRef(false);
  hasVisibleLogsRef.current = liveStreamLogs.length > 0;
  const seqRef = useRef(0);
  const nextLogId = () => `ws-log-${++seqRef.current}`;

  // Resolve container name as a primitive string so background bootstrap polling
  // (which creates new app.services array references) never tears down the log stream.
  const servicesList = app.services || [];
  const targetSvc =
    logContainerFilter !== 'all'
      ? servicesList.find((s) => s.service === logContainerFilter || s.name === logContainerFilter)
      : undefined;
  const resolvedContainerName =
    logContainerFilter !== 'all'
      ? targetSvc?.service || targetSvc?.name || logContainerFilter
      : '';

  useEffect(() => {
    if (activeTab !== 'logs') return;

    const streamKey = `${app.id}::${resolvedContainerName}::${logTimeRange}`;
    const streamChanged = lastStreamKeyRef.current !== streamKey;
    if (streamChanged) {
      lastStreamKeyRef.current = streamKey;
      setLiveStreamLogs([]);
    }

    let cancelled = false;
    let ws: WebSocket | null = null;
    let reconnectTimer: number | undefined;
    let reconnectAttempt = 0;
    let wsHasDelivered = false;
    let pendingWsLines: string[] = [];
    let rafId: number | undefined;
    let flushTimer: number | undefined;

    const maxLogs = 600;
    const mergeEntries = (incoming: ParsedLine[], allowNonTimestampDedup: boolean) => {
      if (incoming.length === 0) return;
      setLiveStreamLogs((prev) => {
        const seen = new Set(prev.slice(-maxLogs).map((entry) => logSignature(entry, true)));
        const seenNoTime = allowNonTimestampDedup
          ? new Set(prev.slice(-maxLogs).map((entry) => logSignature(entry, false)))
          : null;
        const fresh: LogEntry[] = [];
        for (const { hasExplicitTimestamp, ...item } of incoming) {
          if (hasExplicitTimestamp) {
            const sig = logSignature(item, true);
            if (seen.has(sig)) continue;
            seen.add(sig);
            fresh.push(item);
          } else if (seenNoTime) {
            const sig = logSignature(item, false);
            if (seenNoTime.has(sig)) continue;
            seenNoTime.add(sig);
            fresh.push(item);
          } else {
            fresh.push(item);
          }
        }
        if (fresh.length === 0) return prev;
        const keepCount = Math.max(0, maxLogs - fresh.length);
        const combined =
          keepCount === 0
            ? fresh.slice(-maxLogs)
            : [...prev.slice(-keepCount), ...fresh];
        return combined;
      });
    };

    const flushWsBuffer = () => {
      rafId = undefined;
      flushTimer = undefined;
      if (cancelled || pendingWsLines.length === 0) return;
      const batch = pendingWsLines;
      pendingWsLines = [];
      const isInitialBurst = !wsHasDelivered;
      wsHasDelivered = true;
      // Only dedup untimestamped lines on the initial/reconnect replay burst
      const parsed = parseLogLines(batch, resolvedContainerName || 'app', nextLogId);
      mergeEntries(parsed, isInitialBurst);
    };

    const scheduleWsFlush = () => {
      if (rafId !== undefined || flushTimer !== undefined) return;
      if (typeof document !== 'undefined' && document.visibilityState === 'visible') {
        rafId = window.requestAnimationFrame(flushWsBuffer);
      } else {
        flushTimer = window.setTimeout(flushWsBuffer, 50);
      }
    };

    const sinceParam = logTimeRange !== 'live' ? `&since=${encodeURIComponent(logTimeRange)}` : '';
    const containerParam = resolvedContainerName ? `&container=${encodeURIComponent(resolvedContainerName)}` : '';

    // 1. Load initial / historical snapshot via REST immediately
    const fetchInitialSnapshot = async () => {
      if (!hasVisibleLogsRef.current) setIsLogsLoading(true);
      try {
        const res = await fetch(
          `/apps/${encodeURIComponent(app.id)}/logs?tail=200${sinceParam}${containerParam}`,
          { credentials: 'same-origin' }
        );
        if (cancelled || !res.ok) return;
        const data = (await res.json()) as { ok?: boolean; logs?: string[] };
        if (cancelled || !Array.isArray(data?.logs)) return;
        const parsed = parseLogLines(data.logs, resolvedContainerName || 'app', nextLogId);
        // Never overwrite rows already streamed in via WebSocket while REST was in flight
        if (streamChanged && !wsHasDelivered) {
          setLiveStreamLogs(parsed.map(({ hasExplicitTimestamp: _h, ...item }) => item));
        } else {
          mergeEntries(parsed, true);
        }
      } catch {
        // Ignore transient fetch errors; WebSocket stream will still connect if live tail is on
      } finally {
        if (!cancelled) setIsLogsLoading(false);
      }
    };

    void fetchInitialSnapshot();

    // 2. Open persistent WebSocket stream with automatic reconnect when Live Tail is active
    const connectWS = () => {
      if (cancelled || !logLiveTail) return;
      const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
      const wsUrl = `${proto}//${window.location.host}/apps/${encodeURIComponent(
        app.id
      )}/ws/logs?tail=200${sinceParam}${containerParam}`;
      try {
        ws = new WebSocket(wsUrl);
        ws.onopen = () => {
          reconnectAttempt = 0;
        };
        ws.onmessage = (ev) => {
          if (cancelled) return;
          const raw = typeof ev.data === 'string' ? ev.data : '';
          if (!raw) return;
          const lines = raw.split(/\r?\n|\r/).filter((l) => l.trim().length > 0);
          if (lines.length === 0) return;
          pendingWsLines.push(...lines);
          scheduleWsFlush();
        };
        ws.onerror = () => {
          // Handled via onclose reconnect
        };
        ws.onclose = () => {
          ws = null;
          if (cancelled || !logLiveTail) return;
          const delay = Math.min(5000, 1500 * Math.max(1, reconnectAttempt + 1));
          reconnectAttempt += 1;
          reconnectTimer = window.setTimeout(connectWS, delay);
        };
      } catch {
        if (!cancelled && logLiveTail) {
          reconnectTimer = window.setTimeout(connectWS, 3000);
        }
      }
    };

    if (logLiveTail) {
      connectWS();
    }

    return () => {
      cancelled = true;
      if (rafId !== undefined) window.cancelAnimationFrame(rafId);
      if (flushTimer !== undefined) window.clearTimeout(flushTimer);
      if (reconnectTimer) window.clearTimeout(reconnectTimer);
      if (ws) {
        try {
          ws.close();
        } catch {
          // ignore
        }
      }
    };
  }, [activeTab, logLiveTail, logContainerFilter, app.id, resolvedContainerName, logTimeRange]);

  const syntheticLogs: LogEntry[] = useMemo(() => {
    if (liveStreamLogs.length > 0) return liveStreamLogs;
    return app.logs || [];
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
    isLogsLoading,
    filteredLogs,
    logCounts,
    renderLogMessage,
  };
}
