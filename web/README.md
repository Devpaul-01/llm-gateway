# Gateway Console

The admin/observability dashboard for the LLM Gateway. React + TypeScript +
Vite, talking directly to the gateway's `/admin/*` API. See
[`../docs/FRONTEND_ARCHITECTURE.md`](../docs/FRONTEND_ARCHITECTURE.md) for
the full architecture, including why this must be served same-origin with
the gateway in production (the backend's `/admin/*` routes have no CORS).

## Development

```bash
# from the repo root, run the gateway + its dependencies:
docker compose -f docker-compose.yml -f docker-compose.demo.yml up --build

# in another terminal:
cd web
npm install
npm run dev
```

The dev server proxies `/admin` and `/public` to `http://localhost:8080`
(override with `GATEWAY_URL`), so the browser only ever talks to one origin.
You'll need an admin token (`ADMIN_TOKEN` in the gateway's `.env`) to use the
app once it loads.

## Scripts

- `npm run dev` — dev server
- `npm run build` — type-check (`tsc -b`) + production build
- `npm run test` — run tests once
- `npm run test:watch` — watch mode
- `npm run lint` — oxlint
