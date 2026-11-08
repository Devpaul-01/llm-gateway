# Security Policy — llm-gateway

This document describes the security architecture of llm-gateway, a self-hostable Go reverse proxy that holds real third-party provider API credentials on behalf of its operator and brokers access to them via gateway keys it issues. Design tradeoffs are documented explicitly rather than glossed over. A consolidated summary of the same mechanisms also lives in [ARCHITECTURE.md §14](ARCHITECTURE.md#14-security-architecture-summary--full-detail-in-securitymd).

---

## 1. Security Overview

llm-gateway's security model is built around three layers:

1. **Perimeter** — a 10MB body cap on `/v1/chat/completions`, a 1MB cap on every `/admin/*` write, `http.MaxBytesReader` enforcing both, and `ReadHeaderTimeout`/`IdleTimeout` on the server to defend against slow-loris-style connection exhaustion. There is deliberately **no** `WriteTimeout` — a streaming chat completion can legitimately run for as long as the model takes to finish talking, so that timeout is bounded instead by the provider's own connection lifecycle and the client disconnecting.
2. **Identity & Access** — two entirely independent, single-purpose credential types, each scoped to exactly the surface it needs: a **gateway key** per project (authenticates `/v1/*` chat traffic, nothing else) and a single global **admin token** (authenticates every `/admin/*` management/metrics call). There is no user account system, no session, and no RBAC beyond this two-tier split — a deliberate scope decision for a self-hosted, typically single-operator service (§15 covers the tradeoff explicitly).
3. **Data Integrity** — all persistence goes through `database/sql` with parameterized queries (no raw SQL string concatenation anywhere in the codebase); third-party provider API keys are encrypted at rest with AES-256-GCM before they ever reach Postgres, and only ciphertext is ever cached in Redis.

The guiding principle throughout the codebase is **fail open on availability, fail closed on correctness and secrecy**: a Redis outage degrades admission-control enforcement and caching (§9), never credential confidentiality or request correctness. Errors that can't be classified default to the conservative choice (an unrecognized provider failure is treated as retryable-but-not-free-passing; an unparseable request is rejected before touching any shared state).

---

## 2. Supported Versions

This is a single-track project with no parallel maintained release branches.

| Component | Status |
|---|---|
| `main` / latest commit | Actively maintained — receives security fixes |
| Prior commits/tags | Not maintained — no backports |

There is no LTS branch and no semantic versioning scheme in use. Anyone running or evaluating this code should treat only the current `HEAD` of `main` as reflecting the security posture described here.

---

## 3. Reporting a Vulnerability

This project does not currently operate a dedicated `security@` inbox or a bug bounty program.

If you find a vulnerability:

1. **Do not open a public GitHub issue** describing the exploit or any proof-of-concept.
2. Contact the maintainer privately via the contact details on the GitHub profile hosting this repository, or the email address associated with the repository's commits.
3. Include: the affected file/endpoint, reproduction steps, and the potential impact (what an attacker gains — provider credential exposure, cost amplification, data exposure, etc.).
4. Allow reasonable time for a fix before any public disclosure.

Because llm-gateway is typically self-hosted by its operator rather than run as a shared multi-tenant SaaS by the project maintainers, the practical urgency of most findings is "this pattern would be dangerous in a given operator's deployment" rather than "this is actively being exploited against a service we run" — responsible, private disclosure remains the expected norm regardless.

---

## 4. Authentication

llm-gateway has **two** independent authentication mechanisms, deliberately kept separate so that compromising one never grants the other's privileges.

### 4.1 Gateway keys (`/v1/*` — chat completions, model listing)

- **Format and generation.** `gatewaykeys.GenerateKey()` produces 24 bytes from `crypto/rand`, hex-encoded and prefixed `gw_live_` (`internal/gatewaykeys/gatewaykeys.go`). The plaintext key is returned to the caller **exactly once**, at creation (`POST /admin/gateway-keys`); it is never stored in recoverable form and is never returned by any subsequent API call.
- **Storage.** Only `sha256(plaintext)` is persisted in Postgres (`gateway_api_keys.key_hash`, `UNIQUE`), alongside a 12-character display prefix (`key_prefix`) so an operator can recognize a key in a listing without the full value ever being retrievable again. A stolen database dump yields hashes, not usable keys.
- **Lookup.** `RequireGatewayKey` (`internal/httpserver/auth.go`) extracts the `Authorization: Bearer <key>` header, hashes it, and looks it up via `gatewaykeys.LookupCached` — a Redis read-through cache in front of the hashed Postgres lookup (§9). **Both positive and negative results are cached**: an unknown key is cached as a negative marker for 15 seconds, specifically so a client probing many invalid keys in a tight loop can't turn every single guess into a database query — this is a deliberate brute-force cost-amplification defense, not an incidental side effect of the caching layer.
- **Revocation is immediate.** `RevokeAndInvalidate` performs the Postgres status update and the Redis cache eviction in the same call; because `LookupByPlaintext` only ever matches `status = 'active'`, a revoked key stops authenticating on the very next request, not after a cache TTL expires (verified explicitly by `TestE2E_RevokedKeyStopsWorkingImmediately`).
- **Per-key rate limiting.** Every gateway key is independently rate-limited (`ratelimit.AllowRequest`, a sliding-window Lua script) before any other processing happens, so one compromised or misbehaving key cannot exhaust another project's request budget even if they happen to share infrastructure.

### 4.2 Admin token (`/admin/*` — project, credential, gateway-key management; metrics)

- **Single static token**, supplied via the `ADMIN_TOKEN` environment variable, required at startup (`config.Load` fails fast if unset — the process will not start without it).
- **Constant-time comparison.** `RequireAdminToken` (`internal/httpserver/adminauth.go`) compares the provided token against the configured one with `crypto/subtle.ConstantTimeCompare`, specifically to avoid a timing side-channel that could let an attacker infer the correct token byte-by-byte from response-time differences.
- **No rotation mechanism, by design-scope.** There is exactly one admin token at a time; rotating it means changing the environment variable and restarting the process. This is consistent with the project's explicit single-operator scope (§15) rather than an oversight — a future multi-admin or rotating-token model would be a scope expansion, not a bug fix.

### 4.3 What does not exist (do not assume it)

- No password-based login, no OAuth, no session cookies, no JWTs.
- No per-user identity of any kind on the admin surface — every caller with the admin token has full access to every project.
- No multi-factor authentication.
- No account lockout / login-attempt throttling beyond the negative-cache effect described in §4.1 (which applies to gateway keys, not the admin token — the admin token has no equivalent brute-force defense beyond its own length and randomness, since it is a single long-lived operator secret rather than a per-request credential expected to be guessed against at volume).

---

## 5. Authorization

### 5.1 Two-tier model, no finer granularity

- **Gateway keys** authorize exactly `POST /v1/chat/completions` and `GET /v1/models`, scoped to the one project they were issued for (`key.ProjectID` is threaded through the request context by `RequireGatewayKey` and used for every subsequent admission-control and credential lookup — a gateway key for Project A can never resolve or spend Project B's credentials, rate limit, or budget).
- **The admin token** authorizes every `/admin/*` route, for every project, uniformly. There is no project-scoped admin credential and no read-only admin role — possession of the admin token is full administrative control of the entire gateway instance.
- **No role hierarchy, no per-user permissions.** This is the single biggest scope boundary in the project's authorization model, and it is intentional: llm-gateway is built as a self-hosted gateway for a project owner managing their own provider spend and keys, not a multi-tenant SaaS with delegated administration. An operator who needs to hand out scoped, auditable admin access to multiple team members would need to build that layer themselves on top of this gateway (e.g., behind their own reverse-proxy-level auth), and should not assume one exists here.

### 5.2 Public, unauthenticated surface

Three routes intentionally require no authentication at all, and this is a conscious, narrow allow-list rather than a gap:

| Route | Why it's public | What it leaks |
|---|---|---|
| `GET /healthz`, `GET /readyz` | Orchestrator/load-balancer health probes conventionally run unauthenticated | Only `{status, database, redis}` — up/down booleans, nothing about traffic, cost, or content |
| `GET /metrics` | Conventional Prometheus scrape pattern — access control for this route is expected to be a network-level concern (only the cluster's own Prometheus can reach the port/path), not an application-level bearer token that would complicate scrape configuration for no real secrecy benefit | Aggregate request counts/latencies labeled by provider/model/status only — no project IDs, no keys, no prompt content |
| `GET /public/metrics` (opt-in, `PUBLIC_METRICS_ENABLED`) | Built specifically to power a public-facing status/portfolio page | The same `insights.Report` shape as `/admin/metrics`, but scoped to at most one configured `PUBLIC_METRICS_PROJECT_ID` (or all projects, if left empty — an operator choice, not a default that should be made without thinking about it) and **never** containing keys, credentials, prompt content, or project identifiers |

`/public/metrics` is the only one of the three with CORS headers at all (§6), and it is explicitly read-only — there is no public write path anywhere in the system.

---

## 6. CORS — Scoped to Exactly One Endpoint

`withCORS` (`internal/httpserver/metrics.go`) wraps **only** `GET /public/metrics` (and its `OPTIONS` preflight). Every other route — every `/admin/*` route and every `/v1/*` route — gets **no CORS headers whatsoever**. This means a browser-based script running on any origin other than the gateway's own cannot call `/admin/*` or `/v1/*` at all, regardless of whether it somehow obtained a valid token; the browser itself blocks the request before it's ever sent cross-origin with credentials.

This has a direct, named consequence for the project's own frontend (ARCHITECTURE.md §15, §16): the Gateway Console **must** be served same-origin with the gateway in any real deployment. The fix for that constraint is a reverse-proxy routing decision at deploy time, not a widening of the backend's CORS policy — broadening `/admin/*` CORS to make frontend deployment marginally more convenient would directly weaken the admin surface's defense against cross-origin credential misuse, and was deliberately rejected as a solution.

When `PUBLIC_METRICS_ENABLED=true`, `CORS_ALLOWED_ORIGINS` must be set to the specific origin(s) that should be allowed to read it (or `*` for "anyone" — an explicit, deliberate operator choice for a page meant to be public). There is no fallback to a wildcard when the variable is unset; an empty `CORS_ALLOWED_ORIGINS` with `PUBLIC_METRICS_ENABLED=true` means the endpoint runs but no browser-based cross-origin caller is actually allowed to read it (server-to-server / `curl` calls are unaffected, since CORS is a browser enforcement mechanism, not a server-side access control).

---

## 7. Credential Handling — Provider API Keys

Provider API keys (the actual Groq/OpenAI/Mistral/etc. secrets the gateway uses on the operator's behalf) are the most sensitive data this system holds, and are handled with the most layers of protection in the codebase.

### 7.1 Encryption at rest

- **AES-256-GCM** (`internal/crypto/crypto.go`), a 32-byte key supplied via `ENCRYPTION_KEY` (hex-encoded, required at startup — `config.Load` fails fast if absent or malformed).
- A **fresh random nonce per encryption** (`crypto/rand`, `gcm.NonceSize()` bytes) — nonce reuse under the same key is the classical catastrophic failure mode of GCM (it allows forging authenticated ciphertexts and can leak the authentication key), so each `Encrypt` call generates and stores a new one (`provider_credentials.key_nonce`) alongside the ciphertext. Nothing in this codebase ever reuses a nonce.
- **Authenticated encryption, not just confidentiality.** GCM's authentication tag means a tampered ciphertext fails to decrypt rather than silently producing corrupted-but-accepted plaintext — explicitly covered by `internal/crypto/crypto_test.go`'s tamper-detection tests.
- `key_version` is stored per credential (currently always `1`), a forward-compatible field for a future key-rotation scheme — not yet implemented, and operators should not assume rotating `ENCRYPTION_KEY` migrates existing encrypted credentials (doing so makes every previously stored key unrecoverable, as the `.env.example` comment states explicitly: *"losing or changing this makes every stored provider key unreadable"*).

### 7.2 Caching — ciphertext only

`credentials.GetManyCached` (`internal/credentials/cache.go`) caches the **ciphertext and nonce** in Redis, never the decrypted plaintext API key. Decryption happens in-process, on every cache hit, immediately before a candidate adapter is constructed — this costs microseconds and means a Redis compromise (a misconfigured instance exposed to the network, a Redis-level vulnerability, an operator error) yields encrypted blobs, not usable provider credentials, without also separately compromising `ENCRYPTION_KEY`.

### 7.3 Never returned by any API

Once created, a provider credential's plaintext or ciphertext is **never** returned by any endpoint again, admin or otherwise. `GET /admin/projects/{id}/credentials` returns `credentialMetadataResponse`: id, project, provider, label, status, created-at, and a best-effort `last_used_at` derived from `request_logs` — explicitly no key material field exists in that struct at all, so there's no accidental-inclusion risk from a future field being added carelessly; the type itself cannot carry it.

### 7.4 Revocation, not deletion

`credentials.Revoke` / `RevokeAndInvalidate` set `status = 'revoked'` rather than deleting the row. This is a deliberate choice, not an oversight: deleting the row would orphan `request_logs.credential_id` foreign keys (breaking historical audit/last-used queries), while revocation both (a) immediately excludes the credential from `GetByProjectAndProvider`'s `WHERE status = 'active'` filter — meaning it stops being selected as a candidate on the very next `resolveCandidates` call — and (b) evicts the Redis-cached credential list for that project+provider in the same call, so a revoked credential cannot continue being served from a stale cache for its remaining TTL.

### 7.5 Where credentials travel

A decrypted provider API key exists in process memory only for the lifetime of the outbound HTTP request to that provider (`OpenAICompatProvider.Chat` sets it as the `Authorization: Bearer` header on a request built fresh for that call) and is never logged, never written to `request_logs` (which stores `credential_id`, a UUID, not the key itself), and never included in any error message surfaced to a gateway-key caller (a provider's 401/403 response body is relayed only insofar as `ProviderError.Cause` wraps the upstream's own error text, which providers do not echo caller-supplied keys back into).

---

## 8. Input Validation and Request Limits

- **Body size.** `http.MaxBytesReader` caps `/v1/chat/completions` request bodies at 10MB and every `/admin/*` write at 1MB (`adminBodyLimit`, `internal/httpserver/server.go`). A body exceeding the cap is rejected with `413 request_too_large` before JSON decoding is attempted.
- **Parse before touching shared state.** The chat completion handler decodes and validates the request body (non-empty `messages`, `temperature` within `[0, 2]`) **before** any Redis call — a malformed request never consumes a rate-limit slot, a concurrency slot, or budget, and never reaches the database.
- **Provider allow-list.** `POST /admin/credentials` rejects any `provider` value not in the registered provider list (`providers.IsKnownProvider`) with a `400` naming the supported set — credentials cannot be created for an arbitrary, unvalidated provider string.
- **Model/provider resolution is allow-list-based, not free-text routing.** `resolveChoice` only ever dispatches to a `providerRegistry` entry; an unrecognized provider name in a request (whether as an explicit `"provider"` field or inferred from `"provider:model"`) is rejected with a `400` before any credential lookup or outbound call — there is no code path by which gateway-key-authenticated traffic can cause the gateway to make an outbound request to an attacker-controlled host (the only way to introduce a new base URL at all is the operator-controlled `PROVIDER_<NAME>_BASE_URL` environment variable, read once at process startup — never a value accepted from any HTTP request).
- **PATCH semantics are explicit, not permissive.** `updateProjectSettingsRequest`'s `optionalInt` type distinguishes "field absent" (leave unchanged) from "field `null`" (clear override, fall back to default) from "field set" (apply new value) via a custom `UnmarshalJSON` — closing the class of bug where an admin API silently zeroes out unrelated fields on a partial update, or where `null` and "absent" are conflated and a client can't actually clear an override.

---

## 9. Availability and Abuse Protection

### 9.1 Three independent admission gates (full detail: ARCHITECTURE.md §8)

Rate limit (per gateway key), concurrency cap (per project), and daily token budget (per project) are each enforced via a single atomic Redis Lua script, checked before any provider is contacted. Each is individually configurable per project via `PATCH /admin/projects/{id}` and falls back to an operator-set server-wide default otherwise — so a single compromised or misbehaving gateway key cannot exhaust shared infrastructure capacity, and a single project cannot silently run up unbounded cost against the operator's own provider accounts.

### 9.2 Credential cooldown and circuit breaking are abuse/outage mitigations too

A credential that key-faults (401/402/403/429 from a provider) is excluded from candidate selection for 60 seconds (`cooldown`), and a provider with 5 transient failures in 30 seconds has its circuit opened and demoted (`breaker`) — both reduce the gateway's own outbound request volume against an already-struggling or already-rejecting upstream, which is good citizenship toward the provider and reduces the chance of a single bad credential or outage cascading into exhausting every other candidate's retry budget too.

### 9.3 Response cache cannot be used to poison or leak across projects

Cache keys are **scoped per project** (`respcache:<projectID>:<epoch>:<hash>`) — there is no code path by which Project A's cached response can be served to Project B's request, even for an identical prompt, because the project ID is part of the key, not just the prompt hash. Cache *invalidation* (`DELETE /admin/projects/{id}/cache`) is epoch-based specifically so a flush can never race with an in-flight write: `respcache.Set` re-checks the epoch hasn't moved between a request's cache lookup and its write-back, so a response that was already being generated when a flush happened cannot repopulate the cache with what is now stale data.

### 9.4 Partial/truncated stream failures are never silently served as complete

A stream that ends without an explicit `[DONE]` marker **and** without a `finish_reason` is treated as a failure, not a success (`openaicompat.go`: *"A stream that ends without `[DONE]` and without a finish_reason was cut off... stops the failover engine from splicing a second model's answer onto a truncated first one"*). This is both a correctness property and a security-adjacent one: without this check, a truncated response could be cached (§9.3's cacheability gate already excludes this case independently, as defense in depth) or billed as if the full `max_tokens` worth of content had been generated and delivered, misrepresenting both cost and content to the caller.

### 9.5 Graceful degradation under Redis loss

If Redis becomes unreachable: gateway-key lookups fall through to direct (uncached) Postgres queries; project settings and credential lookups do the same; rate limiting, concurrency capping, and budget enforcement effectively stop functioning (the Lua scripts will error, and callers of `ratelimit.AllowRequest`/`concurrency.Acquire`/`budget.CheckAndReserve` currently return an error on Redis failure, which the handler surfaces as a `500` rather than silently allowing the request through unchecked — so the actual failure mode under total Redis loss is "requests start failing with 500s," not "all admission control silently vanishes and the provider gets flooded"). This is the one place the system's fail-open posture is bounded: the admission gates fail *closed* on a Redis error specifically because failing open there would remove cost protection at the exact moment an operator would least want that.

---

## 10. Observability Data — What It Does and Does Not Contain

- **`request_logs`** stores provider/model names, token counts, timing, status, and foreign keys to `project_id` / `gateway_key_id` / `credential_id` — it does **not** store prompt or completion content (no message text, no user input, no model output) anywhere. This is a deliberate scope boundary: the gateway is built to observe *how* requests perform, not to retain *what* was said, which both limits the blast radius of a database compromise and sidesteps a large category of data-handling obligations that storing conversation content would introduce.
- **The `/public/metrics` endpoint** is built from the same `insights.Report` as the authenticated admin endpoint, with the same content scope — aggregate counts, rates, and timing, no project identifiers beyond what the operator explicitly configures via `PUBLIC_METRICS_PROJECT_ID`, no keys, no credentials, no request content. There is no separate "redaction" step because there was never anything sensitive in the underlying data to redact.
- **Structured logs** (`internal/applog`, `log/slog` with a JSON handler to stdout) log operational events (migration application, connection failures, per-request errors passed through `logger.Error`) — error values logged from provider failures include the upstream's own error message (useful for debugging a provider outage) but never a credential, since credentials never appear in any `error` value constructed in this codebase.

---

## 11. Dependency and Supply Chain Posture

- Dependencies are pinned via `go.mod`/`go.sum`; the module list is intentionally small (`pgx` for Postgres, `go-redis` for Redis, `prometheus/client_golang` for metrics, `godotenv` for local `.env` loading — four direct dependencies total at the time of writing).
- CI (`.github/workflows/ci.yml`) runs `go build`, `go vet`, a `gofmt` check, `go test`, and `go test -race` against real Postgres and Redis service containers on every push and pull request to `main`, plus a separate Docker image build job — so a change that breaks the build, introduces a data race, or fails formatting conventions cannot merge silently.
- The production container is built `FROM gcr.io/distroless/static-debian12:nonroot` (ARCHITECTURE.md §15.1) — no shell, no package manager, no OS-level attack surface beyond the statically linked Go binaries themselves and CA certificates; the process runs as the image's built-in non-root UID explicitly (rather than relying on an implicit default), so a future base-image change that silently dropped the non-root default would fail loudly instead of quietly running the gateway as root.

---

## 12. Known Limitations and Explicit Non-Goals

These are documented tradeoffs, not gaps the maintainers are unaware of:

- **No multi-admin / RBAC model** (§5.1) — one admin token, full access, by design for the project's current self-hosted/single-operator scope.
- **No audit log of administrative actions.** `/admin/*` writes (credential creation, key revocation, settings changes) are not themselves recorded anywhere beyond the operator's own infrastructure logs (reverse proxy access logs, container stdout). An operator who needs an administrative audit trail currently needs to provide one at the infrastructure layer.
- **No encryption-key rotation support.** Changing `ENCRYPTION_KEY` makes all previously stored provider credentials permanently undecryptable; there is no re-encryption migration path today.
- **No automatic gateway-key or admin-token rotation/expiry.** Keys are valid indefinitely once created, until explicitly revoked (gateway keys) or the environment variable is changed and the process restarted (admin token).
- **The admin token has no brute-force throttling of its own** (§4.3) — it relies entirely on being a sufficiently long, sufficiently random, operator-chosen secret (the `.env.example` recommends `openssl rand -hex 32`), not on a rate-limited login endpoint, since there is no login endpoint at all — the token is presented directly on every request.
- **Request-log persistence is best-effort, not durable** (ARCHITECTURE.md §13.2) — a process crash in the narrow window between responding to a client and the detached logging goroutine completing can lose that one log row. This is an accepted observability-only tradeoff; it never affects whether the client's actual request succeeded.

---

## 13. Reporting Context

This project does not currently operate as a hosted multi-tenant service run by its maintainers handling third-party production traffic at scale — it is designed to be self-hosted by an operator who controls their own deployment, their own `ADMIN_TOKEN` and `ENCRYPTION_KEY`, and their own provider credentials. Most security findings against the running code will therefore be of the form "this pattern would be dangerous in a given operator's deployment if X," which is exactly the kind of report described in §3 — please still report it privately rather than filing a public issue, even when the immediate blast radius is "only" a self-hosted instance rather than a shared service.
