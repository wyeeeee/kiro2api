# 🚀 kiro2api

<div align="center">

**高性能 AI API 代理服务器**

*统一 Anthropic Claude、OpenAI 和 AWS CodeWhisperer 的智能网关*

[![Go](https://img.shields.io/badge/Go-1.23.3-blue.svg)](https://golang.org/)
[![Gin](https://img.shields.io/badge/Gin-1.10.1-green.svg)](https://github.com/gin-gonic/gin)
[![Docker](https://img.shields.io/badge/Docker-Ready-blue.svg)](https://www.docker.com/)

</div>

## 🎯 为什么选择 kiro2api？
### 💡 四大核心优势

#### 1. 🤖 Claude Code 原生集成

```bash
# 一行配置，立即享受本地代理
export ANTHROPIC_BASE_URL="http://localhost:8080/v1"
export ANTHROPIC_API_KEY="your-kiro-token"

# Claude Code 无感切换，所有功能完美支持
claude-code --model claude-sonnet-4 "帮我重构这段代码"
```

**支持功能**:
- ✅ 完整 Anthropic API 兼容
- ✅ 流式响应零延迟
- ✅ 工具调用完整支持
- ✅ 多模态图片处理

#### 2. 🎛️ 智能多账号池管理

```json
{
  "多账号配置": [
    {"auth": "Social", "refreshToken": "个人账号1", "quota": "高优先级"},
    {"auth": "Social", "refreshToken": "个人账号2", "quota": "备用账号"},
    {"auth": "IdC", "refreshToken": "企业账号", "quota": "无限制"}
  ],
  "智能策略": "optimal - 自动选择可用次数最多的账号"
}
```

**智能特性**:
- 🎯 **最优选择**: 自动选择可用次数最多的账号（设置 `TOKEN_SELECTION_STRATEGY=optimal`）
- 🔄 **故障转移**: 账号用完自动切换到下一个
- 📊 **使用监控**: 实时监控每个账号的使用情况
- ⚡ **选择策略**: 默认顺序使用（`sequential`），可切换为 `optimal`

#### 3. 🏢 双认证方式支持

```bash
# Social 认证 
KIRO_AUTH_TOKEN='[{"auth":"Social","refreshToken":"your-social-token"}]'

# IdC 认证 
KIRO_AUTH_TOKEN='[{
  "auth":"IdC",
  "refreshToken":"enterprise-token",
  "clientId":"enterprise-client-id",
  "clientSecret":"enterprise-secret"
}]'

# 混合认证 - 最佳实践
KIRO_AUTH_TOKEN='[
  {"auth":"IdC","refreshToken":"primary-enterprise"},
  {"auth":"Social","refreshToken":"backup-personal"}
]'
```

#### 4. 📸 图片输入支持（data URL）

```bash
# Claude Code 中直接使用图片
claude-code "分析这张图片的内容" --image screenshot.png

# 支持的图片格式
✅ data URL 的 PNG/JPEG 等常见格式
⚠️ 目前仅支持 `data:` URL，不支持远程 HTTP 图片地址
```

**说明**:
- Claude Code 传入本地图片时会转为 `data:` URL，服务端按照 `Anthropic`/`OpenAI` 规范解析并转发。
- 不做额外图片压缩或远程下载处理，避免引入不必要复杂度（KISS/YAGNI）。

### 🎯 典型使用场景

#### 场景 1: 

```bash
# 解决：本地 kiro2api 代理

# 1. 启动 kiro2api
docker run -d -p 8080:8080 \
  -e KIRO_AUTH_TOKEN='[{"auth":"Social","refreshToken":"your-token"}]' \
  ghcr.io/caidaoli/kiro2api:latest

# 2. 配置 Claude Code
export ANTHROPIC_BASE_URL="http://localhost:8080/v1"

# 3. 享受稳定的 AI 编程体验
claude-code "重构这个函数，提高性能" --file main.go
```

#### 场景 2:

```bash
# 解决：多账号池 

# 3 个 Social 账号轮换使用
KIRO_AUTH_TOKEN='[
  {"auth":"Social","refreshToken":"dev-account-1"},
  {"auth":"Social","refreshToken":"dev-account-2"},
  {"auth":"Social","refreshToken":"dev-account-3"}
]'

# 可用性提升：单账号故障不影响团队工作
```



## 🏗️ 系统架构

```mermaid
graph TB
    subgraph "客户端层"
        A1[Anthropic Client]
        A2[OpenAI Client]
        A3[Claude Code]
        A4[Custom Apps]
    end

    subgraph "kiro2api 核心"
        B[API Gateway]
        C[认证中间件]
        D[请求分析器]
        E[格式转换器]
        F[Token 管理器]
        G[流式处理器]
        H[监控系统]
    end

    subgraph "认证层"
        I1[Social Auth Pool]
        I2[IdC Auth Pool]
        I3[Token Cache]
    end

    subgraph "后端服务"
        J[AWS CodeWhisperer]
    end

    A1 --> B
    A2 --> B
    A3 --> B
    A4 --> B

    B --> C
    C --> D
    D --> E
    E --> F
    F --> G
    G --> H

    F --> I1
    F --> I2
    F --> I3

    G --> J
    G --> K
    G --> L

    style B fill:#e1f5fe
    style F fill:#f3e5f5
    style G fill:#e8f5e8
```

## ✨ 核心特性矩阵

### 🎯 核心功能

| 特性分类 | 功能 | 支持状态 | 描述 |
|----------|------|----------|------|
| 🔄 **API 兼容** | Anthropic API | ✅ | 完整的 Claude API 支持 |
| | OpenAI API | ✅ | ChatCompletion 格式兼容 |
| 🎛️ **负载管理** | 单账号 | ✅ | 基础 Token 管理 |
| | 多账号池 | ✅ | 智能负载均衡 |
| | 故障转移 | ✅ | 自动切换机制 |
| 🔐 **认证方式** | Social 认证 | ✅ | AWS SSO 认证 |
| | IdC 认证 | ✅ | 身份中心认证 |
| | 混合认证 | ✅ | 多认证方式并存 |
| 📊 **监控运维** | 基础日志 | ✅ | 标准日志输出 |
| | 使用监控 | ✅ | 实时使用量统计 |
| ⚡ **性能优化** | 流式响应 | ✅ | SSE 实时传输 |
| | 智能缓存 | ✅ | Token 缓存（无响应缓存） |
| | 并发控制 | ✅ | Token 刷新并发控制 |

### 🚀 高级特性

| 特性 | 描述 | 技术实现 |
|------|------|----------|
| 📸 **多模态支持** | data URL 的 PNG/JPEG 图片 | Base64 编码 + 格式转换 |
| 🛠️ **工具调用** | 完整 Anthropic 工具使用支持 | 状态机 + 生命周期管理 |
| 🔄 **格式转换** | Anthropic ↔ OpenAI ↔ CodeWhisperer | 智能协议转换器 |
| ⚡ **零延迟流式** | 实时流式传输优化 | EventStream 解析 + 对象池 |
| 🎯 **智能选择** | 最优/均衡 Token 策略 | 使用量预测 + 负载均衡 |

## 技术栈

- **Web框架**: gin-gonic/gin v1.10.1
- **JSON处理**: bytedance/sonic v1.14.0  
- **配置管理**: github.com/joho/godotenv v1.5.1
- **Go版本**: 1.23.3
- **容器化**: Docker & Docker Compose 支持

## 快速开始

### 基础运行

```bash
# 克隆并编译
git clone <repository-url>
cd kiro2api
go build -o kiro2api main.go

# 配置环境变量
cp .env.example .env
# 编辑 .env 文件，设置 KIRO_AUTH_TOKEN 或 AWS_REFRESHTOKEN

# 启动服务器
./kiro2api

# 测试API
curl -X POST http://localhost:8080/v1/messages \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer 123456" \
  -d '{"model": "claude-sonnet-4-20250514", "max_tokens": 100, "messages": [{"role": "user", "content": "你好"}]}'
```

### 🐳 Docker 部署

#### 快速开始

Token获取方式：
 - Social tokens: 通常在 ~/.aws/sso/cache/kiro-auth-token.json
 - IdC tokens: 在 ~/.aws/sso/cache/ 目录下的相关JSON文件中
```bash
# 方式一：使用 docker-compose（推荐）
docker-compose up -d

# 方式二：预构建镜像
docker run -d \
  --name kiro2api \
  -p 8080:8080 \
  -e KIRO_AUTH_TOKEN='[{"auth":"Social","refreshToken":"your_token"}]' \
  -e KIRO_CLIENT_TOKEN="123456" \
  ghcr.io/caidaoli/kiro2api:latest

# 方式三：本地构建
docker build -t kiro2api .
docker run -d \
  --name kiro2api \
  -p 8080:8080 \
  --env-file .env \
  kiro2api
```


#### 🔧 配置管理

##### 环境变量文件
```bash
# .env.docker
# 多账号池配置
KIRO_AUTH_TOKEN='[
  {
    "auth": "Social",
    "refreshToken": "arn:aws:sso:us-east-1:999999999999:token/refresh/xxx"
  },
  {
    "auth": "IdC",
    "refreshToken": "arn:aws:identitycenter::us-east-1:999999999999:account/instance/xxx",
    "clientId": "https://oidc.us-east-1.amazonaws.com/clients/xxx",
    "clientSecret": "xxx-secret-key-xxx"
  }
]'

# 负载均衡策略
TOKEN_SELECTION_STRATEGY=sequential

# 服务配置
KIRO_CLIENT_TOKEN=your-secure-token
PORT=8080
GIN_MODE=release

# 生产级日志
LOG_LEVEL=info
LOG_FORMAT=json
LOG_CONSOLE=true

# 性能调优
REQUEST_TIMEOUT_MINUTES=15
SERVER_READ_TIMEOUT_MINUTES=35
SERVER_WRITE_TIMEOUT_MINUTES=35
```

##### Docker Secrets（注意事项）
```bash
# 若使用 Docker Secrets，请将 `KIRO_AUTH_TOKEN` 设置为 secrets 文件路径：
# 例如：/run/secrets/kiro_auth_token
#
# 说明：代码支持将 `KIRO_AUTH_TOKEN` 当作“文件路径”读取；
#      但不支持 `*_FILE` 环境变量约定，也不读取 `KIRO_CLIENT_TOKEN_FILE`。
```


#### 📊 健康检查和监控

```bash
# 健康检查
docker exec kiro2api wget -qO- http://localhost:8080/v1/models

# 查看日志
docker logs -f kiro2api

# 监控资源使用
docker stats kiro2api

# 进入容器调试
docker exec -it kiro2api sh
```

## API 接口

### 支持的端点

- `GET /` - 静态首页（Dashboard）
- `GET /static/*` - 静态资源
- `GET /api/tokens` - Token 池状态与使用信息
- `GET /v1/models` - 获取可用模型列表
- `POST /v1/messages` - Anthropic Claude API 兼容接口（支持流/非流）
- `POST /v1/chat/completions` - OpenAI ChatCompletion API 兼容接口（支持流/非流）

### 认证方式

所有 API 端点都需要在请求头中提供认证信息：

```bash
# 使用 Authorization Bearer 认证
Authorization: Bearer your-auth-token

# 或使用 x-api-key 认证
x-api-key: your-auth-token
```

### 请求示例

```bash
# Anthropic API 格式
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

# OpenAI API 格式
curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer 123456" \
  -d '{
    "model": "claude-sonnet-4-20250514",
    "messages": [
      {"role": "user", "content": "解释一下机器学习的基本概念"}
    ]
  }'

# 流式请求（添加 "stream": true）
curl -N -X POST http://localhost:8080/v1/messages \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer 123456" \
  -d '{
    "model": "claude-sonnet-4-20250514",
    "max_tokens": 200,
    "stream": true,
    "messages": [{"role": "user", "content": "讲个故事"}]
  }'
```

## 支持的模型

| 公开模型名称 | 内部 CodeWhisperer 模型 ID |
|-------------|---------------------------|
| `claude-sonnet-4-5-20250929` | `CLAUDE_SONNET_4_5_20250929_V1_0` |
| `claude-sonnet-4-20250514` | `CLAUDE_SONNET_4_20250514_V1_0` |
| `claude-3-7-sonnet-20250219` | `CLAUDE_3_7_SONNET_20250219_V1_0` |
| `claude-3-5-haiku-20241022` | `auto` |

## 🔧 环境配置指南

### 🏢 多账号池配置

#### 配置方式对比

| 配置方式 | 适用场景 | 优势 | 限制 |
|----------|----------|------|------|
| 🔑 **JSON 配置** | 生产级部署 | 多认证方式、智能负载均衡 | 配置相对复杂 |
| 📝 **环境变量** | 快速测试 | 简单直接、向后兼容 | 功能有限 |

#### JSON 格式配置（推荐）

**多账号池配置示例：**

```bash
# 完整的生产级配置
export KIRO_AUTH_TOKEN='[
  {
    "auth": "Social",
    "refreshToken": "arn:aws:sso:us-east-1:999999999999:token/refresh/social-token-1",
    "description": "开发团队主账号"
  },
  {
    "auth": "Social",
    "refreshToken": "arn:aws:sso:us-east-1:999999999999:token/refresh/social-token-2",
    "description": "开发团队备用账号"
  },
  {
    "auth": "IdC",
    "refreshToken": "arn:aws:identitycenter::us-east-1:999999999999:account/instance/idc-token",
    "clientId": "https://oidc.us-east-1.amazonaws.com/clients/enterprise-client",
    "clientSecret": "enterprise-secret-key",
    "description": "生产级账号"
  }
]'
```


### ⚙️ 系统配置

#### 基础服务配置

```bash
# === 核心配置 ===
KIRO_CLIENT_TOKEN=your-secure-api-key    # API 认证密钥（建议使用强密码）
PORT=8080                                # 服务端口
GIN_MODE=release                         # 运行模式：debug/release/test

# === 性能调优 ===
# 请求超时配置（分钟）
REQUEST_TIMEOUT_MINUTES=15               # 复杂请求超时
SIMPLE_REQUEST_TIMEOUT_MINUTES=2         # 简单请求超时
STREAM_REQUEST_TIMEOUT_MINUTES=30        # 流式请求超时

# 服务器超时配置（分钟）
SERVER_READ_TIMEOUT_MINUTES=35           # 服务器读取超时
SERVER_WRITE_TIMEOUT_MINUTES=35          # 服务器写入超时

```

#### 生产级日志配置

```bash
# === 日志系统 ===
LOG_LEVEL=info                           # 日志级别：debug/info/warn/error
LOG_FORMAT=json                          # 日志格式：text/json
LOG_CONSOLE=true                         # 控制台输出开关
LOG_FILE=/var/log/kiro2api.log          # 日志文件路径（可选）

# === 结构化日志字段 ===
# 自动包含以下字段：
# - timestamp: 时间戳
# - level: 日志级别
# - service: 服务名称
# - request_id: 请求唯一标识
# - user_id: 用户标识（如果可用）
# - token_usage: Token 使用情况
# - response_time: 响应时间
```

#### 高级功能配置

```bash
# === 流式处理优化 ===
DISABLE_STREAM=false                     # 是否禁用流式响应

```



### 架构图

```mermaid
graph LR
    subgraph "客户端层"
        A[Anthropic Client]
        B[OpenAI Client]
    end

    subgraph "kiro2api 核心"
        C[认证中间件]
        D[请求分析器]
        E[格式转换器]
        F[Token 管理器]
        G[流式处理器]
    end

    subgraph "后端服务"
        H[AWS CodeWhisperer]
        I[Social Auth]
        J[IdC Auth]
    end

    A --> C
    B --> C
    C --> D
    D --> E
    E --> F
    F --> G
    G --> H
    F --> I
    F --> J
```

## 故障排除

### 🔍 故障诊断

#### Token 认证问题

```bash
# 检查配置
echo $KIRO_AUTH_TOKEN

# 启用调试日志
LOG_LEVEL=debug ./kiro2api
```

| 错误类型 | 解决方案 |
|----------|----------|
| 🚨 JSON 格式错误 | 使用 JSON 验证器检查格式 |
| 🔐 认证方式错误 | 确认 `auth` 字段为 "Social" 或 "IdC" |
| 📋 参数缺失 | IdC 认证需要 `clientId` 和 `clientSecret` |
| ⏰ Token 过期 | 查看日志中的刷新状态 |

#### API 连接问题

```bash
# 测试 API 连通性
curl -v -H "Authorization: Bearer 123456" \
  http://localhost:8080/v1/models

# 测试流式响应
curl -N --max-time 60 -X POST \
  http://localhost:8080/v1/messages \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer 123456" \
  -d '{"model": "claude-sonnet-4-20250514", "stream": true, "messages": [{"role": "user", "content": "测试"}]}'
```

#### Docker 部署问题

| 问题类型 | 症状 | 解决方案 |
|----------|------|----------|
| 🐳 **容器启动失败** | 容器立即退出 | 检查环境变量配置，查看容器日志 |
| 🔌 **端口冲突** | 端口已被占用 | 修改 docker-compose.yml 中的端口映射 |
| 💾 **数据卷权限** | AWS SSO 缓存失败 | 确保容器用户有权限访问数据卷 |
| 🌐 **网络连接** | 无法访问外部 API | 检查 Docker 网络配置和防火墙设置 |

```bash
# Docker 故障排除命令
# 查看容器状态
docker ps -a

# 查看容器日志
docker logs kiro2api --tail 100 -f

# 检查容器内部
docker exec -it kiro2api sh
ps aux
netstat -tlnp
env | grep KIRO

# 检查服务连通性（本地）
docker exec kiro2api wget -qO- http://localhost:8080/v1/models || echo "服务不可用"

# 重新构建和启动
docker-compose down -v
docker-compose build --no-cache
docker-compose up -d

# 检查资源使用
docker stats kiro2api
```

#### Claude Code 集成问题

| 问题类型 | 症状 | 解决方案 |
|----------|------|----------|
| 🔗 **代理连接失败** | Claude Code 无法连接到 kiro2api | 检查 baseURL 和网络连通性 |
| 🔑 **认证失败** | 401 Unauthorized 错误 | 验证 apiKey 配置和 KIRO_CLIENT_TOKEN |
| 📡 **流式响应中断** | 流式输出不完整 | 检查网络稳定性和超时配置 |

```bash
# Claude Code 集成调试
# 测试基础连接
curl -H "Authorization: Bearer $KIRO_CLIENT_TOKEN" \
  http://localhost:8080/v1/models

# 测试流式响应
curl -N -H "Authorization: Bearer $KIRO_CLIENT_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"model":"claude-sonnet-4-20250514","stream":true,"messages":[{"role":"user","content":"test"}]}' \
  http://localhost:8080/v1/messages

```





## 📚 更多资源

- 📖 **详细开发指南**: [CLAUDE.md](./CLAUDE.md)
- 🏗️ **包结构说明**: 分层架构设计，遵循 SOLID 原则
- ⚡ **性能优化**: 缓存策略、并发控制、内存管理
- 🔧 **核心开发任务**: 扩展功能、性能调优、高级特性
- 🤖 **Claude Code 官方文档**: [claude.ai/code](https://claude.ai/code)
- 🐳 **Docker 最佳实践**: 容器化部署指南

## 🤝 贡献指南

我们欢迎社区贡献！直接提交 Issue 或 Pull Request 即可。

### 快速贡献

1. Fork 项目
2. 创建特性分支 (`git checkout -b feature/amazing-feature`)
3. 提交更改 (`git commit -m 'Add amazing feature'`)
4. 推送到分支 (`git push origin feature/amazing-feature`)
5. 创建 Pull Request

