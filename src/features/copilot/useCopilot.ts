import { useState, useEffect, useCallback, useRef } from 'react';
import { requestJson } from '../../lib/api/http';
import {
  CopilotMessage,
  CopilotStatus,
  AutonomyMode,
  AgentStreamEvent,
  ToolExecution,
} from './types';

export function useCopilot() {
  const [messages, setMessages] = useState<CopilotMessage[]>([
    {
      id: 'welcome',
      role: 'assistant',
      content:
        'Hello! I am your NextDeploy AI assistant powered by oh-my-pi (`omp`). I can manage PHP sites, tune FPM/OPcache settings, analyze slow queries, add custom domains, inspect Docker containers, and optimize server resources. How can I help you today?',
      timestamp: Date.now(),
    },
  ]);
  const [status, setStatus] = useState<CopilotStatus>({
    running: false,
    active: false,
    autonomy_mode: 'safe',
    pending_confirmations: 0,
  });
  const [isProcessing, setIsProcessing] = useState(false);
  const activeMessageIdRef = useRef<string | null>(null);
  const thinkingStartRef = useRef<number | null>(null);

  // Fetch initial status
  const fetchStatus = useCallback(async () => {
    const res = await requestJson<CopilotStatus>('/api/v1/copilot/status');
    if (res.ok && res.data) {
      setStatus(res.data);
    }
  }, []);

  // Connect to SSE event stream
  useEffect(() => {
    fetchStatus();

    const eventSource = new EventSource('/api/v1/copilot/stream');

    eventSource.onmessage = (event) => {
      try {
        if (!event.data || event.data.trim() === ': heartbeat') return;
        const ev: AgentStreamEvent = JSON.parse(event.data);

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
              prev.map((msg) =>
                msg.id === msgId
                  ? {
                      ...msg,
                      toolCalls: [...(msg.toolCalls || []), newTool],
                    }
                  : msg
              )
            );
            break;
          }

          case 'tool_end': {
            const msgId = activeMessageIdRef.current;
            if (!msgId) break;

            setMessages((prev) =>
              prev.map((msg) => {
                if (msg.id !== msgId || !msg.toolCalls) return msg;
                const calls = [...msg.toolCalls];
                const lastIdx = calls.length - 1;
                if (lastIdx >= 0) {
                  calls[lastIdx] = {
                    ...calls[lastIdx],
                    status: 'completed',
                    result: ev.toolResult,
                    endedAt: Date.now(),
                  };
                }
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

          case 'agent_end': {
            const msgId = activeMessageIdRef.current;
            if (msgId) {
              setMessages((prev) =>
                prev.map((msg) =>
                  msg.id === msgId
                    ? {
                        ...msg,
                        isStreaming: false,
                      }
                    : msg
                )
              );
            }
            setIsProcessing(false);
            activeMessageIdRef.current = null;
            thinkingStartRef.current = null;
            fetchStatus();
            break;
          }
        }
      } catch {
        // Ignored unparsable frame
      }
    };

    eventSource.onerror = () => {
      // Auto-reconnect managed by browser EventSource
    };

    return () => {
      eventSource.close();
    };
  }, [fetchStatus]);

  // Send user message
  const sendPrompt = useCallback(
    async (messageText: string) => {
      if (!messageText.trim() || isProcessing) return;

      const userMsg: CopilotMessage = {
        id: `u_${Date.now()}`,
        role: 'user',
        content: messageText.trim(),
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

      const res = await requestJson('/api/v1/copilot/prompt', {
        method: 'POST',
        body: JSON.stringify({ message: messageText.trim() }),
      });

      if (!res.ok) {
        setMessages((prev) =>
          prev.map((msg) =>
            msg.id === assistantId
              ? {
                  ...msg,
                  content: `Error starting agent: ${res.error || 'Server error'}`,
                  isStreaming: false,
                }
              : msg
          )
        );
        setIsProcessing(false);
        activeMessageIdRef.current = null;
      }
    },
    [isProcessing]
  );

  // Confirm or reject an action
  const confirmApproval = useCallback(
    async (id: string, confirmed: boolean) => {
      const res = await requestJson<{ resolved: boolean }>('/api/v1/copilot/confirm', {
        method: 'POST',
        body: JSON.stringify({ id, confirmed }),
      });

      // Clear approval from message
      setMessages((prev) =>
        prev.map((msg) =>
          msg.approval?.id === id ? { ...msg, approval: undefined } : msg
        )
      );

      return res.ok;
    },
    []
  );

  // Abort execution
  const abort = useCallback(async () => {
    await requestJson('/api/v1/copilot/abort', { method: 'POST' });
    setIsProcessing(false);
    if (activeMessageIdRef.current) {
      const msgId = activeMessageIdRef.current;
      setMessages((prev) =>
        prev.map((msg) =>
          msg.id === msgId ? { ...msg, isStreaming: false } : msg
        )
      );
      activeMessageIdRef.current = null;
    }
  }, []);

  // Switch autonomy mode
  const setAutonomyMode = useCallback(
    async (mode: AutonomyMode) => {
      const res = await requestJson<{ mode: AutonomyMode }>('/api/v1/copilot/mode', {
        method: 'POST',
        body: JSON.stringify({ mode }),
      });
      if (res.ok && res.data) {
        setStatus((prev) => ({ ...prev, autonomy_mode: res.data!.mode }));
      }
    },
    []
  );

  return {
    messages,
    status,
    isProcessing,
    sendPrompt,
    confirmApproval,
    abort,
    setAutonomyMode,
  };
}
