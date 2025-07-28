package logger

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Level 日志级别类型
type Level int

const (
	DEBUG Level = iota
	INFO
	WARN
	ERROR
	FATAL
)

// 级别名称映射
var levelNames = map[Level]string{
	DEBUG: "DEBUG",
	INFO:  "INFO",
	WARN:  "WARN",
	ERROR: "ERROR",
	FATAL: "FATAL",
}

// Field 日志字段结构
type Field struct {
	Key   string
	Value interface{}
}

// Logger 简化的日志器
type Logger struct {
	level     Level
	logger    *log.Logger
	mutex     sync.RWMutex
	logFile   *os.File
	writers   []io.Writer
}

var (
	defaultLogger *Logger
	once          sync.Once
)

// 初始化默认logger
func init() {
	defaultLogger = createLogger()
}

// createLogger 创建并配置logger实例
func createLogger() *Logger {
	logger := &Logger{
		level:   INFO,
		writers: []io.Writer{os.Stdout}, // 默认输出到控制台
	}
	
	// 从环境变量设置级别
	if debug := os.Getenv("DEBUG"); debug != "" && (debug == "true" || debug == "1") {
		logger.level = DEBUG
	}
	if logLevel := os.Getenv("LOG_LEVEL"); logLevel != "" {
		if level, err := ParseLevel(logLevel); err == nil {
			logger.level = level
		}
	}
	
	// 设置文件输出
	if logFile := os.Getenv("LOG_FILE"); logFile != "" {
		if file, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644); err == nil {
			logger.logFile = file
			// 检查是否禁用控制台输出
			if os.Getenv("LOG_CONSOLE") == "false" {
				logger.writers = []io.Writer{file} // 只输出到文件
			} else {
				logger.writers = []io.Writer{os.Stdout, file} // 同时输出到控制台和文件
			}
		} else {
			fmt.Fprintf(os.Stderr, "无法打开日志文件 %s: %v\n", logFile, err)
		}
	}
	
	// 创建多写入器
	multiWriter := io.MultiWriter(logger.writers...)
	logger.logger = log.New(multiWriter, "", 0)
	
	return logger
}

// ParseLevel 从字符串解析日志级别
func ParseLevel(s string) (Level, error) {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "DEBUG":
		return DEBUG, nil
	case "INFO":
		return INFO, nil
	case "WARN", "WARNING":
		return WARN, nil
	case "ERROR":
		return ERROR, nil
	case "FATAL":
		return FATAL, nil
	default:
		return INFO, fmt.Errorf("unknown log level: %s", s)
	}
}

// shouldLog 检查是否应该记录指定级别的日志
func (l *Logger) shouldLog(level Level) bool {
	l.mutex.RLock()
	defer l.mutex.RUnlock()
	return l.level <= level
}

// log 内部日志记录方法
func (l *Logger) log(level Level, msg string, fields []Field) {
	if !l.shouldLog(level) {
		return
	}

	// 获取调用者信息
	_, file, line, _ := runtime.Caller(3)
	if idx := strings.LastIndex(file, "/"); idx >= 0 {
		file = file[idx+1:]
	}

	// 构建日志条目
	entry := map[string]interface{}{
		"timestamp": time.Now().Format("2006-01-02T15:04:05.000Z07:00"),
		"level":     levelNames[level],
		"message":   msg,
		"file":      fmt.Sprintf("%s:%d", file, line),
	}

	// 添加字段
	for _, field := range fields {
		entry[field.Key] = field.Value
	}

	// JSON格式输出
	jsonData, _ := json.Marshal(entry)
	l.logger.Println(string(jsonData))

	// Fatal级别退出程序
	if level == FATAL {
		os.Exit(1)
	}
}

// SetLevel 设置日志级别
func SetLevel(level Level) {
	defaultLogger.mutex.Lock()
	defer defaultLogger.mutex.Unlock()
	defaultLogger.level = level
}

// 全局日志函数
func Debug(msg string, fields ...Field) {
	defaultLogger.log(DEBUG, msg, fields)
}

func Info(msg string, fields ...Field) {
	defaultLogger.log(INFO, msg, fields)
}

func Warn(msg string, fields ...Field) {
	defaultLogger.log(WARN, msg, fields)
}

func Error(msg string, fields ...Field) {
	defaultLogger.log(ERROR, msg, fields)
}

func Fatal(msg string, fields ...Field) {
	defaultLogger.log(FATAL, msg, fields)
}

// 字段构造函数
func String(key, val string) Field {
	return Field{Key: key, Value: val}
}

func Int(key string, val int) Field {
	return Field{Key: key, Value: val}
}

func Int64(key string, val int64) Field {
	return Field{Key: key, Value: val}
}

func Bool(key string, val bool) Field {
	return Field{Key: key, Value: val}
}

func Err(err error) Field {
	if err == nil {
		return Field{Key: "error", Value: nil}
	}
	return Field{Key: "error", Value: err.Error()}
}

func Duration(key string, val time.Duration) Field {
	return Field{Key: key, Value: val}
}

func Any(key string, val interface{}) Field {
	return Field{Key: key, Value: val}
}

// Reinitialize 重新初始化默认logger（用于.env文件加载后）
func Reinitialize() {
	if defaultLogger.logFile != nil {
		defaultLogger.logFile.Close()
	}
	defaultLogger = createLogger()
}

// Config 配置结构（兼容性）
type Config struct {
	Level Level
}

// InitLogger 初始化logger（兼容性）
func InitLogger(config Config) error {
	SetLevel(config.Level)
	return nil
}

// ParseConfig 解析配置（兼容性）
func ParseConfig() Config {
	level := INFO
	if debug := os.Getenv("DEBUG"); debug != "" && (debug == "true" || debug == "1") {
		level = DEBUG
	}
	if logLevel := os.Getenv("LOG_LEVEL"); logLevel != "" {
		if l, err := ParseLevel(logLevel); err == nil {
			level = l
		}
	}
	return Config{Level: level}
}

// Close 关闭logger（兼容性）
func Close() error {
	if defaultLogger.logFile != nil {
		return defaultLogger.logFile.Close()
	}
	return nil
}

// Print functions for backwards compatibility
func Print(msg string) {
	fmt.Print(msg)
}

func Printf(format string, args ...interface{}) {
	fmt.Printf(format, args...)
}

func Println(msg string) {
	fmt.Println(msg)
}
