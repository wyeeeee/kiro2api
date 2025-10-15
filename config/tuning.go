package config

import "time"

// Tuning 性能和行为调优参数
// 从硬编码提取为可配置常量，遵循 KISS 原则
const (
	// ========== 文本流式传输调优 ==========

	// 文本聚合已移除（直传SSE），相关阈值删除

	// ========== 解析器配置 ==========

	// ParserBufferSize 解析器环形缓冲区大小
	// 用于流式解析的临时缓冲区
	ParserBufferSize = 512 * 1024 // 512KB

	// ParserTempBufferSize 解析器临时缓冲区大小
	// 用于单个消息的临时缓冲
	ParserTempBufferSize = 16 * 1024 // 16KB

	// ParserMinMessageSize AWS EventStream 最小消息长度
	// 4(totalLen) + 4(headerLen) + 4(preludeCRC) + 4(msgCRC) = 16
	ParserMinMessageSize = 16

	// ParserMaxMessageSize 解析器允许的最大消息长度
	// 防止恶意或损坏的数据导致内存耗尽
	ParserMaxMessageSize = 16 * 1024 * 1024 // 16MB

	// ParserMaxErrors 解析器容忍的最大错误次数
	// 超过此次数后停止解析
	ParserMaxErrors = 10

	// ParseSlowThreshold 解析耗时警告阈值
	// 超过此时间的解析操作会记录警告日志
	ParseSlowThreshold = 100 * time.Millisecond

	// ========== Token缓存配置 ==========

	// TokenCacheTTL Token缓存的生存时间
	// 过期后需要重新刷新
	TokenCacheTTL = 5 * time.Minute

	// TokenCleanupInterval Token缓存清理间隔
	// 后台定期清理过期token的时间间隔
	TokenCleanupInterval = 5 * time.Minute

	// ========== 超时配置 ==========

	// NonStreamParseTimeout 非流式解析的超时时间
	// 防止解析器死循环或hang住
	NonStreamParseTimeout = 10 * time.Second

	// DefaultRequestTimeout 默认请求超时时间（简单请求）
	DefaultRequestTimeout = 2 * time.Minute

	// ComplexRequestTimeout 复杂请求超时时间
	ComplexRequestTimeout = 15 * time.Minute

	// ServerReadTimeout 服务器读取超时
	ServerReadTimeout = 16 * time.Minute

	// ServerWriteTimeout 服务器写入超时
	ServerWriteTimeout = 16 * time.Minute

	// ServerIdleTimeout 服务器空闲连接超时
	ServerIdleTimeout = 120 * time.Second

	// ========== HTTP客户端配置 ==========

	// HTTPClientIdleConnTimeout HTTP客户端空闲连接超时
	HTTPClientIdleConnTimeout = 120 * time.Second

	// HTTPClientConnectTimeout HTTP客户端连接超时
	HTTPClientConnectTimeout = 15 * time.Second

	// HTTPClientKeepAlive HTTP客户端Keep-Alive间隔
	HTTPClientKeepAlive = 60 * time.Second

	// HTTPClientTLSHandshakeTimeout HTTP客户端TLS握手超时
	HTTPClientTLSHandshakeTimeout = 15 * time.Second

	// HTTPClientResponseHeaderTimeout HTTP客户端响应头超时
	HTTPClientResponseHeaderTimeout = 60 * time.Second

	// HTTPClientExpectContinueTimeout HTTP客户端Expect 100-continue超时
	HTTPClientExpectContinueTimeout = 2 * time.Second

	// HTTPClientStreamResponseHeaderTimeout 流式请求响应头超时
	HTTPClientStreamResponseHeaderTimeout = 10 * time.Minute

	// HTTPClientMaxIdleConns HTTP客户端最大空闲连接数
	HTTPClientMaxIdleConns = 200

	// HTTPClientMaxIdleConnsPerHost 每个主机最大空闲连接数
	HTTPClientMaxIdleConnsPerHost = 100

	// HTTPClientMaxConnsPerHost 每个主机最大连接数
	HTTPClientMaxConnsPerHost = 100

	// ========== HTTP服务器配置 ==========

	// MaxHeaderBytes HTTP请求头最大字节数
	MaxHeaderBytes = 1 << 20 // 1MB

	// ========== 调试和监控 ==========

	// DebugPayloadPreviewLength 调试日志中payload预览的最大长度
	DebugPayloadPreviewLength = 128

	// ToolUseIDMinLength tool_use_id的最小有效长度
	ToolUseIDMinLength = 20

	// ToolUseIDMaxLength tool_use_id的最大有效长度
	ToolUseIDMaxLength = 50

	// ========== 对象池配置 ==========

	// ParserPoolSize 解析器对象池大小
	ParserPoolSize = 10

	// BufferPoolSize 缓冲区对象池大小
	BufferPoolSize = 20

	// BufferInitialSize Buffer对象池初始缓冲区大小
	BufferInitialSize = 4096

	// StringBuilderInitialSize StringBuilder初始大小
	StringBuilderInitialSize = 1024

	// ByteSliceInitialSize 字节数组初始大小
	ByteSliceInitialSize = 8192

	// BufferMaxRetainSize Buffer对象最大保留大小（超过则丢弃）
	BufferMaxRetainSize = 64 * 1024

	// StringBuilderMaxRetainSize StringBuilder最大保留大小
	StringBuilderMaxRetainSize = 32 * 1024

	// ByteSliceMaxRetainSize 字节数组最大保留大小
	ByteSliceMaxRetainSize = 128 * 1024

	// MapInitialCapacity Map对象池初始容量
	MapInitialCapacity = 16

	// MapMaxSize Map对象最大大小（超过则丢弃）
	MapMaxSize = 100

	// StringSliceInitialCapacity 字符串数组初始容量
	StringSliceInitialCapacity = 8

	// StringSliceMaxSize 字符串数组最大大小（超过则丢弃）
	StringSliceMaxSize = 1000

	// ========== 解析器超时和重试 ==========

	// ParserValidationTimeout 解析器验证超时
	ParserValidationTimeout = 30 * time.Second

	// ParserHandlerTimeout 处理器超时时间
	ParserHandlerTimeout = 10 * time.Second

	// ========== SSE和流式响应配置 ==========

	// SSERetryDelay SSE重试延迟
	SSERetryDelay = 100 * time.Millisecond

	// StreamDuplicateWindow 重复事件检测时间窗口
	StreamDuplicateWindow = 100 * time.Millisecond
)

// GetParseSlowThreshold 获取解析慢操作阈值（支持运行时配置）
func GetParseSlowThreshold() time.Duration {
	// 未来可以从环境变量读取
	return ParseSlowThreshold
}

// GetTokenCacheTTL 获取Token缓存TTL（支持运行时配置）
func GetTokenCacheTTL() time.Duration {
	// 未来可以从环境变量读取
	return TokenCacheTTL
}
