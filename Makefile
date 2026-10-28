# FastCP Makefile

VERSION    := $(shell cat VERSION)
BUILD_TIME := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
PKG        := github.com/rehmatworks/fastcp/internal/version
LDFLAGS    := -s -w -X $(PKG).Version=$(VERSION) -X $(PKG).BuildTime=$(BUILD_TIME)
GO_IMAGE   := golang:1.27-bookworm

.PHONY: all build build-linux build-linux-arm64 ui ui-check test vet check release-sums clean dev docker install bump

all: build

# Build for the current platform.
build:
	go build -trimpath -ldflags "$(LDFLAGS)" -o bin/fastcp ./cmd/fastcp
	go build -trimpath -ldflags "$(LDFLAGS)" -o bin/fastcp-agent ./cmd/fastcp-agent

# Build Linux binaries in Docker (CGO is needed for PAM and SQLite).
build-linux:
	mkdir -p dist
	docker run --rm -v $(PWD):/app -w /app $(GO_IMAGE) bash -c '\
		apt-get update -qq && apt-get install -y -qq libpam0g-dev > /dev/null && \
		go build -trimpath -ldflags "$(LDFLAGS)" -o dist/fastcp-linux-amd64 ./cmd/fastcp && \
		go build -trimpath -ldflags "$(LDFLAGS)" -o dist/fastcp-agent-linux-amd64 ./cmd/fastcp-agent'
	@echo "Built dist/fastcp-linux-amd64 and dist/fastcp-agent-linux-amd64"

build-linux-arm64:
	mkdir -p dist
	docker run --rm --platform linux/arm64 -v $(PWD):/app -w /app $(GO_IMAGE) bash -c '\
		apt-get update -qq && apt-get install -y -qq libpam0g-dev > /dev/null && \
		go build -trimpath -ldflags "$(LDFLAGS)" -o dist/fastcp-linux-arm64 ./cmd/fastcp && \
		go build -trimpath -ldflags "$(LDFLAGS)" -o dist/fastcp-agent-linux-arm64 ./cmd/fastcp-agent'
	@echo "Built dist/fastcp-linux-arm64 and dist/fastcp-agent-linux-arm64"

# Build the web UI into cmd/fastcp/ui/dist (committed so plain `go build` works).
ui:
	cd web && npm ci && npm run build

# Type-check and unit-test the web UI.
ui-check:
	cd web && npm run check && npm test

vet:
	go vet ./...

test:
	go test ./...

check: vet test ui-check

# Write SHA256SUMS for the release binaries in dist/ and sign it.
# Requires FASTCP_SIGNING_KEY in the environment.
release-sums:
	cd dist && sha256sum fastcp-linux-* fastcp-agent-linux-* > SHA256SUMS
	go run ./tools/releasekey sign dist/SHA256SUMS

# Set the version everywhere: make bump V=3.0.1
bump:
	@test -n "$(V)" || (echo "usage: make bump V=x.y.z" && exit 1)
	@echo "$(V)" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+$$' || (echo "V must look like x.y.z" && exit 1)
	echo "$(V)" > VERSION
	sed -i.bak -E 's/^FASTCP_VERSION_DEFAULT="v[0-9]+\.[0-9]+\.[0-9]+"/FASTCP_VERSION_DEFAULT="v$(V)"/' scripts/install.sh && rm -f scripts/install.sh.bak
	cd web && npm version --no-git-tag-version --allow-same-version $(V) > /dev/null
	@echo "Version set to $(V). Rebuild the UI with 'make ui' before committing."

# Run the panel against the in-memory development agent (macOS only; nothing
# on the machine is changed). Open http://127.0.0.1:2050 and sign in as
# fastcp / fastcp-dev, or run `cd web && npm run dev` for hot reload on
# http://127.0.0.1:5173.
DEV_DIR := /tmp/fastcp-dev
dev: build
	mkdir -p $(DEV_DIR)/data
	test -f $(DEV_DIR)/data/default_admin || echo fastcp > $(DEV_DIR)/data/default_admin
	FASTCP_DEV=1 bin/fastcp-agent --socket $(DEV_DIR)/agent.sock & \
	trap 'kill $$!' EXIT; sleep 1; \
	FASTCP_DEV=1 bin/fastcp --data-dir $(DEV_DIR)/data --agent-socket $(DEV_DIR)/agent.sock --no-tls

# Install FastCP in a container with the real installer (no systemd).
docker:
	docker compose up --build

clean:
	rm -rf bin/ dist/

install: build
	sudo cp bin/fastcp /usr/local/bin/
	sudo cp bin/fastcp-agent /usr/local/bin/
