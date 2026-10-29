import { useQuery } from '@tanstack/react-query';
import { useToken } from '../auth/TokenContext';
import { api } from './endpoints';
import { queryKeys } from './queryKeys';
import type { Window } from './types';

/** The backend's /public/metrics response is cached for 10s server-side
 * (internal/httpserver/metrics.go publicMetricsTTL); polling faster than
 * that just re-requests the same cached document. */
const METRICS_POLL_MS = 15_000;

export function useMetrics(window: Window, projectId?: string) {
  const { token } = useToken();
  return useQuery({
    queryKey: queryKeys.metrics(window, projectId),
    queryFn: () => api.adminMetrics(token!, window, projectId),
    enabled: !!token,
    refetchInterval: METRICS_POLL_MS,
    refetchIntervalInBackground: false,
  });
}

export function useProviderNames() {
  const { token } = useToken();
  return useQuery({
    queryKey: queryKeys.providers(),
    queryFn: () => api.listProviderNames(token!),
    enabled: !!token,
    staleTime: Infinity, // the known-provider list is static code, not data
  });
}

export function useProjects() {
  const { token } = useToken();
  return useQuery({
    queryKey: queryKeys.projects(),
    queryFn: () => api.listProjects(token!),
    enabled: !!token,
  });
}

export function useProject(id: string | undefined) {
  const { token } = useToken();
  return useQuery({
    queryKey: queryKeys.project(id ?? ''),
    queryFn: () => api.getProject(token!, id!),
    enabled: !!token && !!id,
  });
}

export function useCredentials(projectId: string | undefined) {
  const { token } = useToken();
  return useQuery({
    queryKey: queryKeys.credentials(projectId ?? ''),
    queryFn: () => api.listCredentials(token!, projectId!),
    enabled: !!token && !!projectId,
  });
}

export function useGatewayKeys(projectId: string | undefined) {
  const { token } = useToken();
  return useQuery({
    queryKey: queryKeys.gatewayKeys(projectId ?? ''),
    queryFn: () => api.listGatewayKeys(token!, projectId!),
    enabled: !!token && !!projectId,
  });
}
