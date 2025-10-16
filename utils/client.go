package utils

import (
	"crypto/tls"
	"net"
	"net/http"
	"os"
	"strconv"
	"time"

	"kiro2api/config"
	"kiro2api/types"
)

var (
	// SharedHTTPClient 共享的HTTP客户端实例，优化了连接池和性能配置
	SharedHTTPClient *http.Client
	// LongRequestClient 专用于长时间请求的HTTP客户端
	LongRequestClient *http.Client
	// StreamingClient 专用于流式请求的HTTP客户端
	StreamingClient *http.Client
)

func init() {
	// 获取超时配置
	defaultTimeout := getTimeoutFromEnv("REQUEST_TIMEOUT_MINUTES", 15) * time.Minute
	shortTimeout := getTimeoutFromEnv("SIMPLE_REQUEST_TIMEOUT_MINUTES", 2) * time.Minute
	streamTimeout := getTimeoutFromEnv("STREAM_REQUEST_TIMEOUT_MINUTES", 30) * time.Minute

	// 检查TLS配置并记录日志
	skipTLS := shouldSkipTLSVerify()
	if skipTLS {
		// 仅在需要时导入logger包避免循环依赖
		// 使用标准库日志记录TLS状态
		os.Stderr.WriteString("[WARNING] TLS证书验证已禁用 - 仅适用于开发/调试环境\n")
	}

	// 创建高性能的基础传输配置
	createBaseTransport := func() *http.Transport {
		return &http.Transport{
			// 连接池配置优化
			MaxIdleConns:        config.HTTPClientMaxIdleConns,
			MaxIdleConnsPerHost: config.HTTPClientMaxIdleConnsPerHost,
			MaxConnsPerHost:     config.HTTPClientMaxConnsPerHost,
			IdleConnTimeout:     config.HTTPClientIdleConnTimeout,

			// 连接建立优化
			DialContext: (&net.Dialer{
				Timeout:   config.HTTPClientConnectTimeout,
				KeepAlive: config.HTTPClientKeepAlive,
				DualStack: true, // 启用IPv4/IPv6双栈
			}).DialContext,

			// TLS配置优化
			TLSHandshakeTimeout: config.HTTPClientTLSHandshakeTimeout,
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: shouldSkipTLSVerify(), // 基于环境动态控制
				MinVersion:         tls.VersionTLS12,      // 最低TLS 1.2
				MaxVersion:         tls.VersionTLS13,      // 最高TLS 1.3
				CipherSuites: []uint16{
					tls.TLS_AES_256_GCM_SHA384,
					tls.TLS_CHACHA20_POLY1305_SHA256,
					tls.TLS_AES_128_GCM_SHA256,
				},
			},

			// HTTP/2和压缩优化
			ForceAttemptHTTP2:     true,  // 强制尝试HTTP/2
			DisableCompression:    false, // 启用压缩
			WriteBufferSize:       32 * 1024,
			ReadBufferSize:        32 * 1024,
			ResponseHeaderTimeout: config.HTTPClientResponseHeaderTimeout,
		}
	}

	// 短时间请求客户端
	SharedHTTPClient = &http.Client{
		Timeout:   shortTimeout,
		Transport: createBaseTransport(),
	}

	// 长时间请求客户端
	longTransport := createBaseTransport()
	longTransport.ResponseHeaderTimeout = config.ResponseHeaderTimeout
	LongRequestClient = &http.Client{
		Timeout:   defaultTimeout,
		Transport: longTransport,
	}

	// 流式请求客户端（专门优化）
	streamTransport := createBaseTransport()
	streamTransport.MaxIdleConnsPerHost = config.HTTPClientMaxConnsPerHost
	streamTransport.ResponseHeaderTimeout = config.HTTPClientStreamResponseHeaderTimeout
	streamTransport.WriteBufferSize = 64 * 1024
	streamTransport.ReadBufferSize = 64 * 1024
	StreamingClient = &http.Client{
		Timeout:   streamTimeout,
		Transport: streamTransport,
	}
}

// getTimeoutFromEnv 从环境变量获取超时配置（分钟）
func getTimeoutFromEnv(envVar string, defaultMinutes int) time.Duration {
	if env := os.Getenv(envVar); env != "" {
		if minutes, err := strconv.Atoi(env); err == nil && minutes > 0 {
			return time.Duration(minutes)
		}
	}
	return time.Duration(defaultMinutes)
}

// shouldSkipTLSVerify 根据GIN_MODE决定是否跳过TLS证书验证
// GIN_MODE=debug时跳过验证，其他模式启用验证
func shouldSkipTLSVerify() bool {
	return os.Getenv("GIN_MODE") == "debug"
}

// DoRequest 执行HTTP请求（使用默认客户端）
func DoRequest(req *http.Request) (*http.Response, error) {
	return SharedHTTPClient.Do(req)
}

// DoLongRequest 执行长时间请求
func DoLongRequest(req *http.Request) (*http.Response, error) {
	return LongRequestClient.Do(req)
}

// DoStreamingRequest 执行流式请求
func DoStreamingRequest(req *http.Request) (*http.Response, error) {
	return StreamingClient.Do(req)
}

// DoSmartRequest 根据请求复杂度智能选择客户端并执行请求
func DoSmartRequest(httpReq *http.Request, anthropicReq *types.AnthropicRequest) (*http.Response, error) {
	if anthropicReq != nil {
		// 流式请求使用专门的流式客户端
		if anthropicReq.Stream {
			return DoStreamingRequest(httpReq)
		}

		// 非流式请求根据复杂度选择客户端
		if AnalyzeRequestComplexity(*anthropicReq) == ComplexRequest {
			return DoLongRequest(httpReq)
		}
	}

	return DoRequest(httpReq)
}

// GetOptimalClient 获取最优HTTP客户端
