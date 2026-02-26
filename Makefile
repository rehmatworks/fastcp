# FastCP Makefile

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS := -ldflags "-s -w -X main.Version=$(VERSION) -X main.BuildTime=$(BUILD_TIME)"

.PHONY: all build build-linux clean dev install

all: build

# Build for current platform
build:
	go build $(LDFLAGS) -o bin/fastcp ./cmd/fastcp
	go build $(LDFLAGS) -o bin/fastcp-agent ./cmd/fastcp-agent

# Build for Linux (production) - requires Docker due to CGO/PAM
build-linux:
	mkdir -p dist
	docker run --rm -v $(PWD):/app -w /app golang:1.22 bash -c '\
		apt-get update -qq && apt-get install -y -qq libpam0g-dev > /dev/null && \
		go build -ldflags "-s -w" -o dist/fastcp-linux-amd64 ./cmd/fastcp && \
		go build -ldflags "-s -w" -o dist/fastcp-agent-linux-amd64 ./cmd/fastcp-agent && \
		echo "Built AMD64 binaries"'
	@echo "Built binaries in dist/"

# Build ARM64 (optional, requires ARM64 Docker or cross-compile setup)
build-linux-arm64:
	mkdir -p dist
	docker run --rm -v $(PWD):/app -w /app --platform linux/arm64 golang:1.22 bash -c '\
		apt-get update -qq && apt-get install -y -qq libpam0g-dev > /dev/null && \
		go build -ldflags "-s -w" -o dist/fastcp-linux-arm64 ./cmd/fastcp && \
		go build -ldflags "-s -w" -o dist/fastcp-agent-linux-arm64 ./cmd/fastcp-agent && \
		echo "Built ARM64 binaries"'

# Create release tarball
release: build-linux
	mkdir -p dist/fastcp-$(VERSION)
	cp dist/fastcp-linux-amd64 dist/fastcp-$(VERSION)/fastcp
	cp dist/fastcp-agent-linux-amd64 dist/fastcp-$(VERSION)/fastcp-agent
	cp scripts/install.sh dist/fastcp-$(VERSION)/
	cp -r systemd dist/fastcp-$(VERSION)/
	tar -czf dist/fastcp-$(VERSION)-linux-amd64.tar.gz -C dist fastcp-$(VERSION)
	@echo "Created dist/fastcp-$(VERSION)-linux-amd64.tar.gz"

# Development
dev:
	docker compose up -d dev
	docker exec -it fastcp-dev bash /app/docker/dev-run.sh

# Clean
clean:
	rm -rf bin/ dist/

# Install locally (for development)
install: build
	sudo cp bin/fastcp /usr/local/bin/
	sudo cp bin/fastcp-agent /usr/local/bin/
