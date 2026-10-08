# WhereToLive public web

Independent React/Vite public website using the same pinned dependencies as `admin/`. Five locale routes provide a home page, anonymous place search, paginated results and stable place details. The generated TanStack Query client reads `/api/public/places` and `/api/public/places/{slug}` from the authoritative root OpenAPI contract.

There is no production place seed in this increment. An empty catalogue renders an explicit empty state. Visa, tax, costs, sources, accounts, scores and comment translations are still future work; neither fixtures nor synthetic scores are presented as production data.

Run from the repository root in Fish, with user-managed dependency access:

```fish
npm ci --prefix web
npm run dev --prefix web
```

The web port is 5174, separate from admin on 5173. `/api` is proxied to the existing backend on 8080. Production hosting must route locale paths to `index.html` while preserving `/api` routing. The site remains `noindex` until real public content is ready.

`make web-verify` checks formatting, lint, types, component tests and build. `make generate-web` regenerates the public-only client with the pinned Orval version. Generation cleanliness is included in `make verify`.

`make web-e2e` uses installed Chromium and deterministic API contract fixtures. It checks five languages, search submission, locale-preserving stable URLs, missing metadata, empty results, errors and invalid pagination. It does not prove real PostgreSQL behavior. Set `MODURA_E2E_CHROMIUM` if the browser executable is elsewhere.

Real database tests live under `backend/internal/modules/places/postgres` and require `MODURA_TEST_DATABASE_URL` targeting a dedicated database ending in `_test`; they reset its `modura` schema. See the [root README](../README.md#验证) for verification commands.
