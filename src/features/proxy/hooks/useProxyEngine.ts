import { useCallback, useEffect, useRef, useState } from 'react';
import { toast } from 'sonner';
import { backendApi } from '../../../lib/backendApi';
import {
  normalizeProxyEngine,
  resolveCapabilities,
  type ProxyCapabilities,
  type ProxyEngineName,
  type ProxyEngineStatusResponse,
  type ProxyLastSwitch,
  type ProxyPreflightResponse,
} from '../../../lib/api/proxyApi';

/** Lifecycle of an engine switch as seen by the UI. */
export type SwitchPhase = 'idle' | 'starting' | 'running' | 'ok' | 'failed';

const POLL_INTERVAL_MS = 2500;
/** A switch recreates the proxy and re-labels every app; give it generous time. */
const POLL_TIMEOUT_MS = 6 * 60 * 1000;

function switchFinished(ls?: ProxyLastSwitch | null): boolean {
  const st = (ls?.status || '').toLowerCase();
  return st !== '' && st !== 'running';
}

function switchSucceeded(ls?: ProxyLastSwitch | null): boolean {
  return (ls?.status || '').toLowerCase() === 'ok' || Boolean(ls?.ok && !ls?.rolled_back);
}

const sleep = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms));

export function useProxyEngine() {
  const [status, setStatus] = useState<ProxyEngineStatusResponse | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const [preflightLoading, setPreflightLoading] = useState(false);
  const [preflightError, setPreflightError] = useState<string | null>(null);
  const [preflightResult, setPreflightResult] = useState<ProxyPreflightResponse | null>(null);

  const [switchPhase, setSwitchPhase] = useState<SwitchPhase>('idle');
  const [switchError, setSwitchError] = useState<string | null>(null);
  const [switchStartedAt, setSwitchStartedAt] = useState<number | null>(null);
  const [connectionLost, setConnectionLost] = useState(false);
  const [finalSwitch, setFinalSwitch] = useState<ProxyLastSwitch | null>(null);

  const mounted = useRef(true);
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);

  const applyStatus = useCallback((res: Awaited<ReturnType<typeof backendApi.proxyEngineStatus>>) => {
    if (res.ok && res.data) {
      setStatus(res.data);
      return res.data;
    }
    if (res.ok) {
      const fallback: ProxyEngineStatusResponse = {
        ok: true,
        engine: res.engine || 'caddy',
        version: res.version,
        running: res.running,
        up: res.up,
        experimental: res.experimental,
        capabilities: res.capabilities,
        last_apply: res.last_apply,
        last_switch: res.last_switch,
      };
      setStatus(fallback);
      return fallback;
    }
    return null;
  }, []);

  const refresh = useCallback(async () => {
    setLoading(true);
    setError(null);
    const res = await backendApi.proxyEngineStatus();
    if (!applyStatus(res)) {
      setError(res.error || 'Failed to load reverse proxy engine status.');
    }
    setLoading(false);
  }, [applyStatus]);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  // A switch started earlier (another tab, or before a reload) is still running: follow it.
  const resumedRef = useRef(false);
  useEffect(() => {
    if (resumedRef.current || switchPhase !== 'idle') return;
    if ((status?.last_switch?.status || '').toLowerCase() === 'running') {
      resumedRef.current = true;
      void followSwitch(normalizeProxyEngine(status?.last_switch?.to));
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [status]);

  const runPreflight = useCallback(async (target: ProxyEngineName) => {
    setPreflightLoading(true);
    setPreflightError(null);
    setSwitchError(null);
    setPreflightResult(null);
    const res = await backendApi.proxyPreflight(target);
    if (res.data) {
      setPreflightResult(res.data);
      if (!res.ok && !res.data.hard_blocks?.length && !res.data.checks?.length) {
        setPreflightError(res.error || res.data.error || 'Preflight validation failed.');
      }
    } else {
      setPreflightError(res.error || 'Unable to run preflight checks for the target engine.');
    }
    setPreflightLoading(false);
  }, []);

  /** Polls the engine status until the background switch finishes (or times out). */
  const followSwitch = useCallback(
    async (target: ProxyEngineName): Promise<boolean> => {
      setSwitchPhase('running');
      setSwitchStartedAt((prev) => prev ?? Date.now());
      const deadline = Date.now() + POLL_TIMEOUT_MS;
      while (mounted.current && Date.now() < deadline) {
        await sleep(POLL_INTERVAL_MS);
        const res = await backendApi.proxyEngineStatus();
        if (!res.ok) {
          // The panel is usually reached through the proxy being replaced, so short
          // connection drops during the cut-over are expected.
          setConnectionLost(true);
          continue;
        }
        setConnectionLost(false);
        const st = applyStatus(res);
        const ls = st?.last_switch;
        if (!switchFinished(ls)) continue;
        setFinalSwitch(ls || null);
        if (switchSucceeded(ls)) {
          setSwitchPhase('ok');
          toast.success(`Reverse proxy is now running on ${target.charAt(0).toUpperCase()}${target.slice(1)}`);
          return true;
        }
        const msg = ls?.error || ls?.message || 'The switch failed.';
        setSwitchError(msg);
        setSwitchPhase('failed');
        toast.error(ls?.rolled_back ? 'Switch failed and was rolled back' : 'Proxy engine switch failed');
        return false;
      }
      if (mounted.current) {
        setSwitchError('Timed out waiting for the switch to finish. Refresh this page to see the final state.');
        setSwitchPhase('failed');
      }
      return false;
    },
    [applyStatus]
  );

  const executeSwitch = useCallback(
    async (target: ProxyEngineName, experimentalAck: boolean): Promise<boolean> => {
      setSwitchPhase('starting');
      setSwitchError(null);
      setFinalSwitch(null);
      setConnectionLost(false);
      setSwitchStartedAt(Date.now());
      const res = await backendApi.proxySwitch({
        target,
        experimental_ack: experimentalAck,
        yes: true,
      });
      if (!res.ok) {
        setSwitchError(res.error || res.data?.error || 'The panel refused to start the switch.');
        setSwitchPhase('failed');
        await refresh();
        return false;
      }
      return followSwitch(target);
    },
    [followSwitch, refresh]
  );

  const resetSwitch = useCallback(() => {
    setSwitchPhase('idle');
    setSwitchError(null);
    setFinalSwitch(null);
    setConnectionLost(false);
    setSwitchStartedAt(null);
  }, []);

  const activeEngine: ProxyEngineName = normalizeProxyEngine(status?.engine || 'caddy');
  const capabilities: ProxyCapabilities = resolveCapabilities(activeEngine, status?.capabilities);

  return {
    status,
    activeEngine,
    capabilities,
    loading,
    error,
    refresh,
    preflightLoading,
    preflightError,
    preflightResult,
    switchPhase,
    switching: switchPhase === 'starting' || switchPhase === 'running',
    switchError,
    switchStartedAt,
    connectionLost,
    finalSwitch,
    runPreflight,
    executeSwitch,
    resetSwitch,
  };
}
