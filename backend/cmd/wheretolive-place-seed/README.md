# Test place fixtures

This command imports the embedded `test-places.json` through the existing platform
HTTP API. It uses generated OpenAPI request/response models, real administrator
login, session cookies, CSRF, and the places module's transactional audit. It has
no database connection and does not create an administrator.

The embedded fixtures contain **40 test places: 10 countries and 30 cities**.
They were hand-written for development and regression tests, not downloaded from
a geographic source or selected using demand evidence. The user explicitly
classified these as test data. They do not count toward the production candidate
pool or MVP acceptance. Use the command only on a development/test server.
Production geography will come from a future admin import or Agent ingestion
with source attribution, external identifiers, acquisition time and review.

Unknown timezone, currency, coordinates and languages remain null or empty.
Cities attach directly to their country, as allowed by the existing hierarchy.

All new records are **drafts with coverage 0**. The import never publishes,
raises coverage, scores suitability, or modifies existing records. Existing slugs
must agree on type, country and parent; existing countries with a different slug
are resolved by their unique ISO country code and reused without renaming; a mismatch anywhere aborts the preflight
before import writes. Existing metadata, aliases, coverage and publication are
preserved. Exact slugs are resolved across paginated prefix-search results.

Run from `backend/` against a running server. Fish commands:

```fish
set -gx WHERETOLIVE_SEED_USERNAME 'your-platform-username'
read --silent --prompt-str 'Platform password: ' WHERETOLIVE_SEED_PASSWORD
set -gx WHERETOLIVE_SEED_PASSWORD $WHERETOLIVE_SEED_PASSWORD
go run ./cmd/wheretolive-place-seed --base-url http://127.0.0.1:8080
go run ./cmd/wheretolive-place-seed --base-url http://127.0.0.1:8080 --apply --reason 'Load development place fixtures'
set -e WHERETOLIVE_SEED_PASSWORD
```

The default previews missing drafts without creating places. `--apply` requires
an audit reason. HTTPS is required away from loopback; redirects are rejected.
Credentials, tokens and server error bodies are never printed. The command has a
four-minute deadline and revokes its dedicated session on completion or failure.
Other administrator sessions remain independent.

Each creation is a separate API transaction. If a connection fails partway
through, already-created drafts remain: rerun to resume. A concurrent matching
creation is preserved; a concurrent conflicting geography stops the run. There
is no automatic rollback, deletion or modification of existing places. An
ambiguous successful write after a lost response is resolved on the next run.

The candidate JSON is embedded in the binary, so it does not depend on the working
directory after building. Future revisions must validate unique stable slugs,
ISO country codes, parent-first ordering and same-country relationships. Changing
a slug is a new candidate, not a rename migration. The command creates only the
countries and cities listed here and does not assess editorial demand.

Tests cover preview, resume, idempotency, metadata preservation, conflicting
geography, concurrent creation, CSRF/session credentials, logout and redirect
rejection. `make admin-e2e` also exercises the command against the real backend
and PostgreSQL and checks the resulting admin catalogue and public draft hiding.
