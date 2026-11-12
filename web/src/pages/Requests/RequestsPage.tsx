import { useState } from 'react';
import type { RecentRequest, Report, Window } from '../../api/types';
import { Badge } from '../../components/ui/Badge';
import { statusTone } from '../../components/ui/badgeTone';
import { Card } from '../../components/ui/Card';
import { EmptyState } from '../../components/ui/EmptyState';
import { ErrorBanner } from '../../components/ui/ErrorBanner';
import { Skeleton } from '../../components/ui/Skeleton';
import { PageHeader } from '../../components/layout/PageHeader';
import { WindowPicker } from '../../components/layout/WindowPicker';
import { GeneratedAt } from '../../components/layout/GeneratedAt';
import { DASH, formatCount, formatMs, formatTimestamp } from '../../lib/format';
import '../../components/ui/Table.css';

// --- SCREENSHOT MOCK DATA ---------------------------------------------
// Hardcoded fixture so this page renders with realistic data without a
// seeded database or a running gateway. Remove this block (and the
// useMetrics call it replaces below) before merging back to main.
const MOCK_RECENT: RecentRequest[] = [
  { t: '2026-11-12T17:59:41Z', provider: 'openai', model: 'gpt-4o', status: 'success', total_ms: 412, ttft_ms: 188, tokens: 612, cache_hit: false, attempts: 1, stream: true },
  { t: '2026-11-12T17:59:22Z', provider: 'anthropic', model: 'claude-sonnet-4', status: 'success', total_ms: 388, ttft_ms: 201, tokens: 940, cache_hit: false, attempts: 1, stream: true },
  { t: '2026-11-12T17:59:05Z', provider: 'openai', model: 'gpt-4o-mini', status: 'success', total_ms: 3, ttft_ms: null, tokens: 410, cache_hit: true, attempts: 1, stream: false },
  { t: '2026-11-12T17:58:50Z', provider: 'openai', model: 'gpt-4o', status: 'error', total_ms: 512, ttft_ms: null, tokens: 0, cache_hit: false, attempts: 2, stream: false },
  { t: '2026-11-12T17:58:33Z', provider: 'mock-secondary', model: 'mock-chat-1', status: 'success', total_ms: 199, ttft_ms: 175, tokens: 320, cache_hit: false, attempts: 1, stream: true },
  { t: '2026-11-12T17:58:11Z', provider: 'anthropic', model: 'claude-sonnet-4', status: 'success', total_ms: 405, ttft_ms: 210, tokens: 1120, cache_hit: false, attempts: 1, stream: true },
  { t: '2026-11-12T17:57:58Z', provider: 'openai', model: 'gpt-4o', status: 'partial', total_ms: 610, ttft_ms: 300, tokens: 280, cache_hit: false, attempts: 2, stream: true },
  { t: '2026-11-12T17:57:40Z', provider: 'openai', model: 'gpt-4o-mini', status: 'success', total_ms: 4, ttft_ms: null, tokens: 410, cache_hit: true, attempts: 1, stream: false },
  { t: '2026-11-12T17:57:22Z', provider: 'anthropic', model: 'claude-sonnet-4', status: 'canceled', total_ms: 92, ttft_ms: 85, tokens: 40, cache_hit: false, attempts: 1, stream: true },
  { t: '2026-11-12T17:57:04Z', provider: 'openai', model: 'gpt-4o', status: 'success', total_ms: 440, ttft_ms: 195, tokens: 705, cache_hit: false, attempts: 1, stream: true },
];

const MOCK_REPORT: Report = {
  window: '24h',
  generated_at: '2026-11-12T18:00:00Z',
  reliability: {
    requests: 4820,
    succeeded: 4772,
    failed: 48,
    success_rate: 0.9901,
    hit_upstream_failure: 96,
    rescued_by_failover: 91,
    rescue_rate: 0.9479,
    rescued_rate_limits: 40,
    rescued_outages: 38,
    rescued_bad_model: 13,
    upstream_attempts_total: 4940,
    upstream_attempts_failed: 96,
    circuit_reroutes: 6,
  },
  latency: { p50_ms: 212, p95_ms: 480, p99_ms: 710, ttft_p50_ms: 190, ttft_p95_ms: 455 },
  cache: { hits: 812, hit_rate: 0.1685, tokens_saved: 284_300, hit_p50_ms: 4, miss_p50_ms: 398, speedup: 99.5 },
  usage: { tokens_in: 712_400, tokens_out: 938_210, total_tokens: 1_650_610, stream_requests: 3_102 },
  protection: { rate_limited: 22, budget_blocked: 4, concurrency_blocked: 9, total_blocked: 35 },
  providers: [],
  top_models: [],
  timeseries: [],
  recent: MOCK_RECENT,
};
// --- END SCREENSHOT MOCK DATA ------------------------------------------

export function RequestsPage() {
  const [window, setWindow] = useState<Window>('24h');
  const report = MOCK_REPORT;
  const isLoading = false;
  const isError = false;
  const error = null;
  const refetch = () => {};

  return (
    <div>
      <PageHeader
        title="Requests"
        description="The most recent requests handled by the gateway, across every project. Capped at 15 by the API."
        actions={
          <>
            {report && <GeneratedAt iso={report.generated_at} />}
            <WindowPicker value={window} onChange={setWindow} />
          </>
        }
      />

      {isError && <ErrorBanner error={error} onRetry={refetch} />}

      {isLoading && (
        <Card>
          <Skeleton height="300px" />
        </Card>
      )}

      {report && !isLoading && (
        <Card>
          {report.recent.length === 0 ? (
            <EmptyState
              title="No requests yet"
              description="Recent requests appear here as soon as a gateway key is used to call /v1/chat/completions."
            />
          ) : (
            <table className="table">
              <thead>
                <tr>
                  <th>Time</th>
                  <th>Provider</th>
                  <th>Model</th>
                  <th>Status</th>
                  <th className="num">Total</th>
                  <th className="num">TTFT</th>
                  <th className="num">Tokens</th>
                  <th className="num">Attempts</th>
                  <th>Cache</th>
                  <th>Stream</th>
                </tr>
              </thead>
              <tbody>
                {report.recent.map((r, i) => (
                  <tr key={`${r.t}-${i}`}>
                    <td className="mono">{formatTimestamp(r.t)}</td>
                    <td className="mono">{r.provider || DASH}</td>
                    <td className="mono">{r.model || DASH}</td>
                    <td>
                      <Badge tone={statusTone(r.status)}>{r.status}</Badge>
                    </td>
                    <td className="num">{formatMs(r.total_ms)}</td>
                    <td className="num">{formatMs(r.ttft_ms)}</td>
                    <td className="num">{formatCount(r.tokens)}</td>
                    <td className="num">{r.attempts}</td>
                    <td>{r.cache_hit ? <Badge tone="good">hit</Badge> : <span className="muted">miss</span>}</td>
                    <td>{r.stream ? 'yes' : 'no'}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </Card>
      )}
    </div>
  );
}
