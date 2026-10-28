export type AutonomyMode = 'always-ask' | 'always-ask-cached' | 'write' | 'yolo';

export type ThinkingLevel = 'inherit' | 'off' | 'auto' | 'minimal' | 'low' | 'medium' | 'high' | 'xhigh' | 'max' | string;

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
  secret?: boolean;
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
  intent?: string;
  result?: unknown; // omp tool result: {content, details} (or text)
  status: 'running' | 'completed' | 'failed';
  startedAt: number;
  endedAt?: number;
}

export interface CopilotMessage {
  id: string;
  role: 'user' | 'assistant' | 'system';
  kind?: 'compaction' | 'branch' | 'note';
  content: string;
  attached?: string[];
  images?: Array<{ mimeType: string; data: string }>;
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
  group?: string;
  createdAt: number;
  updatedAt: number;
  messageCount: number;
}

export interface SlashSubcommand {
  name: string;
  description?: string;
  usage?: string;
}

export interface SlashCommand {
  name: string;
  description?: string;
  source?: string;
  aliases?: string[];
  input?: { hint?: string };
  subcommands?: SlashSubcommand[];
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
  needsLogin?: boolean;
}

export interface WorkDir {
  kind: 'root' | 'site' | 'temp';
  appId?: string;
  path: string;
  sites?: string[];
}

export interface EngineSettings {
  fastMode: boolean;
  autoCompaction: boolean;
  autoRetry: boolean;
}

export interface UiPrompt {
  id: string;
  kind: 'select' | 'editor';
  title: string;
  options?: string[];
  prefill?: string;
}

export interface Subagent {
  id: string;
  index: number;
  agent: string;
  description?: string;
  status: string; // started | running | completed | failed | aborted
  startedAt: number;
  detached?: boolean;
}

export interface AskOption {
  label: string;
  description?: string;
}

export interface AskQuestion {
  id: string;
  question: string;
  header?: string;
  multi: boolean;
  options: AskOption[];
  recommended?: number;
}

export interface AskPrompt {
  id: string;
  questions: AskQuestion[];
}

export interface SessionStats {
  userMessages: number;
  assistantMessages: number;
  toolCalls: number;
  tokens: { input: number; output: number; reasoning?: number; cacheRead?: number; cacheWrite?: number; total: number };
  cost: number;
  contextUsage?: ContextUsage;
}
