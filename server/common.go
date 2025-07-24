package server

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"

	"kiro2api/auth"
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

	if resp.StatusCode == 403 {
		logger.Warn("Token过期，正在刷新")
		if err := auth.RefreshTokenForServer(); err != nil {
			logger.Error("Token刷新失败", logger.Err(err))
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Token刷新失败: %v", err)})
		} else {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "CodeWhisperer Token 已刷新，请重试"})
		}
	} else {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("CodeWhisperer Error: %s", string(body))})
	}
	return true
}

// validateAPIKey 验证API密钥
func validateAPIKey(c *gin.Context, authToken string) bool {
	providedApiKey := c.GetHeader("Authorization")
	if providedApiKey == "" {
		providedApiKey = c.GetHeader("x-api-key")
	} else {
		providedApiKey = strings.TrimPrefix(providedApiKey, "Bearer ")
	}

	if providedApiKey == "" {
		logger.Warn("请求缺少Authorization或x-api-key头")
		c.JSON(http.StatusUnauthorized, gin.H{"error": "401"})
		return false
	}

	if providedApiKey != authToken {
		logger.Error("authToken验证失败",
			logger.String("expected", "***"),
			logger.String("provided", "***"))
		c.JSON(http.StatusUnauthorized, gin.H{"error": "401"})
		return false
	}

	return true
}

// getMessageContent 从消息中提取文本内容的辅助函数
func getMessageContent(content any) string {
	switch v := content.(type) {
	case string:
		if len(v) == 0 {
			return "answer for user question"
		}
		return v
	case []any:
		var texts []string
		for _, block := range v {
			if m, ok := block.(map[string]any); ok {
				var cb types.ContentBlock
				if data, err := sonic.Marshal(m); err == nil {
					if err := sonic.Unmarshal(data, &cb); err == nil {
						switch cb.Type {
						case "tool_result":
							texts = append(texts, *cb.Content)
						case "text":
							texts = append(texts, *cb.Text)
						}
					}
				}
			}
		}
		if len(texts) == 0 {
			return "answer for user question"
		}
		return strings.Join(texts, "\n")
	default:
		return "answer for user question"
	}
}
