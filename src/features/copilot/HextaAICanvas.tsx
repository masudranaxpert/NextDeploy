import React, { useState, useRef, useEffect } from 'react';
import {
  PanelLeftClose,
  PanelLeftOpen,
  Settings2,
  Trash2,
  User,
  Bot,
  AlertTriangle,
} from 'lucide-react';
import { useCopilot } from './useCopilot';
import { ChatSidebar } from './ChatSidebar';
import { PromptInput } from './PromptInput';
import { Thinking } from './Thinking';
import { ToolCalls } from './ToolCalls';
import { AgentTodos } from './AgentTodos';
import { ApprovalCard } from './ApprovalCard';
import { MarkdownContent } from './MarkdownContent';
import { ProviderAuthModal } from './ProviderAuthModal';

interface HextaAICanvasProps {
  initialSidebarOpen?: boolean;
  isDrawer?: boolean;
  onCloseDrawer?: () => void;
}

export const HextaAICanvas: React.FC<HextaAICanvasProps> = ({
  initialSidebarOpen = true,
  isDrawer = false,
  onCloseDrawer,
}) => {
  const {
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
    isProcessing,
    connectionError,
    sendPrompt,
    confirmApproval,
    abort,
    clearCurrentChat,
    contextUsage,
    slashCommands,
    refreshEngine,
  } = useCopilot();

  const [isSidebarOpen, setIsSidebarOpen] = useState(initialSidebarOpen);
  const [isSettingsOpen, setIsSettingsOpen] = useState(false);
  const messagesEndRef = useRef<HTMLDivElement>(null);

  // Auto-scroll to bottom when messages update
  useEffect(() => {
    messagesEndRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [messages, isProcessing]);

  // Keyboard shortcut: Cmd/Ctrl + N for new chat
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key === 'n') {
        e.preventDefault();
        createNewChat();
      }
    };
    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [createNewChat]);

  const currentSession = sessions.find((s) => s.id === activeSessionId);
  const sessionTitle = currentSession?.title || 'NextDeploy AI';

  return (
    <div
      data-slot="hexta-ai-canvas"
      className="flex h-full w-full overflow-hidden bg-[#090a0b] text-zinc-100"
    >
      {/* Collapsible Left Sidebar */}
      {isSidebarOpen && (
        <ChatSidebar
          sessions={sessions}
          activeSessionId={activeSessionId}
          onSelectSession={switchSession}
          onNewChat={createNewChat}
          onDeleteSession={deleteSession}
          onOpenSettings={() => setIsSettingsOpen(true)}
          className={isDrawer ? 'w-56' : 'w-64'}
        />
      )}

      {/* Main Canvas Area */}
      <div className="flex flex-1 flex-col min-w-0 h-full overflow-hidden">
        {/* Top Header Bar */}
        <header className="flex h-12 items-center justify-between border-b border-white/[0.08] px-4 bg-[#0c0d0e]/80 backdrop-blur-sm shrink-0">
          <div className="flex items-center gap-2.5 min-w-0">
            <button
              type="button"
              onClick={() => setIsSidebarOpen(!isSidebarOpen)}
              className="inline-flex size-8 items-center justify-center rounded-lg text-zinc-400 hover:bg-white/[0.06] hover:text-white transition-colors"
              title={isSidebarOpen ? 'Close sidebar' : 'Open sidebar'}
            >
              {isSidebarOpen ? (
                <PanelLeftClose className="size-4" />
              ) : (
                <PanelLeftOpen className="size-4" />
              )}
            </button>

            <span className="text-xs font-semibold text-zinc-200 truncate">
              {sessionTitle}
            </span>

            {/* Engine status indicator */}
            <div className="flex items-center gap-1.5 rounded-full border border-white/[0.06] bg-white/[0.02] px-2 py-0.5 text-[10px] text-zinc-400">
              <span
                className={`size-1.5 rounded-full ${
                  status.running
                    ? isProcessing
                      ? 'bg-blue-400 animate-pulse'
                      : 'bg-emerald-400'
                    : 'bg-amber-400'
                }`}
              />
              <span className="hidden sm:inline">
                {status.running ? (isProcessing ? 'Thinking' : 'Ready') : 'Starting'}
              </span>
            </div>
          </div>

          <div className="flex items-center gap-1">
            <button
              type="button"
              onClick={clearCurrentChat}
              className="inline-flex size-8 items-center justify-center rounded-lg text-zinc-400 hover:bg-white/[0.06] hover:text-red-400 transition-colors"
              title="Clear messages"
            >
              <Trash2 className="size-3.5" />
            </button>

            <button
              type="button"
              onClick={() => setIsSettingsOpen(true)}
              className="inline-flex size-8 items-center justify-center rounded-lg text-zinc-400 hover:bg-white/[0.06] hover:text-white transition-colors"
              title="Configure API Keys"
            >
              <Settings2 className="size-3.5" />
            </button>

            {isDrawer && onCloseDrawer && (
              <button
                type="button"
                onClick={onCloseDrawer}
                className="ml-1 inline-flex size-8 items-center justify-center rounded-lg text-zinc-400 hover:bg-white/[0.06] hover:text-white transition-colors"
                title="Close Drawer (Esc)"
              >
                ✕
              </button>
            )}
          </div>
        </header>

        {/* Connection Error Banner */}
        {connectionError && (
          <div className="flex items-center gap-2 border-b border-amber-500/20 bg-amber-500/10 px-4 py-2 text-xs text-amber-300 shrink-0">
            <AlertTriangle className="size-4 shrink-0 text-amber-400" />
            <span className="truncate">{connectionError}</span>
          </div>
        )}

        {/* Messages Scroller */}
        <div
          data-slot="message-scroller"
          className="flex-1 overflow-y-auto px-4 py-6 md:px-8 space-y-6"
        >
          {messages.length === 0 ? (
            <div className="flex flex-col items-center justify-center h-full text-center py-12">
              <div className="flex size-12 items-center justify-center rounded-2xl bg-orange-500/10 text-orange-400 border border-orange-500/20 mb-4 shadow-sm">
                <Bot className="size-6" />
              </div>
              <h3 className="text-base font-semibold text-white">How can NextDeploy AI assist you?</h3>
              <p className="mt-1 max-w-sm text-xs text-zinc-400 leading-relaxed">
                Autonomous server management, site diagnostics, Docker containers, PHP OPcache tuning, and edge network management.
              </p>
            </div>
          ) : (
            messages.map((msg) => {
              const isUser = msg.role === 'user';

              return (
                <div
                  key={msg.id}
                  data-slot={isUser ? 'chat-user-message' : 'chat-assistant-message'}
                  className={`flex gap-3.5 ${isUser ? 'justify-end' : 'justify-start'}`}
                >
                  {/* Assistant Avatar */}
                  {!isUser && (
                    <div className="flex size-7 shrink-0 items-center justify-center rounded-lg bg-orange-500/10 text-orange-400 border border-orange-500/20 mt-0.5">
                      <Bot className="size-4" />
                    </div>
                  )}

                  {/* Message Bubble & Cards */}
                  <div className={`flex flex-col min-w-0 max-w-[85%] md:max-w-[78%] ${isUser ? 'items-end' : 'items-start'}`}>
                    {/* User Bubble */}
                    {isUser ? (
                      <div
                        data-slot="bubble"
                        className="rounded-2xl bg-orange-600 px-4 py-2.5 text-xs text-white shadow-sm font-sans whitespace-pre-wrap leading-relaxed"
                      >
                        {msg.content}
                      </div>
                    ) : (
                      <div className="w-full space-y-2">
                        {/* Thinking Accordion */}
                        {msg.thinking && (
                          <Thinking
                            thinking={msg.thinking}
                            durationSec={msg.thinkingDurationSec}
                            isStreaming={msg.isStreaming && !msg.content}
                          />
                        )}

                        {/* Agent Todos / Checkpoints */}
                        {msg.todos && msg.todos.length > 0 && (
                          <AgentTodos todos={msg.todos} />
                        )}

                        {/* Tool Calls Execution Group */}
                        {msg.toolCalls && msg.toolCalls.length > 0 && (
                          <ToolCalls tools={msg.toolCalls} />
                        )}

                        {/* Safe Mode Approval Card */}
                        {msg.approval && (
                          <ApprovalCard
                            approval={msg.approval}
                            onConfirm={(confirmed) =>
                              confirmApproval(msg.approval!.id, confirmed)
                            }
                          />
                        )}

                        {/* Markdown Assistant Content */}
                        {(msg.content || msg.isStreaming) && (
                          <div
                            data-slot="bubble"
                            className="rounded-2xl border border-white/[0.08] bg-[#111215] p-4 shadow-sm"
                          >
                            <MarkdownContent
                              content={msg.content}
                              isStreaming={msg.isStreaming}
                            />
                          </div>
                        )}
                      </div>
                    )}
                  </div>

                  {/* User Avatar */}
                  {isUser && (
                    <div className="flex size-7 shrink-0 items-center justify-center rounded-lg bg-zinc-800 text-zinc-300 border border-white/10 mt-0.5">
                      <User className="size-4" />
                    </div>
                  )}
                </div>
              );
            })
          )}
          <div ref={messagesEndRef} />
        </div>

        {/* Bottom Composer Bar */}
        <div className="border-t border-white/[0.08] bg-[#0c0d0e]/95 p-3 md:p-4 shrink-0">
          <PromptInput
            onSend={sendPrompt}
            onAbort={abort}
            isProcessing={isProcessing}
            models={models}
            activeModel={activeModel}
            thinkingLevel={thinkingLevel}
            contextUsage={contextUsage}
            slashCommands={slashCommands}
            onSelectModel={selectModel}
            onSelectThinking={setThinkingLevel}
            onOpenSettings={() => setIsSettingsOpen(true)}
            onRefreshEngine={refreshEngine}
          />
        </div>
      </div>

      {/* Provider Auth Settings Modal */}
      <ProviderAuthModal
        isOpen={isSettingsOpen}
        onClose={() => {
          setIsSettingsOpen(false);
          refreshEngine();
        }}
        providers={providers}
        loginProviders={loginProviders}
        oauthPrompt={oauthPrompt}
        onStartOAuth={startOAuthLogin}
        onSubmitOAuthInput={submitOAuthInput}
        onSaveKeys={saveProviders}
      />
    </div>
  );
};
