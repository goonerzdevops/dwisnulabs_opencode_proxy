# syntax=docker/dockerfile:1

FROM golang:1.27-alpine AS builder

ARG TARGETOS=linux
ARG TARGETARCH=amd64

RUN apk add --no-cache ca-certificates curl tar

WORKDIR /src

# Cache Go modules first
COPY go.mod go.sum ./
RUN go mod download

# Build static opencode-proxy binary
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" -o /out/opencode-proxy ./cmd/opencode-proxy

# Pre-bake official wireproxy binary for zero-latency WARP IPv6 fallback
RUN set -eux; \
    asset_url=$(curl -fsSL -H "User-Agent: opencode-proxy-docker" \
      https://api.github.com/repos/windtf/wireproxy/releases/latest \
      | grep -oE "https://[^\"]+wireproxy_${TARGETOS}_${TARGETARCH}\.tar\.gz" \
      | head -n 1); \
    test -n "$asset_url"; \
    curl -fsSL -H "User-Agent: opencode-proxy-docker" "$asset_url" \
      | tar -xz -C /out wireproxy; \
    chmod 755 /out/wireproxy; \
    /out/wireproxy -v

FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata curl \
    && addgroup -g 1000 appuser \
    && adduser -u 1000 -G appuser -D -H appuser \
    && mkdir -p /app/state/data/warp /app/state/logs \
    && chown -R appuser:appuser /app

COPY --from=builder /out/opencode-proxy /usr/local/bin/opencode-proxy
COPY --from=builder /out/wireproxy /usr/local/bin/wireproxy

WORKDIR /app/state
USER appuser

ENV TZ=Asia/Jakarta \
    OPENCODE_PROXY_HOST=0.0.0.0 \
    OPENCODE_PROXY_PORT=20144 \
    OPENCODE_PROXY_CONFIG=/app/state/config.json

EXPOSE 20144

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD curl -fsS http://127.0.0.1:20144/health >/dev/null || exit 1

ENTRYPOINT ["/usr/local/bin/opencode-proxy"]
