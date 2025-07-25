package server

import (
	"bytes"
	"fmt"
	"io"
	"net/http"

	"kiro2api/auth"
	"kiro2api/config"
	"kiro2api/converter"
	"kiro2api/logger"
	"kiro2api/types"

	"github.com/bytedance/sonic"
	"github.com/gin-gonic/gin"
)


// buildCodeWhispererRequest 构建通用的CodeWhisperer请求
func buildCodeWhispererRequest(anthropicReq types.AnthropicRequest, accessToken string, isStream bool) (*http.Request, error) {
	cwReq := converter.BuildCodeWhispererRequest(anthropicReq)
	cwReqBody, err := sonic.Marshal(cwReq)
	if err != nil {
		return nil, fmt.Errorf("序列化请求失败: %v", err)
	}

	req, err := http.NewRequest("POST", config.CodeWhispererURL, bytes.NewReader(cwReqBody))
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %v", err)
	}

	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")
	if isStream {
		req.Header.Set("Accept", "text/event-stream")
	}

	return req, nil
}

// handleCodeWhispererError 处理CodeWhisperer API错误响应
func handleCodeWhispererError(c *gin.Context, resp *http.Response) bool {
	if resp.StatusCode == http.StatusOK {
		return false
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		logger.Error("读取错误响应失败", logger.Err(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "读取响应失败"})
		return true
	}

	logger.Error("CodeWhisperer响应错误",
		logger.Int("status_code", resp.StatusCode),
		logger.String("response", string(body)))

	// 如果是403错误，清理token缓存并提示刷新
	if resp.StatusCode == http.StatusForbidden {
		logger.Warn("收到403错误，清理token缓存")
		auth.ClearTokenCache()
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Token已失效，请重试"})
		return true
	}
	
	c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("CodeWhisperer Error: %s", string(body))})
	return true
}
