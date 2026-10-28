import React, { useRef, useEffect } from 'react';
import { Bot, User, Sparkles, Activity, Globe, HardDrive } from 'lucide-react';
import { ReasoningSteps } from './ReasoningSteps';
import { ToolGroup } from './ToolCallCard';
import { ApprovalCard } from './ApprovalCard';
import { CopilotComposer } from './CopilotComposer';
import { useCopilot } from './useCopilot';

interface CopilotThreadProps {
  className?: string;
  onClose?: () => void;
}

export const CopilotThread: React.FC<CopilotThreadProps> = ({ className = '' }) => {
  const {
    messages,
    status,
    isProcessing,
    sendPrompt,
    confirmApproval,
    abort,
    setAutonomyMode,
  } = useCopilot();

  const viewportRef = useRef<HTMLDivElement>(null);

  // Auto scroll to bottom as stream updates
  useEffect(() => {
    if (viewportRef.current) {
      viewportRef.current.scrollTop = viewportRef.current.scrollHeight;
    }
  }, [messages, isProcessing]);

  const quickPrompts = [
    {
      label: 'Diagnose performance & memory',
      icon: <Activity className="h-3 w-3 text-emerald-400" />,
      prompt: 'Diagnose server load, swap, and top memory-consuming PHP processes.',
    },
    {
      label: 'List all PHP sites',
      icon: <Globe className="h-3 w-3 text-blue-400" />,
      prompt: 'List all running PHP sites with their versions and domain status.',
    },
    {
      label: 'Check Server Resources',
      icon: <HardDrive className="h-3 w-3 text-amber-400" />,
      prompt: 'Get server resources: CPU, RAM, 5GB Swap usage, and swappiness value.',
    },
  ];

  return (
    <div
      data-slot="chat-thread"
      className={`flex flex-col h-full bg-[#0a0a0c] text-zinc-100 ${className}`}
    >
      {/* Scrollable conversation thread */}
      <div
        data-slot="message-scroller"
        className="flex-1 min-h-0 overflow-y-auto px-4 py-4 space-y-4"
        ref={viewportRef}
      >
        {messages.map((msg) => {
          const isUser = msg.role === 'user';

          return (
            <div
              key={msg.id}
              data-slot="message"
              className={`flex gap-3 text-xs leading-relaxed ${
                isUser ? 'justify-end' : 'justify-start'
              }`}
            >
              {!isUser && (
                <div className="flex h-7 w-7 items-center justify-center rounded-lg bg-orange-500/10 text-orange-400 border border-orange-500/20 shrink-0 mt-0.5">
                  <Bot className="h-4 w-4" />
                </div>
              )}

              <div
                className={`max-w-[85%] rounded-2xl p-3.5 ${
                  isUser
                    ? 'bg-zinc-800 text-zinc-100 rounded-br-xs border border-white/[0.06]'
                    : 'bg-zinc-900/60 text-zinc-200 rounded-bl-xs border border-white/[0.06] backdrop-blur-xs'
                }`}
              >
                {/* Reasoning trace */}
                {!isUser && msg.thinking && (
                  <ReasoningSteps
                    thinking={msg.thinking}
                    durationSec={msg.thinkingDurationSec}
                    isStreaming={msg.isStreaming && !msg.content}
                  />
                )}

                {/* Tool calls */}
                {!isUser && msg.toolCalls && msg.toolCalls.length > 0 && (
                  <ToolGroup tools={msg.toolCalls} />
                )}

                {/* Safe Mode confirmation card */}
                {!isUser && msg.approval && (
                  <ApprovalCard
                    approval={msg.approval}
                    onConfirm={(confirmed) =>
                      confirmApproval(msg.approval!.id, confirmed)
                    }
                  />
                )}

                {/* Message text */}
                {msg.content && (
                  <div className="whitespace-pre-wrap font-sans text-xs text-zinc-200 selection:bg-orange-500/30">
                    {msg.content}
                    {msg.isStreaming && (
                      <span className="inline-block h-3.5 w-1.5 ml-1 bg-orange-400 animate-pulse align-middle" />
                    )}
                  </div>
                )}
              </div>

              {isUser && (
                <div className="flex h-7 w-7 items-center justify-center rounded-lg bg-zinc-800 text-zinc-300 border border-white/10 shrink-0 mt-0.5">
                  <User className="h-4 w-4" />
                </div>
              )}
            </div>
          );
        })}

        {/* Quick suggestions when conversation is fresh */}
        {messages.length <= 1 && (
          <div className="pt-4">
            <p className="text-[11px] font-semibold uppercase tracking-wider text-zinc-500 mb-2">
              Suggested Tasks
            </p>
            <div className="flex flex-col gap-1.5">
              {quickPrompts.map((q, idx) => (
                <button
                  key={idx}
                  type="button"
                  onClick={() => sendPrompt(q.prompt)}
                  className="flex items-center gap-2.5 rounded-lg border border-white/[0.06] bg-zinc-900/40 px-3 py-2 text-left text-xs text-zinc-300 hover:border-white/15 hover:bg-zinc-900/80 transition-all cursor-pointer active:scale-[0.99]"
                >
                  {q.icon}
                  <span>{q.label}</span>
                </button>
              ))}
            </div>
          </div>
        )}
      </div>

      {/* Composer at bottom */}
      <CopilotComposer
        onSend={sendPrompt}
        onAbort={abort}
        isProcessing={isProcessing}
        autonomyMode={status.autonomy_mode}
        onToggleMode={setAutonomyMode}
      />
    </div>
  );
};
