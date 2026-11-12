import { useState } from 'react';
import type { Report, Window } from '../../api/types';
import { Card } from '../../components/ui/Card';
import { StatTile } from '../../components/ui/StatTile';
import { Skeleton } from '../../components/ui/Skeleton';
import { EmptyState } from '../../components/ui/EmptyState';
import { ErrorBanner } from '../../components/ui/ErrorBanner';
import { PageHeader } from '../../components/layout/PageHeader';
import { WindowPicker } from '../../components/layout/WindowPicker';
import { GeneratedAt } from '../../components/layout/GeneratedAt';
import { VolumeChart } from '../../components/charts/VolumeChart';
import { LatencyChart } from '../../components/charts/LatencyChart';
import { ProviderShareChart } from '../../components/charts/ProviderShareChart';
import { formatCount, formatMs, formatMultiplier, formatPct } from '../../lib/format';
import type { StatTone } from '../../components/ui/StatTile';
import './DashboardPage.css';

/** A success/rescue rate below this reads as "degraded" rather than
 * neutral/good — purely a presentation threshold for color, not a value
 * the backend computes or asserts in any way. */
function rateTone(rate: number | null, good = 0.98, warn = 0.9): StatTone {
  if (rate == null) return 'neutral';
  if (rate >= good) return 'good';
  if (rate >= warn) return 'warn';
  return 'bad';
}

// --- SCREENSHOT MOCK DATA ---------------------------------------------
// Hardcoded fixture so this page renders with realistic data without a
// seeded database or a running gateway. Remove this block (and the
// useMetrics call it replaces below) before merging back to main.
function buildMockTimeseries() {
  const points = [];
  const now = new Date('2026-11-12T18:00:00Z').getTime();
  for (let i = 23; i >= 0; i--) {
    const t = new Date(now - i * 60 * 60 * 1000).toISOString();
    const requests = 180 + Math.round(Math.sin(i / 3) * 60 + Math.random() * 40);
    const success = Math.round(requests * (0.96 + Math.random() * 0.03));
    points.push({
      t,
      requests,
      success,
      cache_hits: Math.round(requests * 0.18),
      rescued: Math.round(requests * 0.02),
      tokens: requests * 340,
      p50_ms: 180 + Math.round(Math.random() * 40),
      p95_ms: 410 + Math.round(Math.random() * 90),
    });
  }
  return points;
}

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
  latency: {
    p50_ms: 212,
    p95_ms: 480,
    p99_ms: 710,
    ttft_p50_ms: 190,
    ttft_p95_ms: 455,
  },
  cache: {
    hits: 812,
    hit_rate: 0.1685,
    tokens_saved: 284_300,
    hit_p50_ms: 4,
    miss_p50_ms: 398,
    speedup: 99.5,
  },
  usage: {
    tokens_in: 712_400,
    tokens_out: 938_210,
    total_tokens: 1_650_610,
    stream_requests: 3_102,
  },
  protection: {
    rate_limited: 22,
    budget_blocked: 4,
    concurrency_blocked: 9,
    total_blocked: 35,
  },
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
  ],
  top_models: [
    { provider: 'openai', model: 'gpt-4o', requests: 2010 },
    { provider: 'anthropic', model: 'claude-sonnet-4', requests: 1200 },
    { provider: 'openai', model: 'gpt-4o-mini', requests: 800 },
  ],
  timeseries: buildMockTimeseries(),
  recent: [],
};
// --- END SCREENSHOT MOCK DATA ------------------------------------------

export function DashboardPage() {
  const [window, setWindow] = useState<Window>('24h');
  const report = MOCK_REPORT;
  const isLoading = false;
  const isError = false;
  const error = null;
  const refetch = () => {};

  return (
    <div>
      <PageHeader
        title="Dashboard"
        description="Overview of gateway reliability, latency, and cost across every project."
        actions={
          <>
            {report && <GeneratedAt iso={report.generated_at} />}
            <WindowPicker value={window} onChange={setWindow} />
          </>
        }
      />

      {isError && <ErrorBanner error={error} onRetry={refetch} />}

      {isLoading && <DashboardSkeleton />}

      {report && !isLoading && (
        <>
          {report.reliability.requests === 0 ? (
            <EmptyState
              title="No requests in this window"
              description="Once a gateway key is used to call /v1/chat/completions, traffic will show up here. Try a longer time window, or send a test request."
            />
          ) : (
            <>
              <div className="dashboard-hero-grid">
                <Card title="Requests">
                  <StatTile
                    label={`in the last ${window}`}
                    value={formatCount(report.reliability.requests)}
                    size="lg"
                  />
                </Card>
                <Card title="Success rate">
                  <StatTile
                    label={`${formatCount(report.reliability.succeeded)} succeeded`}
                    value={formatPct(report.reliability.success_rate)}
                    tone={rateTone(report.reliability.success_rate)}
                    size="lg"
                  />
                </Card>
                <Card title="Rescued by failover">
                  <StatTile
                    label={`of ${formatCount(report.reliability.hit_upstream_failure)} that hit a failure`}
                    value={formatCount(report.reliability.rescued_by_failover)}
                    tone={report.reliability.rescued_by_failover > 0 ? 'good' : 'neutral'}
                    size="lg"
                    footnote={`rescue rate ${formatPct(report.reliability.rescue_rate)}`}
                  />
                </Card>
                <Card title="Blocked before provider">
                  <StatTile
                    label="rate limit + budget + concurrency"
                    value={formatCount(report.protection.total_blocked)}
                    tone={report.protection.total_blocked > 0 ? 'warn' : 'neutral'}
                    size="lg"
                  />
                </Card>
              </div>

              <div className="dashboard-charts-grid">
                <Card title="Request volume" className="dashboard-span-2">
                  <VolumeChart data={report.timeseries} />
                </Card>
                <Card title="Latency (p50 / p95)" className="dashboard-span-2">
                  <LatencyChart data={report.timeseries} />
                </Card>
              </div>

              <div className="dashboard-secondary-grid">
                <Card title="Latency percentiles">
                  <div className="stat-row">
                    <StatTile label="p50" value={formatMs(report.latency.p50_ms)} />
                    <StatTile label="p95" value={formatMs(report.latency.p95_ms)} />
                    <StatTile label="p99" value={formatMs(report.latency.p99_ms)} />
                  </div>
                  <div className="stat-row stat-row--secondary">
                    <StatTile label="TTFT p50" value={formatMs(report.latency.ttft_p50_ms)} />
                    <StatTile label="TTFT p95" value={formatMs(report.latency.ttft_p95_ms)} />
                  </div>
                </Card>

                <Card title="Response cache">
                  <div className="stat-row">
                    <StatTile label="hit rate" value={formatPct(report.cache.hit_rate)} tone="good" />
                    <StatTile label="speedup" value={formatMultiplier(report.cache.speedup)} />
                  </div>
                  <div className="stat-row stat-row--secondary">
                    <StatTile label="hits" value={formatCount(report.cache.hits)} />
                    <StatTile label="tokens saved" value={formatCount(report.cache.tokens_saved)} />
                  </div>
                </Card>

                <Card title="Token usage">
                  <div className="stat-row">
                    <StatTile label="total" value={formatCount(report.usage.total_tokens)} />
                    <StatTile label="streamed" value={formatCount(report.usage.stream_requests)} />
                  </div>
                  <div className="stat-row stat-row--secondary">
                    <StatTile label="in" value={formatCount(report.usage.tokens_in)} />
                    <StatTile label="out" value={formatCount(report.usage.tokens_out)} />
                  </div>
                </Card>
              </div>

              <Card title="Traffic by provider">
                <ProviderShareChart providers={report.providers} />
              </Card>
            </>
          )}
        </>
      )}
    </div>
  );
}

function DashboardSkeleton() {
  return (
    <div className="dashboard-hero-grid">
      {Array.from({ length: 4 }).map((_, i) => (
        <Card key={i}>
          <Skeleton height="1.5rem" width="60%" />
          <div style={{ marginTop: 12 }}>
            <Skeleton height="2.5rem" width="40%" />
          </div>
        </Card>
      ))}
    </div>
  );
}
