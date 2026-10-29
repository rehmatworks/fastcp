# FastCP in Docker, for trying the panel and testing the installer.
#
#   docker compose up --build        then open https://localhost:2050
#
# Production servers should use scripts/install.sh on a VM: without systemd
# the per-user memory and CPU limits are not enforced.

FROM golang:1.27-bookworm AS build
RUN apt-get update -qq && apt-get install -y -qq libpam0g-dev >/dev/null && rm -rf /var/lib/apt/lists/*
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY caddy/go.mod caddy/go.sum ./caddy/
RUN cd caddy && go mod download
COPY . .
RUN VERSION="$(cat VERSION)" && \
    LDFLAGS="-s -w -X github.com/rehmatworks/fastcp/internal/version.Version=${VERSION} -X github.com/rehmatworks/fastcp/internal/version.BuildTime=$(date -u +%Y-%m-%dT%H:%M:%SZ)" && \
    go build -trimpath -ldflags "$LDFLAGS" -o /out/fastcp ./cmd/fastcp && \
    go build -trimpath -ldflags "$LDFLAGS" -o /out/fastcp-agent ./cmd/fastcp-agent && \
    cd caddy && CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /out/caddy .

FROM ubuntu:24.04
ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update -qq && apt-get install -y -qq curl ca-certificates >/dev/null && rm -rf /var/lib/apt/lists/*
COPY --from=build /out/fastcp /out/fastcp-agent /out/caddy /usr/src/fastcp/
COPY scripts/install.sh /usr/src/fastcp/install.sh
COPY docker/entrypoint.sh /usr/local/bin/fastcp-entrypoint
EXPOSE 80 443 2050
VOLUME ["/opt/fastcp/data", "/var/lib/mysql", "/home"]
ENTRYPOINT ["/usr/local/bin/fastcp-entrypoint"]
