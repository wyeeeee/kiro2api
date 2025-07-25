package server

import (
	"bytes"
	"fmt"
	"io"
	"net/http"

	"kiro2api/converter"
	"kiro2api/logger"
	"kiro2api/types"

	"github.com/bytedance/sonic"
	"github.com/gin-gonic/gin"
)

const codeWhispererURL = "https://codewhisperer.us-east-1.amazonaws.com/generateAssistantResponse"

// buildCodeWhispererRequest 构建通用的CodeWhisperer请求
func buildCodeWhispererRequest(anthropicReq types.AnthropicRequest, accessToken string, isStream bool) (*http.Request, error) {
	cwReq := converter.BuildCodeWhispererRequest(anthropicReq)
	cwReqBody, err := sonic.Marshal(cwReq)
	if err != nil {
		return nil, fmt.Errorf("序列化请求失败: %v", err)
	}

	req, err := http.NewRequest("POST", codeWhispererURL, bytes.NewReader(cwReqBody))
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

	// 由于GetToken已经刷新了token，403错误在这里不应该发生
	// 如果仍然发生403，说明刷新后的token也无效
	c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("CodeWhisperer Error: %s", string(body))})
	return true
}
