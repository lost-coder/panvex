# Panvex control-plane + agent — developer Makefile.
# Mirrors CI checks so pre-push and local runs match GitHub Actions.

.PHONY: help test test-fast test-pkg lint vuln check build build-embed \
        sqlc tidy fmt clean install-tools bench gen-settings all \
        gen-openapi-go gen-openapi-ts gen-openapi verify-openapi

# Default target: list available commands.
help:
	@echo "Panvex Makefile targets:"
	@echo ""
	@echo "  make test          Run full go test -race suite (same as CI)"
	@echo "  make test-fast     Run go test without race detector (faster)"
	@echo "  make test-pkg PKG=./internal/controlplane/auth"
	@echo "                     Run tests for a single package"
	@echo "  make lint          Run golangci-lint"
	@echo "  make vuln          Run govulncheck against all packages"
	@echo "  make check         lint + test + vuln (full local CI equivalent)"
	@echo "  make build         Build control-plane and agent binaries"
	@echo "  make build-embed   Build frontend into cmd/control-plane/.embedded-ui"
	@echo "                     then build control-plane with embeddedui tag"
	@echo "  make sqlc          Regenerate internal/dbsqlc from db/queries"
	@echo "  make tidy          go mod tidy"
	@echo "  make fmt           gofmt -w ."
	@echo "  make clean         Remove build artifacts"
	@echo "  make install-tools Install govulncheck, golangci-lint, sqlc"
	@echo "  make bench         Run P2-PERF-05 microbenchmarks"
	@echo "                     (batch writer + event hub + jobs; 3s per bench)"

test:
	go test -race -count=1 ./...

test-fast:
	go test -count=1 ./...

test-pkg:
	@if [ -z "$(PKG)" ]; then echo "usage: make test-pkg PKG=./path/to/pkg"; exit 1; fi
	go test -race -count=1 -v $(PKG)

lint:
	golangci-lint run ./...

vuln:
	govulncheck ./...

check: lint test vuln

build:
	go build -o bin/panvex-control-plane ./cmd/control-plane
	go build -o bin/panvex-agent ./cmd/agent

build-embed:
	cd web && npm ci && npm run build:embed
	go build -tags embeddedui -o bin/panvex-control-plane ./cmd/control-plane

sqlc:
	sqlc generate

tidy:
	go mod tidy

fmt:
	gofmt -w .

clean:
	rm -rf bin/ cmd/control-plane/.embedded-ui

install-tools:
	go install golang.org/x/vuln/cmd/govulncheck@latest
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest
	go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest
	go install github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@latest
	cd web && npm install -D openapi-typescript

# gen-settings runs the registry codegen.
gen-settings:
	go generate ./internal/controlplane/settings/...
	@echo "wrote internal/controlplane/settings/gen/schema.json"
	@echo "wrote docs/settings/reference.md"
	@echo "wrote docs/settings/example.config.toml"

# OpenAPI codegen pipeline (Wave 3.3 — see
# docs/superpowers/plans/2026-05-08-api-codegen.md). The spec at
# openapi/panvex.yaml is the single source of truth; both Go server
# stubs and TypeScript types are regenerated from it.
#
# Generated outputs are committed (matches sqlc / gen-settings) so CI
# does not need the codegen tools on the hot path; verify-openapi
# regenerates and asserts no diff.
gen-openapi-go:
	@command -v oapi-codegen >/dev/null || { echo "oapi-codegen not installed (make install-tools)"; exit 1; }
	oapi-codegen -config openapi/config.yaml openapi/panvex.yaml

gen-openapi-ts:
	@command -v npx >/dev/null || { echo "npx not installed"; exit 1; }
	cd web && npx openapi-typescript ../openapi/panvex.yaml -o src/shared/api/openapi.gen.ts

gen-openapi: gen-openapi-go gen-openapi-ts

verify-openapi: gen-openapi
	@git diff --exit-code -- openapi/ web/src/shared/api/openapi.gen.ts \
	    || { echo ""; echo "OpenAPI generated files are out of date — run 'make gen-openapi'."; exit 1; }

# Control-plane microbenchmarks (batch writer, event bus, bulk insert).
bench:
	go test -bench=. -benchtime=3s -run=^$$ -count=1 -timeout=10m \
	    ./internal/controlplane/server \
	    ./internal/controlplane/jobs
