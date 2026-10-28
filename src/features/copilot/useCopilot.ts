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
} from './types';

const SESSIONS_STORAGE_KEY = 'nd_ai_sessions';
const ACTIVE_SESSION_KEY = 'nd_ai_active_session';
const MESSAGES_PREFIX = 'nd_ai_messages_';

export function useCopilot() {
  // Sessions
  const [sessions, setSessions] = useState<ChatSession[]>(() => {
    try {
      const saved = localStorage.getItem(SESSIONS_STORAGE_KEY);
      if (saved) return JSON.parse(saved);
    } catch {}
    const initSession: ChatSession = {
      id: 'default',
      title: 'Current Conversation',
      createdAt: Date.now(),
      updatedAt: Date.now(),
      messageCount: 1,
    };
    return [initSession];
  });

  const [activeSessionId, setActiveSessionId] = useState<string>(() => {
    return localStorage.getItem(ACTIVE_SESSION_KEY) || 'default';
  });

  // Messages for active session
  const [messages, setMessages] = useState<CopilotMessage[]>(() => {
    try {
      const savedId = localStorage.getItem(ACTIVE_SESSION_KEY) || 'default';
      const savedMsgs = localStorage.getItem(MESSAGES_PREFIX + savedId);
      if (savedMsgs) return JSON.parse(savedMsgs);
    } catch {}
    return [
      {
        id: 'welcome',
        role: 'assistant',
        content:
          'Hello! I am your NextDeploy AI assistant. I can help manage PHP sites, optimize OPcache/FPM, diagnose database bottlenecks, configure domains, inspect Docker containers, and monitor server resources.',
        timestamp: Date.now(),
      },
    ];
  });

  // Models, Thinking, Context & Providers
  const [models, setModels] = useState<AIModel[]>([]);
  const [activeModelId, setActiveModelId] = useState<string>('');
  const [thinkingLevel, setThinkingLevelState] = useState<ThinkingLevel>('low');
  const [contextUsage, setContextUsage] = useState<{
    tokens: number;
    contextWindow: number;
    percent: number;
  } | null>(null);
  const [slashCommands, setSlashCommands] = useState<Array<{ name: string; description?: string; input?: { hint?: string } }>>([]);
  const [providers, setProviders] = useState<ProvidersConfig | null>(null);
  const [loginProviders, setLoginProviders] = useState<LoginProvider[]>([]);
  const [oauthPrompt, setOauthPrompt] = useState<OAuthPrompt | null>(null);

  const activeModel = models.find((m) => m.id === activeModelId) || models[0] || null;

  // Status
  const [status, setStatus] = useState<CopilotStatus>({
    running: false,
    active: false,
    autonomyMode: 'safe',
  });
  const [isProcessing, setIsProcessing] = useState(false);
  const [connectionError, setConnectionError] = useState<string | null>(null);

  const activeMessageIdRef = useRef<string | null>(null);
  const thinkingStartRef = useRef<number | null>(null);

  // Persist sessions
  useEffect(() => {
    try {
      localStorage.setItem(SESSIONS_STORAGE_KEY, JSON.stringify(sessions));
    } catch {}
  }, [sessions]);

  // Persist active session ID
  useEffect(() => {
    try {
      localStorage.setItem(ACTIVE_SESSION_KEY, activeSessionId);
    } catch {}
  }, [activeSessionId]);

  // Persist messages for current session
  useEffect(() => {
    try {
      localStorage.setItem(MESSAGES_PREFIX + activeSessionId, JSON.stringify(messages));
    } catch {}
  }, [messages, activeSessionId]);

  // Switch session
  const switchSession = useCallback((sessionId: string) => {
    setActiveSessionId(sessionId);
    try {
      const saved = localStorage.getItem(MESSAGES_PREFIX + sessionId);
      if (saved) {
        setMessages(JSON.parse(saved));
        return;
      }
    } catch {}
    setMessages([
      {
        id: 'welcome',
        role: 'assistant',
        content: 'Conversation loaded. How can I help you?',
        timestamp: Date.now(),
      },
    ]);
  }, []);

  // Create new chat
  const createNewChat = useCallback(() => {
    const newId = `session_${Date.now()}`;
    const newSession: ChatSession = {
      id: newId,
      title: 'New Conversation',
      createdAt: Date.now(),
      updatedAt: Date.now(),
      messageCount: 1,
    };
    setSessions((prev) => [newSession, ...prev]);
    setActiveSessionId(newId);
    setMessages([
      {
        id: 'welcome',
        role: 'assistant',
        content: 'New chat started. What would you like to work on?',
        timestamp: Date.now(),
      },
    ]);
  }, []);

  // Delete session
  const deleteSession = useCallback((sessionId: string) => {
    try {
      localStorage.removeItem(MESSAGES_PREFIX + sessionId);
    } catch {}
    setSessions((prev) => {
      const filtered = prev.filter((s) => s.id !== sessionId);
      if (filtered.length === 0) {
        const fallback: ChatSession = {
          id: 'default',
          title: 'Current Conversation',
          createdAt: Date.now(),
          updatedAt: Date.now(),
          messageCount: 1,
        };
        setActiveSessionId('default');
        setMessages([
          {
            id: 'welcome',
            role: 'assistant',
            content: 'Hello! How can I assist you?',
            timestamp: Date.now(),
          },
        ]);
        return [fallback];
      }
      return filtered;
    });
  }, []);

  // Fetch status, models, and providers
  const refreshEngine = useCallback(async () => {
    const statusRes = await requestJson<CopilotStatus>('/api/v1/copilot/status');
    if (statusRes.ok && statusRes.data) {
      setStatus(statusRes.data);
      setConnectionError(null);
    } else {
      setConnectionError(statusRes.error || 'Engine offline');
    }

    const modelsRes = await requestJson<{
      active: string;
      thinkingLevel?: ThinkingLevel;
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
      if (modelsRes.data.thinkingLevel) {
        setThinkingLevelState(modelsRes.data.thinkingLevel);
      }
      if (modelsRes.data.contextUsage) {
        setContextUsage(modelsRes.data.contextUsage);
      }
    }

    const cmdRes = await requestJson<{
      commands: Array<{ name: string; description?: string; input?: { hint?: string } }>;
    }>('/api/v1/copilot/commands');
    if (cmdRes.ok && cmdRes.data?.commands) {
      setSlashCommands(cmdRes.data.commands);
    }

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
      await requestJson('/api/v1/copilot/models', {
        method: 'POST',
        body: JSON.stringify({ provider, modelId }),
      });
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
      const payload = {
        openaiKey: keys.openai || keys.openaiKey || '',
        anthropicKey: keys.anthropic || keys.anthropicKey || '',
        deepseekKey: keys.deepseek || keys.deepseekKey || '',
        geminiKey: keys.gemini || keys.geminiKey || '',
        ollamaHost: keys.ollama || keys.ollamaHost || '',
      };
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

            const newTool: ToolExecution = {
              id: `tool_${Date.now()}`,
              name: ev.toolName,
              args: ev.toolArgs,
              status: 'running',
              startedAt: Date.now(),
            };

            setMessages((prev) =>
              prev.map((msg) => {
                if (msg.id !== msgId) return msg;
                const calls = [...(msg.toolCalls || []), newTool];
                return { ...msg, toolCalls: calls };
              })
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
                  if (t.name === ev.toolName && t.status === 'running') {
                    return {
                      ...t,
                      status: ev.isError ? ('failed' as const) : ('completed' as const),
                      result: ev.result,
                      endedAt: Date.now(),
                    };
                  }
                  return t;
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
            }));
            break;
          }

          case 'ready':
          case 'commands_update': {
            refreshEngine();
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
  }, [refreshEngine]);

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
    async (messageText: string, attachedFiles: string[] = []) => {
      if (!messageText.trim() || isProcessing) return;

      const userMsg: CopilotMessage = {
        id: `u_${Date.now()}`,
        role: 'user',
        content: messageText.trim(),
        attached: attachedFiles.length ? attachedFiles : undefined,
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

      setMessages((prev) => {
        const next = [...prev, userMsg, assistantMsg];
        setSessions((sList) =>
          sList.map((s) => {
            if (s.id === activeSessionId) {
              return {
                ...s,
                title:
                  s.title === 'New Conversation' || s.title === 'Current Conversation'
                    ? messageText.slice(0, 36) + (messageText.length > 36 ? '...' : '')
                    : s.title,
                messageCount: next.length,
                updatedAt: Date.now(),
              };
            }
            return s;
          })
        );
        return next;
      });

      let fullPrompt = messageText.trim();
      if (attachedFiles.length > 0) {
        fullPrompt = `[Attached files: ${attachedFiles.join(', ')}]\n\n${fullPrompt}`;
      }

      const res = await requestJson('/api/v1/copilot/prompt', {
        method: 'POST',
        body: JSON.stringify({ message: fullPrompt }),
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
    [isProcessing, activeSessionId]
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

  // Clear current chat
  const clearCurrentChat = useCallback(() => {
    setMessages([
      {
        id: 'welcome',
        role: 'assistant',
        content: 'Chat cleared. How can I assist you?',
        timestamp: Date.now(),
      },
    ]);
  }, []);

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
    setThinkingLevel,
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
