# KIRO_AUTH_TOKEN 快速配置模板

## 基础配置（复制使用）

### 1. 单个 Social 认证
```bash
KIRO_AUTH_TOKEN='[
  {
    "auth": "Social",
    "refreshToken": "YOUR_SOCIAL_REFRESH_TOKEN"
  }
]'
KIRO_CLIENT_TOKEN=123456
PORT=8080
```

### 2. 单个 IdC 认证
```bash
KIRO_AUTH_TOKEN='[
  {
    "auth": "IdC",
    "refreshToken": "YOUR_IDC_REFRESH_TOKEN",
    "clientId": "YOUR_IDC_CLIENT_ID",
    "clientSecret": "YOUR_IDC_CLIENT_SECRET"
  }
]'
KIRO_CLIENT_TOKEN=123456
PORT=8080
```

### 3. 多个 Social 认证（负载均衡）
```bash
KIRO_AUTH_TOKEN='[
  {
    "auth": "Social",
    "refreshToken": "SOCIAL_TOKEN_1"
  },
  {
    "auth": "Social",
    "refreshToken": "SOCIAL_TOKEN_2"
  },
  {
    "auth": "Social",
    "refreshToken": "SOCIAL_TOKEN_3"
  }
]'
KIRO_CLIENT_TOKEN=123456
PORT=8080
TOKEN_SELECTION_STRATEGY=balanced
```

### 4. 混合认证（生产环境推荐）
```bash
KIRO_AUTH_TOKEN='[
  {
    "auth": "Social",
    "refreshToken": "SOCIAL_TOKEN_PRIMARY"
  },
  {
    "auth": "Social",
    "refreshToken": "SOCIAL_TOKEN_BACKUP"
  },
  {
    "auth": "IdC",
    "refreshToken": "IDC_TOKEN_ENTERPRISE",
    "clientId": "IDC_CLIENT_ID",
    "clientSecret": "IDC_CLIENT_SECRET"
  }
]'
KIRO_CLIENT_TOKEN=your-secure-api-key
PORT=8080
TOKEN_SELECTION_STRATEGY=balanced
TOKEN_USAGE_THRESHOLD=5
LOG_LEVEL=info
```

## Docker 环境配置

### docker-compose.yml 环境变量
```yaml
environment:
  - KIRO_AUTH_TOKEN=[{"auth":"Social","refreshToken":"YOUR_TOKEN"}]
  - KIRO_CLIENT_TOKEN=123456
  - PORT=8080
```

### Docker run 命令
```bash
docker run -d \
  --name kiro2api \
  -p 8080:8080 \
  -e KIRO_AUTH_TOKEN='[{"auth":"Social","refreshToken":"YOUR_TOKEN"}]' \
  -e KIRO_CLIENT_TOKEN=123456 \
  kiro2api
```

## 快速测试

配置完成后，启动服务并测试：

```bash
# 启动服务
./kiro2api

# 测试 API
curl -X POST http://localhost:8080/v1/messages \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer 123456" \
  -d '{
    "model": "claude-sonnet-4-20250514",
    "max_tokens": 100,
    "messages": [{"role": "user", "content": "你好"}]
  }'
```

## Token 获取方法

### Social Token
通常位于：
- `~/.aws/sso/cache/` 目录下的 JSON 文件
- 文件名包含 `cache` 或 `token`
- 查找 `refreshToken` 字段

### IdC Token
企业环境获取：
- 联系 AWS 管理员获取 `ClientId` 和 `ClientSecret`
- 从企业 SSO 系统获取 `RefreshToken`

## 常见问题

### Q: JSON 格式错误怎么办？
```bash
# 使用在线 JSON 验证器检查格式
# 或者启用调试日志查看具体错误
LOG_LEVEL=debug ./kiro2api
```

### Q: Token 无效怎么办？
```bash
# 检查 token 是否过期
# 获取新的 refresh token
# 确认认证方式正确（Social 或 IdC）
```

### Q: 如何实现高可用？
```bash
# 配置多个不同的 token
# 使用混合认证方式
# 设置合适的预警阈值
TOKEN_USAGE_THRESHOLD=5
```
