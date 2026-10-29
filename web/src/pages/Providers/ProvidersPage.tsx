import { useState } from 'react';
import { useMetrics } from '../../api/queries';
import type { ProviderRow, Window } from '../../api/types';
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

export function ProvidersPage() {
  const [window, setWindow] = useState<Window>('24h');
  const { data: report, isLoading, isError, error, refetch } = useMetrics(window);

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
