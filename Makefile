# Panvex control-plane + agent — developer Makefile.
# Mirrors CI checks so pre-push and local runs match GitHub Actions.

.PHONY: help test test-fast test-pkg lint vuln check build build-embed \
        sqlc tidy fmt clean install-tools bench all

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

# P2-PERF-05: microbenchmarks used as the Phase 2 performance baseline.
# Output is compared against the numbers in docs/benchmarks/phase2-baseline.md
# to judge the impact of Phase 3 changes (notably PERF-06 bulk insert).
bench:
	go test -bench=. -benchtime=3s -run=^$$ -count=1 -timeout=10m \
	    ./internal/controlplane/server \
	    ./internal/controlplane/jobs
