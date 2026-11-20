import { requestJson } from './http';

/** State of the optional AI assistant (the omp engine downloaded into /data/plugins). */
export interface AiPluginStatus {
  enabled: boolean;
  installed: boolean;
  installing: boolean;
  wanted: boolean;
  version?: string;
  target: string;
  source?: 'plugin' | 'system';
  size_mb?: number;
  disk_mb: number;
  antigravity_mb: number;
  arch: string;
  error?: string;
  progress?: number;
}

type PluginResponse = { ok: boolean; plugin: AiPluginStatus; error?: string };

export const aiApi = {
  aiPluginStatus: () => requestJson<PluginResponse>('/api/v1/ai/plugin'),
  aiPluginEnable: (version?: string) =>
    requestJson<PluginResponse>('/api/v1/ai/plugin/enable', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ version: version || '' }),
    }),
  aiPluginDisable: (purge = false) =>
    requestJson<PluginResponse>('/api/v1/ai/plugin/disable', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ purge }),
    }),
  aiCleanAntigravity: () =>
    requestJson<PluginResponse & { freed_mb: number }>('/api/v1/ai/plugin/clean-antigravity', { method: 'POST' }),
};
