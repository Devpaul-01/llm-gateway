import { usePublicMetrics } from '../../api/queries';
import type { Report } from '../../api/types';
import { Card } from '../../components/ui/Card';
import { StatTile } from '../../components/ui/StatTile';
import { Skeleton } from '../../components/ui/Skeleton';
import { ProviderShareChart } from '../../components/charts/ProviderShareChart';
import { FailureBreakdown } from '../../components/charts/FailureBreakdown';
import { formatCount, formatMs, formatMultiplier, formatPct } from '../../lib/format';
import { RECORDED_RUN } from './recordedRun';
import './DemoPage.css';

/** Public, unauthenticated page at /demo: a reviewer with no admin token
 * should be able to see, concretely, what "the gateway survives a dead
 * provider" means. It tries the real GET /public/metrics first; a
 * running docker-compose.demo.yml stack (see README "Try the failover
 * demo") will populate this live, and the page labels itself LIVE.
 * With nothing reachable -- the far more common case for a cold link --
 * it falls back to RECORDED_RUN, a real captured response from that same
 * recipe, and says so plainly rather than pretending it's live. */
export function DemoPage() {
  const { data, isLoading, isError } = usePublicMetrics('1h');

  const live = !isError && !isLoading && !!data && data.reliability.requests > 0;
  const report: Report = live ? data : RECORDED_RUN;

  return (
    <div className="demo-page">
      <header className="demo-page__header">
        <div className="demo-page__brand">
          <svg width="22" height="22" viewBox="0 0 32 32" fill="none" aria-hidden="true">
            <path
              d="M8 20 L13 11 L18 17 L24 9"
              stroke="var(--accent)"
              strokeWidth="3"
              strokeLinecap="round"
              strokeLinejoin="round"
              fill="none"
            />
            <circle cx="24" cy="9" r="2.2" fill="var(--accent)" />
          </svg>
          <span>llm-gateway</span>
        </div>
        {isLoading ? (
          <span className="demo-badge demo-badge--neutral">Checking for a live gateway…</span>
        ) : live ? (
          <span className="demo-badge demo-badge--live">● Live — real-time from a running gateway</span>
        ) : (
          <span className="demo-badge demo-badge--recorded">Recorded run — no gateway reachable right now</span>
        )}
      </header>

      <section className="demo-page__intro">
        <h1>What happens when an LLM provider goes down mid-traffic?</h1>
        <p>
          This gateway sits in front of multiple LLM providers. When one starts failing — an outage, a rate
          limit, a bad response — it retries the same request against a healthy provider before giving up, and
          opens a circuit breaker so it stops wasting time on the dead one. The numbers below are from the
          exact scenario in the project's README: kill the primary mock provider mid-run, keep sending the same
          traffic, and see what reaches the caller.
        </p>
        {!live && (
          <p className="demo-page__repro">
            These numbers are a real captured run, not invented placeholders. Reproduce it yourself:
            <code>
              docker compose -f docker-compose.yml -f docker-compose.demo.yml up --build
              {'\n'}loadtest seed ... &amp;&amp; loadtest run -n 400 -c 20
              {'\n'}curl -XPOST localhost:9001/chaos -d {'\''}{'{"mode":"down"}'}{'\''}
              {'\n'}loadtest run -n 400 -c 20
            </code>
            Point this page at that gateway (same origin, or <code>GATEWAY_URL</code> at build time) and it
            switches to the live badge automatically.
          </p>
        )}
      </section>

      {isLoading ? (
        <DemoSkeleton />
      ) : (
        <>
          <div className="demo-hero-grid">
            <Card title="Requests served">
              <StatTile label="in this run" value={formatCount(report.reliability.requests)} size="lg" />
            </Card>
            <Card title="Success rate">
              <StatTile
                label={`${formatCount(report.reliability.succeeded)} succeeded, ${formatCount(report.reliability.failed)} failed`}
                value={formatPct(report.reliability.success_rate)}
                tone={report.reliability.failed === 0 ? 'good' : 'warn'}
                size="lg"
              />
            </Card>
            <Card title="Rescued by failover">
              <StatTile
                label={`of ${formatCount(report.reliability.hit_upstream_failure)} requests that hit the dead provider`}
                value={formatCount(report.reliability.rescued_by_failover)}
                tone={report.reliability.rescued_by_failover > 0 ? 'good' : 'neutral'}
                size="lg"
                footnote="none of them turned into an error the caller saw"
              />
            </Card>
            <Card title="Circuit breaker">
              <StatTile
                label="reroutes once the dead provider tripped the breaker"
                value={formatCount(report.reliability.circuit_reroutes)}
                tone="good"
                size="lg"
              />
            </Card>
          </div>

          <div className="demo-secondary-grid">
            <Card title="Latency">
              <div className="stat-row">
                <StatTile label="p50" value={formatMs(report.latency.p50_ms)} />
                <StatTile label="p95" value={formatMs(report.latency.p95_ms)} />
                <StatTile label="p99" value={formatMs(report.latency.p99_ms)} />
              </div>
              <div className="stat-row stat-row--secondary">
                <StatTile label="time to first token (p50)" value={formatMs(report.latency.ttft_p50_ms)} />
              </div>
            </Card>

            <Card title="Response cache">
              <div className="stat-row">
                <StatTile label="hit rate" value={formatPct(report.cache.hit_rate)} tone="good" />
                <StatTile label="speedup on a hit" value={formatMultiplier(report.cache.speedup)} />
              </div>
              <div className="stat-row stat-row--secondary">
                <StatTile label="hit latency (p50)" value={formatMs(report.cache.hit_p50_ms)} />
                <StatTile label="miss latency (p50)" value={formatMs(report.cache.miss_p50_ms)} />
              </div>
            </Card>
          </div>

          {report.providers.length > 0 && (
            <Card title="Traffic by provider">
              <p className="demo-page__chart-caption">
                The dead provider ("mock-primary") only ever receives the handful of requests it took to trip
                its breaker; everything after that goes straight to the healthy one.
              </p>
              <ProviderShareChart providers={report.providers} />
              <div className="demo-provider-failures">
                {report.providers
                  .filter((p) => p.attempts_failed > 0)
                  .map((p) => (
                    <div key={p.provider} className="demo-provider-failures__row">
                      <span className="demo-provider-failures__name">{p.provider}</span>
                      <FailureBreakdown failures={p.failures_by_category} />
                    </div>
                  ))}
              </div>
            </Card>
          )}

          <footer className="demo-page__footer">
            <a href="https://github.com/Devpaul-01/llm-gateway" target="_blank" rel="noreferrer">
              View the source on GitHub →
            </a>
          </footer>
        </>
      )}
    </div>
  );
}

function DemoSkeleton() {
  return (
    <div className="demo-hero-grid">
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
