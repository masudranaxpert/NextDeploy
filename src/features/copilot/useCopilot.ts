import { useState, useEffect, useRef, useCallback } from 'react';
import { requestJson } from '../../lib/api/http';
import type {
  CopilotMessage,
  CopilotStatus,
  ToolExecution,
  AIModel,
  ProvidersConfig,
  LoginProvider,
  OAuthPrompt,
  ThinkingLevel,
  ChatSession,
  AutonomyMode,
  SlashCommand,
  WorkDir,
  EngineSettings,
  UiPrompt,
  AgentTodo,
  Subagent,
  AskPrompt,
  SessionStats,
} from './types';

interface ServerSession {
  path: string;
  id: string;
  title: string;
  updatedAt: number;
  current: boolean;
}

export function useCopilot() {
  // Conversations live in omp itself (session files in the persistent volume);
  // the UI only mirrors them.
  const [sessions, setSessions] = useState<ChatSession[]>([]);
  const [activeSessionId, setActiveSessionId] = useState<string>('');
  const [messages, setMessages] = useState<CopilotMessage[]>([]);

  // Models, Thinking, Context & Providers
  const [models, setModels] = useState<AIModel[]>([]);
  const [activeModelId, setActiveModelId] = useState<string>('');
  const [thinkingLevel, setThinkingLevelState] = useState<ThinkingLevel>('low');
  const [availableThinkingLevels, setAvailableThinkingLevels] = useState<string[]>([]);
  const [contextUsage, setContextUsage] = useState<{
    tokens: number;
    contextWindow: number;
    percent: number;
  } | null>(null);
  const [slashCommands, setSlashCommands] = useState<SlashCommand[]>([]);
  const [providers, setProviders] = useState<ProvidersConfig | null>(null);
  const [loginProviders, setLoginProviders] = useState<LoginProvider[]>([]);
  const [oauthPrompt, setOauthPrompt] = useState<OAuthPrompt | null>(null);
  const [workDir, setWorkDirState] = useState<WorkDir | null>(null);
  const [engineSettings, setEngineSettings] = useState<EngineSettings>({
    fastMode: false,
    autoCompaction: true,
    autoRetry: true,
  });
  const [notice, setNotice] = useState<{ level: string; text: string } | null>(null);
  const [uiPrompt, setUiPrompt] = useState<UiPrompt | null>(null);
  const [todos, setTodos] = useState<AgentTodo[]>([]);
  const [subagents, setSubagents] = useState<Subagent[]>([]);
  const [stats, setStats] = useState<SessionStats | null>(null);
  const [askPrompt, setAskPrompt] = useState<AskPrompt | null>(null);

  const activeModel = models.find((m) => m.id === activeModelId) || models[0] || null;

  // Status
  const [status, setStatus] = useState<CopilotStatus>({
    running: false,
    active: false,
    autonomyMode: 'write',
  });
  const [isProcessing, setIsProcessing] = useState(false);
  const [connectionError, setConnectionError] = useState<string | null>(null);

  const activeMessageIdRef = useRef<string | null>(null);
  const thinkingStartRef = useRef<number | null>(null);

  const refreshSessions = useCallback(async () => {
    const res = await requestJson<{ sessions: ServerSession[] }>('/api/v1/copilot/sessions');
    if (!res.ok || !res.data) return;
    const list = res.data.sessions || [];
    setSessions(
      list.map((x) => ({
        id: x.path,
        title: x.title,
        createdAt: x.updatedAt,
        updatedAt: x.updatedAt,
        messageCount: 0,
      }))
    );
    setActiveSessionId(list.find((x) => x.current)?.path || '');
  }, []);

  const loadHistory = useCallback(async () => {
    const res = await requestJson<{ messages: Array<Record<string, any>> }>('/api/v1/copilot/history');
    if (!res.ok || !res.data) return;
    setMessages(
      (res.data.messages || []).map((m) => ({
        id: m.id,
        role: m.role,
        kind: m.kind,
        attached: m.attached,
        content: m.content || '',
        thinking: m.thinking || undefined,
        timestamp: m.timestamp || Date.now(),
        toolCalls: (m.toolCalls || []).map((t: any) => ({
          id: t.id,
          name: t.name,
          args: t.args,
          result: t.details ? { content: [{ type: 'text', text: t.result || '' }], details: t.details } : t.result,
          status: t.status === 'failed' ? 'failed' : 'completed',
          startedAt: m.timestamp || Date.now(),
          endedAt: m.timestamp || Date.now(),
        })),
      })) as CopilotMessage[]
    );
  }, []);

  const resetView = useCallback(async () => {
    setTodos([]);
    setSubagents([]);
    activeMessageIdRef.current = null;
    setIsProcessing(false);
    await Promise.all([loadHistory(), refreshSessions()]);
  }, [loadHistory, refreshSessions]);

  const switchSession = useCallback(
    async (path: string) => {
      const res = await requestJson('/api/v1/copilot/sessions/open', {
        method: 'POST',
        body: JSON.stringify({ path }),
      });
      if (!res.ok) {
        setNotice({ level: 'error', text: res.error || 'Could not open conversation' });
        return;
      }
      await resetView();
    },
    [resetView]
  );

  const createNewChat = useCallback(async () => {
    const res = await requestJson('/api/v1/copilot/session/new', { method: 'POST' });
    if (!res.ok) {
      setNotice({ level: 'error', text: res.error || 'Could not start a new conversation' });
      return;
    }
    await resetView();
  }, [resetView]);

  const deleteSession = useCallback(
    async (path: string) => {
      const res = await requestJson('/api/v1/copilot/sessions', {
        method: 'DELETE',
        body: JSON.stringify({ path }),
      });
      if (!res.ok) {
        setNotice({ level: 'error', text: res.error || 'Could not delete conversation' });
        return;
      }
      await resetView();
    },
    [resetView]
  );

  // Fetch status, models, and providers
  const refreshEngine = useCallback(async () => {
    const statusRes = await requestJson<CopilotStatus>('/api/v1/copilot/status');
    if (statusRes.ok && statusRes.data) {
      setStatus(statusRes.data);
      setConnectionError(null);
      if (statusRes.data.needsLogin) {
        setNotice({
          level: 'warning',
          text: 'No model provider is signed in yet. Open Provider Settings to log in or add an API key.',
        });
      }
    } else {
      setConnectionError(statusRes.error || 'Engine offline');
    }

    const modelsRes = await requestJson<{
      active: string;
      thinkingLevel?: ThinkingLevel;
      availableThinkingLevels?: string[];
      contextUsage?: { tokens: number; contextWindow: number; percent: number };
      models: AIModel[];
    }>('/api/v1/copilot/models');
    if (modelsRes.ok && modelsRes.data) {
      if (modelsRes.data.models) {
        setModels(modelsRes.data.models);
      }
      if (modelsRes.data.active) {
        setActiveModelId(modelsRes.data.active);
      }
      if (modelsRes.data.availableThinkingLevels) {
        setAvailableThinkingLevels(modelsRes.data.availableThinkingLevels);
      }
      if (modelsRes.data.thinkingLevel) {
        setThinkingLevelState(modelsRes.data.thinkingLevel);
      }
      if (modelsRes.data.contextUsage) {
        setContextUsage(modelsRes.data.contextUsage);
      }
    }

    const cmdRes = await requestJson<{
      commands: SlashCommand[];
    }>('/api/v1/copilot/commands');
    if (cmdRes.ok && cmdRes.data?.commands) {
      setSlashCommands(cmdRes.data.commands);
    }

    const wdRes = await requestJson<WorkDir>('/api/v1/copilot/workdir');
    if (wdRes.ok && wdRes.data) setWorkDirState(wdRes.data);
    const setRes = await requestJson<EngineSettings>('/api/v1/copilot/settings');
    if (setRes.ok && setRes.data) setEngineSettings(setRes.data);

    const provRes = await requestJson<ProvidersConfig>('/api/v1/copilot/providers');
    if (provRes.ok && provRes.data) {
      setProviders(provRes.data);
      if (provRes.data.loginProviders) {
        setLoginProviders(provRes.data.loginProviders);
      }
    }
  }, []);

  // Select Model
  const selectModel = useCallback(
    async (provider: string, modelId: string) => {
      setActiveModelId(modelId);
      const res = await requestJson<{
        ok: boolean;
        availableThinkingLevels?: string[];
      }>('/api/v1/copilot/models', {
        method: 'POST',
        body: JSON.stringify({ provider, modelId }),
      });
      if (res.ok && res.data?.availableThinkingLevels) {
        setAvailableThinkingLevels(res.data.availableThinkingLevels);
      }
      await refreshEngine();
    },
    [refreshEngine]
  );

  // Set Thinking Level
  const setThinkingLevel = useCallback(
    async (level: ThinkingLevel) => {
      setThinkingLevelState(level);
      await requestJson('/api/v1/copilot/thinking', {
        method: 'POST',
        body: JSON.stringify({ level }),
      });
      await refreshEngine();
    },
    [refreshEngine]
  );

  // Start OAuth Login for provider
  const startOAuthLogin = useCallback(async (providerId: string) => {
    await requestJson('/api/v1/copilot/login', {
      method: 'POST',
      body: JSON.stringify({ providerId }),
    });
  }, []);

  // Submit OAuth prompt/code
  const submitOAuthInput = useCallback(
    async (id: string, value: string) => {
      const res = await requestJson('/api/v1/copilot/input', {
        method: 'POST',
        body: JSON.stringify({ id, value }),
      });
      if (res.ok) {
        setOauthPrompt(null);
        await refreshEngine();
      }
      return !!res.ok;
    },
    [refreshEngine]
  );

  // Save Providers (BYOK custom keys)
  const saveProviders = useCallback(
    async (keys: Record<string, string>) => {
      const payload: Record<string, string> = {};
      for (const [k, v] of Object.entries(keys)) {
        if (typeof v === 'string' && v.trim() !== '') payload[k] = v.trim();
      }
      if (Object.keys(payload).length === 0) return true;
      const res = await requestJson('/api/v1/copilot/providers', {
        method: 'POST',
        body: JSON.stringify(payload),
      });
      if (res.ok) {
        await refreshEngine();
      }
      return !!res.ok;
    },
    [refreshEngine]
  );

  // SSE event streaming
  useEffect(() => {
    refreshEngine();
    void loadHistory();
    void refreshSessions();
    void requestJson<{ subagents: Subagent[] }>('/api/v1/copilot/subagents').then((r) => {
      if (r.ok && r.data) setSubagents(r.data.subagents || []);
    });

    const eventSource = new EventSource('/api/v1/copilot/stream');

    eventSource.onmessage = (event) => {
      try {
        if (!event.data || event.data.trim() === ': heartbeat') return;
        const ev = JSON.parse(event.data);

        switch (ev.type) {
          case 'text_delta': {
            const delta = ev.delta || '';
            const msgId = activeMessageIdRef.current;
            if (!msgId) break;

            setMessages((prev) =>
              prev.map((msg) =>
                msg.id === msgId
                  ? {
                      ...msg,
                      content: (msg.content || '') + delta,
                      isStreaming: true,
                    }
                  : msg
              )
            );
            break;
          }

          case 'thinking_delta': {
            const delta = ev.delta || '';
            const msgId = activeMessageIdRef.current;
            if (!msgId) break;

            if (!thinkingStartRef.current) {
              thinkingStartRef.current = Date.now();
            }
            const durationSec = Math.max(
              1,
              Math.round((Date.now() - thinkingStartRef.current) / 1000)
            );

            setMessages((prev) =>
              prev.map((msg) =>
                msg.id === msgId
                  ? {
                      ...msg,
                      thinking: (msg.thinking || '') + delta,
                      thinkingDurationSec: durationSec,
                      isStreaming: true,
                    }
                  : msg
              )
            );
            break;
          }

          case 'tool_start': {
            const msgId = activeMessageIdRef.current;
            if (!msgId || !ev.toolName) break;

            let parsedArgs: Record<string, unknown> | undefined;
            try {
              parsedArgs = ev.toolArgs ? JSON.parse(ev.toolArgs) : undefined;
            } catch {
              parsedArgs = undefined;
            }
            const newTool: ToolExecution = {
              id: ev.toolCallId || `tool_${Date.now()}_${Math.random().toString(36).slice(2, 6)}`,
              name: ev.toolName,
              args: parsedArgs,
              intent: ev.intent || undefined,
              status: 'running',
              startedAt: Date.now(),
            };

            setMessages((prev) =>
              prev.map((msg) =>
                msg.id === msgId ? { ...msg, toolCalls: [...(msg.toolCalls || []), newTool] } : msg
              )
            );
            break;
          }

          case 'tool_update': {
            const msgId = activeMessageIdRef.current;
            if (!msgId || !ev.toolCallId) break;
            setMessages((prev) =>
              prev.map((msg) =>
                msg.id !== msgId
                  ? msg
                  : {
                      ...msg,
                      toolCalls: (msg.toolCalls || []).map((t) =>
                        t.id === ev.toolCallId ? { ...t, result: ev.result } : t
                      ),
                    }
              )
            );
            break;
          }

          case 'tool_end': {
            const msgId = activeMessageIdRef.current;
            if (!msgId || !ev.toolName) break;

            setMessages((prev) =>
              prev.map((msg) => {
                if (msg.id !== msgId) return msg;
                const calls = (msg.toolCalls || []).map((t) => {
                  const same = ev.toolCallId
                    ? t.id === ev.toolCallId
                    : t.name === ev.toolName && t.status === 'running';
                  return same
                    ? {
                        ...t,
                        name: ev.toolName || t.name,
                        status: ev.isError ? ('failed' as const) : ('completed' as const),
                        result: ev.result,
                        endedAt: Date.now(),
                      }
                    : t;
                });
                return { ...msg, toolCalls: calls };
              })
            );
            break;
          }

          case 'approval_required': {
            const msgId = activeMessageIdRef.current;
            if (!msgId || !ev.approval) break;

            setMessages((prev) =>
              prev.map((msg) =>
                msg.id === msgId
                  ? {
                      ...msg,
                      approval: ev.approval,
                    }
                  : msg
              )
            );
            break;
          }

          case 'oauth_open_url': {
            setOauthPrompt((prev) => ({
              ...(prev || { id: ev.token }),
              id: ev.token || prev?.id || 'oauth',
              url: ev.url,
              instructions: ev.instructions,
            }));
            if (ev.url) {
              window.open(ev.url, '_blank', 'noopener,noreferrer');
            }
            break;
          }

          case 'oauth_input_required': {
            setOauthPrompt((prev) => ({
              ...(prev || { id: ev.token }),
              id: ev.token,
              title: ev.delta,
              placeholder: ev.placeholder,
              secret: !!ev.data?.secret,
            }));
            break;
          }

          case 'ready':
          case 'commands_update': {
            refreshEngine();
            break;
          }

          case 'state': {
            const d = ev.data || {};
            if (d.contextUsage) setContextUsage(d.contextUsage);
            void requestJson<SessionStats>('/api/v1/copilot/stats').then((r) => {
              if (r.ok && r.data) setStats(r.data);
            });
            if (d.thinkingLevel) setThinkingLevelState(d.thinkingLevel);
            if (d.model) setActiveModelId(d.model);
            if (typeof d.fastMode === 'boolean' || typeof d.autoCompact === 'boolean') {
              setEngineSettings((prev) => ({
                ...prev,
                fastMode: typeof d.fastMode === 'boolean' ? d.fastMode : prev.fastMode,
                autoCompaction:
                  typeof d.autoCompact === 'boolean' ? d.autoCompact : prev.autoCompaction,
              }));
            }
            if (Array.isArray(d.todoPhases)) {
              const mapStatus = (st: string): AgentTodo['status'] =>
                st === 'completed' ? 'completed' : st === 'in_progress' ? 'in_progress' : st === 'abandoned' || st === 'blocked' ? 'failed' : 'pending';
              const list: AgentTodo[] = [];
              d.todoPhases.forEach((ph: any, pi: number) =>
                (ph.tasks || []).forEach((t: any, ti: number) =>
                  list.push({ id: `${pi}_${ti}`, text: t.content, status: mapStatus(t.status) })
                )
              );
              setTodos(list);
            }
            break;
          }

          case 'notice': {
            if (ev.delta) setNotice({ level: ev.level || 'info', text: ev.delta });
            break;
          }

          case 'command_output': {
            // Output of a locally handled slash command (/skills list, /status, ...).
            const text: string = ev.delta || '';
            if (!text) break;
            const msgId = activeMessageIdRef.current;
            if (msgId) {
              setMessages((prev) =>
                prev.map((msg) =>
                  msg.id === msgId ? { ...msg, content: (msg.content ? msg.content + '\n\n' : '') + text } : msg
                )
              );
            } else {
              setMessages((prev) => [
                ...prev,
                { id: `c_${Date.now()}`, role: 'assistant', content: text, timestamp: Date.now() },
              ]);
            }
            break;
          }

          case 'compaction_start': {
            setNotice({ level: 'info', text: 'Compacting conversation to free context…' });
            break;
          }

          case 'compaction_end': {
            setNotice(
              ev.data?.error
                ? { level: 'error', text: `Compaction failed: ${ev.data.error}` }
                : { level: 'info', text: 'Conversation compacted.' }
            );
            break;
          }

          case 'retry_start': {
            setNotice({
              level: 'warning',
              text: `Retrying (${ev.data?.attempt}/${ev.data?.maxAttempts}): ${ev.delta || 'provider error'}`,
            });
            break;
          }

          case 'retry_end': {
            if (!ev.data?.success) {
              setNotice({ level: 'error', text: ev.delta || 'Retries exhausted.' });
            }
            break;
          }

          case 'error': {
            setNotice({ level: 'error', text: ev.delta || 'Engine error' });
            const msgId = activeMessageIdRef.current;
            if (msgId) {
              setMessages((prev) =>
                prev.map((msg) =>
                  msg.id === msgId
                    ? { ...msg, content: msg.content || `Error: ${ev.delta}`, isStreaming: false }
                    : msg
                )
              );
            }
            setIsProcessing(false);
            activeMessageIdRef.current = null;
            break;
          }

          case 'engine_exit': {
            setIsProcessing(false);
            activeMessageIdRef.current = null;
            refreshEngine();
            break;
          }

          case 'session_info': {
            refreshSessions();
            break;
          }

          case 'workdir': {
            refreshEngine();
            void resetView();
            break;
          }

          case 'subagent_progress':
          case 'subagent': {
            const d = ev.data;
            if (!d?.id && d?.index === undefined) break;
            const key = d.id || `idx_${d.index}`;
            setSubagents((prev) => {
              const i = prev.findIndex((x) => x.id === key || (d.index !== undefined && x.index === d.index && x.agent === d.agent));
              const next: Subagent = {
                id: key,
                index: d.index ?? 0,
                agent: d.agent,
                // Lifecycle frames carry the subagent's name; progress frames only its raw task text.
                description: d.id ? d.description ?? d.id : i >= 0 ? prev[i].description : d.task ?? d.assignment,
                status: d.status || (i >= 0 ? prev[i].status : 'running'),
                startedAt: i >= 0 ? prev[i].startedAt : Date.now(),
                detached: d.detached,
              };
              if (i >= 0) {
                const copy = [...prev];
                copy[i] = { ...prev[i], ...next, id: d.id || prev[i].id, status: d.status || prev[i].status };
                return copy;
              }
              return [...prev, next];
            });
            break;
          }

          case 'ui_select': {
            setUiPrompt({
              id: ev.token,
              kind: 'select',
              title: ev.delta || 'Choose an option',
              options: ev.data?.options || [],
            });
            break;
          }

          case 'ui_editor': {
            setUiPrompt({ id: ev.token, kind: 'editor', title: ev.delta || 'Edit', prefill: ev.placeholder });
            break;
          }

          case 'assistant_error': {
            // reason "aborted" is the user's Stop; anything else is a provider/model failure.
            const msgId = activeMessageIdRef.current;
            if (!msgId) break;
            const text = ev.level === 'aborted' ? 'Stopped.' : `Error: ${ev.delta || 'the model request failed'}`;
            setMessages((prev) =>
              prev.map((msg) =>
                msg.id === msgId
                  ? { ...msg, content: msg.content ? `${msg.content}\n\n_${text}_` : `_${text}_` }
                  : msg
              )
            );
            break;
          }

          case 'login_done': {
            setOauthPrompt(null);
            if (ev.isError) setNotice({ level: 'error', text: `Sign-in failed: ${ev.result || ev.delta}` });
            else setNotice({ level: 'info', text: 'Signed in.' });
            refreshEngine();
            break;
          }

          case 'ui_ask': {
            setAskPrompt({ id: ev.token, questions: ev.data?.questions || [] });
            break;
          }

          case 'ui_cancel': {
            setAskPrompt((prev) => (prev && prev.id === ev.token ? null : prev));
            setUiPrompt((prev) => (prev && prev.id === ev.token ? null : prev));
            break;
          }

          case 'thinking_level_changed': {
            if ((ev as any).thinkingLevel) {
              setThinkingLevelState((ev as any).thinkingLevel);
            }
            break;
          }

          case 'agent_end': {
            const msgId = activeMessageIdRef.current;
            if (msgId) {
              setMessages((prev) =>
                prev.map((msg) =>
                  msg.id === msgId ? { ...msg, isStreaming: false } : msg
                )
              );
            }
            setIsProcessing(false);
            activeMessageIdRef.current = null;
            thinkingStartRef.current = null;
            refreshEngine();
            refreshSessions();
            break;
          }
        }
      } catch {}
    };

    eventSource.onerror = () => {
      // Auto-retrying
    };

    return () => {
      eventSource.close();
    };
  }, [refreshEngine, loadHistory, refreshSessions]);

  // Set Autonomy Mode
  const setAutonomyMode = useCallback(
    async (mode: AutonomyMode) => {
      setStatus((prev) => ({ ...prev, autonomyMode: mode }));
      await requestJson('/api/v1/copilot/mode', {
        method: 'POST',
        body: JSON.stringify({ mode }),
      });
      await refreshEngine();
    },
    [refreshEngine]
  );

  // Send prompt
  const sendPrompt = useCallback(
    async (
      messageText: string,
      attachedFiles: string[] = [],
      images: Array<{ data: string; mimeType: string; name?: string }> = []
    ) => {
      if (!messageText.trim() && images.length === 0) return;

      if (isProcessing) {
        // A turn is running: deliver as a steering message instead of dropping it.
        const queued = await requestJson('/api/v1/copilot/queue', {
          method: 'POST',
          body: JSON.stringify({ message: messageText.trim(), mode: 'steer' }),
        });
        if (queued.ok) {
          setMessages((prev) => [
            ...prev,
            { id: `u_${Date.now()}`, role: 'user', content: messageText.trim(), timestamp: Date.now() },
          ]);
        } else {
          setNotice({ level: 'error', text: queued.error || 'Could not steer the running turn' });
        }
        return;
      }

      const userMsg: CopilotMessage = {
        id: `u_${Date.now()}`,
        role: 'user',
        content: messageText.trim(),
        attached: attachedFiles.length || images.length ? [...attachedFiles, ...images.map((i) => i.name || 'image')] : undefined,
        timestamp: Date.now(),
      };

      const assistantId = `a_${Date.now()}`;
      const assistantMsg: CopilotMessage = {
        id: assistantId,
        role: 'assistant',
        content: '',
        timestamp: Date.now(),
        isStreaming: true,
      };

      activeMessageIdRef.current = assistantId;
      thinkingStartRef.current = null;
      setIsProcessing(true);

      setMessages((prev) => [...prev, userMsg, assistantMsg]);

      let fullPrompt = messageText.trim();
      if (attachedFiles.length > 0) {
        // omp's native file mention syntax; paths are relative to its working directory.
        fullPrompt = `${attachedFiles.map((f) => `@${f}`).join(' ')}\n\n${fullPrompt}`;
      }

      const res = await requestJson('/api/v1/copilot/prompt', {
        method: 'POST',
        body: JSON.stringify({
          message: fullPrompt,
          images: images.map((i) => ({ data: i.data, mimeType: i.mimeType })),
        }),
      });

      if (!res.ok) {
        setIsProcessing(false);
        setMessages((prev) =>
          prev.map((msg) =>
            msg.id === assistantId
              ? {
                  ...msg,
                  content: `Error: ${res.error || 'Failed to dispatch prompt to engine'}`,
                  isStreaming: false,
                }
              : msg
          )
        );
      }
    },
    [isProcessing]
  );

  // Confirm safe mode approval
  const confirmApproval = useCallback(async (id: string, confirmed: boolean) => {
    await requestJson('/api/v1/copilot/confirm', {
      method: 'POST',
      body: JSON.stringify({ id, confirmed }),
    });

    setMessages((prev) =>
      prev.map((msg) => (msg.approval?.id === id ? { ...msg, approval: undefined } : msg))
    );
  }, []);

  // Abort active execution
  const abort = useCallback(async () => {
    await requestJson('/api/v1/copilot/abort', { method: 'POST' });
    setIsProcessing(false);
    activeMessageIdRef.current = null;
  }, []);

  // Change where omp runs: workspace root, one site, or a throw-away temp dir.
  const setWorkDir = useCallback(
    async (kind: WorkDir['kind'], appId?: string) => {
      const res = await requestJson<WorkDir>('/api/v1/copilot/workdir', {
        method: 'POST',
        body: JSON.stringify({ kind, appId: appId || '' }),
      });
      if (res.ok && res.data) {
        setWorkDirState(res.data);
        setTodos([]);
        await refreshEngine();
      } else {
        setNotice({ level: 'error', text: res.error || 'Could not change directory' });
      }
      return !!res.ok;
    },
    [refreshEngine]
  );

  const compactContext = useCallback(async () => {
    setNotice({ level: 'info', text: 'Compacting conversation…' });
    const res = await requestJson('/api/v1/copilot/compact', { method: 'POST', body: '{}' });
    if (!res.ok) setNotice({ level: 'error', text: res.error || 'Compaction failed' });
    return !!res.ok;
  }, []);

  const setEngineSetting = useCallback(
    async (key: 'fast_mode' | 'auto_compaction' | 'auto_retry', enabled: boolean) => {
      const res = await requestJson<EngineSettings>('/api/v1/copilot/settings', {
        method: 'POST',
        body: JSON.stringify({ key, enabled }),
      });
      if (res.ok && res.data) setEngineSettings(res.data);
      else setNotice({ level: 'error', text: res.error || 'Could not update setting' });
    },
    []
  );

  const submitUiInput = useCallback(async (id: string, value: string) => {
    await requestJson('/api/v1/copilot/input', {
      method: 'POST',
      body: JSON.stringify({ id, value }),
    });
    setUiPrompt(null);
  }, []);

  const cancelSubagent = useCallback(async (id: string) => {
    await requestJson('/api/v1/copilot/subagents/cancel', {
      method: 'POST',
      body: JSON.stringify({ id }),
    });
  }, []);

  const submitAnswers = useCallback(
    async (
      id: string,
      answers: Array<{ id: string; selectedOptions: string[]; customInput?: string }> | null
    ) => {
      await requestJson('/api/v1/copilot/answers', {
        method: 'POST',
        body: JSON.stringify(answers ? { id, answers } : { id, cancel: true }),
      });
      setAskPrompt(null);
    },
    []
  );

  const uploadFile = useCallback(async (file: File): Promise<string | null> => {
    const form = new FormData();
    form.append('file', file);
    const res = await requestJson<{ path: string }>('/api/v1/copilot/upload', { method: 'POST', body: form });
    if (res.ok && res.data?.path) return res.data.path;
    setNotice({ level: 'error', text: res.error || `Could not upload ${file.name}` });
    return null;
  }, []);

  const removeProvider = useCallback(
    async (providerId: string) => {
      const acc = await requestJson<{ accounts: Array<{ credentialId: number }> }>(
        `/api/v1/copilot/accounts?provider=${encodeURIComponent(providerId)}`
      );
      for (const a of acc.data?.accounts || []) {
        await requestJson('/api/v1/copilot/logout', {
          method: 'POST',
          body: JSON.stringify({ providerId, credentialId: a.credentialId }),
        });
      }
      await requestJson('/api/v1/copilot/providers', {
        method: 'POST',
        body: JSON.stringify({ [`${providerId}_api_key`]: '' }),
      });
      await refreshEngine();
    },
    [refreshEngine]
  );

  const forkChat = useCallback(async () => {
    const res = await requestJson('/api/v1/copilot/fork', { method: 'POST', body: '{}' });
    if (!res.ok) setNotice({ level: 'error', text: res.error || 'Could not fork' });
    else await resetView();
  }, [resetView]);

  const handoffChat = useCallback(async () => {
    setNotice({ level: 'info', text: 'Summarising this chat into a fresh one…' });
    const res = await requestJson('/api/v1/copilot/handoff', { method: 'POST', body: '{}' });
    if (!res.ok) setNotice({ level: 'error', text: res.error || 'Hand-off failed' });
    else await resetView();
  }, [resetView]);

  const exportChat = useCallback(() => {
    const a = document.createElement('a');
    a.href = '/api/v1/copilot/export';
    a.download = 'conversation.html';
    document.body.appendChild(a);
    a.click();
    a.remove();
  }, []);

  const dismissNotice = useCallback(() => setNotice(null), []);

  const clearCurrentChat = createNewChat;

  return {
    sessions,
    activeSessionId,
    switchSession,
    createNewChat,
    deleteSession,
    messages,
    models,
    activeModel,
    selectModel,
    thinkingLevel,
    availableThinkingLevels,
    setThinkingLevel,
    workDir,
    setWorkDir,
    engineSettings,
    setEngineSetting,
    compactContext,
    forkChat,
    handoffChat,
    exportChat,
    notice,
    dismissNotice,
    uiPrompt,
    submitUiInput,
    todos,
    subagents,
    cancelSubagent,
    removeProvider,
    stats,
    askPrompt,
    submitAnswers,
    uploadFile,
    providers,
    loginProviders,
    oauthPrompt,
    startOAuthLogin,
    submitOAuthInput,
    saveProviders,
    status,
    setAutonomyMode,
    isProcessing,
    connectionError,
    sendPrompt,
    confirmApproval,
    abort,
    clearCurrentChat,
    refreshEngine,
    contextUsage,
    slashCommands,
  };
}
