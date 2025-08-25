# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## 项目概述

`kiro2api` 是一个基于 Go 的高性能 HTTP 代理服务器，提供 Anthropic Claude API 和 OpenAI 兼容的 API 接口，桥接 AWS CodeWhisperer 服务。支持多模态图片输入、实时流式响应、智能请求分析和完整的工具调用功能。

**核心架构**: 双API代理服务器，在 Anthropic API、OpenAI ChatCompletion API 和 AWS CodeWhisperer EventStream 格式之间进行转换。

## 技术架构原则

遵循 KISS、YAGNI、DRY 和 SOLID 原则：
- **单一职责**: 每个包专注于特定功能域
- **开闭原则**: 通过接口扩展而非修改核心逻辑  
- **依赖倒置**: 依赖抽象而非具体实现
- **保持简单**: 避免过度工程化，优先选择简洁解决方案

## 开发命令

```bash
# 编译和运行
go build -o kiro2api main.go
./kiro2api

# 测试
go test ./...
go test ./parser -v
go test ./... -bench=. -benchmem

# 代码质量检查
go vet ./...
go fmt ./...
go mod tidy

# 开发模式运行 (详细日志)
GIN_MODE=debug LOG_LEVEL=debug ./kiro2api

# 生产构建
go build -ldflags="-s -w" -o kiro2api main.go
```

## 技术栈

- **框架**: gin-gonic/gin v1.10.1  
- **JSON**: bytedance/sonic v1.14.0
- **配置**: github.com/joho/godotenv v1.5.1 (直接依赖)
- **Go版本**: 1.23.3

## 环境变量配置

```bash
# 复制配置文件
cp .env.example .env

# 必需环境变量
AWS_REFRESHTOKEN=your_refresh_token_here  # 必需设置
KIRO_CLIENT_TOKEN=123456                  # 默认: 123456
PORT=8080                                 # 默认: 8080

# 日志配置  
LOG_LEVEL=info                            # debug,info,warn,error
LOG_FORMAT=json                           # text,json

# 认证方式 (可选)
AUTH_METHOD=social                        # social(默认) 或 idc
# IDC_CLIENT_ID=your_client_id            # IdC认证时需要
# IDC_CLIENT_SECRET=your_client_secret    # IdC认证时需要
```

## 核心架构

### 请求处理流程
1. **认证**: `PathBasedAuthMiddleware` 验证 API 密钥
2. **请求分析**: `utils.AnalyzeRequestComplexity()` 智能分析复杂度
3. **格式转换**: `converter/` 包处理 Anthropic ↔ OpenAI ↔ CodeWhisperer 转换
4. **流处理**: `RobustEventStreamParser` 和 `CompliantEventStreamParser` 解析 AWS EventStream
5. **响应转换**: 转换回客户端格式

### 智能特性
- **请求复杂度分析**: 根据MaxTokens、内容长度、工具使用等因素动态选择超时时间
- **Token池管理**: 多token轮换和故障转移，每个token最多重试3次
- **工具调用去重**: 基于 `tool_use_id` 的精确去重机制
- **流式优化**: 零延迟流式传输，对象池复用解析器实例

## 包结构 (按职责分层)

### 核心服务层
- **`server/`**: HTTP服务器和请求处理，遵循单一职责原则
  - `server.go` - 服务器初始化和路由配置
  - `handlers.go` - Anthropic API处理器  
  - `openai_handlers.go` - OpenAI API处理器
  - `middleware.go` - 认证中间件
  - `common.go` - 公共响应处理

### 数据转换层  
- **`converter/`**: API格式转换，实现开闭原则
  - `openai.go` - OpenAI ↔ Anthropic 转换
  - `codewhisperer.go` - CodeWhisperer ↔ Anthropic 转换  
  - `content.go` - 内容格式处理
  - `tools.go` - 工具调用转换
  - `system_prompt.go` - 系统提示处理

### 流处理核心
- **`parser/`**: EventStream解析和工具调用处理
  - `robust_parser.go` - 主要解析器实现
  - `compliant_event_stream_parser.go` - 标准兼容解析器
  - `compliant_message_processor.go` - 消息处理和去重
  - `tool_*` - 工具调用状态机和生命周期管理

### 基础设施层
- **`auth/`**: 认证和令牌管理，支持多种认证方式
- **`utils/`**: 工具包，遵循DRY原则避免重复
- **`types/`**: 数据结构定义，保持类型安全
- **`logger/`**: 结构化日志系统
- **`config/`**: 配置常量和模型映射

## API端点

- `POST /v1/messages` - Anthropic Claude API 代理（流式 + 非流式）
- `POST /v1/chat/completions` - OpenAI ChatCompletion API 代理（流式 + 非流式）  
- `GET /v1/models` - 返回可用模型列表

## 核心开发任务

### 扩展功能 (遵循开闭原则)
1. **添加新模型支持** 
   - `config/config.go`: 在 `ModelMap` 中添加模型映射
   - `types/model.go`: 验证结构支持新模型
   - 测试新模型的请求响应转换

2. **认证机制扩展**
   - `server/middleware.go`: 修改 `PathBasedAuthMiddleware` 
   - `auth/token.go`: Token管理和刷新逻辑
   - `utils/token_refresh_manager.go`: Token刷新管理器

### 性能优化 (遵循KISS原则)
1. **流式响应调试**
   - `parser/robust_parser.go`: EventStream解析器错误恢复
   - `parser/compliant_message_processor.go`: 消息处理和去重
   - 验证BigEndian格式的EventStream解析

2. **工具调用优化**  
   - `utils/tool_dedup.go`: 基于 `tool_use_id` 去重逻辑
   - 测试流式/非流式请求工具调用一致性

### 代码质量改进 (遵循DRY和SOLID原则)
1. **移除重复代码**: 抽取公共逻辑到 `utils/` 包
2. **接口抽象**: 为HTTP客户端、解析器创建接口
3. **测试覆盖**: 重点测试 `parser/` 和 `converter/` 包

## 技术栈

- **框架**: gin-gonic/gin v1.10.1
- **JSON**: bytedance/sonic v1.14.0  
- **环境**: github.com/joho/godotenv v1.5.1
- **Go版本**: 1.23.3

## 快速测试

```bash
# 启动服务器
./kiro2api

# 测试API
curl -X POST http://localhost:8080/v1/messages \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer 123456" \
  -d '{"model": "claude-sonnet-4-20250514", "max_tokens": 100, "messages": [{"role": "user", "content": "你好"}]}'

# 测试流式响应
curl -N -X POST http://localhost:8080/v1/messages \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer 123456" \
  -d '{"model": "claude-sonnet-4-20250514", "max_tokens": 200, "stream": true, "messages": [{"role": "user", "content": "讲个故事"}]}'
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