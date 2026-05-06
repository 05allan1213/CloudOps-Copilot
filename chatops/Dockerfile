# 构建阶段
FROM golang:1.26-alpine AS builder

# 使用国内 Go 模块代理
ENV GOPROXY=https://goproxy.cn,direct

WORKDIR /app
COPY server/go.mod server/go.sum ./
RUN go mod download
COPY server/ .
COPY web/ ./web/

# 静态编译，避免 Alpine 下 "not found" 问题
RUN CGO_ENABLED=0 GOOS=linux go build -o chatops-server .

# 运行阶段
FROM alpine:3.19

# 使用国内 Alpine 镜像源
RUN sed -i 's/dl-cdn.alpinelinux.org/mirrors.aliyun.com/g' /etc/apk/repositories && \
    apk --no-cache add ca-certificates tzdata && \
    cp /usr/share/zoneinfo/Asia/Shanghai /etc/localtime

WORKDIR /app
COPY --from=builder /app/chatops-server .
COPY --from=builder /app/web ./web

EXPOSE 8080

CMD ["./chatops-server"]
