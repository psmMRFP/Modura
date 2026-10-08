.PHONY: verify verify-release generate generated-clean generate-go generate-admin backend-format backend-lint backend-test backend-test-integration backend-build admin-verify admin-e2e openapi-validate ownership-check reference-check deps-gate cache-clean

# Go and golangci-lint caches stay at their user-level defaults
# (~/.cache/go-build, ~/.cache/golangci-lint) so every build reuses the global
# caches. Only the transient build scratch directory is pinned into the
# workspace so an interrupted build cannot leave leaked work directories in
# the system temp directory.
$(shell mkdir -p .cache/gotmp)
export GOTMPDIR := $(CURDIR)/.cache/gotmp

verify: generated-clean openapi-validate backend-format backend-lint backend-test backend-build admin-verify web-verify ownership-check reference-check

# Release evidence additionally requires the mandatory PostgreSQL integration
# run plus the dependency vulnerability and license gates. Every integration
# package resets the modura schema and applies the full migration set from an
# empty database, so the integration run also validates migrations.
verify-release: verify backend-test-integration deps-gate

# Browser E2E drives the system Chromium over CDP (no downloads). It needs a
# modura_test database in MODURA_TEST_DATABASE_URL and admin/dist built.
admin-e2e:
	@test -n "$$MODURA_TEST_DATABASE_URL" || { echo "admin-e2e: MODURA_TEST_DATABASE_URL must point at a database named modura_test"; exit 1; }
	cd admin && npm run build
	node admin/e2e/critical-path.mjs

# Dependency gates require the pinned audit tools. Install with (Fish):
#   go install golang.org/x/vuln/cmd/govulncheck@v1.1.4
#   go install github.com/google/go-licenses@v1.6.0
deps-gate:
	@command -v govulncheck >/dev/null 2>&1 || { echo "deps-gate: govulncheck is missing; run: go install golang.org/x/vuln/cmd/govulncheck@v1.1.4"; exit 1; }
	@command -v go-licenses >/dev/null 2>&1 || { echo "deps-gate: go-licenses is missing; run: go install github.com/google/go-licenses@v1.6.0"; exit 1; }
	cd backend && govulncheck ./...
	cd backend && go-licenses check --disallowed_types=forbidden,unknown,restricted ./cmd/... ./internal/...

backend-test-integration:
	@test -n "$$MODURA_TEST_DATABASE_URL" || { echo "backend-test-integration: MODURA_TEST_DATABASE_URL must point at a database named *_test"; exit 1; }
	cd backend && go test -count=1 ./...

openapi-validate:
	python3 scripts/validate-openapi.py

ownership-check:
	python3 scripts/check-table-ownership.py

reference-check:
	python3 scripts/check-reference-exclusion.py

generated-clean:
	@before="$$(find backend/internal/api/generated backend/internal/platform/database/dbgen admin/src/api/generated web/src/api/generated backend/internal/modules/places/postgres/db -type f -print0 | sort -z | xargs -0 sha256sum)"; \
	$(MAKE) generate; \
	after="$$(find backend/internal/api/generated backend/internal/platform/database/dbgen admin/src/api/generated web/src/api/generated backend/internal/modules/places/postgres/db -type f -print0 | sort -z | xargs -0 sha256sum)"; \
	test "$$before" = "$$after"

generate: generate-go generate-admin generate-web

generate-go:
	@mkdir -p backend/internal/api/generated
	oapi-codegen -config api/oapi-codegen.yaml -o backend/internal/api/generated/openapi.gen.go api/openapi.yaml
	cd backend && sqlc generate

generate-admin:
	cd admin && npm run generate

backend-format:
	@test -z "$$(gofmt -l backend)"

backend-lint:
	cd backend && golangci-lint run ./...

backend-test:
	cd backend && go test ./...

backend-build:
	cd backend && go build ./cmd/...

admin-verify:
	cd admin && npm run format:check
	cd admin && npm run lint
	cd admin && npm run typecheck
	cd admin && npm run test
	cd admin && npm run build

# Removes workspace scratch state (.cache/gotmp, e2e browser profiles).
cache-clean:
	rm -rf .cache

.PHONY: web-verify
web-verify:
	@test -d web/node_modules || { echo "web-verify: dependencies missing; run manually: npm ci --prefix web"; exit 1; }
	cd web && npm run format:check
	cd web && npm run lint
	cd web && npm run typecheck
	cd web && npm run test
	cd web && npm run build

.PHONY: generate-web
generate-web:
	cd web && npm run generate

.PHONY: web-e2e
# Uses installed Chromium and deterministic contract fixtures; no downloads.
web-e2e:
	cd web && npm run build
	node web/e2e/public-places.mjs
