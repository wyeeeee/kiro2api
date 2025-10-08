package config

import "time"

// Token管理常量
const (
	// DefaultTokenAvailableCount 默认token可用次数
	DefaultTokenAvailableCount = 100.0

	// TokenCacheKeyFormat token缓存key格式
	TokenCacheKeyFormat = "token_%d"
)

// 消息处理常量
const (
	// MessageIDFormat 消息ID格式
	MessageIDFormat = "msg_%s"

	// MessageIDTimeFormat 消息ID时间格式
	MessageIDTimeFormat = "20060102150405"

	// ParseTimeout 解析超时时间
	ParseTimeout = 10 * time.Second

	// RetryDelay 重试延迟
	RetryDelay = 100 * time.Millisecond

	// LogPreviewMaxLength 日志预览最大长度
	LogPreviewMaxLength = 100
)

// Token估算常量
const (
	// BaseToolsOverhead 基础工具开销（tokens）
	BaseToolsOverhead = 100

	// ShortTextThreshold 短文本阈值（字符数）
	ShortTextThreshold = 100

	// LongTextThreshold 长文本阈值（字符数）
	LongTextThreshold = 1000
)

// HTTP客户端常量
const (
	// MaxConnsPerHost 每个主机的最大连接数
	MaxConnsPerHost = 100

	// ExpectContinueTimeout Expect: 100-continue 超时时间
	ExpectContinueTimeout = 2 * time.Second

	// ResponseHeaderTimeout 响应头超时时间
	ResponseHeaderTimeout = 5 * time.Minute

	// StreamResponseTimeout 流式响应超时时间
	StreamResponseTimeout = 10 * time.Minute

	// SimpleRequestTimeout 简单请求超时时间
	SimpleRequestTimeout = 2 * time.Minute
)

// 对象池常量
const (
	// MapPoolInitialCapacity Map池初始容量
	MapPoolInitialCapacity = 16

	// MapPoolMaxSize Map池最大大小
	MapPoolMaxSize = 100

	// StringBuilderMaxSize StringBuilder最大大小
	StringBuilderMaxSize = 100
)
