import { useState } from 'react';
import { useMetrics } from '../../api/queries';
import type { Window } from '../../api/types';
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

export function DashboardPage() {
  const [window, setWindow] = useState<Window>('24h');
  const { data: report, isLoading, isError, error, refetch } = useMetrics(window);

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
