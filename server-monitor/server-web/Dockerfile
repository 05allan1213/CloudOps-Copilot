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

WORKDIR /src/server-web

ENV CGO_ENABLED=0 \
    GOFLAGS=-trimpath \
    GOPROXY=https://proxy.golang.org,direct

COPY pkg/go.mod pkg/go.sum /src/pkg/
COPY server-web/go.mod server-web/go.sum ./

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go mod download

COPY pkg/ /src/pkg/
COPY server-web/ ./

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go build -ldflags="-s -w" -o /out/web .

FROM ${RUNTIME_IMAGE} AS runtime

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

COPY --from=go-builder /out/web /app/web
COPY --from=prom/prometheus:v2.51.0@sha256:5ccad477d0057e62a7cd1981ffcc43785ac10c5a35522dc207466ff7e7ec845f /bin/promtool /usr/local/bin/promtool
COPY --from=frontend-builder /src/frontend/dist /app/static
COPY runbooks /app/runbooks

ENV GIN_MODE=release \
    RUNBOOK_DIR=/app/runbooks \
    STATIC_DIR=/app/static

USER app:app

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget --no-verbose --tries=1 --spider http://localhost:8080/healthz || exit 1

CMD ["/app/web"]
