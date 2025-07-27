package server

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"strings"
	"time"

	"kiro2api/logger"
	"kiro2api/parser"
	"kiro2api/types"
	"kiro2api/utils"

	"github.com/bytedance/sonic"
	"github.com/gin-gonic/gin"
)

// handleStreamRequest 处理流式请求
func handleStreamRequest(c *gin.Context, anthropicReq types.AnthropicRequest, accessToken string) {
	sender := &AnthropicStreamSender{}
	handleGenericStreamRequest(c, anthropicReq, accessToken, sender, createAnthropicStreamEvents)
}

// handleGenericStreamRequest 通用流式请求处理
func handleGenericStreamRequest(c *gin.Context, anthropicReq types.AnthropicRequest, accessToken string, sender StreamEventSender, eventCreator func(string, string, string) []map[string]any) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")

	messageId := fmt.Sprintf("msg_%s", time.Now().Format("20060102150405"))

	req, err := buildCodeWhispererRequest(anthropicReq, accessToken, true)
	if err != nil {
		sender.SendError(c, "构建请求失败", err)
		return
	}

	resp, err := utils.DoSmartRequestWithMetrics(req, &anthropicReq)
	if err != nil {
		sender.SendError(c, "CodeWhisperer request error", fmt.Errorf("request error: %s", err.Error()))
		return
	}
	defer resp.Body.Close()

	if handleCodeWhispererError(c, resp) {
		return
	}

	// 立即刷新响应头
	c.Writer.Flush()

	// 发送初始事件
	inputContent, _ := utils.GetMessageContent(anthropicReq.Messages[0].Content)
	initialEvents := eventCreator(messageId, inputContent, anthropicReq.Model)
	for _, event := range initialEvents {
		sender.SendEvent(c, event)
	}

	// 创建流式解析器并处理响应
	streamParser := parser.NewStreamParser()
	outputTokens := 0

	buf := make([]byte, 1024)
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			events := streamParser.ParseStream(buf[:n])
			for _, event := range events {
				sender.SendEvent(c, event.Data)

				if event.Event == "content_block_delta" {
					content, _ := utils.GetMessageContent(event.Data)
					outputTokens = len(content)
				}
				c.Writer.Flush()
			}
		}
		if err != nil {
			break
		}
	}

	// 发送结束事件
	finalEvents := createAnthropicFinalEvents(outputTokens)
	for _, event := range finalEvents {
		sender.SendEvent(c, event)
	}
}

// createAnthropicStreamEvents 创建Anthropic流式初始事件
func createAnthropicStreamEvents(messageId, inputContent, model string) []map[string]any {
	events := []map[string]any{
		{
			"type": "message_start",
			"message": map[string]any{
				"id":            messageId,
				"type":          "message",
				"role":          "assistant",
				"content":       []any{},
				"model":         model,
				"stop_reason":   nil,
				"stop_sequence": nil,
				"usage": map[string]any{
					"input_tokens":  len(inputContent),
					"output_tokens": 1,
				},
			},
		},
		{
			"type": "ping",
		},
		{
			"content_block": map[string]any{
				"text": "",
				"type": "text"},
			"index": 0,
			"type":  "content_block_start",
		},
	}
	return events
}

// createAnthropicFinalEvents 创建Anthropic流式结束事件
func createAnthropicFinalEvents(outputTokens int) []map[string]any {
	return []map[string]any{
		{
			"index": 0,
			"type":  "content_block_stop",
		},
		{
			"type": "message_delta",
			"delta": map[string]any{
				"stop_reason":   "end_turn",
				"stop_sequence": nil,
			},
			"usage": map[string]any{
				"output_tokens": outputTokens,
			},
		},
		{
			"type": "message_stop",
		},
	}
}

// handleNonStreamRequest 处理非流式请求
func handleNonStreamRequest(c *gin.Context, anthropicReq types.AnthropicRequest, accessToken string) {
	req, err := buildCodeWhispererRequest(anthropicReq, accessToken, false)
	if err != nil {
		logger.Error("构建请求失败", logger.Err(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("构建请求失败: %v", err)})
		return
	}

	resp, err := utils.DoSmartRequestWithMetrics(req, &anthropicReq)
	if err != nil {
		logger.Error("发送请求失败", logger.Err(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("发送请求失败: %v", err)})
		return
	}
	defer resp.Body.Close()

	if handleCodeWhispererError(c, resp) {
		return
	}

	// 读取响应体
	body, err := utils.ReadHTTPResponse(resp.Body)
	if err != nil {
		logger.Error("读取响应体失败", logger.Err(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("读取响应体失败: %v", err)})
		return
	}

	respBodyStr := string(body)
	events := parser.ParseEvents(body)

	context := ""
	toolName := ""
	toolUseId := ""
	contexts := []map[string]any{}
	partialJsonStr := ""

	for _, event := range events {
		if event.Data != nil {
			if dataMap, ok := event.Data.(map[string]any); ok {
				switch dataMap["type"] {
				case "content_block_start":
					context = ""
					partialJsonStr = ""
					toolUseId = ""
					toolName = ""
				case "content_block_delta":
					if delta, ok := dataMap["delta"]; ok {
						if deltaMap, ok := delta.(map[string]any); ok {
							switch deltaMap["type"] {
							case "text_delta":
								if text, ok := deltaMap["text"]; ok {
									context += text.(string)
								}
							case "input_json_delta":
								// 安全地获取tool_use_id和name，防止nil panic
								if id, ok := deltaMap["id"]; ok && id != nil {
									if idStr, ok := id.(string); ok {
										toolUseId = idStr
									}
								}
								if name, ok := deltaMap["name"]; ok && name != nil {
									if nameStr, ok := name.(string); ok {
										toolName = nameStr
									}
								}
								if partial_json, ok := deltaMap["partial_json"]; ok {
									if strPtr, ok := partial_json.(*string); ok && strPtr != nil {
										partialJsonStr = partialJsonStr + *strPtr
										logger.Debug("接收到partial_json片段(指针)",
											logger.String("fragment", *strPtr),
											logger.Int("total_length", len(partialJsonStr)))
									} else if str, ok := partial_json.(string); ok {
										partialJsonStr = partialJsonStr + str
										logger.Debug("接收到partial_json片段(字符串)",
											logger.String("fragment", str),
											logger.Int("total_length", len(partialJsonStr)))
									} else {
										logger.Debug("partial_json类型错误",
											logger.String("type", fmt.Sprintf("%T", partial_json)),
											logger.Any("value", partial_json))
									}
								} else {
									logger.Debug("工具delta中未找到partial_json字段",
										logger.String("tool_name", toolName),
										logger.String("tool_use_id", toolUseId))
								}
							}
						}
					}
				case "content_block_stop":
					if index, ok := dataMap["index"]; ok {
						switch index {
						case 1:
							if partialJsonStr == "" {
								logger.Debug("工具调用没有参数数据，跳过",
									logger.String("tool_name", toolName),
									logger.String("tool_use_id", toolUseId))
								break
							}
							logger.Debug("Attempting to parse tool call JSON", logger.String("json_data", partialJsonStr))
							toolInput := map[string]any{}
							if err := sonic.Unmarshal([]byte(partialJsonStr), &toolInput); err != nil {
								logger.Error("JSON解析失败",
									logger.String("tool_name", toolName),
									logger.String("tool_use_id", toolUseId),
									logger.Err(err),
									logger.String("data", partialJsonStr))
								break
							}
							if len(toolInput) == 0 {
								logger.Debug("工具参数为空",
									logger.String("tool_name", toolName),
									logger.String("tool_use_id", toolUseId))
							}
							contexts = append(contexts, map[string]any{
								"type":  "tool_use",
								"id":    toolUseId,
								"name":  toolName,
								"input": toolInput,
							})
						case 0:
							contexts = append(contexts, map[string]any{
								"text": context,
								"type": "text",
							})
						}
					}
				}
			}
		}
	}
	if strings.Contains(string(body), "Improperly formed request.") {
		// 增强错误日志记录
		reqBodyBytes, _ := sonic.Marshal(anthropicReq)
		hash := sha256.Sum256(reqBodyBytes)
		logger.Error("CodeWhisperer返回格式错误",
			logger.String("response", respBodyStr),
			logger.Int("request_len", len(reqBodyBytes)),
			logger.String("request_sha256", fmt.Sprintf("%x", hash)),
			logger.Bool("stream", anthropicReq.Stream),
			logger.Int("tools_count", len(anthropicReq.Tools)))
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("请求格式错误: %s", respBodyStr)})
		return
	}
	inputContent, _ := utils.GetMessageContent(anthropicReq.Messages[0].Content)
	anthropicResp := map[string]any{
		"content":       contexts,
		"model":         anthropicReq.Model,
		"role":          "assistant",
		"stop_reason":   "end_turn",
		"stop_sequence": nil,
		"type":          "message",
		"usage": map[string]any{
			"input_tokens":  len(inputContent),
			"output_tokens": len(context),
		},
	}

	c.JSON(http.StatusOK, anthropicResp)
}
