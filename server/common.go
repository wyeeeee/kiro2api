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
	"kiro2api/utils"

	"github.com/gin-gonic/gin"
)

// 统一错误响应函数
func respondWithError(c *gin.Context, statusCode int, message string) {
	c.JSON(statusCode, gin.H{"error": message})
}

func respondWithErrorf(c *gin.Context, statusCode int, format string, args ...interface{}) {
	message := fmt.Sprintf(format, args...)
	c.JSON(statusCode, gin.H{"error": message})
}

// 常用错误响应快捷函数
func respondBadRequest(c *gin.Context, message string) {
	respondWithError(c, http.StatusBadRequest, message)
}

func respondBadRequestf(c *gin.Context, format string, args ...interface{}) {
	respondWithErrorf(c, http.StatusBadRequest, format, args...)
}

func respondInternalServerErrorf(c *gin.Context, format string, args ...interface{}) {
	respondWithErrorf(c, http.StatusInternalServerError, format, args...)
}

func respondNotFound(c *gin.Context, message string) {
	respondWithError(c, http.StatusNotFound, message)
}

// buildCodeWhispererRequest 构建通用的CodeWhisperer请求
func buildCodeWhispererRequest(anthropicReq types.AnthropicRequest, accessToken string, isStream bool) (*http.Request, error) {
	cwReq, err := converter.BuildCodeWhispererRequest(anthropicReq)
	if err != nil {
		return nil, fmt.Errorf("构建CodeWhisperer请求失败: %v", err)
	}

	cwReqBody, err := utils.SafeMarshal(cwReq)
	if err != nil {
		return nil, fmt.Errorf("序列化请求失败: %v", err)
	}

	// 临时调试：记录发送给CodeWhisperer的请求内容
	logger.Debug("发送给CodeWhisperer的请求",
		logger.String("request_body", string(cwReqBody)),
		logger.Int("request_size", len(cwReqBody)))

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

// StreamEventSender 统一的流事件发送接口
type StreamEventSender interface {
	SendEvent(c *gin.Context, data any) error
	SendError(c *gin.Context, message string, err error) error
}

// AnthropicStreamSender Anthropic格式的流事件发送器
type AnthropicStreamSender struct{}

func (s *AnthropicStreamSender) SendEvent(c *gin.Context, data any) error {
	var eventType string
	if dataMap, ok := data.(map[string]any); ok {
		if t, exists := dataMap["type"]; exists {
			eventType = t.(string)
		}
	}

	json, err := utils.SafeMarshal(data)
	if err != nil {
		return err
	}

	logger.Debug("发送SSE事件",
		logger.String("event", eventType),
		logger.String("data", string(json)))

	fmt.Fprintf(c.Writer, "event: %s\n", eventType)
	fmt.Fprintf(c.Writer, "data: %s\n\n", string(json))
	c.Writer.Flush()
	return nil
}

func (s *AnthropicStreamSender) SendError(c *gin.Context, message string, _ error) error {
	errorResp := map[string]any{
		"type": "error",
		"error": map[string]any{
			"type":    "overloaded_error",
			"message": message,
		},
	}
	return s.SendEvent(c, errorResp)
}

// OpenAIStreamSender OpenAI格式的流事件发送器
type OpenAIStreamSender struct{}

func (s *OpenAIStreamSender) SendEvent(c *gin.Context, data any) error {
	json, err := utils.SafeMarshal(data)
	if err != nil {
		return err
	}

	fmt.Fprintf(c.Writer, "data: %s\n\n", string(json))
	c.Writer.Flush()
	return nil
}

func (s *OpenAIStreamSender) SendError(c *gin.Context, message string, _ error) error {
	errorResp := map[string]any{
		"error": map[string]any{
			"message": message,
			"type":    "server_error",
			"code":    "internal_error",
		},
	}

	json, err := utils.FastMarshal(errorResp)
	if err != nil {
		return err
	}

	fmt.Fprintf(c.Writer, "data: %s\n\n", string(json))
	c.Writer.Flush()
	return nil
}
