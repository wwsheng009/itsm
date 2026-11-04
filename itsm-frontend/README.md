# ITSM Frontend (Vite + React)

## Quick Start

1. Node 22+
2. Install deps:

```
npm install
```

3. Configure env:

```
cp .env.example .env.local
# Local direct-backend development may set VITE_API_URL=http://localhost:8090.
# Production should leave it empty so /api/v1/* stays same-origin behind Nginx.
```

4. Dev:

```
npm run dev
```

## Testing

- Type check: `npm run type-check`
- Unit/RTL: `npm run test:unit`
- Integration: `npm run test:integration`
- E2E/Playwright: `npm run test:e2e`

## Lint/Format

```
npm run lint
npm run lint:check
```

## Production build

```bash
npm run build
test -f dist/index.html
```

`npm run preview` serves the built `dist/` locally; production serves the same
static assets through Nginx (SPA fallback to `index.html`).

The browser-facing API base is empty by default. Requests already include
`/api/v1`, while the Vite dev server proxy uses `ITSM_BACKEND_URL`.

## Embedding URL parameters

When the web UI is embedded by the desktop client, the following optional query
parameter controls the **initial** sidebar state:

| URL | Behavior |
|---|---|
| `?sidebar=collapsed` | Start with the left sidebar collapsed |
| `?sidebar=expanded` | Start with the left sidebar expanded |
| no parameter | Keep the existing default (expanded on desktop) |

The parameter is read only once when the layout mounts. The user can still
expand/collapse the sidebar with the Header button afterwards. Aliases such as
`1/0`, `true/false`, `on/off`, `open/closed` are accepted.
