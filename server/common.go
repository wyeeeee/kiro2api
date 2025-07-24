package server

import (
	"fmt"
	"strings"

	"kiro2api/auth"
	"kiro2api/converter"
	"kiro2api/logger"
	"kiro2api/types"

	"github.com/bytedance/sonic"
	"github.com/valyala/fasthttp"
)

const codeWhispererURL = "https://codewhisperer.us-east-1.amazonaws.com/generateAssistantResponse"

// buildCodeWhispererRequest 构建通用的CodeWhisperer请求
func buildCodeWhispererRequest(anthropicReq types.AnthropicRequest, accessToken string, isStream bool) (*fasthttp.Request, error) {
	cwReq := converter.BuildCodeWhispererRequest(anthropicReq)
	cwReqBody, err := sonic.Marshal(cwReq)
	if err != nil {
		return nil, fmt.Errorf("序列化请求失败: %v", err)
	}

	req := fasthttp.AcquireRequest()
	req.SetRequestURI(codeWhispererURL)
	req.Header.SetMethod(fasthttp.MethodPost)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.SetContentType("application/json")
	if isStream {
		req.Header.Set("Accept", "text/event-stream")
	}
	req.SetBody(cwReqBody)

	return req, nil
}

// handleCodeWhispererError 处理CodeWhisperer API错误响应
func handleCodeWhispererError(ctx *fasthttp.RequestCtx, resp *fasthttp.Response) bool {
	if resp.StatusCode() == fasthttp.StatusOK {
		return false
	}

	body := resp.Body()
	logger.Error("CodeWhisperer响应错误",
		logger.Int("status_code", resp.StatusCode()),
		logger.String("response", string(body)))

	if resp.StatusCode() == 403 {
		logger.Warn("Token过期，正在刷新")
		auth.RefreshToken()
		ctx.Error("CodeWhisperer Token 已刷新，请重试", fasthttp.StatusUnauthorized)
	} else {
		ctx.Error(fmt.Sprintf("CodeWhisperer Error: %s", string(body)), fasthttp.StatusInternalServerError)
	}
	return true
}

// validateAPIKey 验证API密钥
func validateAPIKey(ctx *fasthttp.RequestCtx, authToken string) bool {
	providedApiKey := string(ctx.Request.Header.Peek("Authorization"))
	if providedApiKey == "" {
		providedApiKey = string(ctx.Request.Header.Peek("x-api-key"))
	} else {
		providedApiKey = strings.TrimPrefix(providedApiKey, "Bearer ")
	}

	if providedApiKey == "" {
		logger.Warn("请求缺少Authorization或x-api-key头")
		ctx.Error("401", fasthttp.StatusUnauthorized)
		return false
	}

	if providedApiKey != authToken {
		logger.Error("authToken验证失败",
			logger.String("expected", "***"),
			logger.String("provided", "***"))
		ctx.Error("401", fasthttp.StatusUnauthorized)
		return false
	}

	return true
}

// setCORSHeaders 设置CORS头
func setCORSHeaders(ctx *fasthttp.RequestCtx) {
	ctx.Response.Header.Set("Access-Control-Allow-Origin", "*")
	ctx.Response.Header.Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	ctx.Response.Header.Set("Access-Control-Allow-Headers", "Content-Type, Authorization, x-api-key")
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
