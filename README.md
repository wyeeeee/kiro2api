# kiro2api

一个基于 Go 的高性能 HTTP 代理服务器，提供 Anthropic Claude API 和 OpenAI 兼容的 API 接口，桥接 AWS CodeWhisperer 服务。支持多模态图片输入、实时流式响应、智能请求分析和完整的工具调用功能。

**核心架构**: 双API代理服务器，在 Anthropic API、OpenAI ChatCompletion API 和 AWS CodeWhisperer EventStream 格式之间进行转换。

## 功能特性

- **双API代理**：同时支持 Anthropic Claude API 和 OpenAI ChatCompletion API 格式
- **多认证支持**：支持 Social（默认）和 IdC 两种认证方式
- **工具调用支持**：完整的Anthropic工具使用格式，基于 `tool_use_id` 的精确去重逻辑
- **多模态处理**：支持图片输入，自动格式转换，支持PNG、JPEG、GIF、WebP等格式
- **实时流式响应**：自定义 EventStream 解析器，零延迟流式传输
- **智能请求分析**：自动分析请求复杂度，动态调整超时时间
- **Token池管理**：多token轮换使用，故障转移和负载均衡
- **高性能优化**：对象池模式、原子操作缓存、并发优化

## 技术栈

- **Web框架**: gin-gonic/gin v1.10.1
- **JSON处理**: bytedance/sonic v1.14.0
- **环境变量**: github.com/joho/godotenv v1.5.1
- **Go版本**: 1.23.3
- **容器化**: Docker & Docker Compose 支持

## 快速开始

### 基础运行

```bash
# 克隆并编译
git clone <repository-url>
cd kiro2api
go build -o kiro2api main.go

# 配置环境变量
cp .env.example .env
# 编辑 .env 文件，设置 AWS_REFRESHTOKEN

# 启动服务器
./kiro2api

# 测试API
curl -X POST http://localhost:8080/v1/messages \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer 123456" \
  -d '{"model": "claude-sonnet-4-20250514", "max_tokens": 100, "messages": [{"role": "user", "content": "你好"}]}'
```

### 使用 Docker

```bash
# 方式一：使用 docker-compose（推荐）
docker-compose up -d

# 方式二：手动构建运行
docker build -t kiro2api .
docker run -d \
  --name kiro2api \
  -p 8080:8080 \
  -e AWS_REFRESHTOKEN="your_refresh_token" \
  -e KIRO_CLIENT_TOKEN="123456" \
  kiro2api
```

## API 接口

### 支持的端点

- `POST /v1/messages` - Anthropic Claude API 兼容接口（支持流式和非流式）
- `POST /v1/chat/completions` - OpenAI ChatCompletion API 兼容接口（支持流式和非流式）
- `GET /v1/models` - 获取可用模型列表

### 认证方式

所有 API 端点都需要在请求头中提供认证信息：

```bash
# 使用 Authorization Bearer 认证
Authorization: Bearer your-auth-token

# 或使用 x-api-key 认证
x-api-key: your-auth-token
```

### 请求示例

```bash
# Anthropic API 格式
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

# OpenAI API 格式
curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer 123456" \
  -d '{
    "model": "claude-sonnet-4-20250514",
    "messages": [
      {"role": "user", "content": "解释一下机器学习的基本概念"}
    ]
  }'

# 流式请求（添加 "stream": true）
curl -N -X POST http://localhost:8080/v1/messages \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer 123456" \
  -d '{
    "model": "claude-sonnet-4-20250514",
    "max_tokens": 200,
    "stream": true,
    "messages": [{"role": "user", "content": "讲个故事"}]
  }'
```

## 支持的模型

| 公开模型名称 | 内部 CodeWhisperer 模型 ID |
|-------------|---------------------------|
| `claude-sonnet-4-20250514` | `CLAUDE_SONNET_4_20250514_V1_0` |
| `claude-3-7-sonnet-20250219` | `CLAUDE_3_7_SONNET_20250219_V1_0` |
| `claude-3-5-haiku-20241022` | `CLAUDE_3_5_HAIKU_20241022_V1_0` |

## 环境变量配置

### 必需配置

```bash
# Social 认证方式（默认）
AWS_REFRESHTOKEN=your_refresh_token_here  # 必需设置
KIRO_CLIENT_TOKEN=123456                  # 默认: 123456
PORT=8080                                 # 默认: 8080

# IdC 认证方式（可选）
AUTH_METHOD=idc
IDC_CLIENT_ID=your_client_id
IDC_CLIENT_SECRET=your_client_secret
IDC_REFRESH_TOKEN=your_idc_refresh_token
```

### 可选配置

```bash
# 日志配置
LOG_LEVEL=info                            # debug,info,warn,error
LOG_FORMAT=json                           # text,json
LOG_FILE=/var/log/kiro2api.log            # 日志文件路径

# 功能控制
DISABLE_STREAM=false                      # 是否禁用流式响应
GIN_MODE=release                          # Gin模式：debug,release,test

# 超时配置
REQUEST_TIMEOUT_MINUTES=15                # 复杂请求超时
SIMPLE_REQUEST_TIMEOUT_MINUTES=2          # 简单请求超时
```

## 开发命令

```bash
# 测试
go test ./...
go test ./parser -v

# 代码质量检查
go vet ./...
go fmt ./...
go mod tidy

# 开发模式运行
GIN_MODE=debug LOG_LEVEL=debug ./kiro2api

# 生产构建
go build -ldflags="-s -w" -o kiro2api main.go
```

## 故障排除

### Token刷新失败
- 检查 `AWS_REFRESHTOKEN` 环境变量是否正确设置
- 验证token池配置和轮换机制

### 流式响应中断
- 检查客户端连接稳定性
- 验证EventStream解析器状态
- 查看工具调用去重逻辑

### 性能问题
- 监控HTTP客户端连接池状态
- 检查对象池使用情况
- 分析请求复杂度评估准确性

## 开发指南

详细的开发指南请参考 [CLAUDE.md](./CLAUDE.md)，包含：
- 包结构说明
- 核心开发任务
- 架构详解
- 性能优化指南

## 许可证

本项目遵循相应的开源许可证。详情请查看 LICENSE 文件。