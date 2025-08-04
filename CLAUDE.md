# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

此文件为 Claude Code (claude.ai/code) 在此代码库中工作时提供指导。

## 项目概述

这是 `kiro2api`，一个基于 Go 的高性能 HTTP 代理服务器，提供 Anthropic Claude API 和 OpenAI 兼容的 API 接口，桥接 AWS CodeWhisperer 服务。支持多模态图片输入、实时流式响应、智能请求分析和完整的工具调用功能。

**当前版本**: v2.8.1 - 修复工具调用中 `tool_result` 嵌套内容结构处理问题，彻底解决多模态环境下的 "Improperly formed request" 错误，优化调试日志系统。

## 快速开始

```bash
# 编译程序
go build -o kiro2api main.go

# 启动默认服务器 (端口 8080, 认证令牌 "123456")
./kiro2api

# 使用环境变量配置启动
export KIRO_CLIENT_TOKEN="your_token"
export AWS_REFRESHTOKEN="your_refresh_token"  # 必需设置
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
# 构建应用程序
go build -o kiro2api main.go

# 运行测试
go test ./...

# 运行特定包的详细测试
go test ./parser -v
go test ./auth -v
go test ./utils -v

# 代码质量检查
go vet ./...
go fmt ./...

# 清理构建
rm -f kiro2api && go build -o kiro2api main.go

# 查看依赖
go mod tidy
go mod graph
```

## 应用程序命令

```bash
# 启动代理服务器（优先使用环境变量配置）
./kiro2api [端口]

# 示例：
./kiro2api                    # 使用默认配置或.env文件配置
./kiro2api 8080              # 指定端口（环境变量PORT优先级更高）

# 环境变量配置：
export KIRO_CLIENT_TOKEN="your_token"      # 客户端认证token (默认: 123456)
export PORT="8080"                          # 服务端口 (默认: 8080)
export AWS_REFRESHTOKEN="your_refresh"      # AWS刷新token（必需设置，支持多个逗号分隔）
export LOG_LEVEL="info"                     # 日志级别 (默认: info)
export LOG_FORMAT="json"                    # 日志格式 (默认: json)
export GIN_MODE="release"                   # Gin模式 (默认: release)
./kiro2api

# 使用.env文件配置：
# 1. 复制示例配置文件：cp .env.example .env
# 2. 编辑.env文件设置你的配置
# 3. 启动应用：./kiro2api
```

## 架构概述

这是一个**双API代理服务器**，在三种格式之间进行转换：
- **输入**: Anthropic Claude API 或 OpenAI ChatCompletion API 
- **输出**: AWS CodeWhisperer EventStream 格式
- **响应**: 实时流式或标准 JSON 响应

### 核心请求流程
1. **认证**: `AuthMiddleware` 验证来自 `Authorization` 或 `x-api-key` 头的 API 密钥
2. **请求分析**: `utils.AnalyzeRequestComplexity()` 分析请求复杂度，选择合适的客户端和超时配置
3. **格式转换**: `converter/` 包将请求转换为 CodeWhisperer 格式
4. **代理**:  通过 `127.0.0.1:8080` 代理转发到 AWS CodeWhisperer
5. **流处理**: `StreamParser` 处理实时 AWS EventStream 二进制解析
6. **响应转换**: 转换回客户端请求的格式（Anthropic SSE 或 OpenAI 流式）

### 关键架构模式

**智能请求处理**: 全面的请求分析机制：
- `utils.AnalyzeRequestComplexity()` - 根据token数量、内容长度、工具使用、关键词等因素评估复杂度
- `utils.GetClientForRequest()` - 为不同复杂度的请求选择合适的HTTP客户端
- 复杂请求使用15分钟超时，简单请求使用2分钟超时
- 服务器读写超时配置，支持长时间处理
- **复杂度评估因素**: MaxTokens (>4000)、内容长度 (>10K字符)、工具使用、系统提示长度 (>2K字符)、复杂关键词检测

**Token池管理**: 高级认证系统：
- `types.TokenPool` - 支持多个refresh token的池化管理和自动轮换
- 智能故障转移，每个token最多重试3次后自动跳过
- `utils.AtomicTokenCache` - **[高性能]** 原子操作的Token缓存，减少锁竞争
- 负载均衡和健康状态管理
- **热点缓存机制**: 最常用的token使用原子指针避免锁，冷缓存使用sync.Map适合读多写少场景

**工具调用去重**: 标准化工具调用管理：
- `utils.ToolDedupManager` - 基于 `tool_use_id` 的精确去重机制
- 符合 Anthropic 标准，避免基于参数哈希的误判
- 流式和非流式请求统一去重逻辑，确保一致性
- 请求级别的去重管理，防止跨请求状态污染

**中间件链**: gin-gonic 服务器架构：
- `gin.Logger()` 和 `gin.Recovery()` 提供基本功能
- `corsMiddleware()` 处理 CORS
- `PathBasedAuthMiddleware()` 基于路径的认证，仅对 `/v1/*` 端点验证 API 密钥

**流处理架构**:
- 使用滑动窗口缓冲区的 `StreamParser` 进行实时 EventStream 解析
- 立即客户端流式传输（零首字符延迟）
- 使用 `binary.BigEndian` 进行 AWS EventStream 协议的二进制格式解析
- 支持 `assistantResponseEvent` 和 `toolUseEvent` 两种事件类型
- **对象池优化**: `GlobalStreamParserPool` 复用StreamParser实例，减少内存分配
- **高性能JSON处理**: 使用bytedance/sonic库进行JSON序列化/反序列化

## 包结构

**`server/`** - HTTP 服务器和 API 处理器
- `server.go`: 路由配置、服务器启动、中间件链
- `handlers.go`: Anthropic API 端点 (`/v1/messages`) 
- `openai_handlers.go`: OpenAI 兼容性 (`/v1/chat/completions`, `/v1/models`)
- `middleware.go`: **[重构后]** 统一 API 密钥验证中间件
- `common.go`: 共享 HTTP 工具和 CodeWhisperer 错误处理

**`converter/`** - 格式转换层
- 处理 Anthropic ↔ OpenAI ↔ CodeWhisperer 请求/响应转换
- 通过 `config.ModelMap` 进行模型名称映射
- 工具调用格式转换

**`parser/`** - 流处理
- `sse_parser.go`: AWS EventStream 二进制协议解析器
- `StreamParser`: 实时流式处理，支持部分数据处理
- 将 EventStream 事件转换为客户端的 SSE 格式

**`auth/`** - 令牌管理  
- **[v2.4.0后]** 完全依赖环境变量`AWS_REFRESHTOKEN`，不再读取文件
- 通过 `RefreshTokenForServer()` 在 403 错误时自动刷新
- 使用`utils.SharedHTTPClient`进行HTTP请求

**`utils/`** - **[最近优化]** 集中化工具包
- `message.go`: `GetMessageContent()` 函数（消除重复）
- `http.go`: `ReadHTTPResponse()` 标准响应读取
- `client.go`: 配置超时的 `SharedHTTPClient` 和 `LongRequestClient`
- `request_analyzer.go`: **[核心功能]** 请求复杂度分析和客户端选择
- `tool_dedup.go`: **[v2.5.3重构]** 基于 `tool_use_id` 的工具去重管理器
- `image.go`: **[v2.8.0新增]** 多模态图片处理工具，支持格式检测、验证和转换
- `uuid.go`: UUID 生成工具
- `atomic_cache.go`: **[高性能]** 原子操作的Token缓存实现，减少锁竞争
- `json.go`: 高性能JSON序列化工具（SafeMarshal, FastMarshal）

**`types/`** - **[最近重构]** 数据结构定义
- `anthropic.go`: Anthropic API 请求/响应结构
- `openai.go`: OpenAI API 格式结构  
- `codewhisperer.go`: AWS CodeWhisperer API 结构
- `model.go`: 模型映射和配置类型
- `token.go`: **[增强]** 统一token管理结构（`TokenInfo`, `TokenPool`, `TokenCache`）
- `common.go`: 通用结构定义（`Usage`统计，`BaseTool`工具抽象）

**`logger/`** - **[v2.6.0+优化]** 结构化日志系统
- `logger.go`: 完整的日志系统实现，支持结构化字段和JSON格式输出
- 支持环境变量配置：`LOG_LEVEL`, `LOG_FORMAT`, `LOG_FILE`, `LOG_CONSOLE`
- 日志级别管理（DEBUG、INFO、WARN、ERROR、FATAL）
- 多输出支持：控制台、文件或同时输出
- **优化亮点**: 简化架构，移除复杂 writer 接口，提升性能和可维护性
- **性能优化**: 使用原子操作、对象池、可配置的调用栈获取

**`config/`** - 配置常量
- `ModelMap`: 将公开模型名称映射到内部 CodeWhisperer 模型 ID
- `RefreshTokenURL`: Kiro 令牌刷新端点

## API 端点

- `POST /v1/messages` - Anthropic Claude API 代理（流式 + 非流式）
- `POST /v1/chat/completions` - OpenAI ChatCompletion API 代理（流式 + 非流式）  
- `GET /v1/models` - 返回可用模型列表
- `GET /health` - 健康检查（绕过认证）
- `GET /metrics` - 性能指标监控（绕过认证）
- `GET /stats/*` - 各组件统计信息（绕过认证）
- `GET /debug/pprof/*` - 性能分析端点（绕过认证）

## Docker 支持

项目提供完整的容器化支持，包括 Dockerfile 和 docker-compose.yml：

### Dockerfile 特性
- 多平台构建支持（BUILDPLATFORM, TARGETOS, TARGETARCH）
- 多阶段构建，最小化镜像大小
- 非 root 用户运行，增强安全性
- 内置健康检查
- Alpine Linux 基础镜像

### Docker Compose 部署

```bash
# 使用 docker-compose（推荐）
docker-compose up -d

# 查看日志
docker-compose logs -f kiro2api

# 停止服务
docker-compose down
```

### Docker 命令示例

```bash
# 使用预构建镜像
docker run -d \
  --name kiro2api \
  -p 8080:8080 \
  -e AWS_REFRESHTOKEN="your_refresh_token" \
  -e KIRO_CLIENT_TOKEN="123456" \
  ghcr.io/caidaoli/kiro2api:latest

# 本地构建并运行
docker build -t kiro2api .
docker run -d \
  --name kiro2api \
  -p 8080:8080 \
  -e AWS_REFRESHTOKEN="your_refresh_token" \
  -e KIRO_CLIENT_TOKEN="123456" \
  kiro2api
```

## 重要实现细节

**认证**: 采用基于路径的认证策略（PathBasedAuthMiddleware）
- **需要认证**: `/v1/*` 开头的所有端点，需要在 `Authorization: Bearer <token>` 或 `x-api-key: <token>` 头中提供 API 密钥
- **无需认证**: `/health` 等非API端点

**模型映射**: 公开模型名称通过 `config.ModelMap` 映射到内部 CodeWhisperer ID：
- `claude-sonnet-4-20250514` → `CLAUDE_SONNET_4_20250514_V1_0`
- `claude-3-7-sonnet-20250219` → `CLAUDE_3_7_SONNET_20250219_V1_0`
- `claude-3-5-haiku-20241022` → `CLAUDE_3_5_HAIKU_20241022_V1_0`

**流式传输**: 使用自定义二进制 EventStream 解析器（StreamParser）进行实时响应处理
- 支持滑动窗口缓冲区，实现零延迟流式传输
- 处理 `assistantResponseEvent` 和 `toolUseEvent` 两种事件类型
- 自动转换为 Anthropic SSE 格式

**Token池配置**: 高级Token管理，完全基于环境变量：
1. **环境变量 AWS_REFRESHTOKEN**: **必需设置**，支持多个token用逗号分隔
2. **多Token支持**: 智能轮换、故障转移、最多3次重试
3. **智能缓存**: 基于token过期时间的缓存管理

**环境变量配置**: 支持完整的环境变量配置：
```bash
# 必需配置
AWS_REFRESHTOKEN=token1,token2,token3  # 多token支持（必需）

# 可选配置
KIRO_CLIENT_TOKEN=123456               # 客户端认证token（默认: 123456）
PORT=8080                              # 服务端口（默认: 8080）
LOG_LEVEL=info                         # 日志级别：debug,info,warn,error
LOG_FORMAT=json                        # 日志格式：text,json
LOG_FILE=/var/log/kiro2api.log         # 日志文件路径（可选）
LOG_CONSOLE=true                       # 是否输出到控制台（默认: true）
GIN_MODE=release                       # Gin模式：debug,release,test

# 超时配置（分钟）
REQUEST_TIMEOUT_MINUTES=15             # 复杂请求超时（默认: 15）
SIMPLE_REQUEST_TIMEOUT_MINUTES=2       # 简单请求超时（默认: 2）
SERVER_READ_TIMEOUT_MINUTES=16         # 服务器读取超时（默认: 16）
SERVER_WRITE_TIMEOUT_MINUTES=16        # 服务器写入超时（默认: 16）

# 功能控制
DISABLE_STREAM=false                   # 是否禁用流式响应（默认: false）
```

**错误处理**: 智能错误处理和恢复机制
- 403 响应触发自动令牌刷新
- Token池故障转移
- 请求复杂度分析和超时调整

## 高性能实现特性

kiro2api 在设计上特别注重性能优化，采用多种技术来提升并发处理能力和资源利用效率：

### 内存管理优化
- **对象池模式**: 使用 `sync.Pool` 复用 `StreamParser` 和字节缓冲区，减少GC压力
- **预分配策略**: 缓冲区和事件切片预分配容量，避免动态扩容开销
- **热点数据缓存**: Token缓存使用热点+冷缓存两级架构，最常用token通过原子指针无锁访问

### 并发处理优化
- **原子操作**: 日志级别判断、Token缓存访问使用原子操作，减少锁竞争
- **读写分离**: Token池使用读写锁，缓存使用sync.Map适合读多写少场景
- **无锁设计**: 热点Token缓存使用unsafe.Pointer实现无锁访问
- **后台清理**: Token缓存过期清理在后台协程中进行，不影响主流程

### 网络和I/O优化
- **共享客户端**: HTTP客户端复用，减少连接建立开销
- **流式处理**: 零延迟的流式响应，使用滑动窗口缓冲区
- **智能超时**: 根据请求复杂度动态调整超时配置，优化资源利用

### 数据处理优化
- **高性能JSON**: 使用bytedance/sonic替代标准库，提升JSON处理性能
- **二进制协议**: 直接解析AWS EventStream二进制格式，避免文本转换开销
- **请求分析**: 多因素复杂度评估，智能选择处理策略

## 技术栈

- **Web框架**: gin-gonic/gin v1.10.1
- **JSON处理**: bytedance/sonic v1.14.0（高性能）
- **环境变量**: github.com/joho/godotenv v1.5.1
- **HTTP**: 标准 net/http 与共享客户端池
- **流式传输**: 自定义 EventStream 二进制协议解析器
- **并发优化**: sync.Pool, sync.Map, 原子操作
- **Go 版本**: 1.23.3

## 最近重构 (v2.8.0+)

持续优化和功能增强：

### 多模态图片支持 (v2.8.0)
- **完整图片处理管道**: 新增 `utils/image.go`，提供完整的图片处理功能
- **格式自动检测**: 支持PNG、JPEG、GIF、WebP、BMP等主流图片格式的自动检测
- **OpenAI↔Anthropic转换**: 自动转换OpenAI `image_url`格式到Anthropic `image`格式
- **data URL支持**: 完整支持data URL格式的图片输入（`data:image/png;base64,...`）
- **严格验证机制**: 图片格式验证、大小限制（20MB）、编码完整性检查
- **CodeWhisperer集成**: 自动转换为CodeWhisperer所需的图片格式
- **错误处理增强**: 详细的图片处理错误信息和调试支持

### 结构化日志系统优化 (v2.7.0+)
- **JSON格式输出**: 标准 JSON 格式，便于日志分析和监控集成
- **简化架构**: 移除复杂的 writer 接口，提升性能和可维护性
- **多输出支持**: 支持控制台、文件或同时输出到多个目标
- **环境变量配置**: 支持 `LOG_LEVEL`, `LOG_FORMAT`, `LOG_FILE`, `LOG_CONSOLE` 配置
- **调用者信息**: 自动记录文件名和行号，便于调试
- **性能优化**: 使用原子操作进行日志级别判断、对象池复用字节缓冲区、可配置的调用栈获取

### Docker 和部署增强
- **多平台构建**: 支持不同架构的Docker镜像构建
- **安全优化**: 非root用户运行，最小权限原则
- **健康检查**: 内置容器健康检查机制
- **Docker Compose**: 提供完整的编排配置文件

## 重要历史重构记录

### v2.5.3 - 工具调用去重机制标准化
**符合 Anthropic 最佳实践的精确去重**：
- **标准化去重**: 从 `name+input` 哈希改为基于 `tool_use_id` 的标准去重
- **精确识别**: 使用 Anthropic 官方推荐的唯一标识符，避免参数哈希误判
- **一致性保证**: 流式和非流式处理统一使用相同去重逻辑
- **性能提升**: 简化去重算法，移除复杂的 JSON 序列化和 SHA256 计算
- **标准合规**: 完全遵循 Anthropic 工具调用最佳实践

### v2.5.2 - 全局状态污染修复
**请求隔离和状态管理优化**：
- **状态隔离**: 移除全局工具跟踪器，避免跨请求工具调用干扰
- **去重策略改进**: 请求级别的 `tool_use_id` 去重，确保逻辑正确性
- **并发安全**: 每个请求独立维护工具去重状态
- **代码简化**: 移除不必要的全局 mutex 和同步逻辑

### v2.5.1 - 工具调用稳定性增强
**工具处理流程优化**：
- **去重逻辑优化**: 使用 `name+input` 组合创建唯一标识符
- **代码清理**: 移除调试日志，提升代码可读性
- **稳定性保证**: 确保工具调用只处理一次，解决重复执行问题

### v2.5.0 - 智能请求处理系统
**全面的请求分析和资源管理**：
- **复杂度分析**: 多因素评估请求复杂度（token数量、内容长度、工具使用等）
- **动态超时**: 智能调整超时时间，复杂请求15分钟，简单请求2分钟
- **Token池管理**: 多token池化管理和自动轮换机制
- **错误处理增强**: 改进工具结果内容解析和请求验证
- **性能监控**: 请求处理时间监控和性能指标记录

## 历史重构 (v2.4.0)

代码库移除文件依赖重构：
- **移除文件读取**: 完全移除对`~/.aws/sso/cache/kiro-auth-token.json`文件的依赖
- **强制环境变量**: `AWS_REFRESHTOKEN`现在为必需的环境变量，程序启动时检查
- **简化认证逻辑**: 统一使用环境变量作为唯一配置源，提高可预测性
- **快速失败模式**: 程序启动时立即检查必需环境变量，缺失时给出清晰提示并退出
- **清理代码**: 移除`getTokenFilePath()`函数和`path/filepath`导入
- **向后不兼容**: 不再支持文件方式读取token，需要迁移到环境变量配置

## 历史重构 (v2.3.0)

代码库经历了环境变量配置重构：
- **环境变量优先**: clientToken从命令行参数改为环境变量`KIRO_CLIENT_TOKEN`（默认值123456）
- **AWS Token灵活性**: 优先使用环境变量`AWS_REFRESHTOKEN`，不再强制依赖`~/.aws/sso/cache/kiro-auth-token.json`文件
- **.env文件支持**: 自动加载项目根目录的`.env`文件，支持配置管理
- **配置层次化**: 环境变量 > .env文件 > 命令行参数 > 默认值
- **依赖更新**: 添加`github.com/joho/godotenv`依赖用于.env文件加载
- **向后兼容**: 保持原有文件读取方式作为备选方案

## 历史重构 (v2.2.0)

代码库经历了结构优化重构：
- **struct合并优化**: 消除了相似结构的重复定义
  - 合并 `TokenData` 和 `RefreshResponse` 为统一的 `TokenInfo`
  - 统一 `Usage` 结构支持Anthropic和OpenAI格式转换
  - 创建 `BaseTool` 工具抽象和 `ToolSpec` 接口
- **类型别名清理**: 移除了临时兼容性别名，简化类型系统
- **代码重复消除**: 
  - 将三个 `getMessageContent` 副本整合为 `utils.GetMessageContent`
  - 用统一的 `AuthMiddleware` 替换重复的 API 验证
  - 集中化 HTTP 响应读取和客户端管理
- 改善了可维护性并减少了技术债务

## 常见开发任务

### 添加新的模型支持
1. 在 `config/config.go` 的 `ModelMap` 中添加模型映射
   ```go
   "new-model-name": "INTERNAL_MODEL_ID"
   ```
2. 确保 `types/model.go` 中的结构支持新模型
3. 测试新模型的请求和响应转换
4. 更新 README.md 中的模型列表

### 修改认证逻辑
1. 主要逻辑在 `server/middleware.go` 的 `PathBasedAuthMiddleware`
2. Token管理和刷新逻辑在 `auth/token.go`
3. Token池配置在 `types/token.go`
4. 确保所有 `/v1/*` 端点都经过认证验证

### 调试流式响应
1. 检查 `parser/sse_parser.go` 中的 `StreamParser` 和 `ParseEvents`
2. 确认二进制 EventStream 解析逻辑（BigEndian 格式）
3. 验证事件类型处理：`assistantResponseEvent` 和 `toolUseEvent`
4. 测试客户端格式转换（Anthropic SSE vs OpenAI 流式）

### 调试Token池和缓存
1. 检查 `auth/token.go` 中的Token池管理逻辑
2. 验证多Token轮换和故障转移机制
3. 监控Token缓存命中率和过期处理
4. 使用环境变量配置多个refresh token测试

### 调试日志系统
1. 检查 `logger/logger.go` 中的结构化日志实现
2. 配置环境变量：
   - `LOG_LEVEL`: debug, info, warn, error
   - `LOG_FORMAT`: text, json
   - `LOG_FILE`: 日志文件路径
   - `LOG_CONSOLE`: 是否输出到控制台
3. 监控 JSON 格式日志输出，便于问题诊断和性能分析

### 调试请求复杂度分析
1. 检查 `utils/request_analyzer.go` 中的复杂度评估逻辑
2. 验证不同复杂度请求的超时配置
3. 测试HTTP客户端选择机制（SharedHTTPClient vs LongRequestClient）
4. 监控请求处理时间和资源使用情况

### 调试多模态图片处理
1. 检查 `utils/image.go` 中的图片处理管道
2. 验证图片格式检测和验证逻辑
3. 测试OpenAI↔Anthropic格式转换功能
4. 调试data URL解析和base64编码处理
5. 监控图片大小限制和错误处理机制
6. 验证CodeWhisperer图片格式转换

### 工具调用去重调试
1. 验证 `utils/tool_dedup.go` 中基于 `tool_use_id` 的去重逻辑
2. 测试流式和非流式请求的工具调用一致性
3. 确保请求级别的去重管理，避免跨请求状态污染

### 高性能特性调试
1. **原子缓存优化**: 验证 `utils/atomic_cache.go` 中的热点token缓存机制
2. **对象池管理**: 检查 `GlobalStreamParserPool` 的内存复用效果
3. **并发安全**: 验证Token池的并发访问和故障转移机制
4. **性能监控**: 监控原子缓存的命中率和清理效果

## 故障排除

### "Improperly formed request" 错误
这个错误通常出现在工具调用场景中，特别是多模态请求。排查步骤：

1. **检查工具结果内容**：确认 `tool_result` 内容块是否包含嵌套结构
   ```json
   {
     "type": "tool_result",
     "content": [{"text": "实际内容"}]  // 嵌套数组结构
   }
   ```

2. **启用调试日志**：在环境变量中设置详细日志级别
   ```bash
   export LOG_LEVEL=debug
   ./kiro2api
   ```

3. **检查消息内容处理**：查看日志中的 "消息内容处理结果为空" 条目
   - 如果 `text_parts_count=0` 且 `images_count=0`，说明内容提取失败
   - 检查内容结构是否符合预期格式

4. **验证JSON序列化**：确保使用 `utils.SafeMarshal` 而非 `utils.FastMarshal`

**修复历史**：v2.8.1 版本已修复 `tool_result` 嵌套内容结构处理问题。

### Token刷新失败
1. 检查 `AWS_REFRESHTOKEN` 环境变量是否正确设置
2. 验证token池配置和轮换机制
3. 查看token过期时间和自动刷新日志

### 流式响应中断
1. 检查客户端连接稳定性
2. 验证 EventStream 解析器状态
3. 查看工具调用去重逻辑是否影响流式输出
4. 检查工具调用错误处理和调试支持

### Docker 部署调试
1. 验证 `Dockerfile` 多平台构建配置
2. 测试 `docker-compose.yml` 环境变量传递
3. 检查容器健康检查机制（`/health` 端点）
4. 监控容器日志和性能指标

### 性能监控和统计
kiro2api 提供多个监控端点来跟踪系统性能：

**Token池监控**:
- 端点：`GET /stats/token-pool`
- 信息：token总数、失败计数、缓存命中率、热点token状态

**性能指标**:
- 端点：`GET /metrics`
- 信息：请求处理时间、并发连接数、内存使用情况

**健康检查**:
- 端点：`GET /health`
- 功能：检查服务可用性和基本功能状态

**性能分析**:
- 端点：`GET /debug/pprof/*`
- 功能：Go pprof性能分析工具