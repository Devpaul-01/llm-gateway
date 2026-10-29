import type { Window } from './types';

/** Centralized query keys so mutations can invalidate precisely. */
export const queryKeys = {
  metrics: (window: Window, projectId?: string) => ['metrics', window, projectId ?? 'all'] as const,
  providers: () => ['providers'] as const,
  projects: () => ['projects'] as const,
  project: (id: string) => ['projects', id] as const,
  credentials: (projectId: string) => ['credentials', projectId] as const,
  gatewayKeys: (projectId: string) => ['gateway-keys', projectId] as const,
};
