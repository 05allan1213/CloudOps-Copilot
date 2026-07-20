# syntax=docker/dockerfile:1.7@sha256:a57df69d0ea827fb7266491f2813635de6f17269be881f696fbfdf2d83dda33e

ARG NODE_IMAGE=node:20-alpine@sha256:fb4cd12c85ee03686f6af5362a0b0d56d50c58a04632e6c0fb8363f609372293
ARG GO_IMAGE=golang:1.26.5-alpine@sha256:0178a641fbb4858c5f1b48e34bdaabe0350a330a1b1149aabd498d0699ff5fb2
ARG RUNTIME_IMAGE=alpine:3.21@sha256:48b0309ca019d89d40f670aa1bc06e426dc0931948452e8491e3d65087abc07d
ARG VCS_REF=unknown
ARG VCS_SOURCE=unknown
ARG VERSION=dev

FROM ${NODE_IMAGE} AS frontend-builder

WORKDIR /src/frontend

COPY frontend/package.json frontend/package-lock.json ./
RUN --mount=type=cache,target=/root/.npm \
    npm ci --no-audit --prefer-offline

COPY frontend/ ./
RUN npm run build

FROM ${GO_IMAGE} AS go-builder

ARG VCS_REF=unknown
ARG VCS_SOURCE=unknown
ARG VERSION=dev

WORKDIR /src

ENV CGO_ENABLED=0 \
    GOFLAGS=-trimpath \
    GOPROXY=https://proxy.golang.org,direct

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go mod download

COPY cmd/ ./cmd/
COPY internal/ ./internal/
COPY migrations/ ./migrations/

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go build -ldflags="-s -w" -o /out/cloudops-api ./cmd/cloudops-api && \
    go build -ldflags="-s -w" -o /out/cloudops-worker ./cmd/cloudops-worker && \
    go build -ldflags="-s -w -X main.version=${VERSION} -X main.sourceRevision=${VCS_REF}" -o /out/cloudops-demo ./cmd/cloudops-demo && \
    go build -ldflags="-s -w" -o /out/cloudops-migrate ./cmd/cloudops-migrate

FROM ${RUNTIME_IMAGE} AS runtime-base

ARG VCS_REF
ARG VCS_SOURCE
ARG VERSION

LABEL org.opencontainers.image.revision="${VCS_REF}" \
      org.opencontainers.image.source="${VCS_SOURCE}" \
      org.opencontainers.image.version="${VERSION}"

RUN apk add --no-cache ca-certificates tzdata wget \
    && addgroup -S app \
    && adduser -S -G app -h /app app

WORKDIR /app

ENV GIN_MODE=release

USER app:app

FROM runtime-base AS cloudops-control-base

# Keep the runtime validator patched while CI also checks rules against the deployed 2.51 line.
COPY --from=prom/prometheus:v3.13.1@sha256:3c42b892cf723fa54d2f262c37a0e1f80aa8c8ddb1da7b9b0df9455a35a7f893 /bin/promtool /usr/local/bin/promtool
COPY server-monitor/runbooks /app/runbooks

ENV RUNBOOK_DIR=/app/runbooks

FROM cloudops-control-base AS cloudops-api

COPY --from=frontend-builder /src/frontend/dist /app/static

COPY --from=go-builder /out/cloudops-api /app/cloudops-api

ENV STATIC_DIR=/app/static

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget --no-verbose --tries=1 --spider http://localhost:8080/livez || exit 1

CMD ["/app/cloudops-api"]

FROM cloudops-control-base AS cloudops-worker

COPY --from=go-builder /out/cloudops-worker /app/cloudops-worker

EXPOSE 8081

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget --no-verbose --tries=1 --spider http://localhost:8081/livez || exit 1

CMD ["/app/cloudops-worker"]

FROM runtime-base AS cloudops-migrate

COPY --from=go-builder /out/cloudops-migrate /app/cloudops-migrate

CMD ["/app/cloudops-migrate"]

FROM runtime-base AS cloudops-demo

COPY --from=go-builder /out/cloudops-demo /app/cloudops-demo

EXPOSE 8080

HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=3 \
  CMD wget --no-verbose --tries=1 --spider http://localhost:8080/livez || exit 1

CMD ["/app/cloudops-demo"]

# Transitional alias for the existing server-web image reference. It runs API only.
FROM cloudops-api AS runtime
