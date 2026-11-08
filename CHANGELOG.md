# Changelog

This changelog summarizes the notable engineering work behind llm-gateway,
organized by category rather than by dated release, since the project does
not follow a dated release cadence. Each entry reflects a real change in
the project's commit history; nothing here is aspirational.

---

## Reliability & Failover

- **Failover engine rewritten to track attempts, failovers, and real usage**,
  replacing an earlier version that didn't distinguish a first-try success
  from a rescued one. The engine now records, per request: how many
  candidates were tried (`attempts`), how many of those were failovers from
  a prior failure (`failovers`), which provider/model actually produced the
  final answer, and every model that was tried along the way
  (`models_used`) — all of which now flow into both the response's
  `gateway` metadata block and the `request_logs` row.
- **Candidate resolution batches credential lookups, applies circuit-breaker
  demotion, and supports explicit `provider:model` references** in one
  pass, replacing a simpler per-provider lookup. A single `provider:model`
  prefix is only honored when the prefix is a *registered* provider name,
  so model IDs that legitimately contain a colon (e.g. OpenRouter's
  `"...:free"` suffix convention) are never misparsed.
- **Mid-stream failures are categorized and acted on**, not just the
  initial connection failure. `BadModel` evicts the model from that
  provider's cached discovery list; `ProviderTransient` records a circuit-
  breaker failure and provider-health signal; `KeyFault` puts that specific
  credential into a 60-second cooldown. Earlier behavior only reacted to
  `KeyFault`.
- **A stream that ends without a clean `finish_reason` is treated as a
  failure, not a silent success.** Without this, the failover engine could
  splice a second model's continuation onto a truncated first response and
  present it to the caller as one coherent, complete answer.
- **`continue_on_failure` lets a caller explicitly opt in to continuation
  behavior**: on a qualifying mid-stream failure, the engine builds a
  continuation request (original messages + partial content + an explicit
  "continue exactly where you left off" instruction) and retries it on the
  next candidate. Without this flag, a mid-stream failure fails the request
  outright rather than silently stitching output from two models together.
- **A Redis-backed circuit breaker was added per provider**: five transient
  failures within a 30-second window opens the breaker; a single success
  closes it. State lives in Redis (not in-process) so multiple gateway
  instances agree on provider health. An open breaker demotes a provider
  to the back of the candidate list — it is never removed outright, so a
  recovering provider is still periodically retried.
- **Per-credential cooldown** (60s) keeps a credential that just
  key-faulted out of the next request's candidate list without requiring a
  database write, and `CoolingSet` checks many credentials' cooldown state
  in a single Redis round trip rather than one call per credential.
- **Vision-capable routing**: a request containing image content is
  automatically filtered to models known to support vision input; if none
  of the resolved candidates qualify, the request fails clearly rather than
  silently sending images to a text-only model or dropping them.
- **`fallback_models` lets a caller specify an explicit ordered list** of
  additional models to try if the primary fails, resolved the same way as
  the primary model (explicit provider, `provider:model` prefix, or the
  built-in shortcut table) — an unresolvable fallback entry is logged and
  skipped rather than failing the whole request.

## Providers

- **Four separate, near-identical provider adapters were refactored into
  one shared `OpenAICompatProvider`**, parameterized by base URL and two
  small behavior flags (`StreamUsage`, `MaxCompletionTokens`). Every
  provider added since this refactor — OpenAI, DeepSeek, Together,
  Fireworks, Cerebras, xAI, DeepInfra, SambaNova — was a registry entry,
  not a new adapter implementation.
- **Twelve providers are supported out of the box**: Groq, Mistral,
  OpenRouter, Gemini (via its OpenAI-compatible endpoint), OpenAI,
  DeepSeek, Together, Fireworks, Cerebras, xAI, DeepInfra, and SambaNova.
  Groq, Mistral, OpenRouter, and Gemini adapters have dedicated live
  integration tests (skipped in CI unless the corresponding API key env
  var is set).
- **Provider base URLs and stream-usage behavior are overridable per
  provider at runtime** via `PROVIDER_<NAME>_BASE_URL` /
  `PROVIDER_<NAME>_STREAM_USAGE` environment variables, without any code
  change — the mechanism the demo overlay uses to point `groq` and
  `mistral` at local mock upstreams.
- **Live model discovery** (`GET /models` on each provider) is cached in
  Redis for 6 hours with a single-flight lock, so a cache-miss refresh from
  several concurrent requests triggers one upstream call, not a thundering
  herd; a failed-model eviction (`BadModel` category) prunes a stale model
  ID out of the cached list without waiting for the full TTL to expire.
- **Streaming usage extraction handles provider-specific quirks**: Groq
  reports usage under an `x_groq.usage` field rather than the standard
  top-level `usage`, which the shared adapter now checks explicitly.

## Cost & Admission Control

- **Daily token budget reserves atomically and settles to actual usage.**
  A request reserves `max_tokens` plus an estimated prompt-token count
  *before* contacting any provider (closing a race where two concurrent
  requests could both read "under budget" and both proceed); after the
  response completes, the reservation is adjusted to the real token count
  reported by the provider (or an estimate derived from characters
  streamed, if the connection died mid-response) — so a short answer is
  refunded the unused portion rather than permanently billed for the full
  `max_tokens`, and a failed request isn't charged in full.
- **Per-project concurrency capping made atomic and self-healing.** An
  earlier non-atomic acquire path is replaced with a single Lua script;
  concurrency slot counters now self-expire after 15 minutes, so a crashed
  process between `Acquire` and the deferred `Release` cannot permanently
  lock a project's concurrency budget at zero.
- **Sliding-window rate limiting made a single atomic Lua script**,
  replacing a multi-step check-then-record sequence. Rejected requests are
  **not** recorded in the window, so a client hammering a closed door
  doesn't extend its own lockout.
- **Per-project settings (rate limit, concurrency, token budget) are
  configurable via the admin API** and cached in Redis with explicit
  invalidation on write, so a `PATCH` takes effect on the very next
  request rather than after a cache TTL.

## Response Caching

- **Added a Redis-backed response cache for `temperature: 0` requests**,
  with O(1) invalidation. Cache keys are scoped per project and embed a
  per-project epoch counter; `DELETE /admin/projects/{id}/cache` bumps the
  epoch, making every previous entry for that project unreachable
  immediately with no `SCAN`/`DEL` storm over the keyspace — orphaned
  entries simply expire on their own TTL.
- **Cache writes re-check the epoch before storing**, so a response that
  was already in flight when a flush happened cannot repopulate the cache
  with a now-stale answer.
- **Only clean, single-attempt, single-model, `finish_reason: "stop"`
  answers are cached.** A failed-over, continued, or truncated answer is
  never cached — caching a degraded response would mean silently serving
  it as if it were the primary model's clean output on every subsequent
  hit.
- **Callers can opt a single request out of the cache** via `"cache":
  false` in the request body or a `Cache-Control: no-cache` header.
- **`GET /admin/projects/{id}/cache` flush and cache-mode reporting**
  (`X-Gateway-Cache: hit|miss|bypass` header, and the same value in the
  `gateway` response block) give operators and callers visibility into
  whether a given response came from cache.

## HTTP API & Compatibility

- **OpenAI-shaped JSON error bodies everywhere**, replacing an earlier
  mix that included plain-text `http.Error` responses. Most
  OpenAI-compatible SDKs parse error bodies as JSON; a plain-text body
  either fails to surface a readable message or throws a JSON-parse error
  outright. Every error path — chat completions, streaming mid-response
  errors, and every admin endpoint — now returns `{"error": {"message",
  "type", "code"}}` with a `type` drawn from a fixed, documented set.
- **Errors are real HTTP status codes, never a `200` with an error embedded
  in the body.** A failure before the first token is produced returns the
  correct status immediately; a failure *after* streaming has begun is
  surfaced as a terminal SSE error event, since the HTTP status line has
  already been sent by that point.
- **Non-streaming (`stream: false`) support added** alongside the
  original streaming-only implementation, so the gateway serves both
  calling conventions an OpenAI-compatible client might use.
- **Usage and timing metadata on every response**, streaming or not: a
  `gateway` block (request ID, provider, model, cache mode, attempts,
  failovers, TTFT, total duration, whether usage was estimated) alongside
  OpenAI's own `usage` object, plus the same facts exposed as response
  headers (`Server-Timing`, `X-Gateway-Provider`, `X-Gateway-Cache`,
  `X-Gateway-Request-Id`) so the information is visible to proxies and
  `curl -i` without parsing a JSON body.
- **`GET /v1/models` added**, listing the models the calling project can
  route to by bare name given its currently configured provider
  credentials — many OpenAI-compatible clients and UIs call this on
  startup to populate a model picker.
- **Image content in messages** (`image_url` parts in the OpenAI content-array
  format) is parsed and forwarded to vision-capable candidates only.

## Admin API

- **Full project, gateway-key, and credential lifecycle via the admin
  API**: create/list/get projects, `PATCH` partial settings updates,
  create/list/revoke gateway keys, create/list/revoke provider credentials.
- **`PATCH /admin/projects/{id}` made a true partial update.** An earlier
  version didn't correctly distinguish "field omitted" from "field
  explicitly cleared"; a custom `optionalInt` JSON type now makes that
  distinction explicit, so an admin can clear an override back to the
  server default (`null`) without that being indistinguishable from simply
  not mentioning the field.
- **Credential and gateway-key listing endpoints added**, closing a gap
  where those resources could be created and revoked via the API but never
  *discovered* via the API — both list endpoints return metadata only
  (label, status, timestamps, and for gateway keys a display prefix),
  never key material, and include a best-effort `last_used_at` derived
  from `request_logs`.
- **Credentials are validated against a known-provider allow-list** at
  creation time, rejecting an unrecognized provider name with a clear `400`
  rather than silently storing a credential that can never be matched to
  any candidate.
- **Redis caches are evicted on every write that could invalidate them** —
  credential creation/revocation, gateway-key creation/revocation, and
  project settings updates all evict their corresponding cache entries in
  the same call that performs the write, so changes take effect on the
  very next request rather than lingering for a cache TTL.
- **Response-cache flush and provider-listing endpoints added**
  (`DELETE /admin/projects/{id}/cache`, `GET /admin/providers`).

## Observability

- **`request_logs` captures the full lifecycle of a request**: provider,
  model, credential used, status, error category (and separately, the
  *first* error category hit — useful for distinguishing "failed
  immediately" from "failed after some retries"), token counts (and
  whether they were estimated), TTFT, total duration, stream flag, vision
  usage, cache-hit flag, attempt count, failover count, and every model
  tried.
- **Structured logging replaced ad hoc `log.Printf` calls** throughout,
  using `log/slog` with a JSON handler to stdout.
- **Prometheus metrics added**: a request counter labeled by
  provider/model/status, and histograms for time-to-first-token and total
  request duration — registered against a dedicated registry (not the
  global default) so `/metrics` exposes exactly this service's own series.
  This was deliberately sequenced after the core request path, request-log
  persistence, and HTTP layer were already functional and tested, so the
  metrics design could be built against real traffic shapes rather than
  speculated about in advance.
- **The dashboard metrics report (`insights.Report`) computes reliability,
  latency, cache, usage, protection, and per-provider figures** from a
  combination of `request_logs` SQL aggregation and hourly Redis counters,
  exposed via `GET /admin/metrics`.
- **An unauthenticated `GET /public/metrics` endpoint** was added for a
  portfolio or public status page: the same report shape, scoped to at
  most one configured project, cached for 10 seconds in Redis so a popular
  page can't turn into a load test against the database, and wrapped in a
  CORS layer that is applied to this one route only.
- **Hourly Redis counters (`stats` package)** track events that never
  produce a `request_logs` row at all: requests blocked by rate
  limit/budget/concurrency before reaching a provider, and every
  individual upstream attempt (including ones absorbed by failover) —
  without these, a provider that failed several times before a later
  candidate succeeded would show only the one "final" attempt in
  `request_logs`, hiding the retries that actually happened.
- **An unbounded `"all"` metrics window was added**, dropping the
  `created_at` lower bound entirely rather than using a fixed interval —
  specifically so the public demo page doesn't show an artificially empty
  dashboard for seeded or sparse traffic that happens to fall outside a
  fixed recent window.

## Database & Migrations

- **An embedded SQL migration runner** was added (`go:embed`-bundled
  `.sql` files, no external migration-tool dependency), safe to run on
  every boot: it takes a Postgres advisory lock so multiple instances
  starting concurrently don't race, and tracks applied versions in a
  `schema_migrations` table.
- **Baselining for databases bootstrapped outside the runner.** A database
  created by Postgres's own `docker-entrypoint-initdb.d` mechanism (which
  `docker-compose.yml` uses to apply the very first schema file on first
  boot) is detected — the `projects` table exists but `schema_migrations`
  doesn't — and baselined at version 1 automatically, rather than failing
  with "relation already exists."
- **Observability columns added to `request_logs`** in a second migration
  (`request_id`, `cache_hit`, `attempts`, `failover_count`,
  `first_error_category`, `usage_estimated`), additive and
  backward-compatible (`ADD COLUMN IF NOT EXISTS`).
- **The Postgres connection pool is explicitly bounded** (`SetMaxOpenConns`,
  `SetMaxIdleConns`, connection and idle lifetime limits) — the
  unbounded default risks exhausting a managed Postgres instance's own
  connection limit under a traffic spike, and leaks idle connections
  against a connection pooler like Supabase's.

## Security

- **Provider API keys are encrypted at rest with AES-256-GCM**, a fresh
  random nonce generated per encryption (never reused under the same key),
  with authenticated-encryption tamper detection covered explicitly by
  dedicated GCM tamper-detection tests.
- **Only ciphertext is ever cached in Redis** for provider credentials;
  decryption happens in-process, immediately before a provider call, so a
  Redis compromise alone does not expose usable upstream API keys.
- **Gateway keys are stored as a SHA-256 hash, never the plaintext**, with
  only a 12-character display prefix retained for operator recognition in
  listings; the plaintext key is returned exactly once, at creation.
- **Admin-token comparison uses `crypto/subtle.ConstantTimeCompare`**,
  closing a timing side-channel that a naive string comparison would leave
  open.
- **Both gateway-key and credential revocation are immediate**: the
  database write and the Redis cache eviction happen in the same call, so
  a revoked credential or key stops working on the very next request
  rather than lingering for a cache TTL.
- **Admin credential/gateway-key listing endpoints never return key
  material** — the response types for these endpoints simply have no
  field capable of carrying it, so there is no risk of a future change
  accidentally serializing a secret into a listing response.
- **CORS is applied to exactly one route** (`GET /public/metrics`); every
  `/admin/*` and `/v1/*` route gets no CORS headers at all, so a browser
  script on any other origin cannot call them regardless of whether it
  somehow has a valid token.
- **Request body size limits added to every write path**: 10MB on chat
  completions, 1MB on every admin write, both enforced via
  `http.MaxBytesReader` before JSON decoding is attempted.
- **Negative caching on gateway-key lookups** means an unknown key is
  cached as "invalid" for 15 seconds, so a client probing many invalid
  keys in a tight loop can't turn every single guess into a database
  round trip.

## Reliability & Operations

- **Graceful shutdown on `SIGINT`/`SIGTERM`**, replacing an earlier
  implementation with no signal handling at all (which meant an
  orchestrator's rolling-deploy `SIGTERM` killed the process immediately,
  cutting off in-flight streams and skipping the deferred
  concurrency-slot release). `http.Server.Shutdown` is given a 25-second
  grace period to let in-flight requests — including long-lived SSE
  streams — finish naturally before the process exits.
- **Real health checks**, replacing an earlier `/healthz` that
  unconditionally returned `200 ok` regardless of whether Postgres or
  Redis were actually reachable (which defeats the entire point of a
  health-gated rolling deploy, since a check that can never fail provides
  no signal). Both `/healthz` and `/readyz` now ping both dependencies,
  each bounded to a 2-second timeout so a hung dependency can't hang the
  health check itself, and return `503` if either is unreachable.
- **The container `HEALTHCHECK` runs as a mode of the `gateway` binary
  itself** (`-healthcheck` flag) rather than the conventional `curl
  localhost:8080/healthz`, because the distroless final image has no
  shell and no `curl` to run.
- **A multi-stage Dockerfile** builds four statically linked binaries
  (`CGO_ENABLED=0`, trimmed and stripped) in a full Go toolchain stage,
  then ships only those binaries in a
  `gcr.io/distroless/static-debian12:nonroot` final image — no shell, no
  package manager, running as an explicit non-root user.
- **Redis connection bounded with explicit timeouts.** Read/write timeouts
  and pool size are set explicitly rather than left at client defaults,
  since Redis sits on the hot path of nearly every request (auth cache,
  rate limit, budget, concurrency, response cache) — an unbounded call
  there could hang a request indefinitely rather than failing fast.
- **CRLF line endings normalized to LF** across the existing codebase, and
  a `.gitattributes` added to keep it that way going forward.

## Developer & Demo Tooling

- **`cmd/genkey`** — a one-line CLI that prints a random 32-byte hex
  string suitable for `ENCRYPTION_KEY`.
- **`cmd/mockprovider`** — a fake OpenAI-compatible upstream with an
  injectable-failure `/chaos` control endpoint (`ok`, `down`, `ratelimit`,
  `flaky` modes), letting failover and circuit-breaker behavior be
  demonstrated and tested without any real provider API keys.
- **`cmd/loadtest`** — a load generator with a `seed` mode (creates a demo
  project, credentials, and gateway key through the real admin API) and a
  `run` mode (fires concurrent requests, reports success rate, latency,
  and failover statistics) — used both for the README's failover demo
  walkthrough and to populate realistic data for dashboard screenshots.
- **`docker-compose.demo.yml`** overlays two `mockprovider` instances onto
  the standard compose stack, with the gateway's provider base URLs
  pointed at them via environment variables, requiring zero code changes
  and zero real API keys to see failover, caching, and metrics working
  end to end.
- **A seed SQL script** (`scripts/seed_demo_data.sql`) bulk-inserts
  realistic `request_logs` rows for taking dashboard screenshots without
  needing to run a full load test first.
- **GitHub Actions CI** runs `go build`, `go vet`, a `gofmt` check, unit
  and end-to-end tests, `go test -race` against real Postgres and Redis
  service containers, and a separate Docker image build — on every push
  and pull request to `main`.

## Frontend (Gateway Console)

- **A standalone React + TypeScript admin/observability SPA added**
  (`web/`), scaffolded with Vite, a dark-theme design-token system, and a
  typed API client mirroring the Go backend's own response shapes.
- **An admin-token entry gate** (`TokenGate`/`TokenContext`) holds the
  operator-entered token in `sessionStorage` (not `localStorage`, to
  reduce exposure window) and routes a `401` from any call back to the
  gate rather than leaving the UI in a broken authenticated-looking state.
- **Four authenticated pages built out**: Dashboard (summary stat tiles +
  timeseries + latency charts, driven by a validated chart color palette),
  Providers (per-provider scoreboard with a failure-category breakdown),
  Projects / Project Detail (settings form with the backend's own
  tri-state `optionalInt` semantics, credentials, and gateway keys, scoped
  per project to match the backend's own data model), and Requests (recent
  activity table, fixed to the backend's own 15-row cap).
- **A public, token-free `/demo` page added**, routed outside the
  `TokenGate` entirely, rendering the `all`-window public metrics report
  for visitors with no admin token — with a recorded-run fixture as an
  explicit fallback if the live `/public/metrics` endpoint is unreachable,
  so the demo page degrades gracefully rather than showing a blank error
  state to someone evaluating the project.
- **UI primitives built for null-safety from the start**: a shared
  formatting layer treats a backend `null` (genuinely no data for a
  window) as visually distinct from `0`, matching the backend's own
  explicit nullable-field design rather than collapsing the distinction in
  presentation.
- **Settings form state derived during render rather than in a `useEffect`**,
  a correctness fix avoiding a class of stale-state bug common to
  effect-synchronized form state.
- **Vitest + React Testing Library test coverage** added for the two
  places a subtle bug would silently misrepresent real data: the null-safe
  formatting helpers and the API client's error parsing / auth-header
  injection logic, plus explicit coverage of the demo page's live-vs-
  recorded-run fallback behavior.
