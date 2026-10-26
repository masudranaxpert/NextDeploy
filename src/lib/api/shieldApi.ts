import { requestJson } from './http';
import { CreateShieldNodeResponse, ShieldNodesResponse, TestShieldNodeResponse } from '../../types/shield';

const json = (body: unknown): RequestInit => ({
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify(body),
});

const putJson = (body: unknown): RequestInit => ({
  method: 'PUT',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify(body),
});

export const shieldApi = {
  listShieldNodes: () =>
    requestJson<ShieldNodesResponse>('/api/v1/shield-nodes'),

  createShieldNode: (name: string, ip: string) =>
    requestJson<CreateShieldNodeResponse>('/api/v1/shield-nodes', json({ name, ip })),

  updateShieldNode: (id: number, name: string, ip: string) =>
    requestJson<{ ok: boolean; error?: string }>(`/api/v1/shield-nodes/${id}`, putJson({ name, ip })),

  deleteShieldNode: (id: number) =>
    requestJson<{ ok: boolean; error?: string }>(`/api/v1/shield-nodes/${id}`, { method: 'DELETE' }),

  testShieldNode: (id: number) =>
    requestJson<TestShieldNodeResponse>(`/api/v1/shield-nodes/${id}/test`, { method: 'POST' }),

  reassignShieldNode: (id: number, targetShieldId: number | null) =>
    requestJson<{ ok: boolean; error?: string }>(`/api/v1/shield-nodes/${id}/reassign`, json({ target_shield_id: targetShieldId })),
};
