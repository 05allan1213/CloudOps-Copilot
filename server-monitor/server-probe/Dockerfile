# syntax=docker/dockerfile:1.7

ARG GO_IMAGE=golang:1.26-alpine
ARG RUNTIME_IMAGE=alpine:3.21
ARG VCS_REF=unknown
ARG VCS_SOURCE=unknown
ARG VERSION=dev

FROM ${GO_IMAGE} AS builder

WORKDIR /src/server-probe

ENV CGO_ENABLED=0 \
    GOFLAGS=-trimpath \
    GOPROXY=https://proxy.golang.org,direct

COPY pkg/go.mod pkg/go.sum /src/pkg/
COPY server-probe/go.mod server-probe/go.sum ./

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go mod download

COPY pkg/ /src/pkg/
COPY server-probe/ ./

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go build -ldflags="-s -w" -o /out/probe .

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

COPY --from=builder /out/probe /app/probe

USER app:app

EXPOSE 9090

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget --no-verbose --tries=1 --spider http://localhost:9090/healthz || exit 1

CMD ["/app/probe"]
