
# PixAI 图片代理服务 (Go + Docker)

一个高性能、自托管的图片代理服务，用于下载远程图片并存储在本地服务器。专为解决 Cloudflare Workers 在中国大陆访问速度慢的问题而设计。

本服务完全使用 Go 语言编写，通过 Docker 容器化部署，数据直接存储在服务器本地磁盘，确保访问速度快、延迟低。

---

## 🌟 功能特性

- ✅ **本地存储**：图片直接保存在服务器硬盘，告别 R2 跨洋延迟。
- ✅ **高速访问**：部署在国内服务器，图片加载飞快。
- ✅ **自动过期**：文件默认 24 小时后自动清理（可配置）。
- ✅ **安全防护**：
  - API Key 验证，防止接口被滥用。
  - CORS 支持，保护前端应用安全。
  - 请求频率限制（100 次/分钟）。
- ✅ **容错机制**：下载图片失败时，自动重试 3 次。
- ✅ **简单易用**：标准 RESTful API，一行 `curl` 命令即可上传。

---

## 🚀 快速部署

### 1. 准备环境

确保服务器已安装：
- [Docker](https://docs.docker.com/get-docker/)
- [Docker Compose](https://docs.docker.com/compose/install/)

### 2. 克隆项目

```bash
git clone https://github.com/your-username/pixai-proxy.git
cd pixai-proxy
```

### 3. 配置环境变量

复制示例配置文件并进行修改：

```bash
cp .env.example .env
nano .env  # 或使用其他编辑器
```

根据你的实际情况修改 `.env` 文件：

```env
# 自定义图片访问域名
CUSTOM_DOMAIN=https://images.yourdomain.com

# 图片路径前缀
PATH_PREFIX=img

# 允许访问的前端域名 (CORS)
ALLOWED_ORIGINS=https://your-frontend.com,*

# API 密钥 (重要！请修改为强密码)
API_KEY=your-super-secret-api-key-here-please-change-it

# 文件过期时间（小时）
FILE_EXPIRY_HOURS=24

# 服务器监听端口
LISTEN_ADDR=:8080
```

### 4. 启动服务

```bash
docker-compose up --build -d
```

服务将以后台模式启动，监听 `8080` 端口。

---

## 📡 API 使用方法

### 1. 上传图片 (POST /proxy)

向服务发送一个 POST 请求，提供远程图片的 URL。

```bash
curl -X POST http://your-server-ip:8080/proxy \
  -H "Content-Type: application/json" \
  -H "X-API-Key: your-super-secret-api-key-here-please-change-it" \
  -d '{
    "url": "https://example.com/sample.jpg"
  }'
```

#### 请求参数

| 参数 | 类型 | 必填 | 说明 |
| :--- | :--- | :--- | :--- |
| `url` | string | 是 | 要下载的远程图片的完整 URL。 |

#### 成功响应 (HTTP 200)

```json
{
  "success": true,
  "url": "https://images.yourdomain.com/img/1716789012-abc123.jpg",
  "type": "image/jpeg",
  "size": 123456
}
```

#### 错误响应 (HTTP 500)

```json
{
  "error": "图片上传失败，请联系管理员"
}
```

### 2. 访问图片 (GET)

使用 `/proxy` 接口返回的 `url` 直接在浏览器或 `<img>` 标签中访问图片。

```
https://images.yourdomain.com/img/1716789012-abc123.jpg
```

---

## 🗑️ 文件清理

服务会启动一个后台任务，**每小时**扫描一次存储目录 (`./storage`)，自动删除超过 `FILE_EXPIRY_HOURS` 的文件。

---

## 🛠️ 目录结构

```
pixai-proxy/
├── main.go           # 核心 Go 源码
├── go.mod            # Go 模块定义
├── go.sum            # 依赖校验和
├── Dockerfile        # Docker 构建文件
├── docker-compose.yml # 服务编排文件
├── .env.example      # 环境变量示例
└── storage/          # 图片存储目录 (Docker 卷挂载)
```

---

## 📎 注意事项

- 请务必修改 `.env` 中的 `API_KEY`，使用强密码。
- 建议将 `CUSTOM_DOMAIN` 绑定到你的域名，并通过 Nginx 配置 HTTPS。
- `ALLOWED_ORIGINS` 建议不要使用 `*`，应指定具体的前端域名以提高安全性。
- 确保服务器有足够的磁盘空间存放图片。

---
