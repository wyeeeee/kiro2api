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

	"github.com/gin-gonic/gin"
)

// shouldSkipDuplicateToolEvent 检查是否应该跳过重复的工具事件
// 使用基于 tool_use_id 的去重逻辑，符合 Anthropic 标准
func shouldSkipDuplicateToolEvent(event parser.SSEEvent, dedupManager *utils.ToolDedupManager) bool {
	if event.Event != "content_block_start" {
		return false
	}

	if dataMap, ok := event.Data.(map[string]any); ok {
		if contentBlock, exists := dataMap["content_block"]; exists {
			if blockMap, ok := contentBlock.(map[string]any); ok {
				if blockType, ok := blockMap["type"].(string); ok && blockType == "tool_use" {
					// 提取 tool_use_id
					if toolUseId, hasId := blockMap["id"].(string); hasId && toolUseId != "" {
						// 检查工具是否已被处理（基于 tool_use_id）
						if dedupManager.IsToolProcessed(toolUseId) {
							return true // 跳过重复的工具使用
						}
						// 标记工具为已处理
						dedupManager.MarkToolProcessed(toolUseId)
					}
				}
			}
		}
	}

	return false
}

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

	resp, err := executeCodeWhispererRequest(c, anthropicReq, accessToken, true)
	if err != nil {
		sender.SendError(c, "构建请求失败", err)
		return
	}
	defer resp.Body.Close()

	// 立即刷新响应头
	c.Writer.Flush()

	// 发送初始事件，处理包含图片的消息内容
	inputContent := ""
	if len(anthropicReq.Messages) > 0 {
		inputContent, _ = utils.GetMessageContent(anthropicReq.Messages[len(anthropicReq.Messages)-1].Content)
	}
	initialEvents := eventCreator(messageId, inputContent, anthropicReq.Model)
	for _, event := range initialEvents {
		sender.SendEvent(c, event)
	}

	// 从对象池获取流式解析器，处理完后放回池中
	streamParser := parser.GlobalStreamParserPool.Get()
	defer parser.GlobalStreamParserPool.Put(streamParser)

	outputTokens := 0
	dedupManager := utils.NewToolDedupManager() // 请求级别的工具去重管理器

	buf := make([]byte, 1024)
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			events := streamParser.ParseStream(buf[:n])
			for _, event := range events {
				// 在流式处理中添加工具去重逻辑
				if shouldSkipDuplicateToolEvent(event, dedupManager) {
					continue
				}

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
	resp, err := executeCodeWhispererRequest(c, anthropicReq, accessToken, false)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	// 读取响应体
	body, err := utils.ReadHTTPResponse(resp.Body)
	if err != nil {
		handleResponseReadError(c, err)
		return
	}

	respBodyStr := string(body)
	events := parser.ParseEvents(body)

	context := ""
	toolName := ""
	toolUseId := ""
	contexts := []map[string]any{}
	partialJsonStr := ""
	currentToolUse := make(map[string]any)      // 添加当前工具使用跟踪
	dedupManager := utils.NewToolDedupManager() // 请求级别的工具去重管理器

	for _, event := range events {
		if event.Data != nil {
			if dataMap, ok := event.Data.(map[string]any); ok {
				switch dataMap["type"] {
				case "content_block_start":
					context = ""
					partialJsonStr = ""
					toolUseId = ""
					toolName = ""

					// 提取tool_use信息从content_block_start事件
					if contentBlock, ok := dataMap["content_block"]; ok {
						if blockMap, ok := contentBlock.(map[string]any); ok {
							if blockType, ok := blockMap["type"].(string); ok && blockType == "tool_use" {
								// 直接使用content_block中的完整工具信息
								currentToolUse = map[string]any{
									"type":  "tool_use",
									"id":    blockMap["id"],
									"name":  blockMap["name"],
									"input": blockMap["input"], // 可能为空，后续会更新
								}
								// 安全地提取tool信息到局部变量（用于日志）
								if id, ok := blockMap["id"]; ok && id != nil {
									if idStr, ok := id.(string); ok {
										toolUseId = idStr
									}
								}
								if name, ok := blockMap["name"]; ok && name != nil {
									if nameStr, ok := name.(string); ok {
										toolName = nameStr
									}
								}
								partialJsonStr = "" // 重置参数累积
							}
						}
					}
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
									} else if str, ok := partial_json.(string); ok {
										partialJsonStr = partialJsonStr + str
									}
								}
							}
						}
					}
				case "content_block_stop":
					if index, ok := dataMap["index"]; ok {
						switch index {
						case 1:
							// 处理工具使用块完成
							if len(currentToolUse) > 0 {
								// 如果有累积的参数数据，解析并更新工具输入
								if partialJsonStr != "" {
									toolInput := map[string]any{}
									if err := utils.SafeUnmarshal([]byte(partialJsonStr), &toolInput); err != nil {
										logger.Error("JSON解析失败",
											logger.String("tool_name", toolName),
											logger.String("tool_use_id", toolUseId),
											logger.Err(err),
											logger.String("data", partialJsonStr))
									} else {
										currentToolUse["input"] = toolInput
									}
								} else {
									// 确保input字段存在，即使为空
									if _, hasInput := currentToolUse["input"]; !hasInput {
										currentToolUse["input"] = map[string]any{}
									}
								}

								// 使用通用工具去重处理
								if processToolDeduplication(currentToolUse, dedupManager) {
									// 重置工具状态但不添加到contexts
									currentToolUse = make(map[string]any)
									partialJsonStr = ""
									toolUseId = ""
									toolName = ""
									break // 跳过重复工具
								}

								// 添加完整的工具使用块到contexts
								contexts = append(contexts, currentToolUse)

								// 重置工具状态
								currentToolUse = make(map[string]any)
								partialJsonStr = ""
								toolUseId = ""
								toolName = ""
							}
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
		reqBodyBytes, _ := utils.SafeMarshal(anthropicReq)
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
	inputContent := ""
	if len(anthropicReq.Messages) > 0 {
		inputContent, _ = utils.GetMessageContent(anthropicReq.Messages[len(anthropicReq.Messages)-1].Content)
	}
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
