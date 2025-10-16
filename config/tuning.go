package config

import "time"

// Tuning 性能和行为调优参数
// 从硬编码提取为可配置常量，遵循 KISS 原则
const (
	// ========== 解析器配置 ==========

	// ParserMaxErrors 解析器容忍的最大错误次数
	// 超过此次数后停止解析
	ParserMaxErrors = 10

	// ========== Token缓存配置 ==========

	// TokenCacheTTL Token缓存的生存时间
	// 过期后需要重新刷新
	TokenCacheTTL = 5 * time.Minute

	// ========== 超时配置 ==========

	// ServerIdleTimeout 服务器空闲连接超时
	ServerIdleTimeout = 120 * time.Second

	// ========== HTTP客户端配置 ==========

	// HTTPClientIdleConnTimeout HTTP客户端空闲连接超时
	HTTPClientIdleConnTimeout = 120 * time.Second

	// HTTPClientConnectTimeout HTTP客户端连接超时
	HTTPClientConnectTimeout = 15 * time.Second

	// HTTPClientKeepAlive HTTP客户端Keep-Alive间隔
	HTTPClientKeepAlive = 30 * time.Second

	// HTTPClientTLSHandshakeTimeout HTTP客户端TLS握手超时
	HTTPClientTLSHandshakeTimeout = 15 * time.Second

	// HTTPClientResponseHeaderTimeout HTTP客户端响应头超时
	HTTPClientResponseHeaderTimeout = 60 * time.Second

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
)
