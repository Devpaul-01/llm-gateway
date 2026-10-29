import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useToken } from '../auth/TokenContext';
import { api } from './endpoints';
import { queryKeys } from './queryKeys';
import type { CreateCredentialRequest, ProjectSettingsUpdate } from './types';

export function useCreateProject() {
  const { token } = useToken();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (name: string) => api.createProject(token!, name),
    onSuccess: () => qc.invalidateQueries({ queryKey: queryKeys.projects() }),
  });
}

export function useUpdateProjectSettings(projectId: string) {
  const { token } = useToken();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (update: ProjectSettingsUpdate) => api.updateProjectSettings(token!, projectId, update),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: queryKeys.project(projectId) });
      qc.invalidateQueries({ queryKey: queryKeys.projects() });
    },
  });
}

export function useFlushProjectCache(projectId: string) {
  const { token } = useToken();
  return useMutation({
    mutationFn: () => api.flushProjectCache(token!, projectId),
  });
}

export function useCreateCredential(projectId: string) {
  const { token } = useToken();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: Omit<CreateCredentialRequest, 'project_id'>) =>
      api.createCredential(token!, { ...body, project_id: projectId }),
    onSuccess: () => qc.invalidateQueries({ queryKey: queryKeys.credentials(projectId) }),
  });
}

export function useRevokeCredential(projectId: string) {
  const { token } = useToken();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (credentialId: string) => api.revokeCredential(token!, projectId, credentialId),
    onSuccess: () => qc.invalidateQueries({ queryKey: queryKeys.credentials(projectId) }),
  });
}

export function useCreateGatewayKey(projectId: string) {
  const { token } = useToken();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (label: string) => api.createGatewayKey(token!, projectId, label),
    onSuccess: () => qc.invalidateQueries({ queryKey: queryKeys.gatewayKeys(projectId) }),
  });
}

export function useRevokeGatewayKey(projectId: string) {
  const { token } = useToken();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (keyId: string) => api.revokeGatewayKey(token!, projectId, keyId),
    onSuccess: () => qc.invalidateQueries({ queryKey: queryKeys.gatewayKeys(projectId) }),
  });
}
