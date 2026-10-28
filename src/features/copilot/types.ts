export type AutonomyMode = 'safe' | 'autonomous';

export interface CopilotStatus {
  running: boolean;
  active: boolean;
  autonomy_mode: AutonomyMode;
  pending_confirmations: number;
}

export interface ApprovalRequest {
  id: string;
  title: string;
  message: string;
}

export interface ToolExecution {
  id: string;
  name: string;
  args?: Record<string, unknown>;
  result?: string;
  status: 'running' | 'completed' | 'failed';
  startedAt: number;
  endedAt?: number;
}

export interface CopilotMessage {
  id: string;
  role: 'user' | 'assistant';
  content: string;
  thinking?: string;
  thinkingDurationSec?: number;
  toolCalls?: ToolExecution[];
  approval?: ApprovalRequest;
  timestamp: number;
  isStreaming?: boolean;
}

export interface AgentStreamEvent {
  type:
    | 'ready'
    | 'text_delta'
    | 'thinking_delta'
    | 'tool_start'
    | 'tool_end'
    | 'approval_required'
    | 'agent_end'
    | 'error';
  messageId?: string;
  delta?: string;
  toolName?: string;
  toolArgs?: Record<string, unknown>;
  toolResult?: string;
  approval?: ApprovalRequest;
  error?: string;
}
