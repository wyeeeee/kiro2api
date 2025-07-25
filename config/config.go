package config

// ModelMap 模型映射表
var ModelMap = map[string]string{
	"claude-sonnet-4-20250514":  "CLAUDE_SONNET_4_20250514_V1_0",
	"claude-3-5-haiku-20241022": "CLAUDE_3_7_SONNET_20250219_V1_0",
}

// RefreshTokenURL 刷新token的URL
const RefreshTokenURL = "https://prod.us-east-1.auth.desktop.kiro.dev/refreshToken"
