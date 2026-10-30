# ── Stage 1: obfuscated static binary ──────────────────────────
# Cross-compiled on the native build platform so garble (heavy) never runs under
# QEMU: it emits the target architecture from one native toolchain run.
FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS builder
ARG TARGETOS TARGETARCH
# garble obfuscates package/type/func names and (with -literals) string literals;
# pinned to the release that matches this Go toolchain. git is needed by go install.
RUN apk add --no-cache git && go install mvdan.cc/garble@v0.15.0
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# garble forces -trimpath; -literals encrypts string literals (header names, messages).
# The whole binary is obfuscated, not only the licensing package, so the licence
# check cannot be located and cut out by name. JSON struct tags are preserved, so the
# wire protocol and the MCP tool schemas are unchanged.
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH garble -literals build -ldflags="-s -w" -o /out/vchasno-edo-mcp ./cmd/vchasno-edo-mcp

# ── Stage 2: minimal runtime (~20 MB image, ~25 MB RSS) ────────
FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata curl && adduser -D -u 10001 mcp
WORKDIR /app
COPY --from=builder /out/vchasno-edo-mcp /app/vchasno-edo-mcp

# The license server (MCP_LICENSE_SERVER_URL, MCP_LICENSE_PUBLIC_KEY), the
# pre-auth credentials (MCP_LICENSE_KEY, MCP_VCHASNO_TOKEN) and MCP_ISSUER_URL
# are passed at run time (env_file / -e), never baked into the image.
ENV MCP_HOST=0.0.0.0 \
    MCP_PORT=8080 \
    MCP_JWT_KEY_DIR=/app/data \
    MCP_DOWNLOAD_DIR=/app/data/downloads
# /app/data keeps the RSA key, so access tokens survive a restart, the license
# instance id and cached license answers, and the files the download tools write.
VOLUME ["/app/data"]
RUN mkdir -p /app/data/downloads && chown -R mcp:mcp /app
USER mcp
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 CMD curl -sf http://localhost:${MCP_PORT}/health || exit 1
ENTRYPOINT ["/app/vchasno-edo-mcp"]
