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

let cachedShieldNodes: ShieldNode[] | null = null;
let cachedOriginIp: string = '';

export const setCachedShieldNodes = (nodes: ShieldNode[], originIp?: string) => {
  if (Array.isArray(nodes)) cachedShieldNodes = nodes;
  if (originIp) cachedOriginIp = originIp;
};

export const getCachedShieldNodes = (): { nodes: ShieldNode[] | null; originIp: string } => ({
  nodes: cachedShieldNodes,
  originIp: cachedOriginIp,
});

export const shieldApi = {
  listShieldNodes: async () => {
    const res = await requestJson<ShieldNodesResponse>('/api/v1/shield-nodes');
    if (res.ok && Array.isArray(res.nodes)) {
      setCachedShieldNodes(res.nodes, res.origin_ip);
    }
    return res;
  },

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
