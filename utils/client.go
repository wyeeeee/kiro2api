package utils

import (
	"crypto/tls"
	"net"
	"net/http"
	"os"
	"strconv"
	"time"

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
			MaxIdleConns:        200,               // 总连接池大小增加到200
			MaxIdleConnsPerHost: 50,                // 每个主机最大空闲连接数提升到50
			MaxConnsPerHost:     100,               // 每个主机最大连接数增加到100
			IdleConnTimeout:     120 * time.Second, // 空闲连接超时延长到2分钟

			// 连接建立优化
			DialContext: (&net.Dialer{
				Timeout:   15 * time.Second, // 连接超时增加到15秒
				KeepAlive: 60 * time.Second, // Keep-Alive间隔延长到60秒
				DualStack: true,             // 启用IPv4/IPv6双栈
			}).DialContext,

			// TLS配置优化
			TLSHandshakeTimeout: 15 * time.Second, // TLS握手超时
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
			ForceAttemptHTTP2:     true,             // 强制尝试HTTP/2
			DisableCompression:    false,            // 启用压缩
			WriteBufferSize:       32 * 1024,        // 写缓冲区32KB
			ReadBufferSize:        32 * 1024,        // 读缓冲区32KB
			ResponseHeaderTimeout: 60 * time.Second, // 响应头超时1分钟
			ExpectContinueTimeout: 2 * time.Second,  // Expect 100-continue超时
		}
	}

	// 短时间请求客户端
	SharedHTTPClient = &http.Client{
		Timeout:   shortTimeout,
		Transport: createBaseTransport(),
	}

	// 长时间请求客户端
	longTransport := createBaseTransport()
	longTransport.ResponseHeaderTimeout = 5 * time.Minute // 响应头超时延长到5分钟
	LongRequestClient = &http.Client{
		Timeout:   defaultTimeout,
		Transport: longTransport,
	}

	// 流式请求客户端（专门优化）
	streamTransport := createBaseTransport()
	streamTransport.MaxIdleConnsPerHost = 100                // 流式连接池更大
	streamTransport.ResponseHeaderTimeout = 10 * time.Minute // 流式响应头超时更长
	streamTransport.WriteBufferSize = 64 * 1024              // 流式写缓冲区更大
	streamTransport.ReadBufferSize = 64 * 1024               // 流式读缓冲区更大
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
func GetOptimalClient(anthropicReq *types.AnthropicRequest) *http.Client {
	if anthropicReq == nil {
		return SharedHTTPClient
	}

	// 流式请求优先使用流式客户端
	if anthropicReq.Stream {
		return StreamingClient
	}

	// 非流式请求根据复杂度选择
	if AnalyzeRequestComplexity(*anthropicReq) == ComplexRequest {
		return LongRequestClient
	}

	return SharedHTTPClient
}
