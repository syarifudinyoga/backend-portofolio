# ==========================================
# Stage 1: Build Go Binary
# ==========================================
FROM golang:alpine AS builder

ENV GOTOOLCHAIN=auto

RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /app

# 1. Cache dependencies
COPY go.mod go.sum ./
RUN go mod download

# 2. Copy source code
COPY . .

# 3. Build optimized static binary
RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags="-s -w -extldflags '-static'" \
    -o /app/portfolio-api \
    ./cmd/api

# ==========================================
# Stage 2: Minimal Runtime Container
# ==========================================
FROM alpine:3.21 AS runtime

RUN apk add --no-cache ca-certificates tzdata wget && \
    addgroup -g 10001 -S appgroup && \
    adduser -u 10001 -S appuser -G appgroup

WORKDIR /app

COPY --from=builder --chown=appuser:appgroup /app/portfolio-api /app/portfolio-api

USER appuser

EXPOSE 3004

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD wget --no-verbose --tries=1 --spider http://127.0.0.1:3004/api/health || exit 1

ENTRYPOINT ["/app/portfolio-api"]
