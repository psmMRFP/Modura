# WhereToLive

Verifiable information for living abroad, relocation, and remote work. Built on the Modura modular monolith; **Modura Atlas** is the transformation codename.

The transformation is in progress. See [product rules](docs/atlas/README.md) and the [implementation roadmap](docs/atlas/roadmap.md) for scope and delivery status.

## Layout

- `backend/`: Go modular monolith
- `admin/`: React operations and moderation workspace
- `web/`: independent public React website foundation
- `api/`: authoritative OpenAPI contract shared by backend, web, and admin
- `docs/`: architecture, security, development, ADR, and research documents

Read `AGENTS.md` before making changes. The one-command verification entrypoint is `make verify`.

## Backend configuration

The backend fails fast unless `MODURA_DATABASE_URL` and a signing key of at
least 32 bytes in `MODURA_AUTH_SIGNING_KEY` are present. See
`backend/.env.example` for all authentication and HTTP-cookie settings. The
process does not load dotenv files automatically; inject secrets through the
process environment or the deployment secret mechanism.

Real PostgreSQL identity integration tests use `MODURA_TEST_DATABASE_URL` and
require a dedicated database whose name ends in `_test`. See
`docs/development/postgresql-testing.md` for the destructive-safety boundary.

## Database initialization

A missing named application database is automatically created on startup only
when PostgreSQL confirms it does not exist. The connection role and database
must both be dedicated; default `postgres` usage is prohibited. Creation uses
`template1` and requires CREATEDB on the dedicated role. Set
`MODURA_DATABASE_AUTO_CREATE=false` to disable creation after provisioning.
`go run ./cmd/modura-db-init` from `backend/` creates the empty database before
running migrations; it does not require authentication signing configuration.
See [deployment](docs/operations/deployment.md).
