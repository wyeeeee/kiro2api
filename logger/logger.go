package logger

import (
	"fmt"
	"io"
	"log"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/bytedance/sonic"
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
	Value any
}

// Logger 优化的日志器
type Logger struct {
	level        int64       // 使用原子操作的日志级别
	logger       *log.Logger // log.Logger本身线程安全，移除mutex
	logFile      *os.File
	writers      []io.Writer
	enableCaller bool // 控制是否获取调用栈信息
	callerSkip   int  // 调用栈深度
}

var (
	defaultLogger *Logger
)

// 初始化默认logger
func init() {
	defaultLogger = createLogger()
}

// createLogger 创建并配置logger实例
func createLogger() *Logger {
	logger := &Logger{
		level:        int64(INFO),
		writers:      []io.Writer{os.Stdout}, // 默认输出到控制台
		enableCaller: false,                  // 默认禁用调用栈获取
		callerSkip:   3,                      // 默认调用栈深度
	}

	// 从环境变量设置级别
	if debug := os.Getenv("DEBUG"); debug != "" && (debug == "true" || debug == "1") {
		atomic.StoreInt64(&logger.level, int64(DEBUG))
	}
	if logLevel := os.Getenv("LOG_LEVEL"); logLevel != "" {
		if level, err := ParseLevel(logLevel); err == nil {
			atomic.StoreInt64(&logger.level, int64(level))
		}
	}

	// 从环境变量控制优化特性
	if enableCaller := os.Getenv("LOG_ENABLE_CALLER"); enableCaller == "true" || enableCaller == "1" {
		logger.enableCaller = true
	}
	if callerSkip := os.Getenv("LOG_CALLER_SKIP"); callerSkip != "" {
		if skip, err := strconv.Atoi(callerSkip); err == nil && skip > 0 {
			logger.callerSkip = skip
		}
	}

	// 设置文件输出
	if logFile := os.Getenv("LOG_FILE"); logFile != "" {
		if file, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644); err == nil {
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

// shouldLog 检查是否应该记录指定级别的日志（优化：原子操作）
func (l *Logger) shouldLog(level Level) bool {
	return atomic.LoadInt64(&l.level) <= int64(level)
}

// log 内部日志记录方法（优化版本）
func (l *Logger) log(level Level, msg string, fields []Field) {
	if !l.shouldLog(level) {
		return
	}

	// 构建日志条目
	entry := map[string]any{
		"timestamp": time.Now().Format("2006-01-02T15:04:05.000Z07:00"),
		"level":     levelNames[level],
		"message":   msg,
	}

	// 按需获取调用者信息（优化：可配置）
	if l.enableCaller {
		if _, file, line, ok := runtime.Caller(l.callerSkip); ok {
			if idx := strings.LastIndex(file, "/"); idx >= 0 {
				file = file[idx+1:]
			}
			entry["file"] = fmt.Sprintf("%s:%d", file, line)
		}
	}

	// 添加字段
	for _, field := range fields {
		entry[field.Key] = field.Value
	}

	// 优化的JSON序列化（使用sonic高性能库）
	var jsonData []byte
	// 使用sonic的高性能序列化
	jsonData, _ = sonic.Marshal(entry)

	// 直接输出日志 - log.Logger本身已经线程安全！
	l.logger.Println(string(jsonData))

	// Fatal级别退出程序
	if level == FATAL {
		os.Exit(1)
	}
}

// SetLevel 设置日志级别（优化：原子操作）
func SetLevel(level Level) {
	atomic.StoreInt64(&defaultLogger.level, int64(level))
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

func Any(key string, val any) Field {
	return Field{Key: key, Value: val}
}

// Reinitialize 重新初始化默认logger（用于.env文件加载后）
func Reinitialize() {
	if defaultLogger.logFile != nil {
		defaultLogger.logFile.Close()
	}
	defaultLogger = createLogger()
}

// OptimizationConfig 优化配置结构（新增）
type OptimizationConfig struct {
	EnableCaller bool `json:"enable_caller"`
	EnablePool   bool `json:"enable_pool"`
	CallerSkip   int  `json:"caller_skip"`
}

// Config 配置结构（兼容性）
type Config struct {
	Level Level
}
