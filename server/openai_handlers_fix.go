package server

import (
	"bufio"
	"fmt"
	"io"
	"time"

	"kiro2api/logger"
	"kiro2api/parser"
	"kiro2api/types"
	"kiro2api/utils"

	"github.com/gin-gonic/gin"
)

// handleOpenAIStreamRequestFixed 修复版的OpenAI流式请求处理
func handleOpenAIStreamRequestFixed(c *gin.Context, anthropicReq types.AnthropicRequest, tokenInfo types.TokenInfo) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no") // 禁用nginx缓冲

	messageId := fmt.Sprintf("chatcmpl-%s", time.Now().Format("20060102150405"))

	resp, err := executeCodeWhispererRequest(c, anthropicReq, tokenInfo, true)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	// 立即刷新响应头
	c.Writer.Flush()

	sender := &OpenAIStreamSender{}

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
	sender.SendEvent(c, initialEvent)

	// 创建符合AWS规范的流式解析器和去重管理器
	compliantParser := parser.GlobalCompliantParserPool.Get()
	defer parser.GlobalCompliantParserPool.Put(compliantParser)
	dedupManager := utils.NewToolDedupManager()

	// OpenAI 工具调用增量状态
	toolIndexByToolUseId := make(map[string]int)
	toolUseIdByBlockIndex := make(map[int]string)
	nextToolIndex := 0
	sawToolUse := false
	sentFinal := false

	// 添加完整性跟踪
	var lastSuccessfulRead time.Time = time.Now()
	const readTimeout = 30 * time.Second
	totalBytesRead := 0
	messageCount := 0
	hasMoreData := true

	// 使用bufio.Reader提供更好的缓冲管理
	reader := bufio.NewReaderSize(resp.Body, 64*1024) // 64KB缓冲区
	buf := make([]byte, 8192)                         // 8KB读取缓冲

	// 添加错误恢复计数
	consecutiveErrors := 0
	const maxConsecutiveErrors = 3

	for hasMoreData {
		// 设置读取超时
		if conn, ok := resp.Body.(interface{ SetReadDeadline(time.Time) error }); ok {
			conn.SetReadDeadline(time.Now().Add(readTimeout))
		}

		n, err := reader.Read(buf)

		if n > 0 {
			totalBytesRead += n
			lastSuccessfulRead = time.Now()
			consecutiveErrors = 0 // 重置错误计数

			logger.Debug("读取流式数据",
				logger.Int("bytes", n),
				logger.Int("total_bytes", totalBytesRead),
				logger.Int("message_count", messageCount))

			events, parseErr := compliantParser.ParseStream(buf[:n])
			if parseErr != nil {
				logger.Warn("解析流式数据失败，继续处理",
					logger.Err(parseErr),
					logger.Int("bytes", n))
				// 在宽松模式下继续处理
				continue
			}

			for _, event := range events {
				messageCount++

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
									switch deltaMap["type"] {
									case "text_delta":
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
											sender.SendEvent(c, contentEvent)

											logger.Debug("发送文本增量",
												logger.String("text", text.(string)),
												logger.Int("msg_count", messageCount))
										}
									case "input_json_delta":
										// 工具调用参数增量处理（保持原有逻辑）
										handleToolInputDelta(dataMap, deltaMap,
											toolUseIdByBlockIndex, toolIndexByToolUseId,
											messageId, anthropicReq.Model, sender, c)
									}
								}
							}
						case "content_block_start":
							// 处理内容块开始（保持原有逻辑）
							handleContentBlockStart(dataMap,
								&nextToolIndex, &sawToolUse,
								toolIndexByToolUseId, toolUseIdByBlockIndex,
								messageId, anthropicReq.Model, sender, c)
						case "content_block_stop":
							// 记录内容块结束
							logger.Debug("内容块结束",
								logger.Any("data", dataMap),
								logger.Int("msg_count", messageCount))
						case "message_delta":
							// 处理消息结束
							if handleMessageDelta(dataMap, sawToolUse, &sentFinal,
								messageId, anthropicReq.Model, sender, c) {
								// 正常结束
								logger.Info("流式传输正常结束",
									logger.Int("total_bytes", totalBytesRead),
									logger.Int("messages", messageCount))
								hasMoreData = false
							}
						case "message_stop":
							// 消息完全结束
							logger.Info("收到message_stop，流式传输结束",
								logger.Int("total_bytes", totalBytesRead),
								logger.Int("messages", messageCount))
							hasMoreData = false
						}
					}
				}

				// 定期刷新以确保数据及时发送
				if messageCount%5 == 0 {
					c.Writer.Flush()
				}
			}

			// 处理完事件后立即刷新
			c.Writer.Flush()
		}

		// 错误处理
		if err != nil {
			if err == io.EOF {
				logger.Info("流式读取正常结束(EOF)",
					logger.Int("total_bytes", totalBytesRead),
					logger.Int("messages", messageCount))
				hasMoreData = false
			} else if err == io.ErrUnexpectedEOF {
				logger.Warn("流式读取意外结束",
					logger.Err(err),
					logger.Int("total_bytes", totalBytesRead),
					logger.Int("messages", messageCount))

				// 尝试恢复
				consecutiveErrors++
				if consecutiveErrors >= maxConsecutiveErrors {
					logger.Error("连续错误过多，停止读取",
						logger.Int("consecutive_errors", consecutiveErrors))
					hasMoreData = false
				} else {
					// 短暂等待后继续
					time.Sleep(100 * time.Millisecond)
					continue
				}
			} else if netErr, ok := err.(interface{ Timeout() bool }); ok && netErr.Timeout() {
				// 超时处理
				if time.Since(lastSuccessfulRead) > readTimeout {
					logger.Warn("读取超时，结束流式传输",
						logger.Duration("since_last_read", time.Since(lastSuccessfulRead)))
					hasMoreData = false
				} else {
					// 继续等待
					logger.Debug("读取超时但继续等待",
						logger.Duration("since_last_read", time.Since(lastSuccessfulRead)))
					continue
				}
			} else {
				// 其他错误
				logger.Error("流式读取错误",
					logger.Err(err),
					logger.Int("total_bytes", totalBytesRead),
					logger.Int("messages", messageCount))

				consecutiveErrors++
				if consecutiveErrors >= maxConsecutiveErrors {
					hasMoreData = false
				}
			}
		}
	}

	// 确保发送了结束原因（如果还没有发送）
	if !sentFinal && messageCount > 0 {
		finishReason := "stop"
		if sawToolUse {
			finishReason = "tool_calls"
		}

		finalEvent := map[string]any{
			"id":      messageId,
			"object":  "chat.completion.chunk",
			"created": time.Now().Unix(),
			"model":   anthropicReq.Model,
			"choices": []map[string]any{
				{
					"index":         0,
					"delta":         map[string]any{},
					"finish_reason": finishReason,
				},
			},
		}
		sender.SendEvent(c, finalEvent)
		c.Writer.Flush()

		logger.Info("发送最终结束事件",
			logger.String("finish_reason", finishReason))
	}

	// 发送结束标记
	fmt.Fprintf(c.Writer, "data: [DONE]\n\n")
	c.Writer.Flush()

	logger.Info("OpenAI流式响应完成",
		logger.Int("total_bytes_read", totalBytesRead),
		logger.Int("total_messages", messageCount),
		logger.Bool("saw_tool_use", sawToolUse))
}

// handleToolInputDelta 处理工具输入增量
func handleToolInputDelta(dataMap, deltaMap map[string]any,
	toolUseIdByBlockIndex map[int]string,
	toolIndexByToolUseId map[string]int,
	messageId, model string,
	sender *OpenAIStreamSender, c *gin.Context) {

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
					"model":   model,
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
				sender.SendEvent(c, toolDelta)
			}
		}
	}
}

// handleContentBlockStart 处理内容块开始
func handleContentBlockStart(dataMap map[string]any,
	nextToolIndex *int, sawToolUse *bool,
	toolIndexByToolUseId map[string]int,
	toolUseIdByBlockIndex map[int]string,
	messageId, model string,
	sender *OpenAIStreamSender, c *gin.Context) {

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
						toolIndexByToolUseId[toolUseId] = *nextToolIndex
						*nextToolIndex++
					}
					toolUseIdByBlockIndex[toolBlockIndex] = toolUseId
					*sawToolUse = true
					toolIdx := toolIndexByToolUseId[toolUseId]

					// 发送OpenAI工具调用开始增量
					toolStart := map[string]any{
						"id":      messageId,
						"object":  "chat.completion.chunk",
						"created": time.Now().Unix(),
						"model":   model,
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
					sender.SendEvent(c, toolStart)
				}
			}
		}
	}
}

// handleMessageDelta 处理消息增量
func handleMessageDelta(dataMap map[string]any,
	sawToolUse bool, sentFinal *bool,
	messageId, model string,
	sender *OpenAIStreamSender, c *gin.Context) bool {

	if sawToolUse && !*sentFinal {
		if delta, ok := dataMap["delta"].(map[string]any); ok {
			if sr, ok := delta["stop_reason"].(string); ok && sr == "tool_use" {
				endEvent := map[string]any{
					"id":      messageId,
					"object":  "chat.completion.chunk",
					"created": time.Now().Unix(),
					"model":   model,
					"choices": []map[string]any{
						{
							"index":         0,
							"delta":         map[string]any{},
							"finish_reason": "tool_calls",
						},
					},
				}
				sender.SendEvent(c, endEvent)
				*sentFinal = true
				return true
			} else if sr, ok := delta["stop_reason"].(string); ok && sr != "" {
				// 其他结束原因
				endEvent := map[string]any{
					"id":      messageId,
					"object":  "chat.completion.chunk",
					"created": time.Now().Unix(),
					"model":   model,
					"choices": []map[string]any{
						{
							"index":         0,
							"delta":         map[string]any{},
							"finish_reason": "stop",
						},
					},
				}
				sender.SendEvent(c, endEvent)
				*sentFinal = true
				return true
			}
		}
	}
	return false
}
