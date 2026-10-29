/**
 * Types mirroring internal/insights/insights.go and internal/httpserver's
 * admin request/response structs exactly. Every nullable Go field
 * (sql.NullFloat64, *int, *float64) is typed `| null` here on purpose —
 * see docs/FRONTEND_ARCHITECTURE.md §6: null and zero must never be
 * conflated in the UI.
 */

export type Window = '1h' | '6h' | '24h' | '7d';

export const WINDOWS: Window[] = ['1h', '6h', '24h', '7d'];

export interface Reliability {
  requests: number;
  succeeded: number;
  failed: number;
  success_rate: number | null;
  hit_upstream_failure: number;
  rescued_by_failover: number;
  rescue_rate: number | null;
  rescued_rate_limits: number;
  rescued_outages: number;
  rescued_bad_model: number;
  upstream_attempts_total: number;
  upstream_attempts_failed: number;
  circuit_reroutes: number;
}

export interface Latency {
  p50_ms: number | null;
  p95_ms: number | null;
  p99_ms: number | null;
  ttft_p50_ms: number | null;
  ttft_p95_ms: number | null;
}

export interface CacheStats {
  hits: number;
  hit_rate: number | null;
  tokens_saved: number;
  hit_p50_ms: number | null;
  miss_p50_ms: number | null;
  speedup: number | null;
}

export interface Usage {
  tokens_in: number;
  tokens_out: number;
  total_tokens: number;
  stream_requests: number;
}

export interface Protection {
  rate_limited: number;
  budget_blocked: number;
  concurrency_blocked: number;
  total_blocked: number;
}

/** Error categories as classified by internal/providers/errors.go. */
export type FailureCategory = 'key_fault' | 'provider_transient' | 'bad_model';

export interface ProviderRow {
  provider: string;
  requests: number;
  share: number;
  success_rate: number | null;
  p50_ms: number | null;
  p95_ms: number | null;
  ttft_p50_ms: number | null;
  tokens: number;
  attempts_ok: number;
  attempts_failed: number;
  failures_by_category: Partial<Record<FailureCategory, number>>;
}

export interface ModelRow {
  provider: string;
  model: string;
  requests: number;
}

export interface TimeseriesPoint {
  t: string;
  requests: number;
  success: number;
  cache_hits: number;
  rescued: number;
  tokens: number;
  p50_ms: number | null;
  p95_ms: number | null;
}

export type RequestStatus = 'success' | 'error' | 'partial' | 'canceled' | string;

export interface RecentRequest {
  t: string;
  provider: string;
  model: string;
  status: RequestStatus;
  total_ms: number | null;
  ttft_ms: number | null;
  tokens: number;
  cache_hit: boolean;
  attempts: number;
  stream: boolean;
}

export interface Report {
  window: Window;
  generated_at: string;
  reliability: Reliability;
  latency: Latency;
  cache: CacheStats;
  usage: Usage;
  protection: Protection;
  providers: ProviderRow[];
  top_models: ModelRow[];
  timeseries: TimeseriesPoint[];
  recent: RecentRequest[];
}

export interface Project {
  id: string;
  name: string;
  status: string;
  rate_limit_per_min: number | null;
  max_concurrent_streams: number | null;
  token_budget: number | null;
  created_at: string;
}

/** PATCH body: a field omitted leaves that setting alone; set to `null`
 * clears the override back to the server default; set to a number applies
 * it. Matches internal/httpserver/admin.go's `optionalInt`. */
export interface ProjectSettingsUpdate {
  rate_limit_per_min?: number | null;
  max_concurrent_streams?: number | null;
  token_budget?: number | null;
}

export interface CredentialMetadata {
  id: string;
  project_id: string;
  provider: string;
  label: string;
  status: string;
  created_at: string;
  last_used_at: string | null;
}

export interface CreateCredentialRequest {
  project_id: string;
  provider: string;
  label: string;
  api_key: string;
}

export interface GatewayKeyMetadata {
  id: string;
  project_id: string;
  key_prefix: string;
  label: string;
  status: string;
  created_at: string;
  last_used_at: string | null;
}

export interface CreateGatewayKeyResponse {
  id: string;
  key: string;
  project_id: string;
}

export interface ApiErrorBody {
  error: {
    message: string;
    type: string;
    code?: string;
  };
}
