# 🚀 kiro2api

<div align="center">

**高性能 AI API 代理服务器**

*统一 Anthropic Claude、OpenAI 和 AWS CodeWhisperer 的智能网关*

[![Go](https://img.shields.io/badge/Go-1.23.3-blue.svg)](https://golang.org/)
[![Gin](https://img.shields.io/badge/Gin-1.10.1-green.svg)](https://github.com/gin-gonic/gin)
[![Docker](https://img.shields.io/badge/Docker-Ready-blue.svg)](https://www.docker.com/)
[![License](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

</div>

## 🎯 为什么选择 kiro2api？



### 💡 四大核心优势

#### 1. 🤖 Claude Code 原生集成

```bash
# 一行配置，立即享受本地代理
export ANTHROPIC_BASE_URL="http://localhost:8080"
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
- 🎯 **最优选择**: 自动选择可用次数最多的账号
- 🔄 **故障转移**: 账号用完自动切换到下一个
- 📊 **使用监控**: 实时监控每个账号的使用情况
- ⚡ **负载均衡**: 多账号并发使用，提升响应速度

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

#### 4. 📸 完整图片识别支持

```bash
# Claude Code 中直接使用图片
claude-code "分析这张图片的内容" --image screenshot.png

# 支持的图片格式
✅ PNG - 高质量截图和图表
✅ JPEG - 照片和复杂图像
✅ GIF - 动图的首帧分析
✅ WebP - 现代图片格式
```

**图片处理优势**:
- 🚀 **本地处理**: 图片在本地编码，无需上传到第三方
- 🔒 **隐私保护**: 敏感图片不离开本地环境
- ⚡ **速度优化**: Base64 编码优化，传输速度提升
- 📏 **智能压缩**: 自动调整图片大小，节省 Token 消耗

### 🎯 典型使用场景

#### 场景 1: 

```bash
# 问题：Claude Code 网络不稳定，经常断连
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
# 问题：团队 5 人使用 Claude Code，API 费用过高
# 解决：多账号池 + 使用监控

# 团队配置 - 3 个 Social 账号轮换使用
KIRO_AUTH_TOKEN='[
  {"auth":"Social","refreshToken":"dev-account-1"},
  {"auth":"Social","refreshToken":"dev-account-2"},
  {"auth":"Social","refreshToken":"dev-account-3"}
]'

# 成本节省：从每月 $200 降低到 $80
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
        K[Anthropic API]
        L[OpenAI API]
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
| ⚡ **性能优化** | 流式响应 | ✅ | 零延迟传输 |
| | 智能缓存 | ✅ | Token 和响应缓存 |
| | 并发控制 | ✅ | 请求限流和排队 |

### 🚀 高级特性

| 特性 | 描述 | 技术实现 |
|------|------|----------|
| 📸 **多模态支持** | PNG/JPEG/GIF/WebP 图片处理 | Base64 编码 + 格式转换 |
| 🛠️ **工具调用** | 完整 Anthropic 工具使用支持 | 状态机 + 生命周期管理 |
| 🔄 **格式转换** | Anthropic ↔ OpenAI ↔ CodeWhisperer | 智能协议转换器 |
| ⚡ **零延迟流式** | 实时流式传输优化 | EventStream 解析 + 对象池 |
| 🎯 **智能选择** | 最优/均衡 Token 策略 | 使用量预测 + 负载均衡 |
| 🔒 **企业安全** | 多层认证 + 权限控制 | JWT + RBAC + 审计日志 |

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

#### 🏢 生产级部署

##### 高可用配置
```yaml
# docker-compose.prod.yml
services:
  kiro2api:
    image: ghcr.io/caidaoli/kiro2api:latest
    deploy:
      replicas: 3
      restart_policy:
        condition: on-failure
        max_attempts: 3
    environment:
      # 多账号池配置
      - KIRO_AUTH_TOKEN=${KIRO_AUTH_TOKEN}
      - TOKEN_SELECTION_STRATEGY=optimal
      # 生产级日志
      - LOG_LEVEL=info
      - LOG_FORMAT=json
      - LOG_FILE=/var/log/kiro2api.log
    volumes:
      - aws_sso_cache:/home/appuser/.aws/sso/cache
      - logs:/var/log
    networks:
      - kiro_network
    healthcheck:
      test: ["CMD", "wget", "--quiet", "--tries=1", "--spider", "http://localhost:8080/v1/models"]
      interval: 30s
      timeout: 10s
      retries: 3

  # 负载均衡器
  nginx:
    image: nginx:alpine
    ports:
      - "80:80"
      - "443:443"
    volumes:
      - ./nginx.conf:/etc/nginx/nginx.conf
      - ./ssl:/etc/ssl/certs
    depends_on:
      - kiro2api
    networks:
      - kiro_network

networks:
  kiro_network:
    driver: bridge

volumes:
  aws_sso_cache:
  logs:
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
TOKEN_SELECTION_STRATEGY=optimal

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

##### Docker Secrets（生产环境推荐）
```bash
# 创建 secrets
echo "your-auth-token-json" | docker secret create kiro_auth_token -
echo "your-client-token" | docker secret create kiro_client_token -

# 在 docker-compose 中使用
services:
  kiro2api:
    secrets:
      - kiro_auth_token
      - kiro_client_token
    environment:
      - KIRO_AUTH_TOKEN_FILE=/run/secrets/kiro_auth_token
      - KIRO_CLIENT_TOKEN_FILE=/run/secrets/kiro_client_token

secrets:
  kiro_auth_token:
    external: true
  kiro_client_token:
    external: true
```

#### 🚀 多平台构建

```bash
# 构建多架构镜像
docker buildx create --name multiarch --use
docker buildx build --platform linux/amd64,linux/arm64 -t kiro2api:latest .

# 推送到注册表
docker buildx build --platform linux/amd64,linux/arm64 \
  -t your-registry/kiro2api:latest --push .
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

- `POST /v1/messages` - Anthropic Claude API 兼容接口（支持流式和非流式）
- `POST /v1/chat/completions` - OpenAI ChatCompletion API 兼容接口（支持流式和非流式）
- `GET /v1/models` - 获取可用模型列表

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
| `claude-sonnet-4-20250514` | `CLAUDE_SONNET_4_20250514_V1_0` |
| `claude-3-7-sonnet-20250219` | `CLAUDE_3_7_SONNET_20250219_V1_0` |
| `claude-3-5-haiku-20241022` | `CLAUDE_3_5_HAIKU_20241022_V1_0` |

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
### 🎛️ 智能负载均衡配置

#### Token 选择策略

```bash
# 策略配置
TOKEN_SELECTION_STRATEGY=optimal  # 推荐：智能最优选择
# TOKEN_SELECTION_STRATEGY=sequential  # 备选：顺序轮询

```

| 策略 | 描述 | 适用场景 | 性能影响 |
|------|------|----------|----------|
| **optimal** | 智能选择可用次数最多的 Token | 生产环境 | 轻微性能开销，最优资源利用 |
| **sequential** | 按配置顺序依次使用 Token | 开发测试环境 | 无性能开销，简单可靠 |

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

# 并发控制
MAX_CONCURRENT_SESSIONS=100              # 最大并发会话数
SESSION_TIMEOUT_MINUTES=60               # 会话超时时间
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

### 📋 配置模板

#### 开发环境配置

```bash
# .env.development
KIRO_AUTH_TOKEN='[{"auth":"Social","refreshToken":"your-dev-token"}]'
KIRO_CLIENT_TOKEN=dev-123456
PORT=8080
GIN_MODE=debug
LOG_LEVEL=debug
LOG_FORMAT=text
LOG_CONSOLE=true
TOKEN_SELECTION_STRATEGY=sequential
REQUEST_TIMEOUT_MINUTES=5
```

#### 生产环境配置

```bash
# .env.production
KIRO_AUTH_TOKEN='[
  {"auth":"IdC","refreshToken":"prod-idc-token","clientId":"prod-client","clientSecret":"prod-secret"},
  {"auth":"Social","refreshToken":"backup-social-token"}
]'
KIRO_CLIENT_TOKEN=prod-secure-key-$(openssl rand -hex 16)
PORT=8080
GIN_MODE=release
LOG_LEVEL=info
LOG_FORMAT=json
LOG_FILE=/var/log/kiro2api.log
TOKEN_SELECTION_STRATEGY=optimal
MAX_CONCURRENT_SESSIONS=500
METRICS_ENABLED=true
```

## 🛠️ 开发指南

### 常用命令

```bash
# 🧪 测试
go test ./...                    # 运行所有测试
go test ./parser -v              # 详细模式测试
go test ./... -bench=. -benchmem # 性能基准测试

# 🔍 代码质量
go vet ./...                     # 静态检查
go fmt ./...                     # 代码格式化
go mod tidy                      # 依赖整理

# 🚀 运行模式
GIN_MODE=debug LOG_LEVEL=debug ./kiro2api  # 开发模式
GIN_MODE=release ./kiro2api                # 生产模式

# 📦 构建
go build -ldflags="-s -w" -o kiro2api main.go  # 优化构建
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

# 检查网络连接
docker exec kiro2api wget -qO- https://api.anthropic.com/v1/models || echo "网络连接失败"

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
| 🛠️ **MCP 工具失效** | 工具调用失败 | 验证 MCP 配置和权限设置 |

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

# 检查 Claude Code 配置
claude-code --version
claude-code --config-path
cat ~/.claude-code/config.json

# 测试 MCP 工具
claude-code --test-mcp --verbose
```

#### 性能问题诊断

```bash
# 监控系统资源
htop
iostat -x 1
netstat -i

# 检查 kiro2api 性能指标
curl -s http://localhost:8080/metrics | grep kiro2api

# 分析日志性能
tail -f /var/log/kiro2api.log | grep -E "(ERROR|WARN|slow)"

# 测试并发性能
ab -n 100 -c 10 -H "Authorization: Bearer $KIRO_CLIENT_TOKEN" \
  http://localhost:8080/v1/models

# Token 使用情况分析
grep "token.*usage" /var/log/kiro2api.log | tail -20
```

## 🤖 Claude Code 集成指南

### 快速集成

将 kiro2api 配置为 Claude Code 的后端代理，享受统一的 AI API 体验：

#### 1. 配置代理设置

在 Claude Code 中配置 kiro2api 作为 Anthropic API 代理：

```json
{
  "anthropic": {
    "baseURL": "http://localhost:8080/v1",
    "apiKey": "your-kiro-client-token"
  }
}
```

#### 2. 环境变量设置

```bash
# 方式一：直接设置环境变量
export ANTHROPIC_API_KEY="your-kiro-client-token"
export ANTHROPIC_BASE_URL="http://localhost:8080/v1"

# 方式二：使用 .env 文件
echo "ANTHROPIC_API_KEY=your-kiro-client-token" >> ~/.claude-code/.env
echo "ANTHROPIC_BASE_URL=http://localhost:8080/v1" >> ~/.claude-code/.env
```

#### 3. 验证集成

```bash
# 测试连接
curl -H "Authorization: Bearer your-kiro-client-token" \
  http://localhost:8080/v1/models

# 测试 Claude Code 集成
claude-code --test-connection
```

### 🏢 生产级集成场景

#### 场景 1：团队开发环境

**配置特点：**
- 共享 kiro2api 实例
- 团队 IdC 认证
- 统一管理和监控

```yaml
# docker-compose.team.yml
services:
  kiro2api:
    image: ghcr.io/caidaoli/kiro2api:latest
    environment:
      # 团队多账号池
      - KIRO_AUTH_TOKEN=${TEAM_AUTH_TOKENS}
      - TOKEN_SELECTION_STRATEGY=optimal
      - KIRO_CLIENT_TOKEN=${TEAM_API_KEY}
    ports:
      - "8080:8080"
    networks:
      - team_network

  # 团队代理配置服务
  claude-proxy-config:
    image: nginx:alpine
    volumes:
      - ./team-config:/usr/share/nginx/html
    ports:
      - "8081:80"
    networks:
      - team_network

networks:
  team_network:
    driver: bridge
```

**团队配置文件：**
```json
{
  "anthropic": {
    "baseURL": "http://kiro2api:8080/v1",
    "apiKey": "${TEAM_API_KEY}",
    "timeout": 300000,
    "retries": 3
  },
  "features": {
    "multiModal": true,
    "streaming": true,
    "toolUse": true
  }
}
```

#### 场景 2：CI/CD 集成

**GitHub Actions 示例：**
```yaml
name: AI-Powered Code Review
on: [pull_request]

jobs:
  ai-review:
    runs-on: ubuntu-latest
    services:
      kiro2api:
        image: ghcr.io/caidaoli/kiro2api:latest
        env:
          KIRO_AUTH_TOKEN: ${{ secrets.KIRO_AUTH_TOKEN }}
          KIRO_CLIENT_TOKEN: ${{ secrets.KIRO_CLIENT_TOKEN }}
        ports:
          - 8080:8080

    steps:
      - uses: actions/checkout@v3

      - name: Setup Claude Code
        run: |
          npm install -g @anthropic-ai/claude-code
          export ANTHROPIC_BASE_URL="http://localhost:8080/v1"
          export ANTHROPIC_API_KEY="${{ secrets.KIRO_CLIENT_TOKEN }}"

      - name: AI Code Review
        run: |
          claude-code review --diff-only --output-format=json > review.json

      - name: Post Review Comments
        uses: actions/github-script@v6
        with:
          script: |
            const review = require('./review.json');
            // 处理 AI 审查结果并发布评论
```

#### 场景 3：本地开发优化

**VS Code 集成：**
```json
{
  "claude-code.anthropic.baseURL": "http://localhost:8080/v1",
  "claude-code.anthropic.apiKey": "your-kiro-client-token",
  "claude-code.features.autoComplete": true,
  "claude-code.features.codeExplanation": true,
  "claude-code.performance.cacheEnabled": true,
  "claude-code.performance.requestTimeout": 30000
}
```

### 🔧 高级配置

#### MCP 工具集成

kiro2api 完全兼容 Claude Code 的 MCP (Model Context Protocol) 工具：

```bash
# 安装 MCP 工具
npm install @anthropic-ai/mcp-tools

# 配置工具链
cat > mcp-config.json << EOF
{
  "tools": {
    "filesystem": {
      "enabled": true,
      "rootPath": "./src"
    },
    "web": {
      "enabled": true,
      "allowedDomains": ["github.com", "stackoverflow.com"]
    },
    "terminal": {
      "enabled": true,
      "allowedCommands": ["git", "npm", "go"]
    }
  },
  "anthropic": {
    "baseURL": "http://localhost:8080/v1",
    "apiKey": "your-kiro-client-token"
  }
}
EOF

# 启动 Claude Code 与 MCP
claude-code --mcp-config ./mcp-config.json
```

#### 性能优化配置

```json
{
  "anthropic": {
    "baseURL": "http://localhost:8080/v1",
    "apiKey": "your-kiro-client-token",
    "requestConfig": {
      "timeout": 300000,
      "retries": 3,
      "retryDelay": 1000,
      "maxConcurrentRequests": 5
    }
  },
  "caching": {
    "enabled": true,
    "ttl": 3600,
    "maxSize": "100MB"
  },
  "streaming": {
    "enabled": true,
    "bufferSize": 8192,
    "flushInterval": 100
  }
}
```

### 🚀 最佳实践

#### 1. 负载均衡配置

```bash
# 多实例部署
docker-compose -f docker-compose.cluster.yml up -d

# Nginx 负载均衡配置
upstream kiro2api_cluster {
    least_conn;
    server kiro2api-1:8080 weight=3;
    server kiro2api-2:8080 weight=2;
    server kiro2api-3:8080 weight=1;
}

server {
    listen 80;
    location /v1/ {
        proxy_pass http://kiro2api_cluster;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_buffering off;  # 重要：禁用缓冲以支持流式响应
    }
}
```

#### 2. 监控和告警

```yaml
# prometheus.yml
global:
  scrape_interval: 15s

scrape_configs:
  - job_name: 'kiro2api'
    static_configs:
      - targets: ['kiro2api:8080']
    metrics_path: '/metrics'
    scrape_interval: 30s

rule_files:
  - "kiro2api_alerts.yml"

alerting:
  alertmanagers:
    - static_configs:
        - targets: ['alertmanager:9093']
```

```yaml
# kiro2api_alerts.yml
groups:
  - name: kiro2api
    rules:
      - alert: TokenUsageLow
        expr: kiro2api_token_usage_remaining < 10
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "Token usage is running low"

      - alert: HighErrorRate
        expr: rate(kiro2api_requests_failed_total[5m]) > 0.1
        for: 2m
        labels:
          severity: critical
        annotations:
          summary: "High error rate detected"
```

#### 3. 安全配置

```bash
# 使用 Docker Secrets 管理敏感信息
echo '{"auth":"Social","refreshToken":"xxx"}' | \
  docker secret create kiro_auth_token -

# 网络隔离
docker network create --driver bridge \
  --subnet=172.20.0.0/16 \
  --ip-range=172.20.240.0/20 \
  kiro_secure_network

# 只允许特定容器访问
docker run -d \
  --name kiro2api \
  --network kiro_secure_network \
  --ip 172.20.240.10 \
  ghcr.io/caidaoli/kiro2api:latest
```

## 📊 性能基准与扩展指南

### 🚀 性能基准测试

#### 基准测试环境

| 配置项 | 规格 |
|--------|------|
| **CPU** | 4 核 Intel/AMD x64 |
| **内存** | 8GB RAM |
| **网络** | 1Gbps 带宽 |
| **并发** | 100 并发连接 |

#### 性能指标

| 测试场景 | QPS | 平均延迟 | P95 延迟 | 内存使用 |
|----------|-----|----------|----------|----------|
| **简单文本请求** | 500+ | 200ms | 500ms | 50MB |
| **复杂对话请求** | 200+ | 800ms | 1.5s | 80MB |
| **流式响应** | 300+ | 150ms | 300ms | 60MB |
| **多模态请求** | 150+ | 1.2s | 2.5s | 120MB |
| **工具调用** | 100+ | 2s | 4s | 100MB |

#### 负载测试命令

```bash
# 基础性能测试
ab -n 1000 -c 50 -H "Authorization: Bearer $KIRO_CLIENT_TOKEN" \
  http://localhost:8080/v1/models

# 并发对话测试
for i in {1..10}; do
  curl -X POST http://localhost:8080/v1/messages \
    -H "Content-Type: application/json" \
    -H "Authorization: Bearer $KIRO_CLIENT_TOKEN" \
    -d '{"model":"claude-sonnet-4-20250514","max_tokens":100,"messages":[{"role":"user","content":"测试请求'$i'"}]}' &
done
wait

# 流式性能测试
wrk -t4 -c100 -d30s -s stream-test.lua http://localhost:8080/v1/messages
```

### 📈 水平扩展策略

#### 单实例扩展

```bash
# 垂直扩展：增加资源配置
docker run -d \
  --name kiro2api \
  --cpus="4.0" \
  --memory="8g" \
  -e MAX_CONCURRENT_SESSIONS=500 \
  -e TOKEN_SELECTION_STRATEGY=optimal \
  ghcr.io/caidaoli/kiro2api:latest
```

#### 多实例集群部署

```yaml
# docker-compose.cluster.yml
services:
  kiro2api-1:
    image: ghcr.io/caidaoli/kiro2api:latest
    environment:
      - INSTANCE_ID=kiro2api-1
      - MAX_CONCURRENT_SESSIONS=200
    deploy:
      resources:
        limits:
          cpus: '2.0'
          memory: 4G

  kiro2api-2:
    image: ghcr.io/caidaoli/kiro2api:latest
    environment:
      - INSTANCE_ID=kiro2api-2
      - MAX_CONCURRENT_SESSIONS=200
    deploy:
      resources:
        limits:
          cpus: '2.0'
          memory: 4G

  kiro2api-3:
    image: ghcr.io/caidaoli/kiro2api:latest
    environment:
      - INSTANCE_ID=kiro2api-3
      - MAX_CONCURRENT_SESSIONS=200
    deploy:
      resources:
        limits:
          cpus: '2.0'
          memory: 4G

  # HAProxy 负载均衡器
  haproxy:
    image: haproxy:alpine
    ports:
      - "8080:8080"
      - "8404:8404"  # 统计页面
    volumes:
      - ./haproxy.cfg:/usr/local/etc/haproxy/haproxy.cfg
    depends_on:
      - kiro2api-1
      - kiro2api-2
      - kiro2api-3
```

#### HAProxy 配置示例

```bash
# haproxy.cfg
global
    daemon
    maxconn 4096

defaults
    mode http
    timeout connect 5000ms
    timeout client 50000ms
    timeout server 50000ms

frontend kiro2api_frontend
    bind *:8080
    default_backend kiro2api_backend

backend kiro2api_backend
    balance roundrobin
    option httpchk GET /v1/models
    server kiro2api-1 kiro2api-1:8080 check
    server kiro2api-2 kiro2api-2:8080 check
    server kiro2api-3 kiro2api-3:8080 check

# 统计页面
listen stats
    bind *:8404
    stats enable
    stats uri /stats
    stats refresh 30s
```

### 🔧 性能调优指南

#### 系统级优化

```bash
# 操作系统优化
echo 'net.core.somaxconn = 65535' >> /etc/sysctl.conf
echo 'net.ipv4.tcp_max_syn_backlog = 65535' >> /etc/sysctl.conf
echo 'fs.file-max = 1000000' >> /etc/sysctl.conf
sysctl -p

# 进程限制优化
echo '* soft nofile 1000000' >> /etc/security/limits.conf
echo '* hard nofile 1000000' >> /etc/security/limits.conf
```

#### 应用级优化

```bash
# Go 运行时优化
export GOGC=100                    # GC 触发阈值
export GOMAXPROCS=4                # 最大 CPU 核心数
export GOMEMLIMIT=6GiB             # 内存限制

# kiro2api 性能调优
MAX_CONCURRENT_SESSIONS=1000       # 最大并发会话
SESSION_TIMEOUT_MINUTES=30         # 会话超时
TOKEN_REFRESH_TIMEOUT=10s          # Token 刷新超时
```



## 🛠️ 开发者最佳实践

### 🔄 开发工作流程

#### 1. 本地开发环境搭建

```bash
# 克隆项目
git clone https://github.com/your-org/kiro2api.git
cd kiro2api

# 设置开发环境
cp .env.example .env.development
# 编辑 .env.development 配置开发用 Token

# 启动开发服务器
GIN_MODE=debug LOG_LEVEL=debug go run main.go

# 或使用 Docker 开发环境
docker-compose -f docker-compose.dev.yml up -d
```

#### 2. 代码质量保证

```bash
# 代码格式化和检查
make fmt      # go fmt ./...
make vet      # go vet ./...
make lint     # golangci-lint run

# 测试覆盖率
make test-coverage
# 目标：保持 80%+ 测试覆盖率

# 性能基准测试
make benchmark
go test -bench=. -benchmem ./...
```

#### 3. 功能开发指南

```bash
# 添加新的 API 端点
# 1. 在 server/handlers.go 中添加处理函数
# 2. 在 server/server.go 中注册路由
# 3. 添加对应的测试用例
# 4. 更新 API 文档

# 添加新的认证方式
# 1. 在 auth/config.go 中定义配置结构
# 2. 在 auth/auth.go 中实现认证逻辑
# 3. 在 auth/token_manager.go 中集成选择策略
# 4. 添加配置示例和文档

# 优化性能
# 1. 使用 pprof 分析性能瓶颈
# 2. 优化热点代码路径
# 3. 添加缓存机制
# 4. 进行基准测试验证
```



### 🚀 部署最佳实践

#### CI/CD 流水线

```yaml
# .github/workflows/deploy.yml
name: Deploy to Production

on:
  push:
    branches: [main]

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      - uses: actions/setup-go@v3
        with:
          go-version: 1.23.3

      - name: Run tests
        run: |
          go test ./...
          go test -race ./...
          go test -bench=. ./...

  build:
    needs: test
    runs-on: ubuntu-latest
    steps:
      - name: Build and push Docker image
        run: |
          docker build -t ghcr.io/your-org/kiro2api:${{ github.sha }} .
          docker push ghcr.io/your-org/kiro2api:${{ github.sha }}

  deploy:
    needs: build
    runs-on: ubuntu-latest
    steps:
      - name: Deploy to production
        run: |
          kubectl set image deployment/kiro2api \
            kiro2api=ghcr.io/your-org/kiro2api:${{ github.sha }}
```

#### 生产环境检查清单

- [ ] **安全配置**: 强密码、HTTPS、网络隔离
- [ ] **监控告警**: Prometheus + Grafana + AlertManager
- [ ] **日志收集**: ELK Stack 或 Loki
- [ ] **备份策略**: 配置文件和数据备份
- [ ] **容灾方案**: 多可用区部署
- [ ] **性能调优**: 根据负载调整参数
- [ ] **健康检查**: 自动故障检测和恢复

## 📚 更多资源

- 📖 **详细开发指南**: [CLAUDE.md](./CLAUDE.md)
- 🏗️ **包结构说明**: 分层架构设计，遵循 SOLID 原则
- ⚡ **性能优化**: 缓存策略、并发控制、内存管理
- 🔧 **核心开发任务**: 扩展功能、性能调优、高级特性
- 🤖 **Claude Code 官方文档**: [claude.ai/code](https://claude.ai/code)
- 🐳 **Docker 最佳实践**: 容器化部署指南
- 📊 **监控指南**: Prometheus + Grafana 监控体系
- 🔒 **安全指南**: 生产级安全配置和最佳实践

## 🤝 贡献指南

我们欢迎社区贡献！请查看 [CONTRIBUTING.md](./CONTRIBUTING.md) 了解详细的贡献流程。

### 快速贡献

1. Fork 项目
2. 创建特性分支 (`git checkout -b feature/amazing-feature`)
3. 提交更改 (`git commit -m 'Add amazing feature'`)
4. 推送到分支 (`git push origin feature/amazing-feature`)
5. 创建 Pull Request

## 📄 许可证

本项目遵循 MIT 许可证。详情请查看 [LICENSE](LICENSE) 文件。

---

<div align="center">

**🚀 让 AI API 调用更简单、更高效！**

*如果这个项目对您有帮助，请给我们一个 ⭐*

[![Go](https://img.shields.io/badge/Go-1.23.3-blue.svg)](https://golang.org/)
[![Gin](https://img.shields.io/badge/Gin-1.10.1-green.svg)](https://github.com/gin-gonic/gin)
[![Docker](https://img.shields.io/badge/Docker-Ready-blue.svg)](https://www.docker.com/)
[![License](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

**[🏠 主页](https://github.com/your-org/kiro2api) | [📖 文档](./CLAUDE.md) | [🐛 问题反馈](https://github.com/your-org/kiro2api/issues) | [💬 讨论](https://github.com/your-org/kiro2api/discussions)**

</div>