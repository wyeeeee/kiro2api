# kiro2api

一个基于 Go 的高性能 HTTP 代理服务器，提供 Anthropic Claude API 和 OpenAI 兼容的 API 接口，桥接 AWS CodeWhisperer 服务。支持多模态图片输入、实时流式响应、智能请求分析和完整的工具调用功能。

**核心架构**: 双API代理服务器，在 Anthropic API、OpenAI ChatCompletion API 和 AWS CodeWhisperer EventStream 格式之间进行转换。

## 功能特性

- **双API代理**：同时支持 Anthropic Claude API 和 OpenAI ChatCompletion API 格式
- **企业级Token管理**：智能选择策略、原子缓存、并发控制、使用限制监控
- **多认证支持**：支持 Social（默认）和 IdC 两种认证方式，JSON配置 + 传统环境变量兼容
- **智能选择策略**：最优使用和均衡使用策略，基于使用量的智能选择
- **原子缓存系统**：热点token的无锁访问，冷热分离二级缓存
- **实时使用监控**：VIBE资源使用量监控，预警和自动切换
- **工具调用支持**：完整的Anthropic工具使用格式，基于 `tool_use_id` 的精确去重逻辑
- **多模态处理**：支持图片输入，自动格式转换，支持PNG、JPEG、GIF、WebP等格式
- **实时流式响应**：自定义 EventStream 解析器，零延迟流式传输
- **智能请求分析**：自动分析请求复杂度，动态调整超时时间
- **高性能优化**：对象池模式、原子操作缓存、并发优化

## 技术栈

- **Web框架**: gin-gonic/gin v1.10.1
- **JSON处理**: bytedance/sonic v1.14.0  
- **配置管理**: github.com/joho/godotenv v1.5.1
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
# 编辑 .env 文件，设置 KIRO_AUTH_TOKEN 或 AWS_REFRESHTOKEN

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

### Token配置（两种方式，选择其一）

#### 方式一：JSON配置（推荐）

> 📖 **详细配置说明**: 更多配置示例和说明请参考 [`config-examples/kiro_auth_token_examples.md`](./config-examples/kiro_auth_token_examples.md)  
> 🚀 **快速开始**: 可复制的配置模板请参考 [`config-examples/quick-start-templates.md`](./config-examples/quick-start-templates.md)

```bash
# 新的JSON格式配置，支持多认证方式和多token
KIRO_AUTH_TOKEN='[
  {
    "auth": "Social",
    "refreshToken": "your_social_refresh_token_here"
  },
  {
    "auth": "IdC",
    "refreshToken": "your_idc_refresh_token_here",
    "clientId": "your_idc_client_id",
    "clientSecret": "your_idc_client_secret"
  }
]'

KIRO_CLIENT_TOKEN=123456   # API认证密钥
PORT=8080                  # 服务端口
```

#### 方式二：传统环境变量（向后兼容）

```bash
# Social 认证方式（默认）
AWS_REFRESHTOKEN=token1,token2,token3  # 支持逗号分隔多token
KIRO_CLIENT_TOKEN=123456               # API认证密钥
PORT=8080                              # 服务端口

# IdC 认证方式（可选）
AUTH_METHOD=idc
IDC_REFRESH_TOKEN=idc_token1,idc_token2  # 支持逗号分隔多token
IDC_CLIENT_ID=your_client_id
IDC_CLIENT_SECRET=your_client_secret
```

### 可选配置

```bash
# Token管理配置
USAGE_CHECK_INTERVAL=5m         # 使用状态检查间隔
TOKEN_USAGE_THRESHOLD=5         # 可用次数预警阈值
TOKEN_SELECTION_STRATEGY=balanced  # optimal(最优) 或 balanced(均衡)

# 缓存性能配置
CACHE_CLEANUP_INTERVAL=5m       # 缓存清理间隔
TOKEN_CACHE_HOT_THRESHOLD=10    # 热点缓存阈值
TOKEN_REFRESH_TIMEOUT=30s       # Token刷新超时时间

# 日志配置
LOG_LEVEL=info                  # 日志级别: debug,info,warn,error
LOG_FORMAT=json                 # 日志格式: text,json
LOG_FILE=/var/log/kiro2api.log  # 日志文件路径（可选）
LOG_CONSOLE=true                # 控制台输出开关

# 性能调优
REQUEST_TIMEOUT_MINUTES=15      # 复杂请求超时（分钟）
SIMPLE_REQUEST_TIMEOUT_MINUTES=2 # 简单请求超时（分钟）
GIN_MODE=release                # Gin模式：debug,release,test
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

### 常见问题

#### 1. Token配置和认证问题
```bash
# JSON配置方式检查
echo $KIRO_AUTH_TOKEN

# 传统环境变量检查（兼容模式）
echo $AWS_REFRESHTOKEN
echo $IDC_REFRESH_TOKEN
echo $IDC_CLIENT_ID

# 启用调试日志查看token管理详情
LOG_LEVEL=debug ./kiro2api
```

**常见错误解决：**
- `JSON配置格式错误`: 验证KIRO_AUTH_TOKEN的JSON格式是否正确
- `认证方式不匹配`: 确认Auth字段为"Social"或"IdC"
- `IdC认证缺少参数`: IdC方式需要ClientId和ClientSecret
- `Token已过期`: 查看日志中的刷新尝试和失败信息
- `使用限制达到`: 检查VIBE资源使用量和限制
- `并发刷新冲突`: 查看刷新管理器的并发控制日志

#### 2. 流式响应中断
```bash
# 检查客户端连接
curl -N --max-time 60 -X POST http://localhost:8080/v1/messages \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer 123456" \
  -d '{"model": "claude-sonnet-4-20250514", "stream": true, "messages": [...]}'
```

#### 3. 性能问题诊断
```bash
# 开启调试日志
LOG_LEVEL=debug ./kiro2api

# 监控HTTP连接
# 检查日志中的 "http_client" 和 "request_analyzer" 条目
```

#### 4. API认证问题
```bash
# 验证认证头格式
curl -v -H "Authorization: Bearer your_token" http://localhost:8080/v1/models
# 或使用 x-api-key
curl -v -H "x-api-key: your_token" http://localhost:8080/v1/models
```

## 开发指南

详细的开发指南请参考 [CLAUDE.md](./CLAUDE.md)，包含：
- 包结构说明
- 核心开发任务
- 架构详解
- 性能优化指南

## 许可证

本项目遵循相应的开源许可证。详情请查看 LICENSE 文件。