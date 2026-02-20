# FastCP Development Dockerfile
# Multi-stage build for both development and production

# =============================================================================
# Stage 1: Builder
# =============================================================================
FROM golang:1.22-bookworm AS builder

WORKDIR /build

# Install build dependencies
RUN apt-get update && apt-get install -y \
    libpam0g-dev \
    && rm -rf /var/lib/apt/lists/*

# Copy go mod files first for caching
COPY go.mod go.sum* ./
RUN go mod download

# Copy source code
COPY . .

# Build binaries
RUN CGO_ENABLED=1 go build -o /build/bin/fastcp ./cmd/fastcp
RUN CGO_ENABLED=1 go build -o /build/bin/fastcp-agent ./cmd/fastcp-agent

# =============================================================================
# Stage 2: Development Image
# =============================================================================
FROM ubuntu:24.04 AS development

ARG TARGETARCH

# Prevent interactive prompts
ENV DEBIAN_FRONTEND=noninteractive

# Install dependencies
RUN apt-get update && apt-get install -y \
    curl \
    wget \
    git \
    acl \
    mysql-server \
    supervisor \
    libpam0g \
    build-essential \
    libpam0g-dev \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# Install Go 1.22 based on architecture
RUN ARCH=${TARGETARCH:-amd64} && \
    if [ "$ARCH" = "arm64" ]; then GO_ARCH="arm64"; else GO_ARCH="amd64"; fi && \
    curl -fsSL "https://go.dev/dl/go1.22.5.linux-${GO_ARCH}.tar.gz" | tar -C /usr/local -xzf -
ENV PATH="/usr/local/go/bin:/root/go/bin:${PATH}"
ENV GOPATH="/root/go"

# Install FrankenPHP based on architecture
RUN ARCH=${TARGETARCH:-amd64} && \
    if [ "$ARCH" = "arm64" ]; then FP_ARCH="aarch64"; else FP_ARCH="x86_64"; fi && \
    curl -fsSL "https://github.com/dunglas/frankenphp/releases/latest/download/frankenphp-linux-${FP_ARCH}" \
    -o /usr/local/bin/frankenphp && chmod +x /usr/local/bin/frankenphp

# Create directories
RUN mkdir -p /opt/fastcp/bin /opt/fastcp/data /opt/fastcp/config \
    /var/run/fastcp /var/log/fastcp /var/log/supervisor

# Create test user for development
RUN useradd -m -s /bin/bash testuser && echo "testuser:testpass" | chpasswd

# Initialize MySQL data directory
RUN mkdir -p /var/run/mysqld && chown mysql:mysql /var/run/mysqld

# Copy supervisor config and initial Caddyfile
COPY docker/supervisord.conf /etc/supervisor/conf.d/fastcp.conf
COPY docker/Caddyfile /opt/fastcp/config/Caddyfile

# Working directory for development
WORKDIR /app

# Expose ports
EXPOSE 80 443 2087 3306

# Default command
CMD ["/bin/bash"]

# =============================================================================
# Stage 3: Production Image
# =============================================================================
FROM ubuntu:24.04 AS production

ARG TARGETARCH

ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update && apt-get install -y \
    curl \
    acl \
    mysql-server \
    supervisor \
    libpam0g \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# Install FrankenPHP based on architecture
RUN ARCH=${TARGETARCH:-amd64} && \
    if [ "$ARCH" = "arm64" ]; then FP_ARCH="aarch64"; else FP_ARCH="x86_64"; fi && \
    curl -fsSL "https://github.com/dunglas/frankenphp/releases/latest/download/frankenphp-linux-${FP_ARCH}" \
    -o /usr/local/bin/frankenphp && chmod +x /usr/local/bin/frankenphp

# Create directories
RUN mkdir -p /opt/fastcp/bin /opt/fastcp/data /opt/fastcp/config \
    /var/run/fastcp /var/log/fastcp /var/log/supervisor

# Copy binaries from builder
COPY --from=builder /build/bin/fastcp /opt/fastcp/bin/
COPY --from=builder /build/bin/fastcp-agent /opt/fastcp/bin/
COPY docker/supervisord.conf /etc/supervisor/conf.d/fastcp.conf
COPY docker/Caddyfile /opt/fastcp/config/

EXPOSE 80 443 2087

CMD ["/usr/bin/supervisord", "-c", "/etc/supervisor/supervisord.conf"]
