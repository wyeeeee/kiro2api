package logger

import (
	"fmt"
	"strings"
)

// Level 日志级别类型
type Level int

const (
	TRACE Level = iota
	DEBUG
	INFO
	WARN
	ERROR
	FATAL
)

// 级别名称映射
var levelNames = map[Level]string{
	TRACE: "TRACE",
	DEBUG: "DEBUG",
	INFO:  "INFO",
	WARN:  "WARN",
	ERROR: "ERROR",
	FATAL: "FATAL",
}

var nameToLevel = map[string]Level{
	"TRACE": TRACE,
	"DEBUG": DEBUG,
	"INFO":  INFO,
	"WARN":  WARN,
	"ERROR": ERROR,
	"FATAL": FATAL,
}

// String 返回级别的字符串表示
func (l Level) String() string {
	if name, exists := levelNames[l]; exists {
		return name
	}
	return fmt.Sprintf("UNKNOWN(%d)", int(l))
}

// Enabled 检查当前级别是否启用目标级别
func (l Level) Enabled(target Level) bool {
	return l <= target
}

// ParseLevel 从字符串解析日志级别
func ParseLevel(s string) (Level, error) {
	upper := strings.ToUpper(strings.TrimSpace(s))
	if level, exists := nameToLevel[upper]; exists {
		return level, nil
	}
	return INFO, fmt.Errorf("unknown log level: %s", s)
}

// GetColor 获取级别对应的ANSI颜色代码
func (l Level) GetColor() string {
	switch l {
	case TRACE:
		return "\033[37m" // 白色
	case DEBUG:
		return "\033[36m" // 青色
	case INFO:
		return "\033[32m" // 绿色
	case WARN:
		return "\033[33m" // 黄色
	case ERROR:
		return "\033[31m" // 红色
	case FATAL:
		return "\033[35m" // 紫色
	default:
		return "\033[0m" // 重置
	}
}

// ResetColor ANSI颜色重置代码
const ResetColor = "\033[0m"
