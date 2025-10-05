package config

import "time"

// Tuning 性能和行为调优参数
// 从硬编码提取为可配置常量，遵循 KISS 原则
const (
	// ========== 文本流式传输调优 ==========

	// MinTextFlushChars 文本聚合的最小冲刷字符数
	// 避免极短片段导致颗粒化，默认10字符
	MinTextFlushChars = 10

	// TextFlushMaxChars 文本聚合的最大缓冲字符数
	// 达到此阈值即使未遇到标点也强制冲刷
	TextFlushMaxChars = 64

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
