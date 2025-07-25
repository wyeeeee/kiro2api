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
)

func init() {
	// 获取超时配置
	defaultTimeout := getTimeoutFromEnv("REQUEST_TIMEOUT_MINUTES", 15) * time.Minute
	shortTimeout := getTimeoutFromEnv("SIMPLE_REQUEST_TIMEOUT_MINUTES", 2) * time.Minute

	// 共享传输配置
	baseTransport := &http.Transport{
		// 连接池配置优化
		MaxIdleConns:        100,              // 总连接池大小
		MaxIdleConnsPerHost: 20,               // 每个主机最大空闲连接数(从默认2提升到20)
		MaxConnsPerHost:     0,                // 每个主机最大连接数，0表示无限制
		IdleConnTimeout:     90 * time.Second, // 空闲连接超时

		// 连接建立优化
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second, // 连接超时
			KeepAlive: 30 * time.Second, // Keep-Alive间隔
		}).DialContext,

		// TLS配置优化
		TLSHandshakeTimeout: 10 * time.Second, // TLS握手超时
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true, // 保持安全性
		},

		// HTTP/2和压缩优化
		ForceAttemptHTTP2:  true,  // 强制尝试HTTP/2
		DisableCompression: false, // 启用压缩以减少带宽

		// 响应头超时 - 适应长时间请求
		ResponseHeaderTimeout: 2 * time.Minute, // 从30秒提升到2分钟

		// 期望100-continue超时
		ExpectContinueTimeout: 1 * time.Second,
	}

	// 短时间请求客户端（兼容性）
	SharedHTTPClient = &http.Client{
		Timeout:   shortTimeout, // 简单请求2分钟超时
		Transport: baseTransport,
	}

	// 长时间请求客户端
	longTransport := baseTransport.Clone()
	LongRequestClient = &http.Client{
		Timeout:   defaultTimeout, // 复杂请求15分钟超时
		Transport: longTransport,
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

// DoWithMetrics 执行HTTP请求并记录性能指标
func DoWithMetrics(req *http.Request) (*http.Response, error) {
	start := time.Now()
	resp, err := SharedHTTPClient.Do(req)
	duration := time.Since(start)

	// 记录指标
	success := err == nil && resp != nil && resp.StatusCode < 500
	RecordRequest(duration, success)

	return resp, err
}

// DoLongRequestWithMetrics 执行长时间请求并记录性能指标
func DoLongRequestWithMetrics(req *http.Request) (*http.Response, error) {
	start := time.Now()
	resp, err := LongRequestClient.Do(req)
	duration := time.Since(start)

	// 记录指标
	success := err == nil && resp != nil && resp.StatusCode < 500
	RecordRequest(duration, success)

	return resp, err
}

// DoSmartRequestWithMetrics 根据请求复杂度智能选择客户端并执行请求
func DoSmartRequestWithMetrics(httpReq *http.Request, anthropicReq *types.AnthropicRequest) (*http.Response, error) {
	if anthropicReq != nil && AnalyzeRequestComplexity(*anthropicReq) == ComplexRequest {
		return DoLongRequestWithMetrics(httpReq)
	}
	return DoWithMetrics(httpReq)
}
