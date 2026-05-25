# Build context: monorepo root (go.work resolves packages/db)
#
#   docker build -f apps/proxy/Dockerfile -t ghcr.io/meshploy/proxy:latest .
#
FROM golang:1.25.8-alpine AS builder

RUN apk add --no-cache git ca-certificates

WORKDIR /workspace

COPY go.work go.work.sum* ./
COPY apps/api/go.mod apps/api/go.sum* ./apps/api/
COPY apps/proxy/go.mod apps/proxy/go.sum* ./apps/proxy/
COPY apps/cli/go.mod apps/cli/go.sum ./apps/cli/
COPY packages/db/go.mod packages/db/go.sum ./packages/db/

# go work sync requires all modules in go.work to be present.
# Only download deps for the modules we actually build here.
RUN go work sync && \
    cd apps/proxy && go mod download && \
    cd /workspace/packages/db && go mod download

COPY apps/proxy ./apps/proxy
COPY packages/db ./packages/db

WORKDIR /workspace/apps/proxy
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/proxy .

FROM alpine:3.21
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=builder /out/proxy ./proxy
EXPOSE 8081
ENTRYPOINT ["./proxy"]
# channel: main/latest
