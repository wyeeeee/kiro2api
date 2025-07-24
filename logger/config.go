package logger

import (
	"os"
	"strings"
)

// Config 日志配置结构
type Config struct {
	Level      Level
	File       string
	Console    bool
	Color      bool
	Format     FormatType
	TimeFormat string
}

// FormatType 格式类型枚举
type FormatType int

const (
	TextFormat FormatType = iota
	JSONFormat
)

// DefaultConfig 默认配置
var DefaultConfig = Config{
	Level:      INFO,
	Console:    true,
	Color:      true,
	Format:     TextFormat,
	TimeFormat: "2006-01-02 15:04:05",
}

// ParseConfig 解析配置（优先级：命令行 > 环境变量 > 默认）
func ParseConfig() Config {
	// 从默认配置开始
	config := DefaultConfig

	// 应用环境变量配置
	config = applyEnvConfig(config)

	// 应用命令行参数配置
	config = applyCmdConfig(config)

	// 验证配置
	config = validateConfig(config)

	return config
}

// applyEnvConfig 从环境变量应用配置
func applyEnvConfig(config Config) Config {
	// DEBUG环境变量
	if debug := os.Getenv("DEBUG"); debug != "" {
		if parseBool(debug) {
			config.Level = DEBUG
		}
	}

	// LOG_LEVEL环境变量
	if logLevel := os.Getenv("LOG_LEVEL"); logLevel != "" {
		if level, err := ParseLevel(logLevel); err == nil {
			config.Level = level
		}
	}

	// LOG_FILE环境变量
	if logFile := os.Getenv("LOG_FILE"); logFile != "" {
		config.File = logFile
	}

	// LOG_COLOR环境变量
	if logColor := os.Getenv("LOG_COLOR"); logColor != "" {
		config.Color = parseBool(logColor)
	}

	// LOG_FORMAT环境变量
	if logFormat := os.Getenv("LOG_FORMAT"); logFormat != "" {
		if format := parseFormat(logFormat); format != -1 {
			config.Format = format
		}
	}

	// LOG_CONSOLE环境变量
	if logConsole := os.Getenv("LOG_CONSOLE"); logConsole != "" {
		config.Console = parseBool(logConsole)
	}

	return config
}

// applyCmdConfig 从命令行参数应用配置
func applyCmdConfig(config Config) Config {
	args := os.Args

	for i, arg := range args {
		switch {
		case arg == "--debug" || arg == "-d":
			config.Level = DEBUG

		case arg == "--no-color":
			config.Color = false

		case strings.HasPrefix(arg, "--log-level="):
			levelStr := strings.TrimPrefix(arg, "--log-level=")
			if level, err := ParseLevel(levelStr); err == nil {
				config.Level = level
			}

		case arg == "--log-level" || arg == "-l":
			if i+1 < len(args) {
				if level, err := ParseLevel(args[i+1]); err == nil {
					config.Level = level
				}
			}

		case strings.HasPrefix(arg, "--log-file="):
			config.File = strings.TrimPrefix(arg, "--log-file=")

		case arg == "--log-file" || arg == "-f":
			if i+1 < len(args) {
				config.File = args[i+1]
			}

		case strings.HasPrefix(arg, "--log-format="):
			formatStr := strings.TrimPrefix(arg, "--log-format=")
			if format := parseFormat(formatStr); format != -1 {
				config.Format = format
			}
		}
	}

	return config
}

// validateConfig 验证并修正配置
func validateConfig(config Config) Config {
	// 如果指定了文件输出，确保控制台输出也启用（除非明确禁用）
	if config.File != "" && os.Getenv("LOG_CONSOLE") == "" {
		// 默认情况下，有文件输出时仍然启用控制台输出
		config.Console = true
	}

	// 如果没有任何输出方式，强制启用控制台输出
	if !config.Console && config.File == "" {
		config.Console = true
	}

	// 文件输出时，默认使用JSON格式（如果没有明确指定）
	if config.File != "" && os.Getenv("LOG_FORMAT") == "" && !hasCmdFormat() {
		config.Format = JSONFormat
	}

	return config
}

// parseBool 解析布尔值字符串
func parseBool(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	return s == "true" || s == "1" || s == "yes" || s == "on"
}

// parseFormat 解析格式字符串
func parseFormat(s string) FormatType {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "text", "txt":
		return TextFormat
	case "json":
		return JSONFormat
	default:
		return -1 // 无效格式
	}
}

// hasCmdFormat 检查命令行参数中是否有格式指定
func hasCmdFormat() bool {
	for _, arg := range os.Args {
		if strings.HasPrefix(arg, "--log-format=") || arg == "--log-format" {
			return true
		}
	}
	return false
}

// String 返回格式类型的字符串表示
func (f FormatType) String() string {
	switch f {
	case TextFormat:
		return "text"
	case JSONFormat:
		return "json"
	default:
		return "unknown"
	}
}

// IsDebugMode 检查是否启用debug模式
func (c Config) IsDebugMode() bool {
	return c.Level <= DEBUG
}

// ShouldLog 检查指定级别是否应该输出
func (c Config) ShouldLog(level Level) bool {
	return c.Level <= level
}
