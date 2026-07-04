# ---- Build Stage ----
FROM golang:1.26-alpine AS builder

WORKDIR /app

# Copy dependency files first (Docker layer caching)
# If go.mod/go.sum don't change, this layer is cached
# and go mod download won't re-run on every build
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the binary
# CGO_ENABLED=0 — disables C bindings, produces a static binary
# GOOS=linux — cross-compile for Linux (important if building on Windows/Mac)
# -ldflags="-s -w" — strips debug info, reduces binary size significantly
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o gotaskq ./cmd/server

# ---- Run Stage ----
FROM alpine:3.19

WORKDIR /app

# Install ca-certificates — needed for HTTPS calls
# Install wget — needed for healthcheck
RUN apk --no-cache add ca-certificates wget

# Copy only the binary from builder stage
# The final image has no Go toolchain, no source code
# Just the compiled binary — much smaller and more secure
COPY --from=builder /app/gotaskq .

# Copy migrations — needed at runtime
COPY --from=builder /app/migrations ./migrations

EXPOSE 8080

# Healthcheck — Docker will mark container unhealthy if this fails
HEALTHCHECK --interval=30s --timeout=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8080/health || exit 1

CMD ["./gotaskq"]