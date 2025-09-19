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
go test ./...                # 运行所有测试
go test ./parser -v          # 运行parser包测试(详细输出)  
go test ./auth -v            # 运行auth包测试(token管理)
go test ./utils -v           # 运行utils包测试(工具函数)
go test ./converter -v       # 运行converter包测试(格式转换)
go test ./... -bench=. -benchmem  # 基准测试

# 代码质量检查
go vet ./...                 # 静态代码检查
go fmt ./...                 # 代码格式化
go mod tidy                  # 整理依赖

# 运行模式
GIN_MODE=debug LOG_LEVEL=debug ./kiro2api  # 开发模式(详细日志)
GIN_MODE=release ./kiro2api               # 生产模式

# 构建
go build -ldflags="-s -w" -o kiro2api main.go  # 生产构建(压缩)

# Docker部署
docker build -t kiro2api .   # 构建镜像
docker-compose up -d         # 启动服务
```

## 技术栈

- **Web框架**: gin-gonic/gin v1.10.1  
- **JSON处理**: bytedance/sonic v1.14.0 (高性能JSON解析)
- **配置管理**: github.com/joho/godotenv v1.5.1
- **Go版本**: 1.23.3
- **容器化**: Docker + Docker Compose 支持

## 环境变量配置

```bash
# 复制配置文件
cp .env.example .env

# === Token选择策略配置 ===
# 控制多个token时的使用顺序，支持三种策略：
TOKEN_SELECTION_STRATEGY=sequential     # 顺序使用（默认）：按配置顺序依次使用token，用完再用下一个
# TOKEN_SELECTION_STRATEGY=optimal     # 最优使用：选择可用次数最多的token
# TOKEN_SELECTION_STRATEGY=balanced    # 均衡使用：轮询所有可用token

# === Token管理配置（推荐使用JSON格式） ===
# 新的JSON格式配置方式，支持多认证方式和多token。
#
# 示例 1: 单个 Social 认证
# KIRO_AUTH_TOKEN='[{"auth":"Social","refreshToken":"your_social_refresh_token"}]'
#
# 示例 2: 混合认证 (Social + IdC)
# KIRO_AUTH_TOKEN='[{"auth":"Social","refreshToken":"social_token"},{"auth":"IdC","refreshToken":"idc_token","clientId":"idc_client","clientSecret":"idc_secret"}]'
#
# 示例 3: 多个 Social 认证 (用于负载均衡)
# KIRO_AUTH_TOKEN='[{"auth":"Social","refreshToken":"token1"},{"auth":"Social","refreshToken":"token2"}]'
#
# 默认配置结构:
KIRO_AUTH_TOKEN='[{"auth":"Social","refreshToken":"xxx"},{"auth":"IdC","refreshToken":"xxx","clientId":"xxx","clientSecret":"xxx"}]'''' 

# === 兼容传统环境变量（向后兼容） ===
# AWS_REFRESHTOKEN=token1,token2,token3    # 支持逗号分隔多token
# IDC_REFRESH_TOKEN=idc_token1,idc_token2  # 支持逗号分隔多token
# IDC_CLIENT_ID=your_idc_client_id         # IdC认证必需
# IDC_CLIENT_SECRET=your_idc_client_secret # IdC认证必需
# AUTH_METHOD=social                       # social(默认) 或 idc

# === 基础服务配置 ===
KIRO_CLIENT_TOKEN=123456                  # API认证密钥,默认: 123456
PORT=8080                                 # 服务端口,默认: 8080

# === Token使用监控配置 ===
# USAGE_CHECK_INTERVAL=5m                 # 使用状态检查间隔
# TOKEN_USAGE_THRESHOLD=5                 # 可用次数预警阈值
# TOKEN_SELECTION_STRATEGY=balanced       # optimal(最优使用) 或 balanced(均衡使用)

# === 缓存性能配置 ===
# CACHE_CLEANUP_INTERVAL=5m               # 缓存清理间隔
# TOKEN_CACHE_HOT_THRESHOLD=10            # 热点缓存阈值
# TOKEN_REFRESH_TIMEOUT=30s               # Token刷新超时时间

# === 日志配置 ===
LOG_LEVEL=info                            # debug,info,warn,error
LOG_FORMAT=json                           # text,json
# LOG_FILE=/var/log/kiro2api.log          # 可选:日志文件路径
# LOG_CONSOLE=true                        # 控制台输出开关

# === 超时配置 ===
# REQUEST_TIMEOUT_MINUTES=15              # 复杂请求超时(分钟)
# SIMPLE_REQUEST_TIMEOUT_MINUTES=2        # 简单请求超时(分钟)
# SERVER_READ_TIMEOUT_MINUTES=16          # 服务器读取超时
# SERVER_WRITE_TIMEOUT_MINUTES=16         # 服务器写入超时
# DISABLE_STREAM=false                    # 是否禁用流式响应
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
- **企业级Token管理**: 智能token选择、原子缓存、并发控制、使用限制监控
- **双认证方式**: Social和IdC认证，完全向后兼容传统配置
- **高性能缓存**: 原子操作的热点token缓存，冷热分离二级缓存
- **智能选择策略**: 最优使用和均衡使用策略，基于使用量的智能选择
- **流式优化**: 零延迟流式传输，对象池复用解析器实例

## 包结构 (按职责分层)

### 核心服务层
- **`server/`**: HTTP服务器和请求处理，遵循单一职责原则
  - `server.go` - 服务器初始化和路由配置
  - `handlers.go` - Anthropic API处理器
  - `openai_handlers.go` - OpenAI API处理器
  - `middleware.go` - 认证中间件
  - `common.go` - 公共响应处理
  - `sse_state_manager.go` - SSE状态管理器
  - `stop_reason_manager.go` - 停止原因管理器

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
  - `compliant_message_processor.go` - 消息处理
  - `tool_*` - 工具调用状态机和生命周期管理

### 基础设施层
- **`auth/`**: 企业级认证和token管理系统
  - `auth.go` - 认证接口和核心功能
  - `token_manager.go` - token池管理和选择策略
  - `usage_checker.go` - 使用限制实时监控
  - `config.go` - 配置提供者和管理
  - `refresh.go` - token刷新逻辑
- **`utils/`**: 高性能工具包
  - `atomic_cache.go` - 原子操作缓存系统
  - `token_refresh_manager.go` - 并发刷新控制管理器
  - 其他工具，遵循DRY原则避免重复
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

2. **Token选择策略扩展**
   - `auth/token_manager.go`: 实现新的选择策略（扩展现有选择逻辑）
   - `auth/config.go`: 添加新认证方式配置
   - `auth/usage_checker.go`: 扩展使用限制监控功能

### 性能优化 (遵循KISS原则)
1. **缓存系统调优**
   - `utils/atomic_cache.go`: 调整热点缓存阈值
   - 监控缓存命中率和清理效率
   - 优化冷热分离策略

2. **Token选择优化**
   - `auth/token_manager.go`: 优化选择算法性能
   - 调整使用量统计和预测模型
   - 测试不同负载下的选择策略效果

3. **流式响应调试**
   - `parser/robust_parser.go`: EventStream解析器错误恢复
   - `parser/compliant_message_processor.go`: 消息处理
   - 验证BigEndian格式的EventStream解析

### 代码质量改进 (遵循DRY和SOLID原则)
1. **接口抽象**: 为缓存系统、选择策略创建更多接口
2. **测试覆盖**: 重点测试新的token管理系统和缓存机制
3. **性能基准测试**: 建立token选择和缓存系统的性能基准
4. **监控指标**: 完善token使用情况和性能监控


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

### Token配置和刷新问题
```bash
# 检查新的JSON配置方式
echo $KIRO_AUTH_TOKEN

# 检查传统环境变量设置（兼容模式）
echo $AWS_REFRESHTOKEN
echo $IDC_REFRESH_TOKEN
echo $IDC_CLIENT_ID
echo $IDC_CLIENT_SECRET

# 启用调试日志查看token管理详情
LOG_LEVEL=debug ./kiro2api
```

**常见问题排查：**
- **JSON配置格式错误**: 验证KIRO_AUTH_TOKEN的JSON格式是否正确
- **认证方式不匹配**: 确认Auth字段为"Social"或"IdC"
- **IdC认证缺少参数**: IdC方式需要ClientId和ClientSecret
- **Token已过期**: 查看日志中的刷新尝试和失败信息
- **使用限制达到**: 检查VIBE资源使用量和限制
- **并发刷新冲突**: 查看刷新管理器的并发控制日志

### 流式响应中断
```bash
# 测试流式连接
curl -N --max-time 60 -X POST http://localhost:8080/v1/messages \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer 123456" \
  -d '{"model": "claude-sonnet-4-20250514", "stream": true, "messages": [...]}'
```
- 检查客户端连接稳定性
- 验证EventStream解析器状态

### 性能问题
```bash
# 启用详细日志进行调试
LOG_LEVEL=debug ./kiro2api
```
- 监控HTTP客户端连接池状态
- 检查对象池使用情况
- 分析请求复杂度评估准确性