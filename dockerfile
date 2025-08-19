# 使用官方 Go 镜像作为构建环境
FROM golang:1.22-alpine AS builder

# 设置工作目录
WORKDIR /app

# 将 go.mod 和 go.sum 复制到容器中（可选，用于缓存依赖）
COPY go.mod ./

# 将源代码复制到容器中
COPY . .

# 构建 Go 程序，禁用 CGO 以生成静态链接的二进制文件
RUN CGO_ENABLED=0 GOOS=linux go build -o server .

# 最终运行阶段：使用轻量级的 Alpine Linux 镜像
FROM alpine:latest

# 安装运行时依赖：ca-certificates 用于 HTTPS 请求
RUN apk --no-cache add ca-certificates

# 创建非 root 用户（可选，提高安全性）
# RUN adduser -D -s /bin/false appuser

# 设置工作目录
WORKDIR /root/

# 从构建阶段复制编译好的二进制文件
COPY --from=builder /app/server .

# 创建用于存储图片的目录
RUN mkdir -p /storage

# 声明服务端口
EXPOSE 8080

# 启动命令
# 注意：我们不直接运行 server，而是让 docker-compose 通过 .env 文件注入环境变量
CMD ["./server"]