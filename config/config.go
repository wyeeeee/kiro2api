package config

import (
	"os"
	"strings"
)

// AuthMethod 认证方式枚举
type AuthMethod string

const (
	AuthMethodSocial AuthMethod = "social"
	AuthMethodIdC    AuthMethod = "IdC"
)

// GetAuthMethod 获取认证方式，默认为social，不区分大小写
func GetAuthMethod() AuthMethod {
	method := strings.ToLower(strings.TrimSpace(os.Getenv("AUTH_METHOD")))
	switch method {
	case "idc", "id-c", "identity-center":
		return AuthMethodIdC
	case "social", "":
		return AuthMethodSocial
	default:
		// 如果是未知值，记录但使用默认值
		return AuthMethodSocial
	}
}

// ModelMap 模型映射表
var ModelMap = map[string]string{
	"claude-sonnet-4-5-20250929": "CLAUDE_SONNET_4_5_20250929_V1_0",
	"claude-sonnet-4-20250514":   "CLAUDE_SONNET_4_20250514_V1_0",
	"claude-3-7-sonnet-20250219": "CLAUDE_3_7_SONNET_20250219_V1_0",
	"claude-3-5-haiku-20241022":  "auto",
}

// IsStreamDisabled 检查是否禁用了流式请求和输出
func IsStreamDisabled() bool {
	return os.Getenv("DISABLE_STREAM") == "true"
}

// IsSaveRawDataEnabled 检查是否启用原始数据保存（用于调试）
func IsSaveRawDataEnabled() bool {
	return os.Getenv("SAVE_RAW_DATA") == "true"
}

// RefreshTokenURL 刷新token的URL (social方式)
const RefreshTokenURL = "https://prod.us-east-1.auth.desktop.kiro.dev/refreshToken"

// IdcRefreshTokenURL IdC认证方式的刷新token URL
const IdcRefreshTokenURL = "https://oidc.us-east-1.amazonaws.com/token"

// CodeWhispererURL CodeWhisperer API的URL
const CodeWhispererURL = "https://codewhisperer.us-east-1.amazonaws.com/generateAssistantResponse"
