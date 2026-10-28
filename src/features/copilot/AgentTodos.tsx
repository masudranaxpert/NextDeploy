import React from 'react';
import { CheckCircle2, Circle, Loader2, AlertCircle } from 'lucide-react';
import type { AgentTodo } from './types';

interface AgentTodosProps {
  todos: AgentTodo[];
}

export const AgentTodos: React.FC<AgentTodosProps> = ({ todos }) => {
  if (!todos || todos.length === 0) return null;

  const completedCount = todos.filter((t) => t.status === 'completed').length;
  const progressPercent = Math.round((completedCount / todos.length) * 100);

  return (
    <div
      data-slot="card"
      className="my-3 overflow-hidden rounded-xl border border-white/[0.08] bg-[#0c0d0e]/80 backdrop-blur-sm shadow-xs transition-all"
    >
      <div className="flex items-center justify-between border-b border-white/[0.06] px-3.5 py-2 text-xs">
        <div className="flex items-center gap-2 font-medium text-zinc-300">
          <span className="flex size-1.5 rounded-full bg-emerald-400" />
          <span>Execution Plan</span>
        </div>
        <span className="text-[11px] font-mono text-zinc-500">
          {completedCount}/{todos.length} ({progressPercent}%)
        </span>
      </div>

      <div className="divide-y divide-white/[0.04] p-1.5">
        {todos.map((todo) => {
          let icon = <Circle className="size-3.5 text-zinc-600 shrink-0" />;
          let textColor = 'text-zinc-400';

          if (todo.status === 'completed') {
            icon = <CheckCircle2 className="size-3.5 text-emerald-400 shrink-0" />;
            textColor = 'text-zinc-400 line-through decoration-zinc-600';
          } else if (todo.status === 'in_progress') {
            icon = <Loader2 className="size-3.5 text-blue-400 animate-spin shrink-0" />;
            textColor = 'text-zinc-200 font-medium';
          } else if (todo.status === 'failed') {
            icon = <AlertCircle className="size-3.5 text-red-400 shrink-0" />;
            textColor = 'text-red-400';
          }

          return (
            <div
              key={todo.id}
              className="flex items-center gap-2.5 px-2.5 py-1.5 text-xs transition-colors hover:bg-white/[0.02]"
            >
              {icon}
              <span className={`min-w-0 flex-1 truncate ${textColor}`}>{todo.text}</span>
            </div>
          );
        })}
      </div>
    </div>
  );
};
