# CLAUDE.md

此文件为 Claude Code (claude.ai/code) 在此代码库中工作时提供指导。

## 项目概述

这是 `kiro2api`，一个 Go 命令行工具和 HTTP 代理服务器，用于在 Anthropic/OpenAI 格式和 AWS CodeWhisperer 之间桥接 API 请求。它管理 Kiro 认证令牌并提供实时流式响应功能。

**当前版本**: v2.4.0 - 移除文件依赖，AWS_REFRESHTOKEN现在为必需的环境变量。

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

# 运行测试（当前无测试文件）
go test ./...

# 运行特定包的详细测试
go test ./parser -v
go test ./auth -v

# 代码质量检查
go vet ./...
go fmt ./...

# 清理构建
rm -f kiro2api && go build -o kiro2api main.go
```

## 应用程序命令

```bash
# 启动代理服务器（优先使用环境变量配置）
./kiro2api [端口]

# 示例：
./kiro2api                    # 使用默认配置或.env文件配置
./kiro2api 8080              # 指定端口（环境变量PORT优先级更高）

# 环境变量配置：
export KIRO_CLIENT_TOKEN="your_token"  # 客户端认证token (默认: 123456)
export PORT="8080"                      # 服务端口 (默认: 8080)
export AWS_REFRESHTOKEN="your_refresh"  # AWS刷新token（必需设置）
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
2. **格式转换**: `converter/` 包将请求转换为 CodeWhisperer 格式
3. **代理**:  通过 `127.0.0.1:8080` 代理转发到 AWS CodeWhisperer
4. **流处理**: `StreamParser` 处理实时 AWS EventStream 二进制解析
5. **响应转换**: 转换回客户端请求的格式（Anthropic SSE 或 OpenAI 流式）

### 关键架构模式

**统一工具模式**: 重构后，通用函数集中化：
- `utils.GetMessageContent()` - 消息内容提取（消除了跨文件重复）
- `utils.ReadHTTPResponse()` - 标准 HTTP 响应读取
- `utils.SharedHTTPClient` - 60秒超时的单一 HTTP 客户端实例

**中间件链**: gin-gonic 服务器使用：
- `gin.Logger()` 和 `gin.Recovery()` 提供基本功能  
- `corsMiddleware()` 处理 CORS
- `AuthMiddleware(authToken)` 对除 `/health` 外的所有端点进行 API 密钥验证

**流处理架构**: 
- 使用滑动窗口缓冲区的 `StreamParser` 进行实时 EventStream 解析
- 立即客户端流式传输（零首字符延迟）
- 使用 `binary.BigEndian` 进行 AWS EventStream 协议的二进制格式解析

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

**`utils/`** - **[最近重构]** 集中化工具
- `message.go`: `GetMessageContent()` 函数（消除重复）
- `http.go`: `ReadHTTPResponse()` 标准响应读取
- `client.go`: 配置超时的 `SharedHTTPClient`
- `file.go`, `uuid.go`: 文件操作和 UUID 生成

**`types/`** - **[最近重构]** 数据结构定义
- `anthropic.go`: Anthropic API 请求/响应结构
- `openai.go`: OpenAI API 格式结构  
- `codewhisperer.go`: AWS CodeWhisperer API 结构
- `model.go`: 模型映射和配置类型
- `token.go`: 统一token管理结构（`TokenInfo`）
- `common.go`: 通用结构定义（`Usage`统计，`BaseTool`工具抽象）

**`logger/`** - 结构化日志系统
- `logger.go`: 主日志接口
- `config.go`, `level.go`: 配置和日志级别管理
- `formatter.go`, `writer.go`: 输出格式化和写入器

**`config/`** - 配置常量
- `ModelMap`: 将公开模型名称映射到内部 CodeWhisperer 模型 ID
- `RefreshTokenURL`: Kiro 令牌刷新端点

## API 端点

- `POST /v1/messages` - Anthropic Claude API 代理（流式 + 非流式）
- `POST /v1/chat/completions` - OpenAI ChatCompletion API 代理（流式 + 非流式）  
- `GET /v1/models` - 返回可用模型列表
- `GET /health` - 健康检查（绕过认证）

## Docker 支持

项目包含 Dockerfile，支持容器化部署：

```bash
# 构建 Docker 镜像
docker build -t kiro2api .

# 运行容器（默认端口 8080）
docker run -p 8080:8080 kiro2api

```

## 重要实现细节

**认证**: 采用基于路径的认证策略
- **需要认证**: `/v1/*` 开头的所有端点，需要在 `Authorization: Bearer <token>` 或 `x-api-key: <token>` 头中提供 API 密钥
- **无需认证**: `/health`、`/metrics` 等非API端点

**模型映射**: 公开模型名称通过 `config.ModelMap` 映射到内部 CodeWhisperer ID：
- `claude-sonnet-4-20250514` → `CLAUDE_SONNET_4_20250514_V1_0`
- `claude-3-7-sonnet-20250219` → `CLAUDE_3_7_SONNET_20250219_V1_0`
- `claude-3-5-haiku-20241022` → `CLAUDE_3_5_HAIKU_20241022_V1_0`

**代理配置**: 所有 CodeWhisperer 请求都通过代理 `127.0.0.1:8080` 路由

**流式传输**: 使用自定义二进制 EventStream 解析器进行实时响应处理，而非缓冲解析

**认证配置**: 完全基于环境变量的认证配置：
1. **环境变量 AWS_REFRESHTOKEN**: **必需设置**，程序启动时检查此环境变量，未设置则退出
2. **.env文件配置**: 项目根目录的.env文件，支持的变量：
```bash
KIRO_CLIENT_TOKEN=123456        # 客户端认证token（默认：123456）
PORT=8080                       # 服务端口（默认：8080）
AWS_REFRESHTOKEN=your_token     # AWS刷新token（必需设置）
```

**错误处理**: 403 响应触发通过 `auth.RefreshTokenForServer()` 自动令牌刷新

## 技术栈

- **框架**: gin-gonic/gin v1.10.1
- **JSON**: bytedance/sonic（高性能）
- **HTTP**: 标准 net/http 与共享客户端
- **流式传输**: 自定义 EventStream 解析器
- **Go 版本**: 1.23.3

## 最近重构 (v2.4.0)

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
2. 确保 `types/model.go` 中的结构支持新模型
3. 测试新模型的请求和响应转换

### 修改认证逻辑
1. 主要逻辑在 `server/middleware.go` 的 `AuthMiddleware`
2. 令牌刷新逻辑在 `auth/token.go`
3. 确保所有端点（除 `/health`）都使用中间件

### 调试流式响应
1. 检查 `parser/sse_parser.go` 中的 `StreamParser`
2. 确认二进制 EventStream 解析逻辑
3. 验证客户端格式转换（Anthropic SSE vs OpenAI 流式）

### 性能优化
1. 利用 `utils.SharedHTTPClient` 复用连接
2. 确保使用 `bytedance/sonic` 进行 JSON 操作
3. 监控 `StreamParser` 的内存使用情况