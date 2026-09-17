# ── Stage 1: build a static binary ─────────────────────────────
FROM golang:1.25-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/vchasno-edo-mcp ./cmd/vchasno-edo-mcp

# ── Stage 2: minimal runtime (~20 MB image, ~25 MB RSS) ────────
FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata curl && adduser -D -u 10001 mcp
WORKDIR /app
COPY --from=builder /out/vchasno-edo-mcp /app/vchasno-edo-mcp

# The Vchasno token (MCP_VCHASNO_TOKEN) and MCP_ISSUER_URL are passed at run
# time (env_file / -e), never baked into the image.
ENV MCP_HOST=0.0.0.0 \
    MCP_PORT=8080 \
    MCP_JWT_KEY_DIR=/app/data \
    MCP_DOWNLOAD_DIR=/app/data/downloads
# /app/data keeps the RSA key, so access tokens survive a restart, and the
# files the download tools write.
VOLUME ["/app/data"]
RUN mkdir -p /app/data/downloads && chown -R mcp:mcp /app
USER mcp
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 CMD curl -sf http://localhost:${MCP_PORT}/health || exit 1
ENTRYPOINT ["/app/vchasno-edo-mcp"]
