# kiro2api

一个基于 Go 的高性能 API 代理服务器，提供 Anthropic Claude API 和 OpenAI 兼容的 API 接口，桥接 AWS CodeWhisperer 服务。支持实时流式响应、多种认证方式和环境变量配置。

## 功能特性

- **多格式API支持**：同时支持 Anthropic Claude API 和 OpenAI ChatCompletion API 格式
- **完整工具调用支持**：支持Anthropic工具使用格式，包括tool_choice参数和基于 `tool_use_id` 的精确去重逻辑
- **实时流式响应**：自定义 AWS EventStream 解析器，提供零延迟的流式体验
- **高性能架构**：基于 gin-gonic/gin 框架，使用 bytedance/sonic 高性能 JSON 库
- **智能认证管理**：基于环境变量的认证管理，支持.env文件，自动刷新过期令牌
- **Token池管理**：支持多个refresh token轮换使用，提供故障转移和负载均衡
- **智能请求分析**：自动分析请求复杂度，动态调整超时时间和客户端配置
- **增强错误处理**：改进工具结果内容解析和请求验证机制
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
export PORT="8080"
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
### 在Claude Code中使用

```bash
export ANTHROPIC_AUTH_TOKEN=123456 
export ANTHROPIC_BASE_URL=http://localhost:8080
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

- `claude-sonnet-4-20250514` → `CLAUDE_SONNET_4_20250514_V1_0`
- `claude-3-7-sonnet-20250219` → `CLAUDE_3_7_SONNET_20250219_V1_0`
- `claude-3-5-haiku-20241022` → `CLAUDE_3_5_HAIKU_20241022_V1_0`

## 配置管理

### 环境变量配置

程序完全基于环境变量进行配置，支持以下变量：

```bash
# 必需配置
export AWS_REFRESHTOKEN="your_refresh_token"  # 必需设置，支持多个token用逗号分隔

# 可选配置
export KIRO_CLIENT_TOKEN="your_token"         # 客户端认证token（默认：123456）
export PORT="8080"                            # 服务端口（默认：8080）
export LOG_LEVEL="info"                       # 日志级别（默认：info）
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
```

### Token 池管理

- **多Token支持**：支持多个refresh token，用逗号分隔
- **自动轮换**：当一个token失败时自动切换到下一个
- **失败重试**：每个token最多重试3次，超过后自动跳过
- **智能缓存**：基于token自身过期时间进行缓存管理
- **自动刷新**：当收到403错误时自动刷新token

### 智能请求分析

- **复杂度分析**：根据请求的token数量、内容长度、工具使用情况自动评估复杂度
- **动态超时**：复杂请求使用15分钟超时，简单请求使用2分钟超时
- **客户端选择**：自动为不同复杂度的请求选择合适的HTTP客户端
- **性能优化**：基于请求特征进行智能资源分配

刷新URL：`https://prod.us-east-1.auth.desktop.kiro.dev/refreshToken`

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
4. **代理转发** - 通过 `127.0.0.1:8080` 代理转发到 AWS CodeWhisperer
5. **响应解析** - StreamParser 实时解析 AWS EventStream 二进制数据
6. **格式转换** - 将响应转换回客户端请求的格式
7. **返回响应** - 以流式或非流式方式返回给客户端

### 核心特性

- **零延迟流式**: 使用滑动窗口缓冲区的自定义 EventStream 解析器
- **统一中间件**: 集中式的认证、CORS 和错误处理
- **高性能处理**: 共享 HTTP 客户端和优化的 JSON 序列化
- **容错设计**: 自动令牌刷新和优雅的错误处理
- **智能超时**: 基于请求复杂度的动态超时配置
- **精确去重**: 基于 `tool_use_id` 的工具调用去重，符合 Anthropic 标准
- **增强验证**: 改进的请求验证和工具结果内容解析机制

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