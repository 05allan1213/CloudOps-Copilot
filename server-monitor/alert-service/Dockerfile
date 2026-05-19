# ============================================
# 第一阶段：编译
# ============================================
FROM golang:1.26-alpine AS builder

WORKDIR /app/alert-service

COPY pkg/ /app/pkg/
COPY alert-service/go.mod alert-service/go.sum ./

ENV GOPROXY=https://goproxy.cn,direct

RUN go mod download
COPY alert-service/ ./
RUN CGO_ENABLED=0 go build -o /app/alert-service .

# ============================================
# 第二阶段：运行
# ============================================
FROM alpine:3.21

RUN apk add --no-cache tzdata wget \
	&& addgroup -S app \
	&& adduser -S -G app app

WORKDIR /app

COPY --from=builder /app/alert-service .

USER app

EXPOSE 8081

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget --no-verbose --tries=1 --spider http://localhost:8081/healthz || exit 1

CMD ["/app/alert-service"]
