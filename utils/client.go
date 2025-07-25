package utils

import (
	"net/http"
	"time"
)

var (
	// SharedHTTPClient 共享的HTTP客户端实例
	SharedHTTPClient *http.Client
)

func init() {
	// 初始化共享的HTTP客户端，设置合理的超时时间
	SharedHTTPClient = &http.Client{
		Timeout: 60 * time.Second,
	}
}
