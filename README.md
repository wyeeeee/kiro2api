# kiro2api

一个基于 Go 的高性能 API 代理服务器，提供 Anthropic Claude API 和 OpenAI 兼容的 API 接口，桥接 AWS CodeWhisperer 服务。支持实时流式响应、多种认证方式和环境变量配置。

## 功能特性

- **多格式API支持**：同时支持 Anthropic Claude API 和 OpenAI ChatCompletion API 格式
- **实时流式响应**：自定义 AWS EventStream 解析器，提供零延迟的流式体验
- **高性能架构**：基于 gin-gonic/gin 框架，使用 bytedance/sonic 高性能 JSON 库
- **智能认证管理**：基于环境变量的认证管理，支持.env文件，自动刷新过期令牌
- **完善的中间件**：统一的认证、CORS 和日志处理
- **容器化支持**：提供 Dockerfile，支持 Docker 部署

## 技术栈

- **Web框架**: gin-gonic/gin v1.10.1
- **JSON处理**: bytedance/sonic v1.14.0
- **Go版本**: 1.23.3
- **流式解析**: 自定义 AWS EventStream 二进制协议解析器

## 快速开始

### 编译和运行

```bash
# 克隆项目
git clone <repository-url>
cd kiro2api

# 编译
go build -o kiro2api main.go

# 启动服务器（默认端口 8080，默认认证令牌 "123456"）
./kiro2api

# 使用环境变量配置
export KIRO_CLIENT_TOKEN="your_token"
export AWS_REFRESHTOKEN="your_refresh_token"  # 必需设置
export PORT="9000"
./kiro2api

# 使用 .env 文件配置
cp .env.example .env
# 编辑 .env 文件设置你的配置
./kiro2api
```

### 使用 Docker

```bash
# 构建镜像
docker build -t kiro2api .

# 运行容器
docker run -p 8080:8080 kiro2api

```

## API 接口

### 支持的端点

- `POST /v1/messages` - Anthropic Claude API 兼容接口（支持流式和非流式）
- `POST /v1/chat/completions` - OpenAI ChatCompletion API 兼容接口（支持流式和非流式）
- `GET /v1/models` - 获取可用模型列表
- `GET /health` - 健康检查（无需认证）

### 认证方式

所有 API 端点（除 `/health`）都需要在请求头中提供认证信息：

```bash
# 使用 Authorization Bearer 认证
Authorization: Bearer your-auth-token

# 或使用 x-api-key 认证
x-api-key: your-auth-token
```

### 请求示例

#### Anthropic API 格式

```bash
# 非流式请求
curl -X POST http://localhost:8080/v1/messages \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer 123456" \
  -d '{
    "model": "claude-sonnet-4-20250514",
    "max_tokens": 1000,
    "messages": [
      {"role": "user", "content": "你好，请介绍一下你自己"}
    ]
  }'

# 流式请求
curl -X POST http://localhost:8080/v1/messages \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer 123456" \
  -d '{
    "model": "claude-sonnet-4-20250514",
    "max_tokens": 1000,
    "stream": true,
    "messages": [
      {"role": "user", "content": "请写一篇关于人工智能的文章"}
    ]
  }'
```

#### OpenAI API 格式

```bash
# 非流式请求
curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer 123456" \
  -d '{
    "model": "claude-sonnet-4-20250514",
    "messages": [
      {"role": "user", "content": "解释一下机器学习的基本概念"}
    ]
  }'

# 流式请求
curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer 123456" \
  -d '{
    "model": "claude-sonnet-4-20250514",
    "stream": true,
    "messages": [
      {"role": "user", "content": "请详细介绍深度学习"}
    ]
  }'
```

## 支持的模型

当前支持的模型映射：

- `claude-sonnet-4-20250514` → `CLAUDE_SONNET_4_20250514_V1_0`
- `claude-3-5-haiku-20241022` → `CLAUDE_3_7_SONNET_20250219_V1_0`

## 令牌管理

### 令牌文件位置

程序会从以下位置读取认证令牌：
- 文件路径：`~/.aws/sso/cache/kiro-auth-token.json`

令牌文件格式：
```json
{
    "accessToken": "your-access-token",
    "refreshToken": "your-refresh-token", 
    "expiresAt": "2024-01-01T00:00:00Z"
}
```

### 自动令牌刷新

- 当收到 403 错误时，程序会自动尝试刷新令牌
- 刷新 URL：`https://prod.us-east-1.auth.desktop.kiro.dev/refreshToken`

## 开发指南

### 本地开发

```bash
# 安装依赖
go mod download

# 运行测试
go test ./...

# 运行特定包的测试
go test ./parser -v
go test ./auth -v

# 代码质量检查
go vet ./...
go fmt ./...

# 清理构建
rm -f kiro2api && go build -o kiro2api main.go
```

### 项目结构

```
kiro2api/
├── main.go              # 程序入口
├── auth/                # 认证和令牌管理
├── config/              # 配置和常量定义
├── converter/           # API 格式转换
├── logger/              # 结构化日志系统
├── parser/              # 流式响应解析
├── server/              # HTTP 服务器和处理器
├── types/               # 数据结构定义
└── utils/               # 工具函数
```

## 架构说明

### 请求处理流程

1. **接收请求** - gin 路由器接收 HTTP 请求
2. **认证验证** - AuthMiddleware 验证 API 密钥
3. **格式转换** - converter 包将请求转换为 CodeWhisperer 格式
4. **代理转发** - 通过 `127.0.0.1:9000` 代理转发到 AWS CodeWhisperer
5. **响应解析** - StreamParser 实时解析 AWS EventStream 二进制数据
6. **格式转换** - 将响应转换回客户端请求的格式
7. **返回响应** - 以流式或非流式方式返回给客户端

### 核心特性

- **零延迟流式**: 使用滑动窗口缓冲区的自定义 EventStream 解析器
- **统一中间件**: 集中式的认证、CORS 和错误处理
- **高性能处理**: 共享 HTTP 客户端和优化的 JSON 序列化
- **容错设计**: 自动令牌刷新和优雅的错误处理

## 环境变量

可以通过环境变量配置部分行为：

```bash
# 日志级别（可选）
export LOG_LEVEL=info
```

## 注意事项

- 流式响应使用自定义的二进制 EventStream 解析，不是缓冲解析
- 程序启动时会初始化结构化日志系统
- 健康检查端点 `/health` 不需要认证

## 许可证

本项目遵循相应的开源许可证。详情请查看 LICENSE 文件。