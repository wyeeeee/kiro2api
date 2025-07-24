# CLAUDE.md

此文件为 Claude Code (claude.ai/code) 在此代码库中工作提供指导。

## 项目概述

这是一个名为 `kiro2api` 的 Go 命令行工具，用于管理 Kiro 认证令牌，并提供 Anthropic API 和 OpenAI 兼容的 API 代理服务。该工具充当 API 请求与 AWS CodeWhisperer 之间的桥梁，在不同格式之间转换请求和响应。

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
- `./kiro2api claude` - 设置 Claude 地区绕过配置
- `./kiro2api server [port] [authToken]` - 启动 HTTP 代理服务器（默认端口 8080）

## 架构

代码库采用模块化包结构，职责分离清晰：

### 包结构

1. **`auth/`** - 令牌管理
   - `token.go`：令牌 CRUD 操作、文件 I/O、刷新逻辑
   - `claude.go`：Claude 特定的认证设置
   - 从 `~/.aws/sso/cache/kiro-auth-token.json` 读取
   - 通过 Kiro 认证服务处理自动令牌刷新

2. **`server/`** - HTTP 服务器和 API 处理
   - `server.go`：HTTP 服务器初始化和中间件
   - `handlers.go`：Anthropic API 端点（`/v1/messages`）
   - `openai_handlers.go`：OpenAI 兼容端点（`/v1/chat/completions`、`/v1/models`）
   - `common.go`：共享的 HTTP 工具和错误处理
   - 支持流式和非流式请求

3. **`converter/`** - API 转换层
   - `converter.go`：在 Anthropic、OpenAI 和 CodeWhisperer 格式之间转换
   - 处理模型名称映射、消息格式转换和工具调用

4. **`parser/`** - 响应处理
   - `sse_parser.go`：将二进制 CodeWhisperer 响应解析为 SSE 事件
   - 转换为 Anthropic/OpenAI 兼容的流式响应
   - 处理工具使用和文本内容块

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

- **Anthropic 兼容**：`/v1/messages` - 直接 Anthropic API 代理
- **OpenAI 兼容**：`/v1/chat/completions` - OpenAI 格式转换为 Claude
- **模型**：`/v1/models` - 返回可用模型列表
- **健康检查**：基本健康检查端点

### 请求流程

1. 客户端向适当的端点发送 API 请求（`/v1/messages` 或 `/v1/chat/completions`）
2. 服务器使用来自文件系统的令牌或提供的认证头进行请求认证
3. 转换器将请求格式转换为 CodeWhisperer 兼容结构
4. 请求通过硬编码代理 `127.0.0.1:9000` 代理到 AWS CodeWhisperer API
5. 解析器处理二进制响应并转换为适当的流式格式
6. 响应以请求的格式（Anthropic SSE 或 OpenAI 流式）流回客户端

### 令牌管理流程

- 令牌存储在 `~/.aws/sso/cache/kiro-auth-token.json`
- `GetToken()` 读取当前令牌并进行错误处理
- 在 403 认证错误时自动刷新
- 当令牌文件不可用时回退到默认令牌 "123456"
- 跨平台环境变量导出（Windows vs Unix 格式）

## 开发注意事项

- 使用硬编码代理 `127.0.0.1:9000` 进行 AWS CodeWhisperer 请求
- 不同 API 提供商之间需要模型映射（参见 `config/config.go`）
- GitHub Actions 处理跨平台构建（Windows、Linux、macOS）并使用 UPX 压缩
- 所有令牌操作首先尝试读取实际令牌，优雅地回退到默认值
- 日志系统完全可通过环境变量和配置进行配置
- 当前没有测试文件 - 应该为关键组件添加测试

## 重要实现细节

- `ExportEnvVars()` 和 `GenerateAuthToken()` 现在通过 `GetToken()` 使用真实令牌，而不是硬编码值
- 服务器支持基于文件的令牌和传递的认证令牌两种认证方式
- 响应解析处理流式响应中的文本和工具使用内容
- 跨格式兼容性层允许 OpenAI 客户端无缝使用 Claude 模型