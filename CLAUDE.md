# CLAUDE.md

此文件为 Claude Code (claude.ai/code) 在此代码库中工作提供指导。

## 项目概述

这是一个名为 `kiro2api` 的 Go 命令行工具，用于管理 Kiro 认证令牌，并提供 Anthropic API 和 OpenAI 兼容的 API 代理服务。该工具充当 API 请求与 AWS CodeWhisperer 之间的桥梁，在不同格式之间转换请求和响应。

**版本 v2.0.0** 已完成从 fasthttp 到 gin-gonic/gin 框架的重大升级，并实现了真正的实时流式响应。

## 技术栈

- **Web框架**: gin-gonic/gin (v1.10.0)
- **JSON处理**: bytedance/sonic (高性能)
- **HTTP客户端**: 标准 net/http
- **流式处理**: 自定义 StreamParser 实现

## 构建和开发命令

```bash
# 构建应用程序
go build -o kiro2api main.go

# 运行测试
go test ./...

# 运行特定包的详细测试
go test ./parser -v
go test ./auth -v

# 清理构建
rm -f kiro2api && go build -o kiro2api main.go
```

## 应用程序命令

- `./kiro2api read` - 从缓存中读取并显示令牌信息
- `./kiro2api refresh` - 使用刷新令牌刷新访问令牌
- `./kiro2api export` - 为其他工具导出环境变量（使用实际令牌，不是硬编码值）
- `./kiro2api authToken` - 显示当前认证令牌
- `./kiro2api server [port] [authToken]` - 启动 HTTP 代理服务器（默认端口 8080）

## 架构

代码库采用模块化包结构，职责分离清晰：

### 包结构

1. **`auth/`** - 令牌管理
   - `token.go`：令牌 CRUD 操作、文件 I/O、刷新逻辑
   - `claude.go`：Claude 特定的认证设置
   - 从 `~/.aws/sso/cache/kiro-auth-token.json` 读取
   - 通过 Kiro 认证服务处理自动令牌刷新

2. **`server/`** - 基于 gin 的 HTTP 服务器和 API 处理
   - `server.go`：gin 服务器初始化、路由配置和中间件
   - `handlers.go`：Anthropic API 端点（`/v1/messages`）使用 gin.Context
   - `openai_handlers.go`：OpenAI 兼容端点（`/v1/chat/completions`、`/v1/models`）
   - `common.go`：共享的 HTTP 工具、错误处理和请求构建（基于 net/http）
   - **流式特性**: 使用 StreamParser 实现真正的实时流式响应

3. **`converter/`** - API 转换层
   - `converter.go`：在 Anthropic、OpenAI 和 CodeWhisperer 格式之间转换
   - 处理模型名称映射、消息格式转换和工具调用

4. **`parser/`** - 响应处理和流式解析
   - `sse_parser.go`：将二进制 AWS EventStream 响应解析为 SSE 事件
   - **StreamParser**: 新增的实时流式解析器，支持部分 EventStream 数据处理
   - 转换为 Anthropic/OpenAI 兼容的流式响应
   - 处理工具使用和文本内容块
   - **性能优化**: 零延迟首字响应，真正的实时流式处理

5. **`types/`** - 数据结构
   - `anthropic.go`：Anthropic API 请求/响应类型
   - `openai.go`：OpenAI API 请求/响应类型
   - `codewhisperer.go`：AWS CodeWhisperer 集成类型
   - `token.go`：认证令牌数据结构
   - `model.go`：模型信息和列表类型

6. **`config/`** - 配置管理
   - `config.go`：不同 API 格式之间的模型映射，默认常量
   - 包含用于在提供商之间转换模型名称的 `ModelMap`

7. **`logger/`** - 日志系统
   - 具有可配置级别和格式化器的结构化日志
   - 支持 JSON 和控制台输出格式

8. **`utils/`** - 工具函数
   - `file.go`：文件系统操作
   - `uuid.go`：UUID 生成工具

### API 端点

- **Anthropic 兼容**：`/v1/messages` - 直接 Anthropic API 代理，支持真正的实时流式响应
- **OpenAI 兼容**：`/v1/chat/completions` - OpenAI 格式转换为 Claude，完全流式兼容
- **模型**：`/v1/models` - 返回可用模型列表
- **健康检查**：`/health` - 服务器健康检查端点

### 请求流程

#### 非流式请求流程
1. 客户端向适当的端点发送 API 请求（`/v1/messages` 或 `/v1/chat/completions`）
2. gin 服务器使用来自文件系统的令牌或提供的认证头进行请求认证
3. 转换器将请求格式转换为 CodeWhisperer 兼容结构
4. 请求通过硬编码代理 `127.0.0.1:9000` 代理到 AWS CodeWhisperer API
5. 解析器处理完整二进制响应并转换为适当格式
6. 响应以请求的格式返回给客户端

#### 流式请求流程 (v2.0.0 新特性)
1. 客户端向流式端点发送请求
2. gin 服务器立即建立 SSE 连接并发送响应头
3. 请求被转发到 AWS CodeWhisperer API
4. **StreamParser 实时解析** AWS EventStream 二进制数据
5. 每个解析出的事件立即转换为客户端格式
6. 实时推送给客户端，确保零延迟流式体验

### 令牌管理流程

- 令牌存储在 `~/.aws/sso/cache/kiro-auth-token.json`
- `GetToken()` 读取当前令牌并进行错误处理
- 在 403 认证错误时自动刷新
- 当令牌文件不可用时回退到默认令牌 "123456"
- 跨平台环境变量导出（Windows vs Unix 格式）

## 开发注意事项

### v2.0.0 架构变更
- **框架迁移**: 从 fasthttp 升级到 gin-gonic/gin，提供更好的性能和生态系统
- **流式优化**: 实现 StreamParser 用于真正的实时 AWS EventStream 解析
- **JSON性能**: 使用 bytedance/sonic 替代标准 JSON 库
- **HTTP客户端**: 从 fasthttp.Client 迁移到标准 net/http.Client

### 重要实现细节
- 使用硬编码代理 `127.0.0.1:9000` 进行 AWS CodeWhisperer 请求
- 不同 API 提供商之间需要模型映射（参见 `config/config.go`）
- GitHub Actions 处理跨平台构建（Windows、Linux、macOS）并使用 UPX 压缩
- 所有令牌操作首先尝试读取实际令牌，优雅地回退到默认值
- 日志系统完全可通过环境变量和配置进行配置
- **新增**: StreamParser 实现了零拷贝的实时 EventStream 解析

### 流式处理架构
- **缓冲策略**: StreamParser 使用滑动窗口缓冲器处理部分 EventStream 数据
- **实时解析**: 每个网络数据包到达时立即解析，不等待完整响应
- **错误处理**: 优雅处理部分数据和网络中断
- **性能**: 最小化内存分配，使用 binary.BigEndian 高效解析二进制格式

### 测试和质量保证
- 当前没有测试文件 - 应该为关键组件添加测试
- **建议**: 为 StreamParser、gin 处理器和格式转换器添加单元测试
- 性能测试应验证流式响应的延迟改进

### v2.0.0 重要实现细节
- `ExportEnvVars()` 和 `GenerateAuthToken()` 现在通过 `GetToken()` 使用真实令牌，而不是硬编码值
- 服务器支持基于文件的令牌和传递的认证令牌两种认证方式
- StreamParser 处理流式响应中的文本和工具使用内容，支持部分数据解析
- 跨格式兼容性层允许 OpenAI 客户端无缝使用 Claude 模型
- gin.Context 替代 fasthttp.RequestCtx，提供更标准的 Go HTTP 处理