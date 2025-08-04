# kiro2api

一个基于 Go 的高性能 API 代理服务器，提供 Anthropic Claude API 和 OpenAI 兼容的 API 接口，桥接 AWS CodeWhisperer 服务。支持多模态图片输入、实时流式响应、智能请求分析和完整的工具调用功能。

**当前版本**: v2.8.1 - 修复工具调用中 `tool_result` 嵌套内容结构处理问题，彻底解决多模态环境下的 "Improperly formed request" 错误，优化调试日志系统。

## 功能特性

- **多格式API支持**：同时支持 Anthropic Claude API 和 OpenAI ChatCompletion API 格式
- **完整工具调用支持**：支持Anthropic工具使用格式，包括tool_choice参数和基于 `tool_use_id` 的精确去重逻辑
- **多模态图片支持**：完整支持图片输入，自动转换OpenAI `image_url`格式到Anthropic `image`格式，支持PNG、JPEG、GIF、WebP、BMP等格式
- **实时流式响应**：自定义 AWS EventStream 解析器，提供零延迟的流式体验
- **高性能架构**：基于 gin-gonic/gin 框架，使用 bytedance/sonic 高性能 JSON 库
- **智能认证管理**：基于环境变量的认证管理，支持.env文件，自动刷新过期令牌
- **Token池管理**：支持多个refresh token轮换使用，提供故障转移和负载均衡
- **智能请求分析**：自动分析请求复杂度，动态调整超时时间和客户端配置
- **结构化日志系统**：采用JSON格式输出，支持环境变量配置日志级别
- **增强验证机制**：完善的图片格式验证、请求内容验证和工具结果解析机制
- **完善的中间件**：统一的认证、CORS 和日志处理
- **容器化支持**：提供 Dockerfile 和 docker-compose.yml，支持多平台构建和容器化部署
- **高性能优化**：对象池模式、原子操作Token缓存、热点数据无锁访问
- **监控支持**：内置性能指标、健康检查和pprof性能分析端点

## 技术栈

- **Web框架**: gin-gonic/gin v1.10.1
- **JSON处理**: bytedance/sonic v1.14.0（高性能JSON库）
- **环境变量**: github.com/joho/godotenv v1.5.1
- **Go版本**: 1.23.3
- **流式解析**: 自定义 AWS EventStream 二进制协议解析器
- **并发优化**: sync.Pool, sync.Map, 原子操作
- **HTTP**: 标准 net/http 与共享客户端池

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
export PORT="8080"
./kiro2api

# 使用 .env 文件配置
cp .env.example .env
# 编辑 .env 文件设置你的配置
./kiro2api
```

### 使用 Docker

```bash
# 方式一：使用预构建镜像
docker run -d \
  --name kiro2api \
  -p 8080:8080 \
  -e AWS_REFRESHTOKEN="your_refresh_token" \
  -e KIRO_CLIENT_TOKEN="123456" \
  ghcr.io/caidaoli/kiro2api:latest

# 方式二：本地构建镜像
docker build -t kiro2api .
docker run -d \
  --name kiro2api \
  -p 8080:8080 \
  -e AWS_REFRESHTOKEN="your_refresh_token" \
  -e KIRO_CLIENT_TOKEN="123456" \
  kiro2api

# 方式三：使用 docker-compose
docker-compose up -d
```

## API 接口

### 支持的端点

- `POST /v1/messages` - Anthropic Claude API 兼容接口（支持流式和非流式）
- `POST /v1/chat/completions` - OpenAI ChatCompletion API 兼容接口（支持流式和非流式）
- `GET /v1/models` - 获取可用模型列表
- `GET /health` - 健康检查（无需认证）
- `GET /metrics` - 性能指标监控（无需认证）
- `GET /stats/*` - 各组件统计信息（无需认证）
- `GET /debug/pprof/*` - 性能分析端点（无需认证）

### 认证方式

所有 API 端点（除 `/health`）都需要在请求头中提供认证信息：

```bash
# 使用 Authorization Bearer 认证
Authorization: Bearer your-auth-token

# 或使用 x-api-key 认证
x-api-key: your-auth-token
```

### 在 Claude Code 中使用

```bash
export ANTHROPIC_AUTH_TOKEN=123456
export ANTHROPIC_BASE_URL=http://localhost:8080
```

### 在其他应用中使用

```bash
# 作为 Anthropic API 使用
curl -X POST http://localhost:8080/v1/messages \
  -H "Authorization: Bearer 123456" \
  -H "Content-Type: application/json" \
  -d '{"model": "claude-sonnet-4-20250514", "max_tokens": 1000, "messages": [{"role": "user", "content": "Hello"}]}'

# 作为 OpenAI API 使用
curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Authorization: Bearer 123456" \
  -H "Content-Type: application/json" \
  -d '{"model": "claude-sonnet-4-20250514", "messages": [{"role": "user", "content": "Hello"}]}'
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

#### 工具调用示例

```bash
# Anthropic API工具调用格式
curl -X POST http://localhost:8080/v1/messages \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer 123456" \
  -d '{
    "model": "claude-sonnet-4-20250514",
    "max_tokens": 1000,
    "messages": [
      {"role": "user", "content": "请搜索人工智能的信息"}
    ],
    "tools": [
      {
        "name": "web_search",
        "description": "Search the web for information",
        "input_schema": {
          "type": "object",
          "properties": {
            "query": {
              "type": "string",
              "description": "The search query"
            }
          },
          "required": ["query"]
        }
      }
    ],
    "tool_choice": {"type": "auto"}
  }'

# OpenAI API工具调用格式
curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer 123456" \
  -d '{
    "model": "claude-sonnet-4-20250514",
    "messages": [
      {"role": "user", "content": "执行系统命令"}
    ],
    "tools": [
      {
        "type": "function",
        "function": {
          "name": "bash",
          "description": "Execute bash commands",
          "parameters": {
            "type": "object",
            "properties": {
              "command": {
                "type": "string",
                "description": "The command to execute"
              }
            },
            "required": ["command"]
          }
        }
      }
    ],
    "tool_choice": "auto"
  }'
```

#### 多模态图片输入示例

kiro2api 完全支持多模态图片输入，提供完整的图片处理和格式转换功能，自动在OpenAI和Anthropic格式之间转换：

```bash
# OpenAI格式的图片输入
curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer 123456" \
  -d '{
    "model": "claude-3-7-sonnet-20250219",
    "messages": [
      {
        "role": "user",
        "content": [
          {
            "type": "text",
            "text": "图片上是什么内容？"
          },
          {
            "type": "image_url",
            "image_url": {
              "url": "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg=="
            }
          }
        ]
      }
    ]
  }'

# Anthropic格式的图片输入
curl -X POST http://localhost:8080/v1/messages \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer 123456" \
  -d '{
    "model": "claude-3-7-sonnet-20250219",
    "max_tokens": 1000,
    "messages": [
      {
        "role": "user",
        "content": [
          {
            "type": "text",
            "text": "请分析这张图片"
          },
          {
            "type": "image",
            "source": {
              "type": "base64",
              "media_type": "image/png",
              "data": "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg=="
            }
          }
        ]
      }
    ]
  }'
```

**支持的图片格式**：
- PNG (`image/png`)
- JPEG (`image/jpeg`) 
- GIF (`image/gif`)
- WebP (`image/webp`)
- BMP (`image/bmp`)

**图片处理特性**：
- 最大文件大小：20MB
- 支持data URL格式（`data:image/png;base64,...`）
- 自动格式检测和验证
- 二进制文件头检测，确保格式准确性
- Base64编码验证和大小限制检查
- 格式声明与实际内容一致性验证

## 工具调用支持

kiro2api完全支持Anthropic和OpenAI格式的工具调用：

### 支持的功能
- **工具定义**：支持完整的工具schema定义和参数验证
- **tool_choice参数**：支持"auto"、"any"、"tool"、"none"等工具选择策略
- **精确去重**：基于 `tool_use_id` 的标准去重机制，符合 Anthropic 最佳实践
- **流式工具调用**：支持在流式响应中处理工具调用事件
- **错误处理**：完善的工具调用错误处理和调试支持

### 常用工具示例
- `web_search` - 网络搜索工具
- `bash` - 系统命令执行工具
- `file_operations` - 文件操作工具
- 自定义工具 - 支持任意自定义工具定义

## 支持的模型

当前支持的模型映射：

| 公开模型名称 | 内部 CodeWhisperer 模型 ID | 说明 |
|-------------|---------------------------|------|
| `claude-sonnet-4-20250514` | `CLAUDE_SONNET_4_20250514_V1_0` | 最新 Claude-4 Sonnet 模型 |
| `claude-3-7-sonnet-20250219` | `CLAUDE_3_7_SONNET_20250219_V1_0` | Claude-3.7 Sonnet 模型 |
| `claude-3-5-haiku-20241022` | `CLAUDE_3_5_HAIKU_20241022_V1_0` | Claude-3.5 Haiku 模型 |

### 获取模型列表

```bash
curl -X GET http://localhost:8080/v1/models \
  -H "Authorization: Bearer 123456"
```

## 配置管理

### 环境变量配置

程序完全基于环境变量进行配置，支持以下变量：

```bash
# 必需配置
export AWS_REFRESHTOKEN="your_refresh_token"  # 必需设置，支持多个token用逗号分隔

# 可选配置
export KIRO_CLIENT_TOKEN="your_token"         # 客户端认证token（默认：123456）
export PORT="8080"                            # 服务端口（默认：8080）
export LOG_LEVEL="info"                       # 日志级别：debug, info, warn, error（默认：info）
export LOG_FORMAT="json"                      # 日志格式：text, json（默认：json）
export LOG_FILE="/path/to/log/file"           # 日志文件路径（可选）
export LOG_CONSOLE="true"                     # 是否输出到控制台（默认：true）

# 功能控制
export DISABLE_STREAM="false"                 # 是否禁用流式响应（默认：false）
export GIN_MODE="release"                     # Gin模式：debug, release, test（默认：release）

# 超时配置
export REQUEST_TIMEOUT_MINUTES="15"           # 复杂请求超时（默认：15分钟）
export SIMPLE_REQUEST_TIMEOUT_MINUTES="2"    # 简单请求超时（默认：2分钟）
export SERVER_READ_TIMEOUT_MINUTES="16"      # 服务器读取超时（默认：16分钟）
export SERVER_WRITE_TIMEOUT_MINUTES="16"     # 服务器写入超时（默认：16分钟）
```

### .env 文件支持

可以在项目根目录创建 `.env` 文件进行配置：

```bash
# 复制示例配置文件
cp .env.example .env

# 编辑配置文件
KIRO_CLIENT_TOKEN=123456
PORT=8080
AWS_REFRESHTOKEN=your_refresh_token_here
LOG_LEVEL=info
LOG_FORMAT=json
LOG_FILE=/var/log/kiro2api.log
LOG_CONSOLE=true
GIN_MODE=release
REQUEST_TIMEOUT_MINUTES=15
SIMPLE_REQUEST_TIMEOUT_MINUTES=2
SERVER_READ_TIMEOUT_MINUTES=16
SERVER_WRITE_TIMEOUT_MINUTES=16
DISABLE_STREAM=false
```

### Token 池管理

kiro2api 支持高级的 Token 池管理功能：

- **多Token支持**：支持多个refresh token，用逗号分隔配置
- **自动轮换**：智能轮换使用不同的 token，提供负载均衡
- **故障转移**：当一个token失败时自动切换到下一个可用token
- **失败重试**：每个token最多重试3次，超过限制后自动跳过
- **智能缓存**：基于token自身过期时间进行缓存管理，避免不必要的刷新
- **自动刷新**：当收到403错误时自动刷新token

#### Token池配置示例

```bash
# 单个token
export AWS_REFRESHTOKEN="token1"

# 多个token（推荐）
export AWS_REFRESHTOKEN="token1,token2,token3"
```

### 智能请求分析

系统会自动分析每个请求的复杂度并优化处理：

- **复杂度评估**：根据以下因素评估请求复杂度：
  - Token数量（>4000为复杂）
  - 内容长度（>10K字符为复杂）
  - 工具使用情况（有工具调用为复杂）
  - 系统提示长度（>2K字符增加复杂度）
  - 关键词检测（包含"分析"、"详细"等关键词）

- **动态超时配置**：
  - 复杂请求：15分钟超时
  - 简单请求：2分钟超时
  - 服务器读写超时：16分钟

- **客户端选择**：自动为不同复杂度的请求选择合适的HTTP客户端
- **性能优化**：基于请求特征进行智能资源分配

#### Token刷新接口

内部使用的刷新URL：`https://prod.us-east-1.auth.desktop.kiro.dev/refreshToken`

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
├── main.go                      # 程序入口，初始化日志和服务器
├── .env.example                 # 环境变量配置示例
├── Dockerfile                   # Docker 构建文件
├── docker-compose.yml           # Docker Compose 配置
├── auth/
│   └── token.go                 # Token管理，支持多token池和自动轮换
├── config/
│   └── config.go                # 配置常量，模型映射和API端点
├── converter/
│   └── converter.go             # API格式转换，支持OpenAI↔Anthropic↔CodeWhisperer
├── logger/
│   └── logger.go                # 结构化日志系统，支持JSON格式和环境变量配置
├── parser/
│   └── sse_parser.go            # AWS EventStream二进制协议解析器
├── server/
│   ├── server.go                # HTTP服务器和路由配置
│   ├── handlers.go              # Anthropic API处理器
│   ├── openai_handlers.go       # OpenAI API处理器
│   ├── middleware.go            # 认证和CORS中间件
│   └── common.go                # 共享HTTP工具和错误处理
├── types/
│   ├── anthropic.go             # Anthropic API数据结构
│   ├── openai.go                # OpenAI API数据结构
│   ├── codewhisperer.go         # CodeWhisperer API数据结构
│   ├── token.go                 # Token管理相关结构
│   ├── model.go                 # 模型定义结构
│   └── common.go                # 通用数据结构
└── utils/
    ├── client.go                # HTTP客户端管理
    ├── http.go                  # HTTP响应处理工具
    ├── image.go                 # 图片处理和格式转换工具
    ├── message.go               # 消息内容提取工具，支持多模态内容
    ├── request_analyzer.go      # 请求复杂度分析
    ├── tool_dedup.go            # 工具调用去重管理
    └── uuid.go                  # UUID生成工具
```

## 版本历史

### v2.8.1 - 最新版本

**多模态图片处理优化**：
- 修复工具调用中 `tool_result` 嵌套内容结构处理问题
- 彻底解决多模态环境下的 "Improperly formed request" 错误
- 优化调试日志系统，提供更详细的错误信息
- 增强图片处理管道的稳定性和兼容性

### v2.8.0 - 多模态图片支持

**新增功能**：
- 完整图片处理管道：支持PNG、JPEG、GIF、WebP、BMP等格式
- OpenAI↔Anthropic格式自动转换
- data URL支持和严格验证机制
- 图片大小限制（20MB）和编码完整性检查

### v2.7.0+ - 结构化日志系统

**日志系统重构**：
- JSON格式输出，便于日志分析和监控集成
- 简化架构，移除复杂的writer接口
- 多输出支持：控制台、文件或同时输出
- 性能优化：原子操作、对象池、可配置调用栈获取

### v2.5.3 - 工具调用去重标准化

**符合Anthropic最佳实践**：
- 从`name+input`哈希改为基于`tool_use_id`的标准去重
- 精确识别，避免参数哈希误判
- 流式和非流式处理统一去重逻辑

## 架构说明

### 双API代理架构

kiro2api是一个**双API代理服务器**，在三种格式之间进行转换：
- **输入**: Anthropic Claude API 或 OpenAI ChatCompletion API 
- **输出**: AWS CodeWhisperer EventStream 格式
- **响应**: 实时流式或标准 JSON 响应

### 请求处理流程

1. **接收请求** - gin 路由器接收 HTTP 请求
2. **认证验证** - PathBasedAuthMiddleware 验证 API 密钥
3. **请求分析** - 智能分析请求复杂度，选择合适的客户端和超时配置
4. **格式转换** - converter 包将请求转换为 CodeWhisperer 格式
5. **Token管理** - 从Token池获取可用的access token，支持自动轮换
6. **代理转发** - 转发到 AWS CodeWhisperer API
7. **响应解析** - StreamParser 实时解析 AWS EventStream 二进制数据
8. **格式转换** - 将响应转换回客户端请求的格式（Anthropic/OpenAI）
9. **返回响应** - 以流式或非流式方式返回给客户端

### 高性能特性

- **零延迟流式**: 使用滑动窗口缓冲区的自定义 EventStream 解析器
- **智能Token管理**: 多token池支持，自动轮换和故障转移
- **请求复杂度分析**: 根据请求特征动态调整超时和客户端配置
- **对象池优化**: `sync.Pool` 复用StreamParser和字节缓冲区，减少GC压力
- **原子操作缓存**: Token缓存使用热点+冷缓存两级架构，最常用token无锁访问
- **高性能JSON**: 使用bytedance/sonic替代标准库，提升处理性能
- **并发安全设计**: 读写分离、无锁设计、后台清理机制

### 核心功能模块

- **多模态图片处理**: 完整的图片处理管道，自动转换OpenAI `image_url`格式到Anthropic `image`格式
- **精确工具去重**: 基于 `tool_use_id` 的工具调用去重，符合 Anthropic 标准
- **结构化日志**: JSON格式日志输出，便于监控和分析
- **增强验证机制**: 全面的请求验证、工具结果解析和图片内容验证
- **统一中间件**: 集中式的认证、CORS 和错误处理
- **容错设计**: 自动令牌刷新和优雅的错误处理

## Docker 部署

### 使用 docker-compose（推荐）

1. 创建 `.env` 文件：
```bash
cp .env.example .env
# 编辑 .env 文件，设置必要的环境变量
```

2. 启动服务：
```bash
docker-compose up -d
```

3. 查看日志：
```bash
docker-compose logs -f kiro2api
```

4. 停止服务：
```bash
docker-compose down
```

### 健康检查

容器支持健康检查，通过 `/health` 端点监控服务状态：
```bash
# 检查容器健康状态
docker ps
docker inspect kiro2api | grep -A 10 "Health"
```

## 日志系统

项目采用结构化日志系统，支持JSON格式输出：

### 日志配置
- **LOG_LEVEL**: 设置日志级别（debug, info, warn, error）
- **LOG_FORMAT**: 设置日志格式（text, json）
- **LOG_FILE**: 设置日志文件路径（可选）
- **LOG_CONSOLE**: 是否同时输出到控制台（默认：true）

### 日志示例
```json
{
  "timestamp": "2024-01-01T12:00:00.000Z",
  "level": "INFO",
  "message": "启动Anthropic API代理服务器",
  "file": "server.go:188",
  "port": "8080"
}
```

## 故障排除

### 常见问题和解决方案

#### "Improperly formed request" 错误
这个错误通常出现在工具调用场景中，特别是多模态请求：

1. **检查工具结果内容**：确认 `tool_result` 内容块是否包含嵌套结构
2. **启用调试日志**：设置 `LOG_LEVEL=debug` 获取详细信息
3. **检查消息内容处理**：查看日志中的内容提取状态
4. **验证JSON序列化**：确保使用正确的序列化方法

**修复历史**：v2.8.1 版本已修复 `tool_result` 嵌套内容结构处理问题。

#### Token刷新失败
1. 检查 `AWS_REFRESHTOKEN` 环境变量是否正确设置
2. 验证token池配置和轮换机制
3. 查看token过期时间和自动刷新日志
4. 确保token格式正确且未过期

#### 流式响应中断
1. 检查客户端连接稳定性
2. 验证 EventStream 解析器状态
3. 查看工具调用去重逻辑是否影响流式输出
4. 检查网络超时配置

#### 图片处理错误
1. 确认图片格式是否支持（PNG、JPEG、GIF、WebP、BMP）
2. 检查图片大小是否超过20MB限制
3. 验证base64编码是否正确
4. 查看图片格式检测日志

### 监控和调试

#### 性能监控端点
- **Token池监控**: `GET /stats/token-pool` - token状态和缓存命中率
- **性能指标**: `GET /metrics` - 请求处理时间和资源使用
- **健康检查**: `GET /health` - 服务可用性状态
- **性能分析**: `GET /debug/pprof/*` - Go pprof性能分析

#### 日志调试
```bash
# 启用详细日志
export LOG_LEVEL=debug
export LOG_FORMAT=json
export LOG_FILE=/var/log/kiro2api.log
export LOG_CONSOLE=true

# 启动服务
./kiro2api
```

## 贡献指南

### 本地开发环境搭建

1. **克隆项目**：
```bash
git clone <repository-url>
cd kiro2api
```

2. **安装依赖**：
```bash
go mod download
```

3. **配置环境变量**：
```bash
cp .env.example .env
# 编辑 .env 文件，设置必要的配置
```

4. **运行测试**：
```bash
go test ./...
```

5. **启动开发服务器**：
```bash
go run main.go
```

### 代码规范

- 遵循 Go 官方代码规范
- 使用 `go fmt` 格式化代码
- 使用 `go vet` 进行静态检查
- 为新功能编写测试
- 更新相关文档

### 提交规范

- 使用清晰的commit信息
- 每个commit只包含一个逻辑变更
- 在提交前运行测试
- 更新版本信息和CHANGELOG

## 支持和文档

- **项目文档**: 查看 `CLAUDE.md` 了解详细的开发指南
- **问题反馈**: 通过 GitHub Issues 报告问题
- **功能请求**: 通过 GitHub Issues 提交功能建议
- **代码贡献**: 欢迎提交 Pull Request

## 性能基准

### 典型性能指标

- **响应时间**: < 100ms (简单请求)
- **并发支持**: > 1000 并发连接
- **内存使用**: < 100MB (空闲状态)
- **流式延迟**: < 50ms (首字符延迟)

### 优化建议

- 使用多个refresh token提升可用性
- 合理配置超时时间避免资源浪费
- 启用JSON格式日志便于性能分析
- 监控Token缓存命中率和清理效果

## 许可证

本项目遵循相应的开源许可证。详情请查看 LICENSE 文件。