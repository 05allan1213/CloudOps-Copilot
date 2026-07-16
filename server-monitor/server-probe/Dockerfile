# syntax=docker/dockerfile:1.7@sha256:a57df69d0ea827fb7266491f2813635de6f17269be881f696fbfdf2d83dda33e

ARG GO_IMAGE=golang:1.26.5-alpine@sha256:0178a641fbb4858c5f1b48e34bdaabe0350a330a1b1149aabd498d0699ff5fb2
ARG RUNTIME_IMAGE=alpine:3.21@sha256:48b0309ca019d89d40f670aa1bc06e426dc0931948452e8491e3d65087abc07d
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
