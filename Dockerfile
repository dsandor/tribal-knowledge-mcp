# syntax=docker/dockerfile:1

# ─── Stage 1: Build React SPA ───────────────────────────────────────────────
FROM node:22-alpine AS web-builder
WORKDIR /app/web
COPY web/package.json web/package-lock.json ./
RUN npm ci --prefer-offline
COPY web/ ./
RUN npm run build
# Output: /app/web/dist/

# ─── Stage 2: Build Go binary ───────────────────────────────────────────────
FROM golang:1.25-bookworm AS go-builder

# Install gcc and sqlite build dependencies required by CGO / sqlite-vec.
RUN apt-get update && apt-get install -y --no-install-recommends \
    gcc \
    libc6-dev \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

# Download dependencies before copying source (layer cache).
COPY go.mod go.sum ./
RUN go mod download

# Copy web build output so go:embed finds web/dist/.
COPY --from=web-builder /app/web/dist ./web/dist

# Copy the rest of the source.
COPY . .

# Inject git version; fall back to "docker" if .git is absent.
ARG VERSION=docker
RUN CGO_ENABLED=1 GOOS=linux go build \
    -ldflags "-X github.com/dsandor/memory/internal/web.buildVersion=${VERSION} -extldflags '-static'" \
    -tags netgo \
    -o /app/server \
    ./cmd/server

# ─── Stage 3: Runtime image ─────────────────────────────────────────────────
FROM debian:bookworm-slim

# ca-certificates for outbound TLS (Anthropic API, Ollama).
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# Non-root user.
RUN useradd -r -u 1001 -g root memory
USER 1001

WORKDIR /data

COPY --from=go-builder /app/server /usr/local/bin/server

EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/server"]
