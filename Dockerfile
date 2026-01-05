# Build stage
FROM golang:1.23-alpine AS builder

WORKDIR /app

# Install build dependencies
RUN apk add --no-cache git nodejs npm

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy frontend and build
COPY web/ ./web/
RUN cd web && npm ci && npm run build

# Copy backend source
COPY . .

# Build binary
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /fastcp ./cmd/fastcp

# Runtime stage
FROM alpine:3.19

RUN apk add --no-cache ca-certificates curl

WORKDIR /app

# Copy binary
COPY --from=builder /fastcp /usr/local/bin/fastcp

# Create directories
RUN mkdir -p /var/lib/fastcp /var/www /var/log/fastcp /etc/fastcp

EXPOSE 8080 80 443

CMD ["fastcp"]

