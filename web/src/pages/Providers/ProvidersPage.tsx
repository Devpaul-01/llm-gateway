import { useState } from 'react';
import type { ProviderRow, Report, Window } from '../../api/types';
import { Card } from '../../components/ui/Card';
import { EmptyState } from '../../components/ui/EmptyState';
import { ErrorBanner } from '../../components/ui/ErrorBanner';
import { Skeleton } from '../../components/ui/Skeleton';
import { PageHeader } from '../../components/layout/PageHeader';
import { WindowPicker } from '../../components/layout/WindowPicker';
import { GeneratedAt } from '../../components/layout/GeneratedAt';
import { FailureBreakdown } from '../../components/charts/FailureBreakdown';
import { formatCount, formatMs, formatPct } from '../../lib/format';
import '../../components/ui/Table.css';

function sortProviders(providers: ProviderRow[]): ProviderRow[] {
  return [...providers].sort((a, b) => b.requests - a.requests);
}

// --- SCREENSHOT MOCK DATA ---------------------------------------------
// Hardcoded fixture so this page renders with realistic data without a
// seeded database or a running gateway. Remove this block (and the
// useMetrics call it replaces below) before merging back to main.
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
  providers: [
    {
      provider: 'openai',
      requests: 2810,
      share: 0.583,
      success_rate: 0.992,
      p50_ms: 205,
      p95_ms: 460,
      ttft_p50_ms: 180,
      tokens: 980_200,
      attempts_ok: 2788,
      attempts_failed: 22,
      failures_by_category: { provider_transient: 14, bad_model: 8 },
    },
    {
      provider: 'anthropic',
      requests: 1540,
      share: 0.3195,
      success_rate: 0.987,
      p50_ms: 230,
      p95_ms: 505,
      ttft_p50_ms: 205,
      tokens: 540_100,
      attempts_ok: 1520,
      attempts_failed: 20,
      failures_by_category: { provider_transient: 20 },
    },
    {
      provider: 'mock-secondary',
      requests: 470,
      share: 0.0975,
      success_rate: 1,
      p50_ms: 190,
      p95_ms: 398,
      ttft_p50_ms: 175,
      tokens: 130_310,
      attempts_ok: 470,
      attempts_failed: 0,
      failures_by_category: {},
    },
    {
      provider: 'mock-primary',
      requests: 0,
      share: 0,
      success_rate: 0,
      p50_ms: null,
      p95_ms: null,
      ttft_p50_ms: null,
      tokens: 0,
      attempts_ok: 0,
      attempts_failed: 12,
      failures_by_category: { key_fault: 12 },
    },
  ],
  top_models: [
    { provider: 'openai', model: 'gpt-4o', requests: 2010 },
    { provider: 'anthropic', model: 'claude-sonnet-4', requests: 1200 },
  ],
  timeseries: [],
  recent: [],
};
// --- END SCREENSHOT MOCK DATA ------------------------------------------

export function ProvidersPage() {
  const [window, setWindow] = useState<Window>('24h');
  const report = MOCK_REPORT;
  const isLoading = false;
  const isError = false;
  const error = null;
  const refetch = () => {};

  const providers = report ? sortProviders(report.providers) : [];

  return (
    <div>
      <PageHeader
        title="Providers"
        description="Reliability, latency, and failure breakdown for every upstream the gateway has routed to."
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
          <Skeleton height="240px" />
        </Card>
      )}

      {report && !isLoading && (
        <Card>
          {providers.length === 0 ? (
            <EmptyState
              title="No provider activity in this window"
              description="A provider shows up here once the gateway has routed at least one attempt to it, including attempts that failed. Try a longer time window."
            />
          ) : (
            <table className="table">
              <thead>
                <tr>
                  <th>Provider</th>
                  <th className="num">Requests</th>
                  <th className="num">Share</th>
                  <th className="num">Success rate</th>
                  <th className="num">p50</th>
                  <th className="num">p95</th>
                  <th className="num">TTFT p50</th>
                  <th className="num">Tokens</th>
                  <th className="num">Attempts ok / failed</th>
                  <th>Failures</th>
                </tr>
              </thead>
              <tbody>
                {providers.map((p) => (
                  <tr key={p.provider}>
                    <td className="mono">{p.provider}</td>
                    <td className="num">{formatCount(p.requests)}</td>
                    <td className="num">{formatPct(p.share, 0)}</td>
                    <td className="num">{formatPct(p.success_rate)}</td>
                    <td className="num">{formatMs(p.p50_ms)}</td>
                    <td className="num">{formatMs(p.p95_ms)}</td>
                    <td className="num">{formatMs(p.ttft_p50_ms)}</td>
                    <td className="num">{formatCount(p.tokens)}</td>
                    <td className="num">
                      {formatCount(p.attempts_ok)} / {formatCount(p.attempts_failed)}
                    </td>
                    <td>
                      <FailureBreakdown failures={p.failures_by_category} />
                    </td>
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
