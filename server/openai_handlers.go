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

	"github.com/gin-gonic/gin"
)

// handleOpenAINonStreamRequest 处理OpenAI非流式请求
func handleOpenAINonStreamRequest(c *gin.Context, anthropicReq types.AnthropicRequest, accessToken string) {
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

	events := parser.ParseEvents(body)

	allContent := "" // 累积所有文本内容
	contexts := []map[string]any{}
	currentToolUse := make(map[string]any)
	toolInputBuffer := ""
	currentBlockContent := ""                   // 当前块的文本内容
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
							if err := utils.SafeUnmarshal([]byte(toolInputBuffer), &toolInput); err != nil {
								logger.Error("JSON解析失败", logger.Err(err), logger.String("data", toolInputBuffer))
								break
							}
							currentToolUse["input"] = toolInput
						}

						// 使用通用工具去重处理
						if processToolDeduplication(currentToolUse, dedupManager) {
							// 重置工具状态但不添加到contexts
							currentToolUse = make(map[string]any)
							toolInputBuffer = ""
							break // 跳过重复工具
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
	stopReason := "end_turn"
	for _, blk := range contexts {
		if t, ok := blk["type"].(string); ok && t == "tool_use" {
			stopReason = "tool_use"
			break
		}
	}
	anthropicResp := map[string]any{
		"content":       contexts,
		"model":         anthropicReq.Model,
		"role":          "assistant",
		"stop_reason":   stopReason,
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

	resp, err := executeCodeWhispererRequest(c, anthropicReq, accessToken, true)
	if err != nil {
		return
	}
	defer resp.Body.Close()

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

	// OpenAI 工具调用增量状态
	toolIndexByToolUseId := make(map[string]int)  // tool_use_id -> tool_calls 数组索引
	toolUseIdByBlockIndex := make(map[int]string) // 内容块 index -> tool_use_id
	nextToolIndex := 0
	sawToolUse := false
	sentFinal := false

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
									} else if deltaMap["type"] == "input_json_delta" {
										// 工具调用参数增量
										// 找到对应的tool_use和OpenAI tool_calls索引
										toolBlockIndex := 0
										if idxAny, ok := dataMap["index"]; ok {
											switch v := idxAny.(type) {
											case int:
												toolBlockIndex = v
											case int32:
												toolBlockIndex = int(v)
											case int64:
												toolBlockIndex = int(v)
											case float64:
												toolBlockIndex = int(v)
											}
										}
										if toolUseId, ok := toolUseIdByBlockIndex[toolBlockIndex]; ok {
											if toolIdx, ok := toolIndexByToolUseId[toolUseId]; ok {
												var partial string
												if pj, ok := deltaMap["partial_json"]; ok {
													switch s := pj.(type) {
													case string:
														partial = s
													case *string:
														if s != nil {
															partial = *s
														}
													}
												}
												if partial != "" {
													toolDelta := map[string]any{
														"id":      messageId,
														"object":  "chat.completion.chunk",
														"created": time.Now().Unix(),
														"model":   anthropicReq.Model,
														"choices": []map[string]any{
															{
																"index": 0,
																"delta": map[string]any{
																	"tool_calls": []map[string]any{
																		{
																			"index": toolIdx,
																			"type":  "function",
																			"function": map[string]any{
																				"arguments": partial,
																			},
																		},
																	},
																},
																"finish_reason": nil,
															},
														},
													}
													sendOpenAIEvent(c, toolDelta)
												}
											}
										}
									}
								}
							}
						case "content_block_start":
							if contentBlock, ok := dataMap["content_block"]; ok {
								if blockMap, ok := contentBlock.(map[string]any); ok {
									if blockType, _ := blockMap["type"].(string); blockType == "tool_use" {
										toolUseId, _ := blockMap["id"].(string)
										toolName, _ := blockMap["name"].(string)
										// 获取内容块索引
										toolBlockIndex := 0
										if idxAny, ok := dataMap["index"]; ok {
											switch v := idxAny.(type) {
											case int:
												toolBlockIndex = v
											case int32:
												toolBlockIndex = int(v)
											case int64:
												toolBlockIndex = int(v)
											case float64:
												toolBlockIndex = int(v)
											}
										}
										if toolUseId != "" {
											if _, exists := toolIndexByToolUseId[toolUseId]; !exists {
												toolIndexByToolUseId[toolUseId] = nextToolIndex
												nextToolIndex++
											}
											toolUseIdByBlockIndex[toolBlockIndex] = toolUseId
											sawToolUse = true
											toolIdx := toolIndexByToolUseId[toolUseId]
											// 发送OpenAI工具调用开始增量
											toolStart := map[string]any{
												"id":      messageId,
												"object":  "chat.completion.chunk",
												"created": time.Now().Unix(),
												"model":   anthropicReq.Model,
												"choices": []map[string]any{
													{
														"index": 0,
														"delta": map[string]any{
															"tool_calls": []map[string]any{
																{
																	"index": toolIdx,
																	"id":    toolUseId,
																	"type":  "function",
																	"function": map[string]any{
																		"name":      toolName,
																		"arguments": "",
																	},
																},
															},
														},
														"finish_reason": nil,
													},
												},
											}
											sendOpenAIEvent(c, toolStart)
										}
									}
								}
							}
						case "message_delta":
							// 将Claude的tool_use结束映射为OpenAI的finish_reason=tool_calls
							if sawToolUse && !sentFinal {
								if delta, ok := dataMap["delta"].(map[string]any); ok {
									if sr, ok := delta["stop_reason"].(string); ok && sr == "tool_use" {
										endEvent := map[string]any{
											"id":      messageId,
											"object":  "chat.completion.chunk",
											"created": time.Now().Unix(),
											"model":   anthropicReq.Model,
											"choices": []map[string]any{
												{
													"index":         0,
													"delta":         map[string]any{},
													"finish_reason": "tool_calls",
												},
											},
										}
										sendOpenAIEvent(c, endEvent)
										sentFinal = true
									}
								}
							}
						case "content_block_stop":
							// 忽略，最终结束由message_delta驱动
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
	json, err := utils.SafeMarshal(data)
	if err != nil {
		logger.Error("序列化OpenAI事件失败", logger.Err(err))
		return
	}

	fmt.Fprintf(c.Writer, "data: %s\n\n", string(json))
}
