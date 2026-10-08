# WhereToLive Agent Contract

Version: 1.1.0
Effective date: 2026-10-08

This file is the executable entry point for every human or coding agent working in this repository. Read it before changing files.

The product is **WhereToLive**; **WhereToLive Atlas** is the transformation codename.
The implementation baseline is [docs/atlas](../docs/atlas/README.md). Code, database schema, configuration, and deployment identifiers use the
WhereToLive name; no legacy deployment compatibility is required.

## 1. Authority and language

Rules use these meanings:

- **MUST / MUST NOT**: mandatory. A deviation requires an accepted ADR that identifies the affected rule.
- **SHOULD / SHOULD NOT**: default. Deviations must be justified in the change description or an ADR when architectural.
- **MAY**: optional.

When rules conflict, apply this order:

1. Applicable legal, license, security, and user instructions.
2. This `AGENTS.md`.
3. Accepted ADRs that explicitly amend a rule in this file.
4. Referenced architecture/security/development documents.
5. `Project Constitution.md`.
6. Examples and research notes.

The Constitution defines intent. This file defines execution. Research documents are evidence, not implementation specifications.

## 2. Required workflow

Before implementation, an agent MUST:

1. Read the relevant sections of this file and linked documents.
2. Inspect the existing module, API contract, migrations, and tests before proposing new structure.
3. State the real problem solved by any new abstraction, dependency, interface, or deployable service.
4. Prefer an existing component or the standard library when it keeps the design clear.
5. Create an ADR before adopting a non-default foundational technology or crossing a boundary explicitly protected below.

During implementation, an agent MUST keep dependencies explicit, pass `context.Context` downward, preserve tenant and authorization invariants, and update contract/tests/docs in the same change.

Before completion, an agent MUST run `make verify`. Until that target exists, run every available equivalent check and clearly report missing checks. Generated files MUST be clean after regeneration.

An agent MUST NOT expand the requested scope merely to make the architecture look complete.

Downloaded dependencies and build caches MUST use the tool's normal persistent user-level cache or a clearly named workspace-local cache. They MUST NOT be placed in `/tmp`, `/var/tmp`, or an operating-system temporary equivalent. Ephemeral test outputs that are not dependency/build caches may use the system temporary directory when appropriate.

Because dependency network access is user-managed, an agent MUST NOT download or install packages, modules, toolchains, generators, browser binaries, containers, or other dependencies. When required dependencies are unavailable, the agent MUST stop at that boundary, give the user short commands valid for the active shell to install them manually, and wait for confirmation before continuing. The agent MAY inspect installed versions and use dependencies already present on the system.

The user's interactive shell is **Fish**. Every terminal command given to the user MUST be valid Fish syntax and MUST NOT use Bash-only assignment, expansion, chaining, or control-flow syntax. Commands SHOULD be short, directly copyable single lines; use separate commands for multi-step workflows unless atomic execution is necessary.

## 3. Repository and module structure

The default architecture is a modular monolith. Backend and admin frontend are separate projects in `backend/` and `admin/`. Business modules live under `backend/internal/modules/<module>` so Go's `internal` mechanism protects the application boundary. A module SHOULD expose a small application API and keep domain, persistence, and transport details private.

The intended top-level layout is:

```text
backend/cmd/                 executable composition roots
backend/internal/modules/    business modules
backend/internal/platform/   shared technical infrastructure
api/                 authoritative HTTP contract
proto/               gRPC contracts when independently justified
admin/               operations and moderation React workspace
web/                 public WhereToLive React website
agents/              versioned runtime research/feedback policies and prompts
docs/                architecture, security, ADRs, and research
skills/              project-specific agent skills when introduced
```

Module rules are defined in [module boundaries](../docs/architecture/module-boundaries.md). In summary:

- Every database table MUST have exactly one owning module.
- A module MUST NOT import another module's private packages, access its repository, or modify its tables.
- Cross-module behavior MUST use the owning module's public application API or a documented event.
- Module dependency cycles are forbidden.
- A module boundary is not a deployment boundary; local calls are the default.
- A new service or gRPC boundary requires an ADR and a present operational reason.

## 4. API contract

WhereToLive uses contract-first HTTP APIs with hybrid implementation:

- `api/openapi.yaml` MUST be the root and sole authoritative HTTP API contract. It MAY use relative `$ref` files under `api/`; those files are part of the same contract, not a second schema.
- Go handlers MUST implement the contract. Go types, annotations, and routes MUST NOT become a separate source of API truth.
- TypeScript clients and TanStack Query hooks MUST be generated from the contract into an explicitly marked generated directory.
- Generated code MUST NOT be edited manually.
- Generator names and versions MUST be pinned.
- CI MUST validate the OpenAPI document, run contract tests, regenerate artifacts, and fail if the working tree changes.
- Breaking-change detection MUST be added before the first stable external API release.
- gRPC contracts live only under `proto/` and MUST NOT duplicate an HTTP contract without an explicit boundary reason.

OpenAPI operation definitions SHOULD reference the same stable resource/action identifiers used by authorization.

## 5. Data access

The defaults are PostgreSQL, sqlc, and explicit SQL.

- Migrations MUST use `golang-migrate/migrate` compatible SQL files under one documented migrations directory.
- Application/use-case code owns transaction boundaries. Repositories MUST accept the transaction-capable database handle supplied by the application layer; repositories MUST NOT silently start nested business transactions.
- IDs MUST use UUIDv7 stored as PostgreSQL `uuid`. Externally supplied IDs MUST be validated at the boundary.
- Timestamps MUST use `timestamptz`, be written/read as UTC, and be represented by `time.Time`. Presentation timezone conversion belongs at the edge.
- `NULL` MUST mean semantically absent or unknown. Required fields MUST be `NOT NULL`; empty strings, zero IDs, and sentinel values such as `-1` MUST NOT represent missing relationships.
- Hard delete is the default. Soft delete MAY be used only when restore/retention is a real domain requirement and its uniqueness, querying, and purge semantics are documented.
- Optimistic locking is not enabled by default. Add a version column for aggregates with demonstrated concurrent-edit or lost-update risk.
- `sqlx` MAY be used for genuinely dynamic queries that sqlc cannot express clearly. The reason MUST be documented next to the repository or in an ADR if broadly adopted.
- GORM is non-default and requires an accepted ADR.
- Schema constraints MUST enforce important uniqueness and referential invariants; application checks alone are insufficient.

## 6. Tenant and authorization safety

The mandatory tenant rules are defined in [tenant invariants](../docs/security/tenant-invariants.md).

- Tenant identity MUST be resolved and verified server-side. A client header, path, query, or body value is never trusted by itself.
- Tenant-owned records, unique constraints, queries, relationships, jobs, and audit events MUST carry an explicit verified `TenantID`.
- Cross-tenant access is denied by default. Platform operations require a dedicated capability, explicit target tenant, reason, and audit event.
- Tenant-owned repositories MUST require tenant scope in their API; invisible global SQL/query injection is forbidden.
- Every cross-record assignment MUST verify same-tenant ownership.
- Tenant-isolation integration tests are mandatory.

Authorization uses stable subject/resource/action/tenant/scope concepts. Menus, routes, and buttons are UI projections and MUST NOT be the authority. HTTP and gRPC boundaries enforce authorization; frontend hiding is only presentation. Data scopes MUST be resolved explicitly and passed as typed input to the owning repository. Arbitrary SQL scope fragments are forbidden.

## 7. Authentication

Phase 1 authentication is defined in [authentication](../docs/security/authentication.md). It includes username or email plus password, Argon2id, short-lived access tokens, rotating refresh tokens backed by server-side sessions, reuse detection, and revocation after account/security changes.

MFA, SAML, enterprise SSO, passkeys, service accounts, social login, and operating a full OAuth authorization server are deferred unless explicitly requested.

Credentials, tokens, secrets, session identifiers, and sensitive personal data MUST NOT appear in logs, error details, URLs, source control, or generated artifacts.

WhereToLive public registration and email verification follow [ADR 0006](../docs/adr/0006-public-registration.md). Consumer accounts use the server-resolved community tenant under [ADR 0005](../docs/adr/0005-community-tenant-and-public-data.md); they receive no tenant administration authority. Public flows remain disabled until delivery and abuse controls are configured and tested.

## 8. Go and error-handling rules

- Dependencies MUST be passed through explicit constructors. Hidden service locators and global request containers are forbidden.
- Interfaces SHOULD be defined by consumers and only for a real substitution boundary. Do not add `IThing`, `ThingImpl`, base-service hierarchies, or interfaces with one speculative implementation.
- Prefer composition and small concrete types.
- Domain/application packages MUST NOT depend on Gin or HTTP/gRPC status types.
- Errors MUST preserve semantic identity and useful context using `errors.Is`, `errors.As`, and `%w` wrapping.
- HTTP/gRPC error mapping MUST occur at the transport boundary.
- Request cancellation and deadlines MUST propagate through `context.Context`; context MUST NOT be used as an untyped bag for optional business parameters.
- Cross-cutting behavior SHOULD use middleware, decorators, or explicit application calls, not reflection-heavy AOP-style mechanisms.

## 9. Frontend rules

- Apply these frontend rules to both `admin/` and `web/`; each has a synchronized committed package lock and uses `npm ci`.
- Organize `admin/src` and `web/src` by feature, with shared application composition and generated API directories.
- `admin/package-lock.json` MUST be committed and synchronized with `package.json`. Local clean installs and CI MUST use `npm ci`; dependency changes are the only time `npm install` or `npm install --package-lock-only` may update the lock, and they require user-managed network access.
- Use React, TypeScript, Vite, React Router, TanStack Query, Ant Design, native `fetch`, and the generated OpenAPI client by default.
- Server state belongs in TanStack Query, URL state in React Router, local UI state in React state, and standard forms in Ant Design Form.
- Do not hand-write API types already present in the contract, hand-fetch routine server state with `useEffect`, introduce Redux by default, scatter permission strings, or create layered Axios wrappers.
- Centralize authentication/error transport behavior and render controls from canonical permission data while relying on server enforcement.

## 10. Production baseline

Before Phase 1 is complete, the application MUST provide:

- graceful shutdown and bounded startup/shutdown;
- liveness and dependency-aware readiness endpoints;
- structured `slog` logging with request/trace correlation;
- OpenTelemetry-compatible HTTP, gRPC, database, metrics, and trace instrumentation where applicable;
- validated configuration with documented precedence and fail-fast behavior;
- secrets sourced outside committed configuration;
- forward database migrations with documented recovery/rollback procedure;
- server, request-header, request-body, idle, and outbound dependency timeouts;
- request-body size limits and input validation;
- an explicit least-privilege CORS policy and, when cookies are used, a CSRF policy;
- secure password/session storage and comprehensive sensitive-data redaction;
- consistent non-leaking security error responses;
- dependency vulnerability and license checks in CI.

Multi-region disaster recovery, Kubernetes operators, automatic scaling, WAF operation, distributed transactions, message middleware, sharding, and automated backup orchestration are deferred. Backup/restore requirements still MUST be documented before production use.

## 11. Testing and CI

The verification baseline is defined in [Definition of Done](../docs/development/definition-of-done.md).

Backend changes MUST include the applicable unit, PostgreSQL integration, HTTP/OpenAPI contract, authorization, and tenant-isolation tests. Frontend changes MUST pass formatting, lint, typecheck, build, critical component tests, and the minimal critical-path E2E suite.

CI MUST eventually enforce formatting, lint/static analysis, tests, builds, generated-code cleanliness, dependency/module architecture, OpenAPI validation, and migration application from an empty database. “Where practical” does not waive tenant, authorization, contract, or migration safety checks.

## 12. Audit and observability

Important writes MUST produce a business audit record containing actor, tenant, action, resource, resource ID, timestamp, result, and trace/request correlation. Where loss would make the business change unauditable, the audit strategy MUST be transactionally reliable.

Audit records, application logs, traces, and metrics are different products with different retention/access rules. Do not treat ordinary logs as the audit ledger. Do not implement auditing through invisible SQL interception or AOP annotations.

## 13. Reference-source and license policy

`_reference/**`, if present, is read-only research material. The legacy root `SpringBlade-boot/**`, while it exists, has the same status.

Production code MUST NOT import, build, link, execute, package, or depend on reference source. Reference trees SHOULD NOT be committed to the main repository; keeping one requires an ADR explaining need, ownership, update/removal policy, and license treatment.

Research findings belong under `docs/research/`. Before copying or modifying third-party code, record its repository, revision, original file, license, copyright, modifications, and NOTICE obligations. Design inspiration alone does not authorize copying. Research records are evidence, not implementation specifications; current business requirements are defined by [Atlas domain boundaries](../docs/atlas/domain.md).

## 14. ADRs, exceptions, and amendments

Use [docs/adr](../docs/adr/README.md) for durable decisions that change a default, introduce a foundational dependency, alter a security boundary, or create a deployable service. Routine implementation details do not require an ADR.

An exception MUST identify the exact rule, scope, reason, risks, compensating controls, owner, and expiry/review condition. Permanent exceptions amend this contract through an accepted ADR and a version/changelog update.

WhereToLive source, AI, privacy, and commercial-independence rules are defined in [Atlas governance](../docs/atlas/policies.md). Advertising MUST NOT influence coverage priority, Data Score, Resident Score, or Your Fit.

### Changelog

- 2026-10-08: Unify code, configuration, commands, contracts, and database identifiers under WhereToLive; user confirmed no existing deployments require legacy compatibility.

- 1.1.0 (2026-10-08): Adopt WhereToLive product direction, public web rules, community boundary and scoped public registration decisions.

- 1.0.0 (2026-08-28): Initial executable contract derived from the Project Constitution and SpringBlade business research.
