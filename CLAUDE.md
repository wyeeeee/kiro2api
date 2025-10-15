# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## 项目概述

`kiro2api` 是一个高性能 HTTP 代理服务器，在 Anthropic API、OpenAI ChatCompletion API 和 AWS CodeWhisperer EventStream 格式之间进行转换。支持流式响应、工具调用和智能请求分析。

## 代码探索规范

**⚠️ 强制要求：优先使用 Serena MCP 工具进行代码探索和编辑**

- **代码探索**: 使用 `mcp__serena__get_symbols_overview` 获取文件概览，然后使用 `mcp__serena__find_symbol` 精确读取符号
- **代码搜索**: 使用 `mcp__serena__search_for_pattern` 进行模式搜索
- **代码编辑**: 使用 `mcp__serena__replace_symbol_body`、`insert_after_symbol`、`insert_before_symbol` 进行符号级编辑
- **依赖分析**: 使用 `mcp__serena__find_referencing_symbols` 查找引用关系

**禁止**: 直接使用 `Read` 工具读取整个 Go 源代码文件，除非是配置文件（`.json`、`.yaml`、`.env`）或文档文件（`.md`）。

## 开发命令

```bash
# 编译和运行
go build -o kiro2api main.go
./kiro2api

# 测试
go test ./...                          # 运行所有测试
go test ./parser -v                    # 单包测试(详细输出)
go test ./... -bench=. -benchmem       # 基准测试

# 代码质量
go vet ./...                           # 静态检查
go fmt ./...                           # 格式化
golangci-lint run                      # Linter

# 运行模式
GIN_MODE=debug LOG_LEVEL=debug ./kiro2api  # 开发模式
GIN_MODE=release ./kiro2api                # 生产模式

# 生产构建
go build -ldflags="-s -w" -o kiro2api main.go
```

## 技术栈

- **Web框架**: gin-gonic/gin v1.11.0
- **JSON处理**: bytedance/sonic v1.14.1
- **Go版本**: 1.24.0

## 核心架构

### 请求处理流程
1. **认证**: `PathBasedAuthMiddleware` 验证 API 密钥
2. **请求分析**: `utils.AnalyzeRequestComplexity()` 智能分析复杂度，动态选择超时时间
3. **格式转换**: `converter/` 包处理 Anthropic ↔ OpenAI ↔ CodeWhisperer 转换
4. **流处理**: `CompliantEventStreamParser` 解析 AWS EventStream (BigEndian格式)
5. **响应转换**: 转换回客户端格式

### 包职责
- **`server/`**: HTTP服务器、路由、处理器、中间件
- **`converter/`**: API格式转换 (OpenAI/CodeWhisperer ↔ Anthropic)
- **`parser/`**: EventStream解析、工具调用处理、会话管理
- **`auth/`**: Token管理系统 (顺序选择策略、并发控制、使用限制监控)
- **`utils/`**: 请求分析、Token估算、HTTP工具、消息处理
- **`types/`**: 数据结构定义
- **`logger/`**: 结构化日志
- **`config/`**: 配置常量和模型映射

### 关键特性
- **Token管理**: 顺序选择策略，支持 Social 和 IdC 双认证方式
- **流式优化**: 零延迟流式传输，直接内存分配（已移除对象池）
- **智能超时**: 根据 MaxTokens、内容长度、工具使用等因素动态调整
- **多模态支持**: data URL 的 PNG/JPEG 图片输入

## 内存管理原则

**重要**: 项目已移除 `sync.Pool` 对象池，遵循 KISS 和 YAGNI 原则：
- 使用 `bytes.NewBuffer(nil)` 代替 Buffer 池
- 使用 `var sb strings.Builder` 代替 StringBuilder 池
- 使用 `make([]byte, size)` 代替 ByteSlice 池
- 使用 `make(map[string]any)` 代替 Map 池
- 信任 Go 1.24 的 GC 和编译器逃逸分析优化

**何时考虑对象池**：仅在以下场景重新引入
- 实测 QPS > 1000
- 对象大小 > 10KB
- 基准测试证明 > 20% 性能提升

## 环境变量

详见 `.env.example`。关键配置：

```bash
# Token配置 (JSON格式，顺序策略)
KIRO_AUTH_TOKEN='[{"auth":"Social","refreshToken":"xxx"}]'

# 多账号池配置示例
KIRO_AUTH_TOKEN='[
  {"auth":"Social","refreshToken":"token1"},
  {"auth":"IdC","refreshToken":"token2","clientId":"xxx","clientSecret":"xxx"}
]'

# 服务配置
KIRO_CLIENT_TOKEN=123456
PORT=8080

# 日志配置
LOG_LEVEL=info                # debug,info,warn,error
LOG_FORMAT=json               # text,json
LOG_CONSOLE=true              # 控制台输出
LOG_FILE=/var/log/kiro2api.log  # 可选文件输出
```

## API端点

- `POST /v1/messages` - Anthropic Claude API 代理（支持流/非流）
- `POST /v1/chat/completions` - OpenAI ChatCompletion API 代理（支持流/非流）
- `POST /v1/messages/count_tokens` - Token 计数接口
- `GET /v1/models` - 可用模型列表
- `GET /api/tokens` - Token池状态与使用信息（无需认证）

## 支持的模型

| 公开模型名称 | 内部 CodeWhisperer 模型 ID |
|-------------|---------------------------|
| `claude-sonnet-4-5-20250929` | `CLAUDE_SONNET_4_5_20250929_V1_0` |
| `claude-sonnet-4-20250514` | `CLAUDE_SONNET_4_20250514_V1_0` |
| `claude-3-7-sonnet-20250219` | `CLAUDE_3_7_SONNET_20250219_V1_0` |
| `claude-3-5-haiku-20241022` | `auto` |

## 故障排除

```bash
# 调试模式
LOG_LEVEL=debug ./kiro2api

# 测试连接
curl -X POST http://localhost:8080/v1/messages \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer 123456" \
  -d '{"model": "claude-sonnet-4-20250514", "max_tokens": 100, "messages": [{"role": "user", "content": "测试"}]}'

# 测试流式响应
curl -N -X POST http://localhost:8080/v1/messages \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer 123456" \
  -d '{"model": "claude-sonnet-4-20250514", "stream": true, "max_tokens": 100, "messages": [{"role": "user", "content": "测试"}]}'
```

**常见问题：**
- JSON配置格式错误：验证 `KIRO_AUTH_TOKEN` 格式
- 认证方式错误：确认 `auth` 字段为 "Social" 或 "IdC"
- Token过期：查看日志刷新状态
- 流式响应中断：检查网络稳定性和超时配置

## Docker 部署

```bash
# 使用 docker-compose（推荐）
docker-compose up -d

# 查看日志
docker logs -f kiro2api

# 健康检查
docker exec kiro2api wget -qO- http://localhost:8080/v1/models
```

## Claude Code 集成

```bash
# 配置环境变量
export ANTHROPIC_BASE_URL="http://localhost:8080/v1"
export ANTHROPIC_API_KEY="your-kiro-token"

# 直接使用
claude-code --model claude-sonnet-4 "帮我重构这段代码"
```

**支持功能**:
- 完整 Anthropic API 兼容
- 流式响应零延迟
- 工具调用完整支持
- 多模态图片处理（data URL）
