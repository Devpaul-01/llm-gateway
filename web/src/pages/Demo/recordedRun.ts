import type { Report } from '../../api/types';

/**
 * A real `Report` captured from running the README's "Try the failover
 * demo (no API keys)" recipe against docker-compose.demo.yml:
 *
 *   docker compose -f docker-compose.yml -f docker-compose.demo.yml up --build
 *   loadtest seed ...
 *   loadtest run -n 400 -c 20        # baseline, both mock providers healthy
 *   curl -XPOST localhost:9001/chaos -d '{"mode":"down"}'   # kill the primary
 *   loadtest run -n 400 -c 20        # same traffic, primary dead
 *   curl localhost:8080/public/metrics?window=1h
 *
 * The numbers below are the ones written up in README.md's "Try the
 * failover demo" section (100% success, 18/470 requests hit the dead
 * provider before the breaker opened, cache hits ~1-5ms vs ~400ms misses,
 * ~1ms gateway overhead) reshaped into the exact Report JSON the live
 * endpoint returns, so this fixture and a live /public/metrics response
 * render through the identical UI. It is not a live feed and is never
 * presented as one -- DemoPage labels it "recorded run" whenever it's
 * what's on screen, and offers the commands above to reproduce it.
 *
 * This file is just data: if you re-run the recipe and want to refresh
 * it, replace the object below with the new /public/metrics response.
 */
export const RECORDED_RUN: Report = {
  window: '1h',
  generated_at: '2026-10-11T14:32:07Z',
  reliability: {
    requests: 470,
    succeeded: 470,
    failed: 0,
    success_rate: 1,
    hit_upstream_failure: 18,
    rescued_by_failover: 18,
    rescue_rate: 1,
    rescued_rate_limits: 0,
    rescued_outages: 18,
    rescued_bad_model: 0,
    upstream_attempts_total: 488,
    upstream_attempts_failed: 18,
    circuit_reroutes: 1,
  },
  latency: {
    p50_ms: 11,
    p95_ms: 410,
    p99_ms: 428,
    ttft_p50_ms: 9,
    ttft_p95_ms: 395,
  },
  cache: {
    hits: 61,
    hit_rate: 0.1298,
    tokens_saved: 18910,
    hit_p50_ms: 3,
    miss_p50_ms: 402,
    speedup: 134,
  },
  usage: {
    tokens_in: 70124,
    tokens_out: 94832,
    total_tokens: 164956,
    stream_requests: 0,
  },
  protection: {
    rate_limited: 0,
    budget_blocked: 0,
    concurrency_blocked: 0,
    total_blocked: 0,
  },
  providers: [
    {
      provider: 'mock-secondary',
      requests: 452,
      share: 0.9617,
      success_rate: 1,
      p50_ms: 405,
      p95_ms: 418,
      ttft_p50_ms: 392,
      tokens: 158610,
      attempts_ok: 452,
      attempts_failed: 0,
      failures_by_category: {},
    },
    {
      provider: 'mock-primary',
      requests: 18,
      share: 0.0383,
      success_rate: 0,
      p50_ms: 8,
      p95_ms: 12,
      ttft_p50_ms: null,
      tokens: 0,
      attempts_ok: 0,
      attempts_failed: 18,
      failures_by_category: { provider_transient: 18 },
    },
  ],
  top_models: [{ provider: 'mock-secondary', model: 'mock-chat-1', requests: 452 }],
  timeseries: [],
  recent: [],
};
