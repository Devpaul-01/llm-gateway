# Frontend Architecture

Observability and admin dashboard for the LLM Gateway, built as a standalone
single-page app in `web/`. This document is based entirely on the backend as
it exists today (`internal/httpserver`, `internal/insights`, `internal/metrics`,
`internal/providers`) — no endpoint or capability below is assumed or invented.

## 1. What the backend actually offers

### 1.1 Routes (`internal/httpserver/server.go`)

| Method | Path | Auth | Purpose |
|---|---|---|---|
| GET | `/healthz`, `/readyz` | none | DB + Redis liveness |
| GET | `/metrics` | none | Raw Prometheus exposition (for a scraper, not this UI) |
| POST | `/v1/chat/completions` | gateway key | Not part of the admin UI |
| GET | `/v1/models` | gateway key | Not part of the admin UI |
| GET/POST/PATCH | `/admin/projects[/{id}]` | admin token | Project CRUD + settings |
| POST/GET/DELETE | `/admin/gateway-keys`, `/admin/projects/{id}/gateway-keys[/{keyID}]` | admin token | Gateway key issuance/list/revoke |
| POST/GET/DELETE | `/admin/credentials`, `/admin/projects/{id}/credentials[/{credentialID}]` | admin token | Provider credential issuance/list/revoke |
| DELETE | `/admin/projects/{id}/cache` | admin token | Flush response cache for a project |
| GET | `/admin/providers` | admin token | List of known provider names |
| GET | `/admin/metrics?project_id=&window=` | admin token | The full `insights.Report` (below) |
| GET | `/public/metrics?window=` | none, CORS-gated | Same report shape, no auth, read-only, 10s cached |

This is the complete surface. There is no login/session endpoint — auth is a
single static bearer token compared with `subtle.ConstantTimeCompare`
(`adminauth.go`). There is no per-user identity, no RBAC, no refresh token.

### 1.2 The critical constraint: CORS only covers `/public/*`

`withCORS` (`metrics.go`) wraps **only** `/public/metrics`. Every `/admin/*`
route gets no CORS headers at all. A browser SPA hosted on a different origin
than the gateway cannot call `/admin/*` — the request will be blocked by the
browser regardless of whether the token is correct. This is deliberate
(`server.go`'s comment: "the admin and /v1 routes deliberately get no CORS
headers").

Consequence for this frontend: **the admin dashboard must be served
same-origin with the gateway** (reverse-proxied, e.g. nginx/Caddy routing
`/` to the static build and `/admin`, `/public` through to the Go binary on
the same host/origin), or run in an environment the operator controls where
this isn't an issue (e.g. opening the built `index.html` via the gateway's
own origin, or a local reverse proxy during development). The architecture
below is built around that assumption; it does not add a backend proxy route,
since "do not add new backend endpoints unless a genuine integration gap
makes them necessary" — the actual gap here is deployment-level (origin
matching), not something a new Go endpoint would fix.

For local development, Vite's dev server proxy (`vite.config.ts`) forwards
`/admin` and `/public` to the gateway's origin so the browser only ever talks
to one origin — see §7.

### 1.3 The metrics report shape (`internal/insights/insights.go`)

`GET /admin/metrics` and `GET /public/metrics` return one JSON document,
`insights.Report`:

```ts
interface Report {
  window: "1h" | "6h" | "24h" | "7d";
  generated_at: string; // RFC3339
  reliability: {
    requests: number; succeeded: number; failed: number;
    success_rate: number | null;          // succeeded / (requests - canceled)
    hit_upstream_failure: number;         // requests that hit >=1 failure
    rescued_by_failover: number;          // ...but still succeeded
    rescue_rate: number | null;           // rescued / hit_upstream_failure
    rescued_rate_limits: number; rescued_outages: number; rescued_bad_model: number;
    upstream_attempts_total: number; upstream_attempts_failed: number;
    circuit_reroutes: number;
  };
  latency: { p50_ms, p95_ms, p99_ms, ttft_p50_ms, ttft_p95_ms: number | null };
  cache: {
    hits: number; hit_rate: number | null; tokens_saved: number;
    hit_p50_ms, miss_p50_ms: number | null; speedup: number | null;
  };
  usage: { tokens_in, tokens_out, total_tokens, stream_requests: number };
  protection: { rate_limited, budget_blocked, concurrency_blocked, total_blocked: number };
  providers: Array<{
    provider: string; requests: number; share: number;
    success_rate: number | null; p50_ms, p95_ms, ttft_p50_ms: number | null;
    tokens: number; attempts_ok: number; attempts_failed: number;
    failures_by_category: Record<string, number>; // keys: key_fault | provider_transient | bad_model
  }>;
  top_models: Array<{ provider: string; model: string; requests: number }>;
  timeseries: Array<{
    t: string; requests: number; success: number; cache_hits: number;
    rescued: number; tokens: number; p50_ms, p95_ms: number | null;
  }>;
  recent: Array<{
    t: string; provider: string; model: string; status: string;
    total_ms, ttft_ms: number | null; tokens: number; cache_hit: boolean;
    attempts: number; stream: boolean;
  }>;
}
```

Every `*_ms` and `*_rate`/`*_hit_rate` field is a nullable pointer in Go,
serialized as JSON `null` when there's no data (e.g. a window with zero
requests, or zero cache hits). **The UI must render "no data" distinctly from
"0"** — this is an explicit requirement and the backend already does the work
of distinguishing them; the frontend must not coerce `null` to `0`.

`protection` counters come from Redis (`stats.Read`), independent of Postgres
request_logs, and can be nonzero even when `requests` is low (they count
rejections that happened *before* reaching a provider).

Windows are fixed to exactly `1h | 6h | 24h | 7d` (`insights.ParseWindow`);
anything else silently falls back to `24h` server-side, so the UI only
offers those four.

### 1.4 Error shape

Every admin/chat error path returns `{"error": {"message": string, "type": string, "code"?: string}}`
(`apierror.go`, `errors.go`). `type` is one of `invalid_request_error |
authentication_error | rate_limit_error | internal_error | service_unavailable
| upstream_error | api_error | not_found_error`. The client parses this
consistently rather than assuming plain text.

### 1.5 What does not exist (do not build UI for)

- No login/session/user accounts — only the one admin token.
- No audit log / activity history endpoint.
- No way to update a project's `name` (PATCH only touches rate limit,
  concurrency, token budget) or to delete a project.
- No endpoint to rotate a gateway key or credential (only create + revoke).
- No pagination on `/admin/metrics`'s `recent` (hard-capped at 15 rows) or on
  credentials/gateway-key lists — small admin surface, not infinite data.
- No websocket/SSE for metrics — the dashboard polls.

## 2. Stack

- **React 18 + TypeScript**, built with **Vite** (fast dev server with a
  built-in dev proxy, which directly solves the CORS/same-origin constraint
  above for local development; standard for a small admin SPA with no SSR
  need — nothing here benefits from server rendering).
- **React Router** for the four top-level views (§4).
- **TanStack Query** for all server state: polling `/admin/metrics` on an
  interval, cache invalidation after mutations (create/revoke), request
  dedup. This replaces hand-rolled `useEffect` fetch/loading/error plumbing.
- **Recharts** for the timeseries/latency charts — handles the
  gap-filled, nullable-point timeseries data cleanly and stays lightweight.
- Plain CSS with CSS custom properties (design tokens) for the dark theme;
  no CSS framework dependency for a UI this size.
- **Vitest + React Testing Library** for unit/component tests.

No state-management library beyond TanStack Query + a small amount of React
context (the admin token). No GraphQL — the backend is REST/JSON only.

## 3. Project structure

```
web/
  src/
    api/
      client.ts          # fetch wrapper: base URL, auth header, error parsing
      types.ts            # Report, ProjectResponse, etc. — mirrors Go structs exactly
      queries.ts           # TanStack Query hooks (useMetrics, useProjects, ...)
      mutations.ts          # useCreateProject, useRevokeKey, ...
    auth/
      TokenContext.tsx     # admin token in memory + sessionStorage, login gate
    components/
      layout/               # Shell, Sidebar, TopBar, WindowPicker
      charts/                # TimeseriesChart, LatencyChart, DonutShare
      ui/                    # Card, StatTile, Table, Badge, Skeleton, EmptyState
    pages/
      Dashboard/
      Providers/
      Projects/
        ProjectDetail/        # credentials + gateway keys for one project
      Requests/
    hooks/
      usePolling.ts
    lib/
      format.ts              # ms/number/percent formatting, null-safe
    App.tsx
    main.tsx
  index.html
  vite.config.ts
  package.json
  tsconfig.json
```

## 4. Pages

1. **Dashboard** (`/`) — the overview. Summary stat tiles (requests,
   success rate, p50/p95/p99, rescue rate, cache hit rate, tokens saved,
   total blocked), a request-volume + success timeseries chart, a
   latency (p50/p95) timeseries chart, and a provider share donut/bar.
   Window picker (1h/6h/24h/7d) and optional project filter drive every
   query on the page.
2. **Providers** (`/providers`) — one row per provider from `report.providers`:
   share, success rate, p50/p95, TTFT, attempts ok/failed, and the
   `failures_by_category` breakdown rendered as a small stacked bar per row.
   Providers that only ever failed (no successful request_logs row) still
   appear, per the backend's own handling.
3. **Projects** (`/projects`, `/projects/:id`) — list + create (`name` only,
   per the API). Detail view: settings form (rate limit / concurrency /
   token budget, each tri-state: unset/leave-alone vs explicit value vs
   null-to-clear, matching `optionalInt` semantics), cache flush button,
   and the project's credentials + gateway keys (§ below) scoped to that
   project only — matching the backend's own scoping.
4. **Requests** (`/requests`) — table of `report.recent` (max 15, newest
   first) with status badges, provider/model, total/TTFT ms, tokens, cache
   hit indicator, attempts, stream flag. This is a read view only; there is
   no request-detail or search endpoint to build further into.

Credentials and gateway keys are **not** top-level pages — the backend scopes
every credential/key list and create call to a `project_id`, so they live
inside Project Detail, matching the real data model instead of inventing a
cross-project view the API can't answer.

## 5. Data flow

- `TokenContext` holds the admin token (never a default; the operator pastes
  it in on first load) and exposes it to `api/client.ts` via a header.
  Token lives in `sessionStorage` (cleared on tab close) — not `localStorage`
  — to reduce exposure window, and never in a URL or query string.
- All reads go through TanStack Query hooks in `api/queries.ts`. The
  Dashboard/Providers/Requests pages all key their `useMetrics(window,
  projectId)` query the same way so window/project changes anywhere are
  consistent and cached.
- `useMetrics` polls every 15s by default while its tab is focused
  (`refetchInterval` + `refetchIntervalInBackground: false`), matching the
  backend's own 10s public-metrics cache TTL — polling faster would just
  re-request the same cached/near-identical document.
- Mutations (create project, create/revoke credential or key, update
  settings, flush cache) invalidate the relevant query keys on success so
  the UI reflects the change on the next poll tick without a manual refresh.
- A failed auth check (401 from any admin call) clears the token and returns
  to the token-entry gate, rather than silently retrying.

## 6. Observability presentation choices

- **Null vs zero is always visually distinct.** A null latency/rate renders
  as "—" with a muted style and a tooltip ("no data in this window"), never
  as "0" or "0%" — this is a direct, literal requirement given the backend's
  own `*ms(f sql.NullFloat64) *int` / `ratio()` nil-on-empty design.
- Percentages (`success_rate`, `hit_rate`, `rescue_rate`, `share`) are stored
  as 0–1 floats server-side; the client formats with one shared `formatPct`
  helper so rounding/precision is consistent everywhere.
- The timeseries is already gap-filled with zero-valued points by the
  backend (`insights.timeseries`), so the chart layer does not need its own
  interpolation/gap logic — it trusts the server's buckets as given.
- Units are explicit in every chart/stat label (ms, tokens, req/s is not
  computed — the backend gives counts per bucket, not a rate — so the UI
  presents "requests per {bucket}" rather than inventing a req/s figure the
  backend doesn't provide).
- `generated_at` is shown as a small "as of HH:MM:SS" indicator so a stale
  poll (e.g. after a network drop) is visible rather than silently trusted.

## 7. Local development against the real backend

```
docker compose -f docker-compose.yml -f docker-compose.demo.yml up --build
```

`vite.config.ts` proxies `/admin` and `/public` to `http://localhost:8080`,
so `npm run dev` talks to the real gateway on one browser-visible origin and
exercises the real `/admin/metrics` and CRUD endpoints — no mocking layer,
per the brief's "do not invent endpoints or assume capabilities."

## 8. Security

- The admin token is operator-entered, kept in memory/`sessionStorage`,
  sent only as `Authorization: Bearer <token>` to same-origin `/admin/*`
  calls — never logged, never put in a URL.
- `/public/metrics` is used only where explicitly appropriate (no token
  available/needed) — the Dashboard does not fall back to it silently if an
  admin call fails, since that would show a different (unscoped) dataset
  without saying so.
- No secrets are rendered back by the API (`credentialMetadataResponse`
  never includes key material) except the one-time plaintext gateway key in
  `createGatewayKeyResponse` — the UI shows that value exactly once, in a
  copy-to-clipboard panel, with an explicit "you won't be able to see this
  again" notice, and does not persist it in any client state beyond that view.
- No new CORS/proxy surface is added to the Go backend; same-origin serving
  is a deployment concern documented here, not solved by widening the
  backend's existing, deliberately narrow CORS policy.

## 9. Testing

- `lib/format.ts` (null-safe formatting) and `api/client.ts` (error parsing,
  auth header injection) get unit tests — these are the two places a subtle
  bug would silently misrepresent real data.
- Key components (StatTile with a null value, the window picker, the
  settings form's tri-state logic) get React Testing Library tests.
- `npm run build` (`tsc` + Vite production build) must pass as part of
  "definition of done."
