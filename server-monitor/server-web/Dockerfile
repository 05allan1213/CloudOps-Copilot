# ============================================
# 第一阶段：前端构建
# ============================================
FROM node:20-alpine AS frontend-builder

WORKDIR /app

COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci

COPY frontend/ ./
RUN npm run build

# ============================================
# 第二阶段：Go 编译
# ============================================
FROM golang:1.26-alpine AS go-builder

WORKDIR /app/server-web

COPY pkg/ /app/pkg/
COPY server-web/go.mod server-web/go.sum ./

ENV GOPROXY=https://goproxy.cn,direct

RUN go mod download
COPY server-web/ ./
RUN CGO_ENABLED=0 go build -o /app/web .

# ============================================
# 第三阶段：运行
# ============================================
FROM alpine:3.21

RUN apk add --no-cache tzdata wget

WORKDIR /app

COPY --from=go-builder /app/web .
COPY --from=prom/prometheus:v2.51.0 /bin/promtool /usr/local/bin/promtool
COPY --from=frontend-builder /app/dist ./static
COPY runbooks ./runbooks

ENV STATIC_DIR=/app/static
ENV RUNBOOK_DIR=/app/runbooks

RUN addgroup -S app && adduser -S -G app app
USER app

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget --no-verbose --tries=1 --spider http://localhost:8080/healthz || exit 1

CMD ["/app/web"]
