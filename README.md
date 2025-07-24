
# kiro2api

这是一个名为 `kiro2api` 的 Go 命令行工具，用于管理 Kiro 认证令牌，并提供 Anthropic API 和 OpenAI 兼容的 API 代理服务。该工具充当 API 请求与 AWS CodeWhisperer 之间的桥梁，在不同格式之间转换请求和响应。

## 功能

- **令牌管理**：从 `~/.aws/sso/cache/kiro-auth-token.json` 读取和刷新访问令牌
- **API 代理**：在 Anthropic、OpenAI 和 AWS CodeWhisperer API 格式之间转换
- **环境变量导出**：为其他工具导出环境变量（使用实际令牌，不是硬编码值）
- **Claude 地区绕过**：配置 Claude 认证设置
- **流式响应支持**：支持流式和非流式请求处理

## 编译

```bash
go build -o kiro2api main.go
```

## 开发和测试

```bash
# 运行测试
go test ./...

# 运行特定包的详细测试
go test ./parser -v
go test ./auth -v

# 清理构建
rm -f kiro2api && go build -o kiro2api main.go
```

## 自动构建

本项目使用GitHub Actions进行自动构建：

-   当创建新的GitHub Release时，会自动构建Windows、Linux和macOS版本的可执行文件并上传到Release页面
-   当推送代码到main分支或创建Pull Request时，会自动运行测试

## 使用方法

### 1. 读取令牌信息

```bash
./kiro2api read
```

### 2. 刷新访问令牌

```bash
./kiro2api refresh
```

### 3. 导出环境变量

```bash
# Linux/macOS
eval $(./kiro2api export)

# Windows
./kiro2api export
```

### 4. 显示认证令牌

```bash
./kiro2api authToken
```

### 5. 设置 Claude 地区绕过

```bash
./kiro2api claude
```

### 6. 启动 API 代理服务器

```bash
# 使用默认端口 8080
./kiro2api server

# 指定自定义端口
./kiro2api server 9000

# 指定端口和认证令牌
./kiro2api server 8080 your-auth-token
```

## 代理服务器功能

启动服务器后支持以下 API 端点：

- **Anthropic 兼容**：`/v1/messages` - 直接 Anthropic API 代理
- **OpenAI 兼容**：`/v1/chat/completions` - OpenAI 格式转换为 Claude
- **模型列表**：`/v1/models` - 返回可用模型列表
- **健康检查**：基本健康检查端点

### Anthropic API 格式

```bash
curl -X POST http://localhost:8080/v1/messages \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer your-auth-token" \
  -d '{
    "model": "claude-sonnet-4-20250514",
    "max_tokens": 1000,
    "messages": [
      {"role": "user", "content": "Hello, Claude!"}
    ]
  }'
```

### OpenAI 兼容 API 格式

```bash
curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer your-auth-token" \
  -d '{
    "model": "claude-sonnet-4-20250514",
    "messages": [
      {"role": "user", "content": "Hello, Claude!"}
    ]
  }'
```

### 获取模型列表

```bash
curl -X GET http://localhost:8080/v1/models \
  -H "Authorization: Bearer your-auth-token"
```

## 架构说明

项目采用模块化包结构：

- **`auth/`** - 令牌管理和认证逻辑
- **`server/`** - HTTP 服务器和 API 处理
- **`converter/`** - API 格式转换层
- **`parser/`** - 响应解析和处理
- **`types/`** - 数据结构定义
- **`config/`** - 配置管理和模型映射
- **`logger/`** - 结构化日志系统
- **`utils/`** - 工具函数

## 令牌文件格式

令牌存储在 `~/.aws/sso/cache/kiro-auth-token.json`：

```json
{
    "accessToken": "your-access-token",
    "refreshToken": "your-refresh-token",
    "expiresAt": "2024-01-01T00:00:00Z"
}
```

## 环境变量

工具会设置以下环境变量：

- `ANTHROPIC_BASE_URL`: http://localhost:8080
- `ANTHROPIC_AUTH_TOKEN`: 当前的访问令牌

## 请求流程

1. 客户端向 API 端点发送请求
2. 服务器使用令牌或认证头进行认证
3. 转换器将请求格式转换为 CodeWhisperer 兼容结构
4. 通过代理 `127.0.0.1:9000` 转发到 AWS CodeWhisperer API
5. 解析器处理响应并转换为适当的流式格式
6. 以请求的格式返回给客户端

## 跨平台支持

-   Windows: 使用 `set` 命令格式
-   Linux/macOS: 使用 `export` 命令格式
-   自动检测用户目录路径
