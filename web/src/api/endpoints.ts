import { adminRequest, publicRequest } from './client';
import type {
  CreateCredentialRequest,
  CreateGatewayKeyResponse,
  CredentialMetadata,
  GatewayKeyMetadata,
  Project,
  ProjectSettingsUpdate,
  Report,
  Window,
} from './types';

function metricsPath(window: Window, projectId?: string): string {
  const params = new URLSearchParams({ window });
  if (projectId) params.set('project_id', projectId);
  return `?${params.toString()}`;
}

export const api = {
  // --- Metrics -------------------------------------------------------
  adminMetrics: (token: string, window: Window, projectId?: string) =>
    adminRequest<Report>(token, `/admin/metrics${metricsPath(window, projectId)}`),

  publicMetrics: (window: Window, projectId?: string) =>
    publicRequest<Report>(`/public/metrics${metricsPath(window, projectId)}`),

  // --- Providers -------------------------------------------------------
  listProviderNames: (token: string) =>
    adminRequest<{ providers: string[] }>(token, '/admin/providers'),

  // --- Projects -------------------------------------------------------
  listProjects: (token: string) => adminRequest<Project[]>(token, '/admin/projects'),

  getProject: (token: string, id: string) => adminRequest<Project>(token, `/admin/projects/${id}`),

  createProject: (token: string, name: string) =>
    adminRequest<{ id: string }>(token, '/admin/projects', { method: 'POST', body: { name } }),

  updateProjectSettings: (token: string, id: string, update: ProjectSettingsUpdate) =>
    adminRequest<void>(token, `/admin/projects/${id}`, { method: 'PATCH', body: update }),

  flushProjectCache: (token: string, id: string) =>
    adminRequest<void>(token, `/admin/projects/${id}/cache`, { method: 'DELETE' }),

  // --- Credentials -------------------------------------------------------
  listCredentials: (token: string, projectId: string) =>
    adminRequest<CredentialMetadata[]>(token, `/admin/projects/${projectId}/credentials`),

  createCredential: (token: string, body: CreateCredentialRequest) =>
    adminRequest<void>(token, '/admin/credentials', { method: 'POST', body }),

  revokeCredential: (token: string, projectId: string, credentialId: string) =>
    adminRequest<void>(token, `/admin/projects/${projectId}/credentials/${credentialId}`, { method: 'DELETE' }),

  // --- Gateway keys -------------------------------------------------------
  listGatewayKeys: (token: string, projectId: string) =>
    adminRequest<GatewayKeyMetadata[]>(token, `/admin/projects/${projectId}/gateway-keys`),

  createGatewayKey: (token: string, projectId: string, label: string) =>
    adminRequest<CreateGatewayKeyResponse>(token, '/admin/gateway-keys', {
      method: 'POST',
      body: { project_id: projectId, label },
    }),

  revokeGatewayKey: (token: string, projectId: string, keyId: string) =>
    adminRequest<void>(token, `/admin/projects/${projectId}/gateway-keys/${keyId}`, { method: 'DELETE' }),
};
