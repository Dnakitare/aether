# Production Dockerfile for Aether Runtime
# Multi-stage build with security hardening and optimization
#
# Build: docker build -t aether:latest -f deployments/docker/Dockerfile .
# Run:   docker run -p 8080:8080 aether:latest
#
# Security features:
# - Multi-stage build (minimal final image ~10MB)
# - Distroless base (no shell, minimal attack surface)
# - Non-root user (UID 10001)
# - No unnecessary packages or tools
# - Build-time dependency verification
# - Static binary compilation
# - Read-only root filesystem compatible
#
# Size optimizations:
# - Distroless base (smallest possible)
# - Stripped binary (no debug symbols)
# - Layer caching for dependencies
# - Only essential files copied

# Build arguments for versioning and metadata
ARG GO_VERSION=1.24
ARG VERSION=0.2.0-beta
ARG COMMIT_SHA=unknown
ARG BUILD_DATE=unknown

# Stage 1: Build stage
FROM golang:${GO_VERSION}-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git ca-certificates tzdata

# Create non-root user for final image
RUN addgroup -g 10001 -S aether && \
    adduser -u 10001 -S aether -G aether

# Set working directory
WORKDIR /build

# Copy go mod files first for better caching
COPY go.mod go.sum ./
RUN go mod download && go mod verify

# Copy source code
COPY . .

# Build the application with version information
# CGO_ENABLED=0 for static binary (works with distroless)
# -ldflags flags:
#   -w: disable DWARF generation (no debug info)
#   -s: disable symbol table
#   -extldflags "-static": build static binary
#   -X: inject version information
ARG VERSION
ARG COMMIT_SHA
ARG BUILD_DATE

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-w -s -extldflags '-static' \
              -X 'main.Version=${VERSION}' \
              -X 'main.CommitSHA=${COMMIT_SHA}' \
              -X 'main.BuildDate=${BUILD_DATE}'" \
    -a -installsuffix cgo \
    -trimpath \
    -o aether \
    ./cmd/aether

# Verify the binary
RUN chmod +x /build/aether && \
    /build/aether version || true

# Stage 2: Runtime stage (distroless)
FROM gcr.io/distroless/static:nonroot

# Copy timezone data
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo

# Copy CA certificates for HTTPS
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# Copy user information
COPY --from=builder /etc/passwd /etc/passwd
COPY --from=builder /etc/group /etc/group

# Copy the binary
COPY --from=builder --chown=10001:10001 /build/aether /usr/local/bin/aether

# Switch to non-root user
USER 10001:10001

# Expose API port
EXPOSE 8080

# Health check endpoint
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD ["/usr/local/bin/aether", "healthcheck"]

# Set OCI metadata labels
ARG VERSION
ARG COMMIT_SHA
ARG BUILD_DATE

LABEL org.opencontainers.image.title="Aether Runtime" \
      org.opencontainers.image.description="Production-ready runtime platform for AI agents" \
      org.opencontainers.image.vendor="Aether" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.licenses="MIT" \
      org.opencontainers.image.source="https://github.com/dnakitare/aether" \
      org.opencontainers.image.documentation="https://github.com/dnakitare/aether/blob/main/README.md" \
      org.opencontainers.image.revision="${COMMIT_SHA}" \
      org.opencontainers.image.created="${BUILD_DATE}" \
      org.opencontainers.image.base.name="gcr.io/distroless/static:nonroot" \
      maintainer="team@aether.io"

# Run the application
ENTRYPOINT ["/usr/local/bin/aether"]
CMD ["server"]
