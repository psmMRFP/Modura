# Phase 1 Remediation Register

Status: active
Last reviewed: 2026-09-04

This register records gaps found by the full Phase 1 implementation audit. It
separates work required before Phase 1 can be called complete from later work.
Mail delivery is excluded until a testable mail service is available; raw
invitation and recovery secrets must remain unavailable through public APIs.

## Repair order

Work is grouped by difficulty, but security and correctness take priority over
cosmetic work. Each group must leave the repository passing `make verify`.

### Now: small, high-value repairs

- [x] Return the canonical non-leaking Problem response for generated route
  parameter binding failures.
- [x] Add platform logout that revokes the current server-side session and
  clears platform cookies.
- [x] Keep one tenant-provisioning idempotency key stable across an uncertain
  request and its retries.
- [x] Make tenant and platform session restoration leave the loading state when
  the network request rejects.
- [x] Correct the Phase 1 plan status so implementation claims match evidence.

### Now: medium product-completion repairs

- [ ] Add the tenant user catalogue, detail projection, disable and unlock
  operations with tenant isolation, authorization, transactional audit, and an
  admin workflow.
- [x] Add self-profile read/update and expose password change in the admin UI.
- [x] Add safe platform tenant profile update.
- [x] Complete department update and position update/status management.
- [x] Complete tenant and platform dictionary/configuration write workflows,
  including optimistic-conflict handling.
- [x] Make route access and actions derive consistently from canonical
  permissions, including direct URL navigation.
- [x] Make custom-department policy editing usable and filter scope choices to
  combinations accepted by the backend.
- [ ] Add audit filtering, paging, state detail, and the documented platform
  audit query surface.

### Now: large verification and hardening repairs

- [ ] Add critical-path browser E2E for tenant login, platform tenant
  administration, organization, authorization, settings, and logout.
- [ ] Add authentication throttling/locking and privacy-conscious security
  events for login, refresh replay, password changes and session revocation.
- [ ] Resolve tenant and audit table ownership violations. Non-owning modules
  must use the owning module's public transaction-capable application API.
- [ ] Extend `make verify` and CI with OpenAPI validation, mandatory PostgreSQL
  integration execution, empty-database migration validation, generated diff,
  module/table ownership checks, and reference-source exclusion checks.
- [ ] Complete the production HTTP/configuration/observability baseline:
  request-body limits, header timeout, least-privilege CORS, redaction,
  OpenTelemetry-compatible traces and metrics, and bounded dependencies.
- [ ] Document and exercise migration recovery, deployment, backup/restore and
  release procedures.
- [ ] Add dependency vulnerability and license policy gates.

## Future repairs

These do not block Phase 1 unless promoted by a new product requirement:

- breaking OpenAPI change detection before the first stable external release;
- automatic audit-retention purge after retention ownership is decided;
- asymmetric access-token signing or external key management when deployment
  topology requires it;
- Redis, background jobs, durable notification delivery, MFA and enterprise
  identity capabilities only after their existing deferral is explicitly
  lifted;
- broad CRUD/page generation, report design, messaging, sharding and service
  extraction only with a concrete business or operational reason.

## Evidence from the 2026-09-04 audit

- `make verify` passed, including generation cleanliness, Go lint/unit tests and
  build, and admin format/lint/typecheck/component tests/build.
- All current PostgreSQL integration tests passed when
  `MODURA_TEST_DATABASE_URL` was loaded explicitly.
- The normal `make verify` invocation can skip those integration tests, so it is
  not yet complete release evidence.
- The admin suite currently contains four tests and no browser E2E suite.
