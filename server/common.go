package server

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"

	"kiro2api/auth"
	"kiro2api/config"
	"kiro2api/converter"
	"kiro2api/logger"
	"kiro2api/types"
	"kiro2api/utils"

	"github.com/gin-gonic/gin"
)

// respondErrorWithCode 标准化的错误响应结构
// 统一返回: {"error": {"message": string, "code": string}}
func respondErrorWithCode(c *gin.Context, statusCode int, code string, format string, args ...interface{}) {
	c.JSON(statusCode, gin.H{
		"error": gin.H{
			"message": fmt.Sprintf(format, args...),
			"code":    code,
		},
	})
}

// respondError 简化封装，依据statusCode映射默认code
func respondError(c *gin.Context, statusCode int, format string, args ...interface{}) {
	var code string
	switch statusCode {
	case http.StatusBadRequest:
		code = "bad_request"
	case http.StatusUnauthorized:
		code = "unauthorized"
	case http.StatusForbidden:
		code = "forbidden"
	case http.StatusNotFound:
		code = "not_found"
	case http.StatusTooManyRequests:
		code = "rate_limited"
	default:
		code = "internal_error"
	}
	respondErrorWithCode(c, statusCode, code, format, args...)
}

// 通用请求处理错误函数
func handleRequestBuildError(c *gin.Context, err error) {
	logger.Error("构建请求失败", logger.Err(err))
	respondError(c, http.StatusInternalServerError, "构建请求失败: %v", err)
}

func handleRequestSendError(c *gin.Context, err error) {
	logger.Error("发送请求失败", logger.Err(err))
	respondError(c, http.StatusInternalServerError, "发送请求失败: %v", err)
}

func handleResponseReadError(c *gin.Context, err error) {
	logger.Error("读取响应体失败", logger.Err(err))
	respondError(c, http.StatusInternalServerError, "读取响应体失败: %v", err)
}

// 通用请求执行函数
func executeCodeWhispererRequest(c *gin.Context, anthropicReq types.AnthropicRequest, tokenInfo types.TokenInfo, isStream bool) (*http.Response, error) {
	req, err := buildCodeWhispererRequest(anthropicReq, tokenInfo, isStream)
	if err != nil {
		handleRequestBuildError(c, err)
		return nil, err
	}

	resp, err := utils.DoSmartRequest(req, &anthropicReq)
	if err != nil {
		handleRequestSendError(c, err)
		return nil, err
	}

	if handleCodeWhispererError(c, resp) {
		resp.Body.Close()
		return nil, fmt.Errorf("CodeWhisperer API error")
	}

	// AWS请求成功，扣减VIBE使用次数
	auth.DecrementVIBECount(tokenInfo.AccessToken)

	return resp, nil
}

// execCWRequest 供测试覆盖的请求执行入口（可在测试中替换）
var execCWRequest = executeCodeWhispererRequest

// buildCodeWhispererRequest 构建通用的CodeWhisperer请求
func buildCodeWhispererRequest(anthropicReq types.AnthropicRequest, tokenInfo types.TokenInfo, isStream bool) (*http.Request, error) {
	cwReq, err := converter.BuildCodeWhispererRequest(anthropicReq, tokenInfo.ProfileArn)
	if err != nil {
		return nil, fmt.Errorf("构建CodeWhisperer请求失败: %v", err)
	}

	cwReqBody, err := utils.SafeMarshal(cwReq)
	if err != nil {
		return nil, fmt.Errorf("序列化请求失败: %v", err)
	}

	// 临时调试：记录发送给CodeWhisperer的请求内容
	// 补充：当工具直传启用时输出工具名称预览
	var toolNamesPreview string
	if len(cwReq.ConversationState.CurrentMessage.UserInputMessage.UserInputMessageContext.Tools) > 0 {
		names := make([]string, 0, len(cwReq.ConversationState.CurrentMessage.UserInputMessage.UserInputMessageContext.Tools))
		for _, t := range cwReq.ConversationState.CurrentMessage.UserInputMessage.UserInputMessageContext.Tools {
			if t.ToolSpecification.Name != "" {
				names = append(names, t.ToolSpecification.Name)
			}
		}
		toolNamesPreview = strings.Join(names, ",")
	}

	logger.Debug("发送给CodeWhisperer的请求",
		logger.Int("request_size", len(cwReqBody)),
		logger.String("request_body", string(cwReqBody)),
		logger.Int("tools_count", len(cwReq.ConversationState.CurrentMessage.UserInputMessage.UserInputMessageContext.Tools)),
		logger.String("tools_names", toolNamesPreview))

	req, err := http.NewRequest("POST", config.CodeWhispererURL, bytes.NewReader(cwReqBody))
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %v", err)
	}

	req.Header.Set("Authorization", "Bearer "+tokenInfo.AccessToken)
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
		respondError(c, http.StatusInternalServerError, "%s", "读取响应失败")
		return true
	}

	logger.Error("CodeWhisperer响应错误",
		logger.Int("status_code", resp.StatusCode),
		logger.Int("response_len", len(body)),
		logger.String("response_body", string(body)))

	// 如果是403错误，清理token缓存并提示刷新
	if resp.StatusCode == http.StatusForbidden {
		logger.Warn("收到403错误，清理token缓存")
		auth.ClearTokenCache()
		respondErrorWithCode(c, http.StatusUnauthorized, "unauthorized", "%s", "Token已失效，请重试")
		return true
	}

	respondErrorWithCode(c, http.StatusInternalServerError, "cw_error", "CodeWhisperer Error: %s", string(body))
	return true
}

// 兜底转义：用于防止将可疑工具标签文本以原始'<'形式直出
// 仅对普通文本(text_delta)生效，不影响工具事件或参数JSON
func sanitizeSuspiciousToolText(s string) (string, bool) {
	if s == "" || !strings.Contains(s, "<") {
		return s, false
	}

	// 新增：检查是否为完整的、合法的工具调用XML块，如果是，则跳过转义
	trimmed := strings.TrimSpace(s)
	if (strings.HasPrefix(trimmed, "<tool_use>") && strings.HasSuffix(trimmed, "</tool_use>")) ||
		(strings.HasPrefix(trimmed, "<tool_result>") && strings.HasSuffix(trimmed, "</tool_result>")) {
		// 进一步验证内部结构的完整性（简单检查）
		if strings.Contains(trimmed, "</tool_name>") && strings.Contains(trimmed, "</tool_parameters>") {
			logger.Debug("检测到完整工具调用XML块，跳过转义", logger.String("block", trimmed))
			return s, false
		}
		if strings.Contains(trimmed, "<tool_use_id>") && strings.Contains(trimmed, "<content>") {
			logger.Debug("检测到完整工具结果XML块，跳过转义", logger.String("block", trimmed))
			return s, false
		}
	}

	orig := s
	replacements := [][2]string{
		// 带空格的错误格式标签 - 优先处理
		{"< tool_result>", ""},
		{"< /tool_result>", ""},
		{"< tool_use>", "＜tool_use＞"},
		{"< /tool_use>", "＜/tool_use＞"},
		{"< tool_parameter>", "＜tool_parameter＞"},
		{"< /tool_parameter>", "＜/tool_parameter＞"},
		{"< tool_name>", "＜tool_name＞"},
		{"< /tool_name>", "＜/tool_name＞"},
		{"< tool_parameters>", "＜tool_parameters＞"},
		{"< /tool_parameters>", "＜/tool_parameters＞"},
		{"< bash>", "＜bash＞"},
		{"< /bash>", "＜/bash＞"},
		{"< write>", "＜write＞"},
		{"< /write>", "＜/write＞"},
		{"< file_path>", "＜file_path＞"},
		{"< /file_path>", "＜/file_path＞"},
		{"< command>", "＜command＞"},
		{"< /command>", "＜/command＞"},
		{"< content>", "＜content＞"},
		{"< /content>", "＜/content＞"},
		// 标准格式标签（tool_result 直接移除，其余转全角）
		{"<tool_result>", ""},
		{"</tool_result>", ""},
		{"<tool_", "＜tool_"},
		{"</tool_", "＜/tool_"},
		{"<tool ", "＜tool "},
		{"</tool", "＜/tool"},
		{"<tool_use", "＜tool_use"},
		{"</tool_use>", "＜/tool_use＞"},
		{"<tool_name>", "＜tool_name＞"},
		{"</tool_name>", "＜/tool_name＞"},
		{"<tool_parameters>", "＜tool_parameters＞"},
		{"</tool_parameters>", "＜/tool_parameters＞"},
		{"<tool_parameter>", "＜tool_parameter＞"},
		{"</tool_parameter>", "＜/tool_parameter＞"},
		{"<parameters>", "＜parameters＞"},
		{"</parameters>", "＜/parameters＞"},
		{"<tool_parameter", "＜tool_parameter"},
		{"<bash>", "＜bash＞"},
		{"</bash>", "＜/bash＞"},
		{"<write>", "＜write＞"},
		{"</write>", "＜/write＞"},
		{"<file_path>", "＜file_path＞"},
		{"</file_path>", "＜/file_path＞"},
		{"<command>", "＜command＞"},
		{"</command>", "＜/command＞"},
		{"<content>", "＜content＞"},
		{"</content>", "＜/content＞"},
	}
	changed := false
	for _, p := range replacements {
		if strings.Contains(s, p[0]) {
			s = strings.ReplaceAll(s, p[0], p[1])
			changed = true
		}
	}
	// 泛化处理遗留的 <tool...> 和带空格的 < tool...> 形式
	// 但是要避免误伤正常的工具调用文本
	if strings.Contains(s, "< tool") {
		s = strings.ReplaceAll(s, "< tool", "＜tool")
		changed = true
	}
	if strings.Contains(s, "< /tool") {
		s = strings.ReplaceAll(s, "< /tool", "＜/tool")
		changed = true
	}
	// 只在特定情况下转义 <tool：
	// 1. 不是完整的工具调用标签
	// 2. 不是以 <tool_ 开头的正常标签
	// 3. 不是以 <tool_calls> 等正常格式
	if !changed && strings.Contains(s, "<tool") {
		// 检查是否是不完整的标签片段
		isFragment := !strings.Contains(s, "<tool_") &&
			!strings.Contains(s, "<tool>") &&
			!strings.Contains(s, "</tool") &&
			len(s) < 20 // 片段通常很短

		if isFragment {
			s = strings.ReplaceAll(s, "<tool", "＜tool")
			changed = true
		}
	}
	if changed {
		preview := s
		if len(preview) > 120 {
			preview = preview[:120] + "..."
		}
		logger.Debug("SSE文本兜底转义触发",
			logger.Int("orig_len", len(orig)),
			logger.Int("new_len", len(s)),
			logger.String("preview", preview))
	}
	return s, changed
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

	// 对文本增量进行兜底转义
	if dataMap, ok := data.(map[string]any); ok {
		if t, exists := dataMap["type"]; exists {
			eventType = t.(string)
		}

		// 仅对 content_block_delta 中的 text_delta 进行兜底转义
		if eventType == "content_block_delta" {
			if delta, ok := dataMap["delta"].(map[string]any); ok {
				if deltaType, _ := delta["type"].(string); deltaType == "text_delta" {
					if text, ok := delta["text"].(string); ok && text != "" {
						sanitized, changed := sanitizeSuspiciousToolText(text)
						if changed {
							delta["text"] = sanitized
							dataMap["delta"] = delta
							data = dataMap
						}
					}
				}
			}
		}
	}

	json, err := utils.SafeMarshal(data)
	if err != nil {
		return err
	}

	// 压缩日志：仅记录事件类型与负载长度
	logger.Debug("发送SSE事件",
		logger.String("event", eventType),
		logger.Int("payload_len", len(json)),
		logger.String("payload_preview", string(json)),
	)

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
	// 对OpenAI格式的文本内容进行兜底转义
	if dataMap, ok := data.(map[string]any); ok {
		if choices, ok := dataMap["choices"].([]map[string]any); ok && len(choices) > 0 {
			if delta, ok := choices[0]["delta"].(map[string]any); ok {
				// 检查是否为文本内容（而非工具调用）
				if content, ok := delta["content"].(string); ok && content != "" {
					if _, hasToolCalls := delta["tool_calls"]; !hasToolCalls {
						sanitized, changed := sanitizeSuspiciousToolText(content)
						if changed {
							delta["content"] = sanitized
							choices[0]["delta"] = delta
							dataMap["choices"] = choices
							data = dataMap
						}
					}
				}
			}
		}
	}

	json, err := utils.SafeMarshal(data)
	if err != nil {
		return err
	}

	// 压缩日志：记录负载长度
	logger.Debug("发送OpenAI SSE事件", logger.Int("payload_len", len(json)))

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
