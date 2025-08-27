# KIRO_AUTH_TOKEN 配置示例

本文档提供了 `KIRO_AUTH_TOKEN` 环境变量的详细配置示例和说明。

## 概述

`KIRO_AUTH_TOKEN` 是新的 JSON 格式配置方式，支持多种认证方式和多个 token，提供更灵活的配置选项。

## 基本结构

```json
[
  {
    "auth": "认证方式",
    "refreshToken": "刷新令牌",
    "clientId": "客户端ID（IdC方式必需）",
    "clientSecret": "客户端密钥（IdC方式必需）"
  }
]
```

## 配置示例

### 1. 单个 Social 认证

```bash
KIRO_AUTH_TOKEN='[
  {
    "auth": "Social",
    "refreshToken": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4gRG9lIiwiaWF0IjoxNTE2MjM5MDIyfQ.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c"
  }
]'
```

### 2. 单个 IdC 认证

```bash
KIRO_AUTH_TOKEN='[
  {
    "auth": "IdC",
    "refreshToken": "idc_refresh_token_here_12345",
    "clientId": "my-client-id-12345",
    "clientSecret": "my-client-secret-abcdef"
  }
]'
```

### 3. 多个 Social 认证（推荐用于负载均衡）

```bash
KIRO_AUTH_TOKEN='[
  {
    "auth": "Social",
    "refreshToken": "social_token_1_here"
  },
  {
    "auth": "Social",
    "refreshToken": "social_token_2_here"
  },
  {
    "auth": "Social",
    "refreshToken": "social_token_3_here"
  }
]'
```

### 4. 混合认证（Social + IdC）

```bash
KIRO_AUTH_TOKEN='[
  {
    "auth": "Social",
    "refreshToken": "social_refresh_token_here"
  },
  {
    "auth": "IdC",
    "refreshToken": "idc_refresh_token_here",
    "clientId": "idc-client-id",
    "clientSecret": "idc-client-secret"
  }
]'
```

### 5. 多个 IdC 认证

```bash
KIRO_AUTH_TOKEN='[
  {
    "auth": "IdC",
    "refreshToken": "idc_token_1",
    "clientId": "client-id-1",
    "clientSecret": "client-secret-1"
  },
  {
    "auth": "IdC", 
    "refreshToken": "idc_token_2",
    "clientId": "client-id-2",
    "clientSecret": "client-secret-2"
  }
]'
```

### 6. 完整的生产环境配置示例

```bash
# 主要配置
KIRO_AUTH_TOKEN='[
  {
    "auth": "Social",
    "refreshToken": "eyJhbGciOiJSUzI1NiIsInR5cCIgOiAiSldUIiwia2lkIiA6ICJRVVExUlVZeU5EQXpOVEUzTlRVNU1UQkZNVFEyT0RNek1UTTFOVFE1TlRrMU1UQkZNVFEyT0RNek1UTTFOVFE5In0"
  },
  {
    "auth": "Social", 
    "refreshToken": "eyJhbGciOiJSUzI1NiIsInR5cCIgOiAiSldUIiwia2lkIiA6ICJRVVExUlVZeU5EQXpOVEUzTlRVNU1UQkZNVFEyT0RNek1UTTFOVFE1TlRrMU1UQkZNVFEyT0RNek1UTTFOVFE5In1"
  },
  {
    "auth": "IdC",
    "refreshToken": "prod_idc_refresh_token_secure_12345",
    "clientId": "prod-aws-client-id-67890",
    "clientSecret": "prod-aws-client-secret-abcdef"
  }
]'

# API 认证密钥
KIRO_CLIENT_TOKEN=your-secure-api-key-here

# 服务配置
PORT=8080

# Token 管理配置
USAGE_CHECK_INTERVAL=5m
TOKEN_USAGE_THRESHOLD=5
TOKEN_SELECTION_STRATEGY=balanced

# 性能优化
REQUEST_TIMEOUT_MINUTES=15
SIMPLE_REQUEST_TIMEOUT_MINUTES=2
```

## 字段说明

| 字段 | 类型 | 必需 | 说明 |
|------|------|------|------|
| `Auth` | string | 是 | 认证方式，支持 "Social" 或 "IdC" |
| `RefreshToken` | string | 是 | AWS 刷新令牌 |
| `ClientId` | string | IdC时必需 | IdC 认证的客户端 ID |
| `ClientSecret` | string | IdC时必需 | IdC 认证的客户端密钥 |

## 认证方式说明

### Social 认证
- 适用于个人开发者和小型项目
- 只需要 `RefreshToken`
- 设置简单，配置少

### IdC 认证
- 适用于企业环境和大型项目
- 需要 `RefreshToken`、`ClientId` 和 `ClientSecret`
- 提供更高的安全性和控制能力

## 最佳实践

### 1. 安全性建议
- 使用环境变量文件（`.env`）存储敏感信息
- 不要将真实的 token 提交到版本控制系统
- 定期轮换 refresh token
- 在生产环境中使用强密码策略

### 2. 性能优化
- 配置多个 token 以实现负载均衡
- 使用 `balanced` 策略以获得更好的资源利用率
- 监控 token 使用量，及时添加新的 token

### 3. 故障恢复
- 配置多种认证方式作为备用
- 设置合适的 `TOKEN_USAGE_THRESHOLD` 进行预警
- 定期检查 token 的有效性

## 环境变量文件示例

创建 `.env` 文件：

```bash
# ===========================================
# KIRO2API 配置文件
# ===========================================

# Token 配置（JSON 格式，推荐）
KIRO_AUTH_TOKEN='[
  {
    "auth": "Social",
    "refreshToken": "your_social_refresh_token_here"
  },
  {
    "auth": "IdC",
    "refreshToken": "your_idc_refresh_token_here",
    "clientId": "your_idc_client_id",
    "clientSecret": "your_idc_client_secret"
  }
]'

# API 认证
KIRO_CLIENT_TOKEN=123456

# 服务配置
PORT=8080

# Token 管理
USAGE_CHECK_INTERVAL=5m
TOKEN_USAGE_THRESHOLD=5
TOKEN_SELECTION_STRATEGY=balanced

# 缓存配置
CACHE_CLEANUP_INTERVAL=5m
TOKEN_CACHE_HOT_THRESHOLD=10
TOKEN_REFRESH_TIMEOUT=30s

# 日志配置
LOG_LEVEL=info
LOG_FORMAT=json
LOG_CONSOLE=true

# 性能调优
REQUEST_TIMEOUT_MINUTES=15
SIMPLE_REQUEST_TIMEOUT_MINUTES=2
GIN_MODE=release
```

## 验证配置

启动服务后，检查日志中是否有以下信息：

```log
{"level":"info","msg":"Token配置加载成功","count":2,"social_count":1,"idc_count":1}
{"level":"info","msg":"Token管理器初始化完成","strategy":"balanced"}
```

## 故障排除

### 常见错误

1. **JSON 格式错误**
   ```log
   {"level":"error","msg":"解析KIRO_AUTH_TOKEN失败","error":"invalid character..."}
   ```
   解决：检查 JSON 格式，使用在线 JSON 验证器验证

2. **IdC 认证参数缺失**
   ```log
   {"level":"error","msg":"IdC认证缺少必需参数","missing":"clientId"}
   ```
   解决：为 IdC 认证添加 `ClientId` 和 `ClientSecret`

3. **Token 无效**
   ```log
   {"level":"error","msg":"Token刷新失败","error":"unauthorized"}
   ```
   解决：检查 token 是否过期，更新为有效的 refresh token

## 迁移指南

### 从传统环境变量迁移

如果您当前使用传统的环境变量配置：

```bash
# 旧配置
AWS_REFRESHTOKEN=token1,token2,token3
```

可以迁移到新的 JSON 格式：

```bash
# 新配置
KIRO_AUTH_TOKEN='[
  {"auth": "Social", "refreshToken": "token1"},
  {"auth": "Social", "refreshToken": "token2"},
  {"auth": "Social", "refreshToken": "token3"}
]'
```

> **注意**: 系统保持向后兼容，旧的环境变量配置仍然可以使用。
