<div align="center">

# WhereToLive

### Find your next place to live, starting with information you can trust.

An open platform for long-term living, relocation, study abroad, and remote work.

[![CI](https://github.com/psmMRFP/WhereToLive/actions/workflows/ci.yml/badge.svg)](https://github.com/psmMRFP/WhereToLive/actions/workflows/ci.yml)
[![License](https://img.shields.io/badge/License-AGPL%203.0-blue.svg)](LICENSE)
[![Stage](https://img.shields.io/badge/Stage-Early%20Development-orange.svg)](#current-status)

**English** · [简体中文](README.zh-CN.md) · [Deutsch](README.de.md) · [Français](README.fr.md) · [Español](README.es.md)

[Why WhereToLive?](#why-wheretolive) · [Current status](#current-status) · [Local development](#local-development) · [Contributing](#contributing)

</div>

---

## Why WhereToLive?

Choosing where to live takes more than a travel guide. Visa options, tax rules, rent, everyday costs, and the experiences of people who have lived there should be accessible in one place.

WhereToLive brings together **verifiable facts, resident experiences, and personal preferences**, with clear sources, effective dates, and change history.

> How a place performs objectively and how well it suits you are two different questions.

| System             | Question                                                  | Planned scale                     |
| ------------------ | --------------------------------------------------------- | --------------------------------- |
| **Data Score**     | How does the place perform on objective measures?         | 0–100, with individual dimensions |
| **Resident Score** | What do verified residents think?                         | 1–10, using Bayesian shrinkage    |
| **Your Fit**       | Does it match your budget, language needs, and lifestyle? | Based on your own weights         |

These systems remain independent and are not implemented yet. **Modura Atlas** is the transformation codename; **WhereToLive** is the product name.

## Current status

The project is in early development, building on the existing Modura framework. The public place directory and operational foundation work; there is no production place dataset yet.

| Implemented             | Capabilities                                                                                                                 |
| ----------------------- | ---------------------------------------------------------------------------------------------------------------------------- |
| Public website          | Anonymous place search, pagination, and detail pages                                                                         |
| Multilingual UI         | English, 简体中文, Deutsch, Français, Español                                                                                |
| Geography               | Countries, regions, cities, districts, islands, stable slugs, and multilingual aliases                                       |
| Place administration    | Draft creation, editing, publication, withdrawal, version conflict protection, and transactional audit                       |
| API contract            | Shared OpenAPI for Go and both frontends, with generated types and query clients                                             |
| Database initialization | Automatic creation of a missing dedicated database; the default `postgres` role and database are prohibited                  |
| Consumer accounts       | Registration, email verification, login, session restoration and recovery; disabled until deployment controls are configured |

**Next:** sources, evidence, and fact versions → Research Agent → visas, tax, and living costs → unified feedback → residency verification and reviews → personal fit.

Planned review translation is available when the reader's language differs from the original review. Users can enable automatic translation and always view the original.

Information and scoring dimensions are planned as admin-managed definitions, with separate visibility and scoring switches. Web3 / cryptocurrency friendliness and foreign-exchange / capital controls are candidate dimensions. This configuration capability is not implemented yet.

## Product principles

- **Evidence first:** AI assists with research, extraction, translation, and comparison; it is not an information source.
- **Traceable history:** time-sensitive facts retain versions, sources, effective dates, and verification timestamps.
- **Separate information streams:** official facts, community experiences, and current alerts are clearly distinguished.
- **Minimal personal data:** residency documents stay private, are not sent to third-party LLMs by default, and are deleted after review while necessary verification metadata is retained.
- **Commercial independence:** advertising cannot affect coverage priority, Data Score, Resident Score, or Your Fit.

These are design commitments; the corresponding business features are being implemented incrementally.

## Technology and layout

**Go modular monolith + PostgreSQL + React.** Business modules use local calls. Initial search uses PostgreSQL without a separate search cluster or a microservice architecture.

```text
WhereToLive/
├── backend/     Go API, business modules, SQL queries, and migrations
├── web/         Public React website
├── admin/       React operations and moderation interface
├── api/         Authoritative HTTP contract and generation configuration
├── scripts/     Contract, ownership, and source-boundary checks
└── .github/     CI workflows
```

The frontends use React, Vite, React Router, TanStack Query, and Ant Design. Database queries use sqlc; HTTP types and clients are generated from OpenAPI.

## Local development

### 1. Prepare your environment

Repository configuration is authoritative: currently **Go 1.27, Node.js ≥ 26, and npm ≥ 12**. Full verification also requires installed `oapi-codegen`, `sqlc`, `golangci-lint`, Python 3, and Make. Pinned tool versions are in the [CI configuration](.github/workflows/ci.yml), which uses PostgreSQL 17.

Install locked frontend dependencies from the repository root:

```fish
npm ci --prefix admin
npm ci --prefix web
```

### 2. Configure the backend

Use [backend/.env.example](backend/.env.example) as a reference. Supply configuration through the process environment or deployment secret mechanism. The application does not automatically load `.env` files.

| Variable                      | Purpose                                                                       |
| ----------------------------- | ----------------------------------------------------------------------------- |
| `MODURA_DATABASE_URL`         | Connection URL for a dedicated PostgreSQL role and named database; required   |
| `MODURA_AUTH_SIGNING_KEY`     | Signing key of at least 32 bytes; required                                    |
| `MODURA_AUTH_COOKIE_SECURE`   | Set to `false` for local HTTP development; keep `true` with TLS in production |
| `MODURA_DATABASE_AUTO_CREATE` | Defaults to `true`; may be set to `false` after provisioning                  |

Database creation is attempted only when PostgreSQL explicitly reports that the target does not exist. It connects through `template1`, creates from `template0`, and requires `CREATEDB` on the dedicated role. **Creating the database does not apply schema migrations.**

Initialize an empty database without authentication signing configuration:

```fish
cd backend
go run ./cmd/modura-db-init
```

Then apply the [database migrations](backend/internal/platform/database/migrations) in order using a `golang-migrate` compatible tool. Start the API from `backend/`:

```fish
go run ./cmd/modura
```

### 3. Start the frontends

Run each command in a separate terminal at the repository root:

```fish
npm run dev --prefix web
```

```fish
npm run dev --prefix admin
```

| Service         | Local address           |
| --------------- | ----------------------- |
| Public website  | `http://localhost:5174` |
| Admin interface | `http://localhost:5173` |
| Backend API     | `http://localhost:8080` |

Development servers proxy `/api` to the backend. Public place URLs use `/{locale}/places/{slug}`; switching language preserves the place slug. Without published places, the website shows an empty directory. The public site currently remains `noindex`.

## Verification

From the repository root:

```fish
make verify
```

Checks include generation consistency, OpenAPI validation, Go formatting and static analysis, unit tests, both frontends' formatting / lint / types / component tests / builds, and table ownership and source-boundary checks.

Real PostgreSQL integration tests require `MODURA_TEST_DATABASE_URL` pointing to a dedicated database whose name ends in `_test`. They reset its `modura` schema. Never point these tests at a business database.

```fish
make backend-test-integration
```

Public browser checks use installed Chromium and deterministic API fixtures:

```fish
make web-e2e
```

They verify page interactions and do not replace database integration tests. Admin E2E uses `make admin-e2e` and requires a database named `modura_test`. Full release verification uses `make verify-release`, including dependency vulnerability and license checks.

## Contributing

Read [AGENTS.md](AGENTS.md), then inspect existing modules and the [OpenAPI contract](api/openapi.yaml). Update the HTTP contract, implementation, generated clients, and tests together. Do not manually edit generated files.

Architecture and product working documents currently live outside the repository; historical documentation links may be unavailable. Keep local secrets, environment configuration, residency documents, and private user data out of commits.

Report problems and suggestions through [Issues](https://github.com/psmMRFP/WhereToLive/issues), or submit a pull request. Keep all five README versions aligned when updating project status or development instructions.

## License

[GNU AGPL v3.0](LICENSE) (`AGPL-3.0-only`). Third-party dependencies and data sources retain their respective licenses.
