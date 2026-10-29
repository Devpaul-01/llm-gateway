import type { ApiErrorBody } from './types';

/** Thrown for any non-2xx admin API response. Carries the parsed body's
 * message/type when the backend returned its standard JSON error shape
 * (internal/httpserver/apierror.go, errors.go), falling back to the raw
 * status text if the body wasn't parseable JSON (shouldn't happen against
 * this backend, but a client that assumes it would corrupt the error UI). */
export class ApiError extends Error {
  readonly status: number;
  readonly type: string;
  readonly code?: string;

  constructor(status: number, message: string, type: string, code?: string) {
    super(message);
    this.name = 'ApiError';
    this.status = status;
    this.type = type;
    this.code = code;
  }

  /** True for a 401 — the admin token is missing/wrong, caller should
   * clear it and return to the token gate. */
  get isAuthError(): boolean {
    return this.status === 401;
  }
}

export interface RequestOptions {
  method?: 'GET' | 'POST' | 'PATCH' | 'DELETE';
  body?: unknown;
  signal?: AbortSignal;
}

/** All admin calls go through here. Base path is empty — Vite's dev proxy
 * (vite.config.ts) and the production reverse proxy both keep /admin and
 * /public same-origin, so there is no separate API base URL to configure. */
async function request<T>(path: string, token: string | null, opts: RequestOptions = {}): Promise<T> {
  const headers: Record<string, string> = {};
  if (token) headers['Authorization'] = `Bearer ${token}`;
  if (opts.body !== undefined) headers['Content-Type'] = 'application/json';

  const res = await fetch(path, {
    method: opts.method ?? 'GET',
    headers,
    body: opts.body !== undefined ? JSON.stringify(opts.body) : undefined,
    signal: opts.signal,
  });

  if (!res.ok) {
    let message = res.statusText || `Request failed (${res.status})`;
    let type = 'api_error';
    let code: string | undefined;
    try {
      const parsed = (await res.json()) as ApiErrorBody;
      if (parsed?.error?.message) {
        message = parsed.error.message;
        type = parsed.error.type ?? type;
        code = parsed.error.code;
      }
    } catch {
      // Body wasn't JSON (or was empty) — keep the statusText fallback above.
    }
    throw new ApiError(res.status, message, type, code);
  }

  // 204/No Content responses (e.g. cache flush, key revoke) have no body.
  if (res.status === 204 || res.headers.get('content-length') === '0') {
    return undefined as T;
  }
  return (await res.json()) as T;
}

export function adminRequest<T>(token: string | null, path: string, opts?: RequestOptions): Promise<T> {
  return request<T>(path, token, opts);
}

export function publicRequest<T>(path: string, opts?: RequestOptions): Promise<T> {
  return request<T>(path, null, opts);
}
