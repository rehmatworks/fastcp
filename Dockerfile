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
    software-properties-common \
    gnupg2 \
    apt-transport-https \
    lsb-release \
    ca-certificates \
    restic \
    && rm -rf /var/lib/apt/lists/*

# Install Go 1.22 based on architecture
RUN ARCH=${TARGETARCH:-amd64} && \
    if [ "$ARCH" = "arm64" ]; then GO_ARCH="arm64"; else GO_ARCH="amd64"; fi && \
    curl -fsSL "https://go.dev/dl/go1.22.5.linux-${GO_ARCH}.tar.gz" | tar -C /usr/local -xzf -
ENV PATH="/usr/local/go/bin:/root/go/bin:${PATH}"
ENV GOPATH="/root/go"

# Install Caddy (plain reverse proxy)
RUN ARCH=${TARGETARCH:-amd64} && \
    if [ "$ARCH" = "arm64" ]; then CADDY_ARCH="arm64"; else CADDY_ARCH="amd64"; fi && \
    curl -fsSL "https://caddyserver.com/api/download?os=linux&arch=${CADDY_ARCH}" \
    -o /usr/local/bin/caddy && chmod +x /usr/local/bin/caddy

# Install Ondrej PHP versions + common modules
RUN add-apt-repository -y ppa:ondrej/php && \
    apt-get update && \
    COMMON_PHP_MODULES="bcmath bz2 cli common curl fpm gd gmp igbinary imagick imap intl mbstring mysql opcache readline redis soap sqlite3 xml xmlrpc zip" && \
    for v in 8.2 8.3 8.4 8.5; do \
      for m in $COMMON_PHP_MODULES; do \
        apt-get install -y "php${v}-${m}" || true; \
      done; \
      apt-get install -y "php${v}" "php${v}-fpm" || true; \
    done && \
    rm -rf /var/lib/apt/lists/*

# Create directories
RUN mkdir -p /opt/fastcp/bin /opt/fastcp/data /opt/fastcp/config /opt/fastcp/run \
    /var/log/fastcp /var/log/supervisor /opt/fastcp/phpmyadmin && chmod 1777 /opt/fastcp/run

# Create test user for development
RUN useradd -m -s /bin/bash testuser && echo "testuser:testpass" | chpasswd

# Download and configure phpMyAdmin
RUN curl -fsSL https://files.phpmyadmin.net/phpMyAdmin/5.2.1/phpMyAdmin-5.2.1-all-languages.tar.gz \
    | tar -xz -C /opt/fastcp/phpmyadmin --strip-components=1

# Generate encryption secret for dev
RUN openssl rand -base64 32 > /opt/fastcp/data/.secret && chmod 600 /opt/fastcp/data/.secret

# Initialize MySQL data directory
RUN mkdir -p /var/run/mysqld && chown mysql:mysql /var/run/mysqld

# Copy supervisor config and initial Caddyfile
COPY docker/supervisord.conf /etc/supervisor/conf.d/fastcp.conf
COPY docker/Caddyfile /opt/fastcp/config/Caddyfile

# Working directory for development
WORKDIR /app

# Expose ports
EXPOSE 80 443 2050 3306

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
    restic \
    && rm -rf /var/lib/apt/lists/*

# Install Caddy based on architecture
RUN ARCH=${TARGETARCH:-amd64} && \
    if [ "$ARCH" = "arm64" ]; then CADDY_ARCH="arm64"; else CADDY_ARCH="amd64"; fi && \
    curl -fsSL "https://caddyserver.com/api/download?os=linux&arch=${CADDY_ARCH}" \
    -o /usr/local/bin/caddy && chmod +x /usr/local/bin/caddy

# Create directories
RUN mkdir -p /opt/fastcp/bin /opt/fastcp/data /opt/fastcp/config /opt/fastcp/run \
    /var/log/fastcp /var/log/supervisor && chmod 1777 /opt/fastcp/run

# Copy binaries from builder
COPY --from=builder /build/bin/fastcp /opt/fastcp/bin/
COPY --from=builder /build/bin/fastcp-agent /opt/fastcp/bin/
COPY docker/supervisord.conf /etc/supervisor/conf.d/fastcp.conf
COPY docker/Caddyfile /opt/fastcp/config/

EXPOSE 80 443 2050

CMD ["/usr/bin/supervisord", "-c", "/etc/supervisor/supervisord.conf"]
