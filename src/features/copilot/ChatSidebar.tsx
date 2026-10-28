import React from 'react';
import { Plus, MessageSquare, Trash2, Key, Bot } from 'lucide-react';
import type { ChatSession } from './types';

interface ChatSidebarProps {
  sessions: ChatSession[];
  activeSessionId: string;
  onSelectSession: (id: string) => void;
  onNewChat: () => void;
  onDeleteSession: (id: string) => void;
  onOpenSettings: () => void;
  className?: string;
}

export const ChatSidebar: React.FC<ChatSidebarProps> = ({
  sessions,
  activeSessionId,
  onSelectSession,
  onNewChat,
  onDeleteSession,
  onOpenSettings,
  className = '',
}) => {
  return (
    <aside
      data-slot="sidebar"
      className={`flex h-full flex-col border-r border-white/[0.08] bg-[#090a0b] text-zinc-300 w-64 shrink-0 select-none ${className}`}
    >
      {/* Header */}
      <div data-slot="sidebar-header" className="p-3 border-b border-white/[0.06]">
        <div className="flex items-center justify-between mb-3 px-1">
          <div className="flex items-center gap-2">
            <div className="flex size-7 items-center justify-center rounded-lg bg-orange-500/10 text-orange-400 border border-orange-500/20">
              <Bot className="size-4" />
            </div>
            <span className="font-semibold text-sm tracking-tight text-white">NextDeploy AI</span>
          </div>
        </div>

        <button
          type="button"
          onClick={onNewChat}
          className="flex w-full items-center justify-between rounded-xl border border-white/[0.08] bg-white/[0.03] px-3 py-2 text-xs font-medium text-zinc-200 hover:border-white/[0.14] hover:bg-white/[0.06] hover:text-white transition-all shadow-xs"
        >
          <div className="flex items-center gap-2">
            <Plus className="size-3.5 text-orange-400" />
            <span>New Chat</span>
          </div>
          <kbd className="rounded border border-white/[0.1] bg-white/[0.04] px-1.5 py-0.5 text-[10px] text-zinc-400 font-mono">
            ⌘N
          </kbd>
        </button>
      </div>

      {/* Sessions list */}
      <div data-slot="sidebar-content" className="flex-1 overflow-y-auto p-2 space-y-1">
        <div className="px-2 py-1 text-[10px] font-semibold uppercase tracking-wider text-zinc-500">
          Recent Conversations
        </div>

        {sessions.length === 0 ? (
          <div className="px-3 py-6 text-center text-xs text-zinc-600">
            No conversations yet.
          </div>
        ) : (
          sessions.map((sess) => {
            const isActive = sess.id === activeSessionId;
            return (
              <div
                key={sess.id}
                data-slot="sidebar-menu-item"
                className={`group relative flex items-center justify-between rounded-xl px-2.5 py-2 text-xs transition-colors cursor-pointer ${
                  isActive
                    ? 'bg-white/[0.08] text-white font-medium'
                    : 'text-zinc-400 hover:bg-white/[0.04] hover:text-zinc-200'
                }`}
                onClick={() => onSelectSession(sess.id)}
              >
                <div className="flex items-center gap-2 min-w-0 flex-1 pr-1">
                  <MessageSquare
                    className={`size-3.5 shrink-0 ${
                      isActive ? 'text-orange-400' : 'text-zinc-500 group-hover:text-zinc-400'
                    }`}
                  />
                  <span className="truncate">{sess.title || 'Untitled Session'}</span>
                </div>

                <button
                  type="button"
                  onClick={(e) => {
                    e.stopPropagation();
                    onDeleteSession(sess.id);
                  }}
                  className="opacity-0 group-hover:opacity-100 p-1 text-zinc-500 hover:text-red-400 transition-all rounded"
                  title="Delete chat"
                >
                  <Trash2 className="size-3" />
                </button>
              </div>
            );
          })
        )}
      </div>

      {/* Footer: Provider keys settings */}
      <div data-slot="sidebar-footer" className="p-2 border-t border-white/[0.06]">
        <button
          type="button"
          onClick={onOpenSettings}
          className="flex w-full items-center gap-2.5 rounded-xl px-3 py-2 text-xs font-medium text-zinc-400 hover:bg-white/[0.05] hover:text-zinc-200 transition-colors"
        >
          <Key className="size-3.5 text-zinc-400" />
          <span>API Keys & Providers</span>
        </button>
      </div>
    </aside>
  );
};
