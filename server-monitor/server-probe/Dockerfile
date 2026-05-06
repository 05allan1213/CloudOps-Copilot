# ============================================
# 第一阶段：编译（Builder Stage）
# ============================================
# 使用 Go 官方镜像作为编译环境
FROM golang:1.26-alpine AS builder

# 设置工作目录。服务模块保持在 /app/server-probe，确保 go.mod 中的 ../pkg 指向 /app/pkg。
WORKDIR /app/server-probe

# 先复制 go.mod 依赖文件和共享 pkg 模块，提高依赖缓存命中率。
COPY pkg/ /app/pkg/
COPY server-probe/go.mod server-probe/go.sum ./

# 设置 Go 代理，加速依赖下载
ENV GOPROXY=https://goproxy.cn,direct

# 下载依赖
RUN go mod download

# 复制源代码到容器
COPY server-probe/ ./

# 静态编译（CGO_ENABLED=0 禁用 CGO，生成纯静态二进制）
RUN CGO_ENABLED=0 go build -o /app/probe .

# ============================================
# 第二阶段：运行（Runtime Stage）
# ============================================
# 使用 Alpine 最小化镜像，减小镜像体积
FROM alpine:3.19

RUN apk add --no-cache tzdata

WORKDIR /app

COPY --from=builder /app/probe .

EXPOSE 9090

CMD ["/app/probe"]
