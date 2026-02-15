# Production Dockerfile for Aether
# Multi-stage build with security hardening
#
# Security features:
# - Multi-stage build (minimal final image)
# - Distroless base (no shell, reduced attack surface)
# - Non-root user
# - No unnecessary packages or tools
# - Build-time dependency verification
# - Minimal file permissions

# Stage 1: Build stage
FROM golang:1.21-alpine AS builder

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

# Build the application
# CGO_ENABLED=0 for static binary (works with distroless)
# -ldflags to strip debug info and reduce size
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags='-w -s -extldflags "-static"' \
    -a -installsuffix cgo \
    -o aether \
    ./cmd/aether

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

# Set metadata
LABEL org.opencontainers.image.title="Aether" \
      org.opencontainers.image.description="AI Agent Runtime Platform" \
      org.opencontainers.image.vendor="Aether" \
      org.opencontainers.image.licenses="MIT" \
      org.opencontainers.image.source="https://github.com/aether/aether"

# Run the application
ENTRYPOINT ["/usr/local/bin/aether"]
CMD ["serve"]
