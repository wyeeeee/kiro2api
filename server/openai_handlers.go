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

	resp, err := utils.SharedHTTPClient.Do(req)
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

	content := ""
	contexts := []map[string]any{}
	currentToolUse := make(map[string]any)
	toolInputBuffer := ""

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
									content += text.(string)
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
							if blockMap["type"] == "tool_use" {
								currentToolUse = map[string]any{
									"type": "tool_use",
									"id":   blockMap["id"],
									"name": blockMap["name"],
								}
								toolInputBuffer = ""
							}
						}
					}
				case "content_block_stop":
					if content != "" {
						contexts = append(contexts, map[string]any{
							"text": content,
							"type": "text",
						})
						content = ""
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
						contexts = append(contexts, currentToolUse)
						currentToolUse = make(map[string]any)
						toolInputBuffer = ""
					}
				}
			}
		}
	}

	// 构建Anthropic响应
	anthropicResp := map[string]any{
		"content":       contexts,
		"model":         anthropicReq.Model,
		"role":          "assistant",
		"stop_reason":   "end_turn",
		"stop_sequence": nil,
		"type":          "message",
		"usage": map[string]any{
			"input_tokens":  len(utils.GetMessageContent(anthropicReq.Messages[0].Content)),
			"output_tokens": len(content),
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

	openaiMessageId := fmt.Sprintf("chatcmpl-%s", time.Now().Format("20060102150405"))

	req, err := buildCodeWhispererRequest(anthropicReq, accessToken, true)
	if err != nil {
		sendOpenAIErrorEvent(c, "构建请求失败", err)
		return
	}

	resp, err := utils.SharedHTTPClient.Do(req)
	if err != nil {
		sendOpenAIErrorEvent(c, "CodeWhisperer request error", fmt.Errorf("request error: %s", err.Error()))
		return
	}
	defer resp.Body.Close()

	if handleCodeWhispererError(c, resp) {
		return
	}

	// 立即刷新响应头
	c.Writer.Flush()

	// 发送初始流响应
	initialResp := types.OpenAIStreamResponse{
		ID:      openaiMessageId,
		Object:  "chat.completion.chunk",
		Created: time.Now().Unix(),
		Model:   anthropicReq.Model,
		Choices: []types.OpenAIStreamChoice{
			{
				Index: 0,
				Delta: struct {
					Role    string `json:"role,omitempty"`
					Content string `json:"content,omitempty"`
				}{
					Role: "assistant",
				},
			},
		},
	}
	sendOpenAIStreamEvent(c, initialResp)

	// 创建流式解析器
	streamParser := parser.NewStreamParser()

	// 流式读取并解析EventStream响应
	buf := make([]byte, 1024)
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			// 解析当前数据块
			events := streamParser.ParseStream(buf[:n])

			// 处理解析出的事件
			for _, event := range events {
				if event.Data != nil {
					if dataMap, ok := event.Data.(map[string]any); ok {
						if eventType, ok := dataMap["type"].(string); ok {
							if eventType == "content_block_delta" {
								if delta, ok := dataMap["delta"]; ok {
									if deltaMap, ok := delta.(map[string]any); ok {
										if deltaMap["type"] == "text_delta" {
											if text, ok := deltaMap["text"]; ok {
												deltaResp := types.OpenAIStreamResponse{
													ID:      openaiMessageId,
													Object:  "chat.completion.chunk",
													Created: time.Now().Unix(),
													Model:   anthropicReq.Model,
													Choices: []types.OpenAIStreamChoice{
														{
															Index: 0,
															Delta: struct {
																Role    string `json:"role,omitempty"`
																Content string `json:"content,omitempty"`
															}{
																Content: text.(string),
															},
														},
													},
												}
												sendOpenAIStreamEvent(c, deltaResp)

												// 立即刷新以确保实时性
												c.Writer.Flush()
											}
										}
									}
								}
							}
						}
					}
				}
			}
		}
		if err != nil {
			break
		}
	}

	// 发送完成响应
	finishReasonStop := "stop"
	finishResp := types.OpenAIStreamResponse{
		ID:      openaiMessageId,
		Object:  "chat.completion.chunk",
		Created: time.Now().Unix(),
		Model:   anthropicReq.Model,
		Choices: []types.OpenAIStreamChoice{
			{
				Index:        0,
				FinishReason: &finishReasonStop,
				Delta: struct {
					Role    string `json:"role,omitempty"`
					Content string `json:"content,omitempty"`
				}{},
			},
		},
	}
	sendOpenAIStreamEvent(c, finishResp)

	// 发送 [DONE] 标记
	c.Writer.WriteString("data: [DONE]\n\n")
	c.Writer.Flush()
}

// sendOpenAIStreamEvent 发送OpenAI流式事件
func sendOpenAIStreamEvent(c *gin.Context, data types.OpenAIStreamResponse) {
	jsonData, err := sonic.Marshal(data)
	if err != nil {
		return
	}

	c.Writer.WriteString(fmt.Sprintf("data: %s\n\n", string(jsonData)))
	c.Writer.Flush()
}

// sendOpenAIErrorEvent 发送OpenAI格式的错误事件
func sendOpenAIErrorEvent(c *gin.Context, message string, _ error) {
	errorResp := map[string]any{
		"error": map[string]any{
			"message": message,
			"type":    "server_error",
			"code":    "internal_error",
		},
	}

	jsonData, err := sonic.Marshal(errorResp)
	if err != nil {
		return
	}

	c.Writer.WriteString(fmt.Sprintf("data: %s\n\n", string(jsonData)))
	c.Writer.Flush()
}
