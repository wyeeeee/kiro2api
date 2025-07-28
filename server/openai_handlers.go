package server

import (
	"fmt"
	"net/http"
	"time"

	"kiro2api/converter"
	"kiro2api/logger"
	"kiro2api/parser"
	"kiro2api/types"
	"kiro2api/utils"

	"github.com/bytedance/sonic"
	"github.com/gin-gonic/gin"
)

// handleOpenAINonStreamRequest 处理OpenAI非流式请求
func handleOpenAINonStreamRequest(c *gin.Context, anthropicReq types.AnthropicRequest, accessToken string) {
	req, err := buildCodeWhispererRequest(anthropicReq, accessToken, false)
	if err != nil {
		logger.Error("构建请求失败", logger.Err(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("构建请求失败: %v", err)})
		return
	}

	resp, err := utils.DoSmartRequest(req, &anthropicReq)
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

	events := parser.ParseEvents(body)

	allContent := "" // 累积所有文本内容
	contexts := []map[string]any{}
	currentToolUse := make(map[string]any)
	toolInputBuffer := ""
	currentBlockContent := "" // 当前块的文本内容
	dedupManager := utils.NewToolDedupManager() // OpenAI端点的工具去重管理器

	for _, event := range events {
		if event.Data != nil {
			if dataMap, ok := event.Data.(map[string]any); ok {
				switch dataMap["type"] {
				case "content_block_delta":
					if delta, ok := dataMap["delta"]; ok {
						if deltaMap, ok := delta.(map[string]any); ok {
							switch deltaMap["type"] {
							case "text_delta":
								if text, ok := deltaMap["text"]; ok {
									textStr := text.(string)
									currentBlockContent += textStr
									allContent += textStr
								}
							case "input_json_delta":
								if partialJson, ok := deltaMap["partial_json"]; ok {
									switch v := partialJson.(type) {
									case string:
										toolInputBuffer += v
									case *string:
										if v != nil {
											toolInputBuffer += *v
										}
									}
								}
							}
						}
					}
				case "content_block_start":
					if contentBlock, ok := dataMap["content_block"]; ok {
						if blockMap, ok := contentBlock.(map[string]any); ok {
							switch blockMap["type"] {
							case "tool_use":
								currentToolUse = map[string]any{
									"type": "tool_use",
									"id":   blockMap["id"],
									"name": blockMap["name"],
								}
								toolInputBuffer = ""
							case "text":
								currentBlockContent = "" // 重置当前块内容
							}
						}
					}
				case "content_block_stop":
					if currentBlockContent != "" {
						contexts = append(contexts, map[string]any{
							"text": currentBlockContent,
							"type": "text",
						})
						currentBlockContent = ""
					}
					// 完成工具使用块
					if len(currentToolUse) > 0 {
						// 解析完整的工具参数
						if toolInputBuffer != "" {
							toolInput := map[string]any{}
							if err := sonic.Unmarshal([]byte(toolInputBuffer), &toolInput); err != nil {
								logger.Error("JSON解析失败", logger.Err(err), logger.String("data", toolInputBuffer))
								break
							}
							currentToolUse["input"] = toolInput
						}

						// 基于 tool_use_id 的工具去重检查
						var currentToolUseId string
						if id, hasId := currentToolUse["id"].(string); hasId {
							currentToolUseId = id
						}

						if currentToolUseId != "" {
							// 检查工具是否已被处理（基于 tool_use_id）
							if dedupManager.IsToolProcessed(currentToolUseId) {
								// 重置工具状态但不添加到contexts
								currentToolUse = make(map[string]any)
								toolInputBuffer = ""
								break // 跳过重复工具
							}
							// 标记工具为已处理
							dedupManager.MarkToolProcessed(currentToolUseId)
						}

						contexts = append(contexts, currentToolUse)
						currentToolUse = make(map[string]any)
						toolInputBuffer = ""
					}
				}
			}
		}
	}

	// 处理剩余的文本内容（如果事件流没有明确的content_block_stop）
	if currentBlockContent != "" {
		contexts = append(contexts, map[string]any{
			"text": currentBlockContent,
			"type": "text",
		})
	}

	// 构建Anthropic响应
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
			"output_tokens": len(allContent),
		},
	}

	// 转换为OpenAI格式
	openaiMessageId := fmt.Sprintf("chatcmpl-%s", time.Now().Format("20060102150405"))
	openaiResp := converter.ConvertAnthropicToOpenAI(anthropicResp, anthropicReq.Model, openaiMessageId)

	c.JSON(http.StatusOK, openaiResp)
}

// handleOpenAIStreamRequest 处理OpenAI流式请求
func handleOpenAIStreamRequest(c *gin.Context, anthropicReq types.AnthropicRequest, accessToken string) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")

	messageId := fmt.Sprintf("chatcmpl-%s", time.Now().Format("20060102150405"))

	req, err := buildCodeWhispererRequest(anthropicReq, accessToken, true)
	if err != nil {
		logger.Error("构建请求失败", logger.Err(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("构建请求失败: %v", err)})
		return
	}

	resp, err := utils.DoSmartRequest(req, &anthropicReq)
	if err != nil {
		logger.Error("发送请求失败", logger.Err(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("发送请求失败: %v", err)})
		return
	}
	defer resp.Body.Close()

	if handleCodeWhispererError(c, resp) {
		return
	}

	// 立即刷新响应头
	c.Writer.Flush()

	// 发送初始OpenAI事件
	initialEvent := map[string]any{
		"id":      messageId,
		"object":  "chat.completion.chunk",
		"created": time.Now().Unix(),
		"model":   anthropicReq.Model,
		"choices": []map[string]any{
			{
				"index": 0,
				"delta": map[string]any{
					"role": "assistant",
				},
				"finish_reason": nil,
			},
		},
	}
	sendOpenAIEvent(c, initialEvent)

	// 创建流式解析器和去重管理器
	streamParser := parser.NewStreamParser()
	dedupManager := utils.NewToolDedupManager() // OpenAI流式端点的工具去重管理器

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

				if event.Data != nil {
					if dataMap, ok := event.Data.(map[string]any); ok {
						switch dataMap["type"] {
						case "content_block_delta":
							if delta, ok := dataMap["delta"]; ok {
								if deltaMap, ok := delta.(map[string]any); ok {
									if deltaMap["type"] == "text_delta" {
										if text, ok := deltaMap["text"]; ok {
											// 发送文本内容的增量
											contentEvent := map[string]any{
												"id":      messageId,
												"object":  "chat.completion.chunk",
												"created": time.Now().Unix(),
												"model":   anthropicReq.Model,
												"choices": []map[string]any{
													{
														"index": 0,
														"delta": map[string]any{
															"content": text.(string),
														},
														"finish_reason": nil,
													},
												},
											}
											sendOpenAIEvent(c, contentEvent)
										}
									}
								}
							}
						case "content_block_stop":
							// 发送结束事件
							endEvent := map[string]any{
								"id":      messageId,
								"object":  "chat.completion.chunk",
								"created": time.Now().Unix(),
								"model":   anthropicReq.Model,
								"choices": []map[string]any{
									{
										"index":         0,
										"delta":         map[string]any{},
										"finish_reason": "stop",
									},
								},
							}
							sendOpenAIEvent(c, endEvent)
						}
					}
				}
				c.Writer.Flush()
			}
		}
		if err != nil {
			break
		}
	}

	// 发送结束标记
	fmt.Fprintf(c.Writer, "data: [DONE]\n\n")
	c.Writer.Flush()
}

func sendOpenAIEvent(c *gin.Context, data any) {
	json, err := sonic.Marshal(data)
	if err != nil {
		logger.Error("序列化OpenAI事件失败", logger.Err(err))
		return
	}

	fmt.Fprintf(c.Writer, "data: %s\n\n", string(json))
}
