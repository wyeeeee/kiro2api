# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## 项目概述

`kiro2api` 是一个基于 Go 的高性能 HTTP 代理服务器，提供 Anthropic Claude API 和 OpenAI 兼容的 API 接口，桥接 AWS CodeWhisperer 服务。支持多模态图片输入、实时流式响应、智能请求分析和完整的工具调用功能。

**核心架构**: 双API代理服务器，在 Anthropic API、OpenAI ChatCompletion API 和 AWS CodeWhisperer EventStream 格式之间进行转换。

## 快速开始

```bash
# 编译程序
go build -o kiro2api main.go

# 启动默认服务器 (端口 8080, 认证令牌 "123456")
./kiro2api

# 使用环境变量配置启动
export AWS_REFRESHTOKEN="your_refresh_token"  # 必需设置
export KIRO_CLIENT_TOKEN="your_token"
export PORT="8080"
./kiro2api

# 或使用 .env 文件配置
cp .env.example .env
# 编辑 .env 文件设置你的配置
./kiro2api

# 测试 API (使用默认认证)
curl -X POST http://localhost:8080/v1/messages \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer 123456" \
  -d '{"model": "claude-sonnet-4-20250514", "max_tokens": 100, "messages": [{"role": "user", "content": "你好"}]}'
```

## 开发命令

```bash
# 构建
go build -o kiro2api main.go

# 测试
go test ./...

# 运行特定包测试
go test ./parser -v
go test ./test -v

# 性能基准测试
go test ./test -bench=BenchmarkRealIntegration -benchmem

# 代码质量检查
go vet ./...
go fmt ./...

# 依赖整理
go mod tidy

# 开发模式运行 (调试模式，详细日志)
GIN_MODE=debug ./kiro2api

# Docker 测试
docker-compose up -d

# 生产构建
go build -ldflags="-s -w" -o kiro2api main.go
```

## 工具解析测试

```bash
# 使用.env文件启动服务器进行工具解析测试
# 确保.env文件中配置了有效的AWS_REFRESHTOKEN
./kiro2api

# 测试工具调用（在另一个终端）
curl -X POST http://localhost:8080/v1/messages \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer 123456" \
  -d '{
    "model": "claude-sonnet-4-20250514",
    "max_tokens": 1000,
    "messages": [{
      "role": "user",
      "content": "我正在对你进行debug我要求在一次调用1个工具，调用工具生成一个Output文件夹紧接着生成一个output.txt 然后在里面写上一首诗20字"
    }]
  }'

# 查看应用日志（根据LOG_FILE配置）
tail -f /var/log/kiro2api.log
```

## 应用程序命令

```bash
# 启动代理服务器（优先使用环境变量配置）
./kiro2api [端口]

# 示例：
./kiro2api                    # 使用默认配置或.env文件配置
./kiro2api 8080              # 指定端口（环境变量PORT优先级更高）

# 环境变量配置：
export AWS_REFRESHTOKEN="token1,token2,token3"  # 多token支持（必需）
export KIRO_CLIENT_TOKEN="your_token"          # 客户端认证token (默认: 123456)
export PORT="8080"                              # 服务端口（默认: 8080）
export LOG_LEVEL="info"                         # 日志级别（默认: info）
export LOG_FORMAT="json"                        # 日志格式（默认: json）
export GIN_MODE="release"                       # Gin模式（默认: release）
./kiro2api
```

## 架构概述

### 核心请求流程
1. **认证**: `PathBasedAuthMiddleware` 验证 API 密钥（Authorization 或 x-api-key）
2. **请求分析**: `utils.AnalyzeRequestComplexity()` 分析请求复杂度
3. **客户端选择**: `utils.DoSmartRequest()` 智能选择 HTTP 客户端
4. **格式转换**: `converter/` 包将请求转换为 CodeWhisperer 格式
5. **代理**: 转发到 AWS CodeWhisperer API
6. **流处理**: `StreamParser` 处理实时 AWS EventStream 二进制解析
7. **响应转换**: 转换回客户端请求的格式

### 关键架构模式

**智能请求处理**:
- 根据复杂度选择客户端：简单请求(2分钟超时) vs 复杂请求(15分钟超时)
- 复杂度评估因素：MaxTokens (>4000)、内容长度 (>10K字符)、工具使用、系统提示长度

**Token池管理**:
- 支持多个refresh token的池化管理和自动轮换
- 智能故障转移，每个token最多重试3次
- 原子操作的Token缓存，减少锁竞争

**工具调用去重**:
- 基于 `tool_use_id` 的精确去重机制
- 流式和非流式请求统一去重逻辑
- 请求级别的去重管理，防止跨请求状态污染

**流处理架构**:
- 使用滑动窗口缓冲区的 `StreamParser` 进行实时 EventStream 解析
- 支持零延迟流式传输
- 对象池优化：复用解析器实例

## 包结构

**`server/`** - HTTP 服务器和 API 处理器
- `server.go`: 路由配置、服务器启动、中间件链
- `handlers.go`: Anthropic API 端点 (`/v1/messages`) 
- `openai_handlers.go`: OpenAI 兼容性 (`/v1/chat/completions`, `/v1/models`)
- `middleware.go`: 统一 API 密钥验证中间件
- `common.go`: 共享 HTTP 工具和错误处理

**`converter/`** - 格式转换层
- 处理 Anthropic ↔ OpenAI ↔ CodeWhisperer 请求/响应转换
- 通过 `config.ModelMap` 进行模型名称映射
- 工具调用格式转换

**`parser/`** - 流处理
- `robust_parser.go`: 增强的 AWS EventStream 解析器，支持环形缓冲区和错误恢复
- `compliant_message_processor.go`: 符合标准的消息处理器，支持工具调用去重
- `compliant_event_stream_parser.go`: 符合标准的EventStream解析器
- `compliant_parser_pool.go`: 解析器对象池
- `header_parser.go`: EventStream头部解析器
- `ring_buffer.go`: 环形缓冲区实现
- `session_manager.go`: 会话管理器
- `tool_call_fsm.go`: 工具调用状态机
- `validation_rules.go`: 验证规则
- `xml_tool_parser.go`: XML工具解析器
- 将 EventStream 事件转换为客户端的 SSE 格式

**`auth/`** - 令牌管理  
- 支持两种认证方式：Social（默认）和 IdC 认证
- 通过 `RefreshTokenForServer()` 在 403 错误时自动刷新
- IdC 认证使用 OIDC 协议进行 token 刷新
- 使用`utils.SharedHTTPClient`进行HTTP请求

**`utils/`** - 集中化工具包
- `client.go`: HTTP客户端管理和智能选择
- `request_analyzer.go`: 请求复杂度分析
- `tool_dedup.go`: 工具调用去重管理器
- `image.go`: 多模态图片处理工具
- `atomic_cache.go`: 原子操作的Token缓存
- `json.go`: 高性能JSON序列化工具
- `logger.go`: 增强的日志系统，支持多级别和格式化输出
- `raw_data_saver.go`: 调试数据保存工具

**`types/`** - 数据结构定义
- `anthropic.go`: Anthropic API 请求/响应结构
- `openai.go`: OpenAI API 格式结构  
- `codewhisperer.go`: AWS CodeWhisperer API 结构
- `token.go`: 统一token管理结构

**`logger/`** - 结构化日志系统
- 支持环境变量配置：`LOG_LEVEL`, `LOG_FORMAT`, `LOG_FILE`, `LOG_CONSOLE`
- 原子操作优化，支持结构化字段和JSON格式输出

**`config/`** - 配置常量
- `ModelMap`: 将公开模型名称映射到内部 CodeWhisperer 模型 ID
- `RefreshTokenURL`: Kiro 令牌刷新端点

## API 端点

- `POST /v1/messages` - Anthropic Claude API 代理（流式 + 非流式）
- `POST /v1/chat/completions` - OpenAI ChatCompletion API 代理（流式 + 非流式）  
- `GET /v1/models` - 返回可用模型列表

## 环境变量配置

项目支持两种认证方式：Social（默认）和 IdC 认证。使用 `.env` 文件进行环境配置：

```bash
# 复制示例配置
cp .env.example .env

# 认证方式配置
# AUTH_METHOD=social                     # 认证方式：social(默认) 或 idc

# Social 认证方式（默认）
# AWS_REFRESHTOKEN=your_refresh_token_here  # 必需设置

# IdC 认证方式
# AUTH_METHOD=idc
# IDC_CLIENT_ID=your_client_id
# IDC_CLIENT_SECRET=your_client_secret  
# IDC_REFRESH_TOKEN=your_idc_refresh_token

# 其他环境变量:
# PORT=8080                              # 服务端口（默认: 8080）
# KIRO_CLIENT_TOKEN=123456               # 客户端认证token（默认: 123456）
# LOG_LEVEL=info                         # 日志级别：debug,info,warn,error
# LOG_FORMAT=json                        # 日志格式：text,json
# GIN_MODE=release                       # Gin模式：debug,release,test
# REQUEST_TIMEOUT_MINUTES=15             # 复杂请求超时（默认: 15）
# SIMPLE_REQUEST_TIMEOUT_MINUTES=2       # 简单请求超时（默认: 2）
# SERVER_READ_TIMEOUT_MINUTES=16         # 服务器读取超时（默认: 16）
# SERVER_WRITE_TIMEOUT_MINUTES=16        # 服务器写入超时（默认: 16）
# DISABLE_STREAM=false                   # 是否禁用流式响应（默认: false）
```

## Claude Code 测试配置

本章节专门为使用 Claude Code (claude.ai/code) 进行开发测试提供配置指南。

### 测试环境变量配置

创建测试环境的 `.env` 配置：

```bash
# 复制示例配置作为测试配置
cp .env.example .env

# 编辑 .env 文件，根据需要修改以下配置：

# ============================================================================
# 测试环境认证配置
# ============================================================================

# Social 认证方式（默认）
AUTH_METHOD=social
AWS_REFRESHTOKEN=your_test_refresh_token_here

# 或使用 IdC 认证方式
# AUTH_METHOD=idc
# IDC_CLIENT_ID=your_test_client_id
# IDC_CLIENT_SECRET=your_test_client_secret
# IDC_REFRESH_TOKEN=your_test_refresh_token

# ============================================================================
# 测试环境基础配置
# ============================================================================

# 测试服务端口
PORT=8080

# 测试认证token
KIRO_CLIENT_TOKEN=123456

# ============================================================================
# 调试配置
# ============================================================================

# 开发调试模式
GIN_MODE=debug

# 详细日志级别
LOG_LEVEL=debug

# 易读的文本格式日志
LOG_FORMAT=text

# ============================================================================
# 超时配置（可根据测试需要调整）
# ============================================================================

# 缩短超时时间以加快测试
REQUEST_TIMEOUT_MINUTES=5
SIMPLE_REQUEST_TIMEOUT_MINUTES=1
SERVER_READ_TIMEOUT_MINUTES=6
SERVER_WRITE_TIMEOUT_MINUTES=6

# 测试时可选择禁用流式响应
# DISABLE_STREAM=true
```

### Claude Code 开发工作流

#### 1. 启动测试服务器

```bash
# 使用 .env 配置文件启动服务器
./kiro2api

# 验证服务器启动
curl -X GET http://localhost:8080/v1/models \
  -H "Authorization: Bearer 123456"
```

#### 2. Claude Code 测试端点配置

在 Claude Code 中使用以下配置进行测试：

```bash
# API 基础 URL
BASE_URL=http://localhost:8080

# 测试认证
AUTHORIZATION="Bearer 123456"

# 或使用 x-api-key 头
API_KEY="123456"
```

#### 3. 功能验证命令

```bash
# 测试简单对话请求
curl -X POST http://localhost:8080/v1/messages \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer 123456" \
  -d '{
    "model": "claude-sonnet-4-20250514",
    "max_tokens": 100,
    "messages": [{"role": "user", "content": "你好，这是一个测试"}]
  }' | jq .

# 测试 OpenAI 兼容性
curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer 123456" \
  -d '{
    "model": "claude-sonnet-4-20250514",
    "max_tokens": 100,
    "messages": [{"role": "user", "content": "测试 OpenAI 格式"}]
  }' | jq .
```

#### 4. 流式响应测试

```bash
# 测试流式 Anthropic API
curl -N -X POST http://localhost:8080/v1/messages \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer 123456" \
  -d '{
    "model": "claude-sonnet-4-20250514",
    "max_tokens": 200,
    "stream": true,
    "messages": [{"role": "user", "content": "请给我讲一个短故事"}]
  }'

# 测试流式 OpenAI 兼容API
curl -N -X POST http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer 123456" \
  -d '{
    "model": "claude-sonnet-4-20250514",
    "max_tokens": 200,
    "stream": true,
    "messages": [{"role": "user", "content": "请给我讲一个短故事"}]
  }'
```

### 调试配置最佳实践

#### 1. 日志配置

```bash
# 开发时使用详细日志
LOG_LEVEL=debug
LOG_FORMAT=text
LOG_CONSOLE=true

# 生产时使用结构化日志
LOG_LEVEL=info
LOG_FORMAT=json
LOG_FILE=/var/log/kiro2api.log
```

#### 2. 超时配置调优

```bash
# 开发测试：使用较短超时
REQUEST_TIMEOUT_MINUTES=5
SIMPLE_REQUEST_TIMEOUT_MINUTES=1

# 生产环境：使用标准超时
REQUEST_TIMEOUT_MINUTES=15
SIMPLE_REQUEST_TIMEOUT_MINUTES=2
```

#### 3. 性能测试配置

```bash
# 禁用流式响应进行基准测试
DISABLE_STREAM=true

# 使用生产模式进行性能测试
GIN_MODE=release
LOG_LEVEL=warn
```

### 常见测试问题排查

#### Token 认证问题
```bash
# 检查 AWS_REFRESHTOKEN 是否正确设置
echo $AWS_REFRESHTOKEN

# 验证 token 格式
curl -X POST https://prod.us-east-1.auth.desktop.kiro.dev/refreshToken \
  -H "Content-Type: application/json" \
  -d '{"refreshToken": "'"$AWS_REFRESHTOKEN"'"}'
```

#### 端口冲突问题
```bash
# 检查端口占用
lsof -i :8081

# 使用不同端口
PORT=8082 ./kiro2api
```

#### 连接问题诊断
```bash
# 检查服务器连通性
nc -zv localhost 8081

# 检查 HTTP 服务
curl -I http://localhost:8081/v1/models
```

### 测试数据和用例

#### 基础功能测试用例

```json
{
  "simple_text": {
    "model": "claude-sonnet-4-20250514",
    "max_tokens": 100,
    "messages": [{"role": "user", "content": "你好"}]
  },
  "complex_request": {
    "model": "claude-sonnet-4-20250514",
    "max_tokens": 4000,
    "system": "你是一个专业的代码审查助手",
    "messages": [
      {"role": "user", "content": "请帮我审查这段Python代码的性能问题..."}
    ]
  },
  "multimodal_test": {
    "model": "claude-sonnet-4-20250514",
    "max_tokens": 500,
    "messages": [
      {
        "role": "user",
        "content": [
          {"type": "text", "text": "这张图片显示了什么？"},
          {
            "type": "image",
            "source": {
              "type": "base64",
              "media_type": "image/jpeg",
              "data": "base64_encoded_image_data_here"
            }
          }
        ]
      }
    ]
  }
}
```

#### 工具调用测试用例

```json
{
  "tool_use_test": {
    "model": "claude-sonnet-4-20250514",
    "max_tokens": 1000,
    "tools": [
      {
        "name": "get_weather",
        "description": "获取指定城市的天气信息",
        "input_schema": {
          "type": "object",
          "properties": {
            "city": {"type": "string", "description": "城市名称"}
          },
          "required": ["city"]
        }
      }
    ],
    "messages": [
      {"role": "user", "content": "北京现在的天气怎么样？"}
    ]
  }
}
```

## 重要实现细节

**认证**: 基于路径的认证策略，支持两种认证方式
- 需要认证：`/v1/*` 开头的所有端点
- 支持 `Authorization: Bearer <token>` 或 `x-api-key: <token>`
- **Social 认证**：默认认证方式，使用 AWS_REFRESHTOKEN 环境变量
- **IdC 认证**：使用 IDC_CLIENT_ID、IDC_CLIENT_SECRET、IDC_REFRESH_TOKEN 环境变量

**模型映射**:
- `claude-sonnet-4-20250514` → `CLAUDE_SONNET_4_20250514_V1_0`
- `claude-3-7-sonnet-20250219` → `CLAUDE_3_7_SONNET_20250219_V1_0`
- `claude-3-5-haiku-20241022` → `CLAUDE_3_5_HAIKU_20241022_V1_0`

**流式传输**: 自定义二进制 EventStream 解析器
- 支持滑动窗口缓冲区，实现零延迟流式传输
- 处理 `assistantResponseEvent` 和 `toolUseEvent` 两种事件类型
- 自动转换为 Anthropic SSE 格式

**Token池配置**: 高级Token管理
- 支持多token智能轮换和故障转移
- 基于token过期时间的缓存管理
- 每个token最多重试3次后自动跳过


### 内存管理优化
- **对象池模式**: 使用 `sync.Pool` 复用 `StreamParser` 和字节缓冲区
- **预分配策略**: 缓冲区和事件切片预分配容量
- **热点数据缓存**: Token缓存使用热点+冷缓存两级架构

### 并发处理优化
- **原子操作**: 日志级别判断、Token缓存访问使用原子操作
- **读写分离**: Token池使用读写锁，缓存使用sync.Map
- **无锁设计**: 热点Token缓存使用unsafe.Pointer实现无锁访问

### 网络和I/O优化
- **共享客户端**: HTTP客户端复用，减少连接建立开销
- **流式处理**: 零延迟的流式响应，使用滑动窗口缓冲区
- **智能超时**: 根据请求复杂度动态调整超时配置

### 数据处理优化
- **高性能JSON**: 使用bytedance/sonic替代标准库
- **二进制协议**: 直接解析AWS EventStream二进制格式
- **请求分析**: 多因素复杂度评估，智能选择处理策略

## 技术栈

- **Web框架**: gin-gonic/gin v1.10.1
- **JSON处理**: bytedance/sonic v1.14.0
- **环境变量**: github.com/joho/godotenv v1.5.1
- **HTTP**: 标准 net/http 与共享客户端池
- **流式传输**: 自定义 EventStream 二进制协议解析器
- **并发优化**: sync.Pool, sync.Map, 原子操作
- **Go 版本**: 1.23.3
- **容器化**: Docker & Docker Compose 支持

## 核心开发任务

### 添加新的模型支持
1. 在 `config/config.go` 的 `ModelMap` 中添加模型映射
2. 确保 `types/model.go` 中的结构支持新模型
3. 测试新模型的请求和响应转换

### 修改认证逻辑
1. 主要逻辑在 `server/middleware.go` 的 `PathBasedAuthMiddleware`
2. Token管理和刷新逻辑在 `auth/token.go`
3. Social和IdC认证逻辑都在 `auth/token.go` 中实现
4. Token池配置在 `types/token.go`
5. Token刷新管理器在 `utils/token_refresh_manager.go`

### 调试流式响应
1. 检查 `parser/robust_parser.go` 中的 `RobustEventStreamParser` 和增强的解析逻辑
2. 验证 `parser/compliant_message_processor.go` 中的消息处理和工具调用去重
3. 确认二进制 EventStream 解析逻辑（BigEndian 格式）
4. 验证事件类型处理：`assistantResponseEvent` 和 `toolUseEvent`
5. 测试环形缓冲区的错误恢复机制

### 调试请求复杂度分析
1. 检查 `utils/request_analyzer.go` 中的复杂度评估逻辑
2. 验证不同复杂度请求的超时配置
3. 测试HTTP客户端选择机制（`utils/client.go`）

### 调试多模态图片处理
1. 检查 `utils/image.go` 中的图片处理管道
2. 验证图片格式检测和验证逻辑
3. 测试OpenAI↔Anthropic格式转换功能

### 工具调用去重调试
1. 验证 `utils/tool_dedup.go` 中基于 `tool_use_id` 的去重逻辑
2. 测试流式和非流式请求的工具调用一致性

## 性能调优

### HTTP客户端优化
- **连接池配置**: MaxIdleConns=200, MaxIdleConnsPerHost=50, MaxConnsPerHost=100
- **超时配置**: 复杂请求15分钟，简单请求2分钟，流式请求30分钟
- **TLS优化**: 支持TLS 1.2-1.3，现代密码套件

### 流处理优化
- 对象池：复用解析器实例
- **缓冲区管理**: 预分配4KB缓冲区，支持滑动窗口
- **内存复用**: 事件切片预分配，减少GC压力

### Token缓存优化
- **热点缓存**: 常用token使用原子指针无锁访问
- **冷缓存**: 不常用token使用sync.Map存储
- **后台清理**: 过期token在后台协程中清理

## 故障排除

### Token刷新失败
1. 检查 `AWS_REFRESHTOKEN` 环境变量是否正确设置
2. 验证token池配置和轮换机制
3. 查看token过期时间和自动刷新日志

### 流式响应中断
1. 检查客户端连接稳定性
2. 验证 EventStream 解析器状态
3. 查看工具调用去重逻辑是否影响流式输出

### 性能问题
1. 监控HTTP客户端连接池状态
2. 检查StreamParser对象池使用情况
3. 验证Token缓存命中率和清理效果
4. 分析请求复杂度评估的准确性

# 开发时注意
- 本程序运行需要的环境变量设置在.env文件，支持Social（默认）和IdC两种认证方式
- 项目使用了增强的流式解析器，支持错误恢复和环形缓冲区
- 完整的测试系统位于 `test/` 目录，支持原始数据回放和集成测试
- Anthropic工具调用规范文档 https://docs.anthropic.com/en/docs/agents-and-tools/tool-use/overview