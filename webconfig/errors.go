package webconfig

import "fmt"

// ConfigError 配置错误类型
type ConfigError struct {
	Message string
	Code    string
}

// Error 实现error接口
func (e *ConfigError) Error() string {
	return fmt.Sprintf("配置错误 [%s]: %s", e.Code, e.Message)
}

// NewConfigError 创建配置错误
func NewConfigError(format string, args ...interface{}) *ConfigError {
	return &ConfigError{
		Message: fmt.Sprintf(format, args...),
		Code:    "CONFIG_ERROR",
	}
}

// predefined error codes
const (
	ErrCodeConfigNotFound    = "CONFIG_NOT_FOUND"
	ErrCodeConfigInvalid     = "CONFIG_INVALID"
	ErrCodePasswordIncorrect = "PASSWORD_INCORRECT"
	ErrCodeAuthRequired      = "AUTH_REQUIRED"
	ErrCodeTokenInvalid      = "TOKEN_INVALID"
)