package logger

import (
	"fmt"
	"os"
	"sync"
	"time"
)

// Logger 主要Logger结构
type Logger struct {
	config    Config
	formatter Formatter
	writer    Writer
	level     Level
	mutex     sync.RWMutex
}

// 全局实例
var (
	defaultLogger *Logger
	once          sync.Once
)

// InitLogger 初始化日志器
func InitLogger(config Config) error {
	logger, err := NewLogger(config)
	if err != nil {
		return err
	}

	// 关闭旧的logger
	if defaultLogger != nil {
		defaultLogger.Close()
	}

	defaultLogger = logger
	return nil
}

// NewLogger 创建新的Logger实例
func NewLogger(config Config) (*Logger, error) {
	// 创建格式化器
	var formatter Formatter
	switch config.Format {
	case JSONFormat:
		formatter = NewJSONFormatter(config.TimeFormat)
	default:
		formatter = NewConsoleFormatter(config.Color, config.TimeFormat)
	}

	// 创建输出器
	var writers []Writer

	// 控制台输出
	if config.Console {
		writers = append(writers, NewConsoleWriter())
	}

	// 文件输出
	if config.File != "" {
		fileWriter, err := NewFileWriter(config.File)
		if err != nil {
			// 如果文件输出失败，记录错误但继续使用控制台输出
			fmt.Fprintf(os.Stderr, "Failed to create file writer: %v\n", err)
		} else {
			writers = append(writers, fileWriter)
		}
	}

	// 如果没有任何输出器，至少创建控制台输出
	if len(writers) == 0 {
		writers = append(writers, NewConsoleWriter())
	}

	var writer Writer
	if len(writers) == 1 {
		writer = writers[0]
	} else {
		writer = NewMultiWriter(writers...)
	}

	return &Logger{
		config:    config,
		formatter: formatter,
		writer:    writer,
		level:     config.Level,
	}, nil
}

// getDefaultLogger 获取默认日志器
func getDefaultLogger() *Logger {
	once.Do(func() {
		config := ParseConfig()
		logger, err := NewLogger(config)
		if err != nil {
			// 如果创建失败，使用最基本的配置
			logger, _ = NewLogger(DefaultConfig)
		}
		defaultLogger = logger
	})
	return defaultLogger
}

// Close 关闭日志器
func (l *Logger) Close() error {
	l.mutex.Lock()
	defer l.mutex.Unlock()

	if l.writer != nil {
		return l.writer.Close()
	}
	return nil
}

// log 内部日志记录方法
func (l *Logger) log(level Level, msg string, fields []Field) {
	if !l.shouldLog(level) {
		return
	}

	l.mutex.RLock()
	defer l.mutex.RUnlock()

	// 获取调用者信息
	file, line, function := GetCallerInfo(4) // 跳过4层调用栈

	entry := LogEntry{
		Time:     time.Now(),
		Level:    level,
		Message:  msg,
		Fields:   fields,
		File:     file,
		Line:     line,
		Function: function,
	}

	data := l.formatter.Format(entry)
	l.writer.Write(data)
}

// logf 内部格式化日志记录方法
func (l *Logger) logf(level Level, format string, args ...interface{}) {
	if !l.shouldLog(level) {
		return
	}

	msg := fmt.Sprintf(format, args...)
	l.log(level, msg, nil)
}

// shouldLog 检查是否应该记录指定级别的日志
func (l *Logger) shouldLog(level Level) bool {
	return l.level <= level
}

// SetLevel 设置日志级别
func (l *Logger) SetLevel(level Level) {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	l.level = level
}

// GetLevel 获取当前日志级别
func (l *Logger) GetLevel() Level {
	l.mutex.RLock()
	defer l.mutex.RUnlock()
	return l.level
}

// IsDebugEnabled 检查是否启用debug模式
func (l *Logger) IsDebugEnabled() bool {
	return l.shouldLog(DEBUG)
}

// 全局API函数

// Debug 输出debug级别日志
func Debug(msg string, fields ...Field) {
	getDefaultLogger().log(DEBUG, msg, fields)
}

// Debugf 输出格式化debug级别日志
func Debugf(format string, args ...interface{}) {
	getDefaultLogger().logf(DEBUG, format, args...)
}

// Info 输出info级别日志
func Info(msg string, fields ...Field) {
	getDefaultLogger().log(INFO, msg, fields)
}

// Infof 输出格式化info级别日志
func Infof(format string, args ...interface{}) {
	getDefaultLogger().logf(INFO, format, args...)
}

// Warn 输出warn级别日志
func Warn(msg string, fields ...Field) {
	getDefaultLogger().log(WARN, msg, fields)
}

// Warnf 输出格式化warn级别日志
func Warnf(format string, args ...interface{}) {
	getDefaultLogger().logf(WARN, format, args...)
}

// Error 输出error级别日志
func Error(msg string, fields ...Field) {
	getDefaultLogger().log(ERROR, msg, fields)
}

// Errorf 输出格式化error级别日志
func Errorf(format string, args ...interface{}) {
	getDefaultLogger().logf(ERROR, format, args...)
}

// Fatal 输出fatal级别日志并退出程序
func Fatal(msg string, fields ...Field) {
	getDefaultLogger().log(FATAL, msg, fields)
	os.Exit(1)
}

// Fatalf 输出格式化fatal级别日志并退出程序
func Fatalf(format string, args ...interface{}) {
	getDefaultLogger().logf(FATAL, format, args...)
	os.Exit(1)
}

// 用户界面专用函数（不受日志级别限制）

// Print 输出消息（不受日志级别限制）
func Print(msg string) {
	fmt.Print(msg)
}

// Printf 输出格式化消息（不受日志级别限制）
func Printf(format string, args ...interface{}) {
	fmt.Printf(format, args...)
}

// Println 输出消息并换行（不受日志级别限制）
func Println(msg string) {
	fmt.Println(msg)
}

// 全局工具函数

// IsDebugEnabled 检查是否启用debug模式
func IsDebugEnabled() bool {
	return getDefaultLogger().IsDebugEnabled()
}

// SetLevel 设置全局日志级别
func SetLevel(level Level) {
	getDefaultLogger().SetLevel(level)
}

// GetLevel 获取全局日志级别
func GetLevel() Level {
	return getDefaultLogger().GetLevel()
}

// Close 关闭全局日志器
func Close() error {
	if defaultLogger != nil {
		return defaultLogger.Close()
	}
	return nil
}
