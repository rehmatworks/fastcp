.PHONY: all build build-frontend build-backend run dev clean install-deps docker-dev docker-prod docker-build

# Variables
BINARY_NAME=fastcp
GO_BUILD_FLAGS=-ldflags="-s -w"

all: build

# Install dependencies
install-deps:
	@echo "Installing Go dependencies..."
	cd . && go mod download
	@echo "Installing Node dependencies..."
	cd web && npm install

# Build frontend
build-frontend:
	@echo "Building frontend..."
	cd web && npm run build

# Build backend
build-backend:
	@echo "Building backend..."
	CGO_ENABLED=0 go build $(GO_BUILD_FLAGS) -o bin/$(BINARY_NAME) ./cmd/fastcp

# Build both
build: build-frontend build-backend
	@echo "Build complete! Binary at bin/$(BINARY_NAME)"

# Run in development mode
dev:
	@echo "Starting FastCP in development mode..."
	@echo "Data directory: ./.fastcp/"
	@echo "Admin panel: http://localhost:8080"
	@echo "Proxy: http://localhost:8000"
	@echo ""
	FASTCP_DEV=1 go run ./cmd/fastcp

# Run frontend dev server (with hot reload)
dev-frontend:
	cd web && npm run dev

# Run production binary
run: build
	./bin/$(BINARY_NAME)

# Clean build artifacts
clean:
	rm -rf bin/
	rm -rf web/node_modules/
	rm -rf internal/static/dist/*.js internal/static/dist/*.css internal/static/dist/*.html internal/static/dist/assets/

# Format code
fmt:
	go fmt ./...
	cd web && npm run lint --fix 2>/dev/null || true

# Run tests
test:
	go test -v ./...

# Build for Linux (cross-compile from any OS)
build-linux: build-frontend
	@echo "Building for Linux x86_64..."
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build $(GO_BUILD_FLAGS) -o bin/$(BINARY_NAME)-linux-x86_64 ./cmd/fastcp
	@echo "Building for Linux ARM64..."
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build $(GO_BUILD_FLAGS) -o bin/$(BINARY_NAME)-linux-aarch64 ./cmd/fastcp
	@echo "Linux builds complete!"

# Create release
release: clean build-linux
	@echo "Creating release archives..."
	cd bin && sha256sum $(BINARY_NAME)-* > checksums.txt
	@echo "Release files ready in bin/"

# ==================== Docker ====================

# Run in Docker (development with hot reload)
docker-dev:
	@echo "Starting FastCP in Docker (development mode)..."
	docker compose up dev

# Run in Docker (production test)
docker-prod:
	@echo "Starting FastCP in Docker (production mode)..."
	docker compose up prod --build

# Build Docker image
docker-build:
	@echo "Building Docker image..."
	docker build -t fastcp:latest .

# Stop Docker containers
docker-down:
	docker compose down

# View Docker logs
docker-logs:
	docker compose logs -f

# Help
help:
	@echo "FastCP Makefile"
	@echo ""
	@echo "Usage:"
	@echo "  make install-deps   Install all dependencies"
	@echo "  make build          Build frontend and backend"
	@echo "  make dev            Run in development mode (uses ./.fastcp/)"
	@echo "  make dev-frontend   Run frontend dev server with hot reload"
	@echo "  make run            Build and run production binary"
	@echo "  make clean          Clean build artifacts"
	@echo "  make test           Run tests"
	@echo "  make build-linux    Cross-compile for Linux (x86_64 + ARM64)"
	@echo "  make release        Build and prepare release files"
	@echo ""
	@echo "Docker:"
	@echo "  make docker-dev     Run in Docker with hot reload"
	@echo "  make docker-prod    Run production build in Docker"
	@echo "  make docker-build   Build Docker image"
	@echo "  make docker-down    Stop Docker containers"
	@echo ""
	@echo "Environment Variables:"
	@echo "  FASTCP_DEV=1        Enable development mode (local directories)"
	@echo "  FASTCP_DATA_DIR     Override data directory"
	@echo "  FASTCP_SITES_DIR    Override sites directory"
	@echo "  FASTCP_LOG_DIR      Override log directory"
	@echo "  FASTCP_CONFIG_DIR   Override config directory"
	@echo "  FASTCP_BINARY       Override FrankenPHP binary path"
	@echo "  FASTCP_PORT         Override proxy HTTP port (default: 80/8000)"
	@echo "  FASTCP_SSL_PORT     Override proxy HTTPS port (default: 443/8443)"
	@echo "  FASTCP_LISTEN       Override admin panel listen address"

