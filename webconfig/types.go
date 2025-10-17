package webconfig

import "time"

// WebConfig 主配置结构
type WebConfig struct {
	LoginPassword  string        `json:"loginPassword"`
	ServiceConfig  ServiceConfig `json:"serviceConfig"`
	AuthTokens     []AuthToken   `json:"authTokens"`
	LogConfig      LogConfig     `json:"logConfig"`
	TimeoutConfig  TimeoutConfig `json:"timeoutConfig"`
	CreatedAt      time.Time     `json:"createdAt"`
	UpdatedAt      time.Time     `json:"updatedAt"`
}

// ServiceConfig 服务基础配置
type ServiceConfig struct {
	Port        int    `json:"port"`         // HTTP服务端口
	GinMode     string `json:"ginMode"`      // Gin框架运行模式: debug, release, test
	ClientToken string `json:"clientToken"`  // API客户端认证token
}

// AuthToken 认证Token配置
type AuthToken struct {
	ID            string `json:"id"`             // 唯一标识
	Auth          string `json:"auth"`           // 认证方式: Social, IdC
	RefreshToken  string `json:"refreshToken"`   // 刷新token
	ClientID      string `json:"clientId,omitempty"` // IdC认证的客户端ID
	ClientSecret  string `json:"clientSecret,omitempty"` // IdC认证的客户端密钥
	Enabled       bool   `json:"enabled"`        // 是否启用
	IsPrimary     bool   `json:"isPrimary"`      // 是否为主要使用的Token
	LastUsed      *time.Time `json:"lastUsed,omitempty"` // 最后使用时间
	ErrorCount    int    `json:"errorCount"`     // 错误次数
	Description   string `json:"description"`    // 描述信息
}

// LogConfig 日志配置
type LogConfig struct {
	Level         string `json:"level"`           // 日志级别: debug, info, warn, error, fatal
	Format        string `json:"format"`          // 日志格式: text, json
	Console       bool   `json:"console"`         // 控制台输出开关
	File          string `json:"file,omitempty"`  // 日志文件路径(可选)
	EnableCaller  bool   `json:"enableCaller"`    // 是否启用调用栈信息
	CallerSkip    int    `json:"callerSkip"`      // 调用栈深度跳过
}

// TimeoutConfig 超时配置
type TimeoutConfig struct {
	RequestMinutes       int `json:"requestMinutes"`       // 复杂请求超时时间(分钟)
	SimpleRequestMinutes int `json:"simpleRequestMinutes"` // 简单请求超时时间(分钟)
	StreamMinutes        int `json:"streamMinutes"`        // 流式请求超时时间(分钟)
	ServerReadMinutes    int `json:"serverReadMinutes"`    // 服务器读取超时(分钟)
	ServerWriteMinutes   int `json:"serverWriteMinutes"`   // 服务器写入超时(分钟)
}

// GetDefaultConfig 获取默认配置
func GetDefaultConfig() *WebConfig {
	now := time.Now()
	return &WebConfig{
		LoginPassword: "", // 需要初次设置
		ServiceConfig: ServiceConfig{
			Port:        8083,
			GinMode:     "release",
			ClientToken: "", // 需要用户设置
		},
		AuthTokens: []AuthToken{},
		LogConfig: LogConfig{
			Level:        "info",
			Format:       "json",
			Console:      true,
			File:         "",
			EnableCaller: false,
			CallerSkip:   3,
		},
		TimeoutConfig: TimeoutConfig{
			RequestMinutes:       15,
			SimpleRequestMinutes: 2,
			StreamMinutes:        30,
			ServerReadMinutes:    16,
			ServerWriteMinutes:   16,
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
}

// Validate 验证配置有效性
func (c *WebConfig) Validate() error {
	// 验证服务配置
	if c.ServiceConfig.Port < 1 || c.ServiceConfig.Port > 65535 {
		return NewConfigError("端口号必须在 1-65535 范围内")
	}

	if c.ServiceConfig.GinMode != "debug" && c.ServiceConfig.GinMode != "release" && c.ServiceConfig.GinMode != "test" {
		return NewConfigError("Gin模式必须是 debug, release 或 test")
	}

	if c.ServiceConfig.ClientToken == "" {
		return NewConfigError("客户端认证token不能为空")
	}

	// 验证日志配置
	validLogLevels := map[string]bool{
		"debug": true, "info": true, "warn": true, "error": true, "fatal": true,
	}
	if !validLogLevels[c.LogConfig.Level] {
		return NewConfigError("无效的日志级别")
	}

	if c.LogConfig.Format != "text" && c.LogConfig.Format != "json" {
		return NewConfigError("日志格式必须是 text 或 json")
	}

	// 验证超时配置
	if c.TimeoutConfig.RequestMinutes <= 0 || c.TimeoutConfig.RequestMinutes > 120 {
		return NewConfigError("请求超时时间必须在 1-120 分钟范围内")
	}

	if c.TimeoutConfig.SimpleRequestMinutes <= 0 || c.TimeoutConfig.SimpleRequestMinutes > 60 {
		return NewConfigError("简单请求超时时间必须在 1-60 分钟范围内")
	}

	if c.TimeoutConfig.StreamMinutes <= 0 || c.TimeoutConfig.StreamMinutes > 180 {
		return NewConfigError("流式请求超时时间必须在 1-180 分钟范围内")
	}

	// 验证Token配置
	for i, token := range c.AuthTokens {
		if token.Auth != "Social" && token.Auth != "IdC" {
			return NewConfigError("Token #%d: 认证方式必须是 Social 或 IdC", i+1)
		}

		if token.RefreshToken == "" {
			return NewConfigError("Token #%d: 刷新token不能为空", i+1)
		}

		if token.Auth == "IdC" && (token.ClientID == "" || token.ClientSecret == "") {
			return NewConfigError("Token #%d: IdC认证需要客户端ID和密钥", i+1)
		}
	}

	return nil
}

// Clone 克隆配置
func (c *WebConfig) Clone() *WebConfig {
	clone := *c

	// 深拷贝slice
	clone.AuthTokens = make([]AuthToken, len(c.AuthTokens))
	copy(clone.AuthTokens, c.AuthTokens)

	// 深拷贝时间指针
	for i, token := range clone.AuthTokens {
		if token.LastUsed != nil {
			lastUsed := *token.LastUsed
			clone.AuthTokens[i].LastUsed = &lastUsed
		}
	}

	return &clone
}