import { useState } from 'react';
import { useMetrics } from '../../api/queries';
import type { Window } from '../../api/types';
import { Badge, statusTone } from '../../components/ui/Badge';
import { Card } from '../../components/ui/Card';
import { EmptyState } from '../../components/ui/EmptyState';
import { ErrorBanner } from '../../components/ui/ErrorBanner';
import { Skeleton } from '../../components/ui/Skeleton';
import { PageHeader } from '../../components/layout/PageHeader';
import { WindowPicker } from '../../components/layout/WindowPicker';
import { GeneratedAt } from '../../components/layout/GeneratedAt';
import { DASH, formatCount, formatMs, formatTimestamp } from '../../lib/format';
import '../../components/ui/Table.css';

export function RequestsPage() {
  const [window, setWindow] = useState<Window>('24h');
  const { data: report, isLoading, isError, error, refetch } = useMetrics(window);

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
