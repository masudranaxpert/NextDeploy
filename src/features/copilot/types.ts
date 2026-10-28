export type AutonomyMode = 'safe' | 'autonomous';

export type ThinkingLevel = 'off' | 'minimal' | 'low' | 'medium' | 'high' | 'xhigh' | 'max';

export interface AIModel {
  id: string;
  name: string;
  provider: string;
  reasoning?: boolean;
}

export interface LoginProvider {
  id: string;
  name: string;
  authenticated: boolean;
  available?: boolean;
}

export interface ProviderStatus {
  configured: boolean;
  maskedKey?: string;
  host?: string;
}

export interface ProvidersConfig {
  loginProviders?: LoginProvider[];
  openai: ProviderStatus;
  anthropic: ProviderStatus;
  deepseek: ProviderStatus;
  gemini: ProviderStatus;
  ollama: ProviderStatus;
}

export interface ApprovalRequest {
  id: string;
  title: string;
  message: string;
}

export interface OAuthPrompt {
  id: string;
  url?: string;
  instructions?: string;
  title?: string;
  placeholder?: string;
}

export interface AgentTodo {
  id: string;
  text: string;
  status: 'pending' | 'in_progress' | 'completed' | 'failed';
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
  todos?: AgentTodo[];
  approval?: ApprovalRequest;
  timestamp: number;
  isStreaming?: boolean;
}

export interface ChatSession {
  id: string;
  title: string;
  createdAt: number;
  updatedAt: number;
  messageCount: number;
}

export interface SlashCommand {
  name: string;
  description?: string;
  source?: string;
  input?: { hint?: string };
}

export interface ContextUsage {
  tokens: number;
  contextWindow: number;
  percent: number;
}

export interface CopilotStatus {
  running: boolean;
  active: boolean;
  autonomyMode: AutonomyMode;
  binaryPath?: string;
}
