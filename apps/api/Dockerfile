# ── Build stage ────────────────────────────────────────────────────────────────
# Build context must be the monorepo root so the Go workspace can resolve
# the packages/db replace directive.
#
#   docker build -f apps/api/Dockerfile -t ghcr.io/meshploy/api:latest .
#
FROM golang:1.25.8-alpine AS builder

RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /workspace

# Copy workspace and module manifests first for layer caching.
COPY go.work go.work.sum* ./
COPY apps/api/go.mod apps/api/go.sum ./apps/api/
COPY apps/proxy/go.mod apps/proxy/go.sum* ./apps/proxy/
COPY apps/cli/go.mod apps/cli/go.sum ./apps/cli/
COPY packages/db/go.mod packages/db/go.sum ./packages/db/

# go work sync requires all modules in go.work to be present.
# Only download deps for the modules we actually build here.
RUN go work sync && \
    cd apps/api && go mod download && \
    cd /workspace/packages/db && go mod download

# Copy full source.
COPY apps/api ./apps/api
COPY packages/db ./packages/db

# Build a fully static binary.
WORKDIR /workspace/apps/api
ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w -X github.com/meshploy/apps/api/internal/version.Current=${VERSION}" \
    -o /out/api \
    .

# ── Runtime stage ──────────────────────────────────────────────────────────────
FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata postgresql-client

WORKDIR /app
COPY --from=builder /out/api ./api

EXPOSE 4000

ENTRYPOINT ["./api"]
# channel: main/latest
