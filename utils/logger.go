package utils

import (
	"kiro2api/logger"
)

// Logger 日志器接口，匹配logger包的功能
type Logger interface {
	Debug(msg string, fields ...logger.Field)
	Info(msg string, fields ...logger.Field)
	Warn(msg string, fields ...logger.Field)
	Error(msg string, fields ...logger.Field)
	Fatal(msg string, fields ...logger.Field)
}

// defaultLoggerWrapper 包装默认logger以满足接口
type defaultLoggerWrapper struct{}

func (w *defaultLoggerWrapper) Debug(msg string, fields ...logger.Field) {
	logger.Debug(msg, fields...)
}

func (w *defaultLoggerWrapper) Info(msg string, fields ...logger.Field) {
	logger.Info(msg, fields...)
}

func (w *defaultLoggerWrapper) Warn(msg string, fields ...logger.Field) {
	logger.Warn(msg, fields...)
}

func (w *defaultLoggerWrapper) Error(msg string, fields ...logger.Field) {
	logger.Error(msg, fields...)
}

func (w *defaultLoggerWrapper) Fatal(msg string, fields ...logger.Field) {
	logger.Fatal(msg, fields...)
}

// GetLogger 获取默认日志器实例
func GetLogger() Logger {
	return &defaultLoggerWrapper{}
}

// 导出logger包的常用函数
var (
	String   = logger.String
	Int      = logger.Int
	Int64    = logger.Int64
	Bool     = logger.Bool
	Err      = logger.Err
	Duration = logger.Duration
	Any      = logger.Any
)

// Float64 创建float64类型的日志字段
func Float64(key string, val float64) logger.Field {
	return logger.Field{Key: key, Value: val}
}