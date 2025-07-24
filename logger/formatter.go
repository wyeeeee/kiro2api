package logger

import (
	"encoding/json"
	"fmt"
	"runtime"
	"strings"
	"time"
)

// LogEntry 日志条目结构
type LogEntry struct {
	Time     time.Time
	Level    Level
	Message  string
	Fields   []Field
	File     string
	Line     int
	Function string
}

// Formatter 格式化器接口
type Formatter interface {
	Format(entry LogEntry) []byte
}

// ConsoleFormatter 控制台格式化器
type ConsoleFormatter struct {
	EnableColor bool
	TimeFormat  string
}

// NewConsoleFormatter 创建控制台格式化器
func NewConsoleFormatter(enableColor bool, timeFormat string) *ConsoleFormatter {
	return &ConsoleFormatter{
		EnableColor: enableColor,
		TimeFormat:  timeFormat,
	}
}

// Format 格式化日志条目为控制台输出格式
func (f *ConsoleFormatter) Format(entry LogEntry) []byte {
	var builder strings.Builder

	// 时间戳
	builder.WriteString("[")
	builder.WriteString(entry.Time.Format(f.TimeFormat))
	builder.WriteString("] ")

	// 日志级别（带颜色）
	if f.EnableColor {
		builder.WriteString(entry.Level.GetColor())
	}

	levelStr := fmt.Sprintf("[%-5s]", entry.Level.String())
	builder.WriteString(levelStr)

	if f.EnableColor {
		builder.WriteString(ResetColor)
	}

	builder.WriteString(" ")

	// 文件位置信息（简化路径）
	if entry.File != "" {
		fileName := getShortFileName(entry.File)
		builder.WriteString(fmt.Sprintf("[%s:%d] ", fileName, entry.Line))
	}

	// 主消息
	builder.WriteString(entry.Message)

	// 结构化字段
	if len(entry.Fields) > 0 {
		builder.WriteString(" ")
		for i, field := range entry.Fields {
			if i > 0 {
				builder.WriteString(" ")
			}
			builder.WriteString(fmt.Sprintf("%s=%s", field.Key, field.FormatValue()))
		}
	}

	builder.WriteString("\n")
	return []byte(builder.String())
}

// JSONFormatter JSON格式化器
type JSONFormatter struct {
	TimeFormat string
}

// NewJSONFormatter 创建JSON格式化器
func NewJSONFormatter(timeFormat string) *JSONFormatter {
	return &JSONFormatter{
		TimeFormat: timeFormat,
	}
}

// Format 格式化日志条目为JSON格式
func (f *JSONFormatter) Format(entry LogEntry) []byte {
	data := map[string]interface{}{
		"timestamp": entry.Time.Format(time.RFC3339Nano),
		"level":     entry.Level.String(),
		"message":   entry.Message,
	}

	// 添加文件信息
	if entry.File != "" {
		data["file"] = fmt.Sprintf("%s:%d", getShortFileName(entry.File), entry.Line)
		if entry.Function != "" {
			data["function"] = entry.Function
		}
	}

	// 添加结构化字段
	if len(entry.Fields) > 0 {
		fields := make(map[string]interface{})
		for _, field := range entry.Fields {
			fields[field.Key] = field.Value
		}
		data["fields"] = fields
	}

	jsonData, err := json.Marshal(data)
	if err != nil {
		// 如果JSON序列化失败，返回简单格式
		fallback := fmt.Sprintf(`{"timestamp":"%s","level":"%s","message":"JSON encoding error: %s"}`,
			entry.Time.Format(time.RFC3339),
			entry.Level.String(),
			err.Error())
		return []byte(fallback + "\n")
	}

	return append(jsonData, '\n')
}

// getShortFileName 获取简化的文件名
func getShortFileName(file string) string {
	// 只保留文件名和最后一级目录
	parts := strings.Split(file, "/")
	if len(parts) >= 2 {
		return parts[len(parts)-2] + "/" + parts[len(parts)-1]
	}
	if len(parts) == 1 {
		return parts[0]
	}
	return file
}

// GetCallerInfo 获取调用者信息
func GetCallerInfo(skip int) (file string, line int, function string) {
	pc, file, line, ok := runtime.Caller(skip)
	if !ok {
		return "unknown", 0, "unknown"
	}

	fn := runtime.FuncForPC(pc)
	if fn != nil {
		function = fn.Name()
		// 简化函数名，只保留包名和函数名
		if lastSlash := strings.LastIndex(function, "/"); lastSlash >= 0 {
			function = function[lastSlash+1:]
		}
	}

	return file, line, function
}
