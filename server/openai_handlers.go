package server

import (
	"bufio"
	"fmt"
	"time"

	"kiro2api/converter"
	"kiro2api/logger"
	"kiro2api/parser"
	"kiro2api/types"

	"github.com/bytedance/sonic"
	"github.com/valyala/fasthttp"
)

// handleOpenAINonStreamRequest 处理OpenAI非流式请求
func handleOpenAINonStreamRequest(ctx *fasthttp.RequestCtx, anthropicReq types.AnthropicRequest, accessToken string) {
	req, err := buildCodeWhispererRequest(anthropicReq, accessToken, false)
	if err != nil {
		logger.Error("构建请求失败", logger.Err(err))
		ctx.Error(fmt.Sprintf("构建请求失败: %v", err), fasthttp.StatusInternalServerError)
		return
	}
	defer fasthttp.ReleaseRequest(req)

	resp := fasthttp.AcquireResponse()
	defer fasthttp.ReleaseResponse(resp)

	if err := fasthttpClient.Do(req, resp); err != nil {
		logger.Error("发送请求失败", logger.Err(err))
		ctx.Error(fmt.Sprintf("发送请求失败: %v", err), fasthttp.StatusInternalServerError)
		return
	}

	if handleCodeWhispererError(ctx, resp) {
		return
	}

	cwRespBody := resp.Body()

	events := parser.ParseEvents(cwRespBody)

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
							fmt.Printf("Tool input buffer: %s\n", toolInputBuffer)
							var inputParams map[string]any
							if err := sonic.UnmarshalString(toolInputBuffer, &inputParams); err == nil {
								currentToolUse["input"] = inputParams
								fmt.Printf("Parsed tool use input: %+v\n", inputParams)
							} else {
								logger.Warn("解析工具输入参数失败", logger.Err(err), logger.String("input", toolInputBuffer))
							}
						}
						contexts = append(contexts, currentToolUse)
						currentToolUse = make(map[string]any)
						toolInputBuffer = ""
					}
				}
			}
		}
	}

	// 如果没有通过content_block_stop事件添加内容，但累积了content，则手动添加
	if len(contexts) == 0 && content != "" {
		contexts = append(contexts, map[string]any{
			"text": content,
			"type": "text",
		})
	}

	// 构造中间的Anthropic响应格式
	anthropicResp := map[string]any{
		"content":       contexts,
		"model":         anthropicReq.Model,
		"role":          "assistant",
		"stop_reason":   "end_turn",
		"stop_sequence": nil,
		"type":          "message",
		"usage": map[string]any{
			"input_tokens":  len(getMessageContent(anthropicReq.Messages[0].Content)),
			"output_tokens": len(content),
		},
	}

	// 转换为OpenAI格式
	openaiMessageId := fmt.Sprintf("chatcmpl-%s", time.Now().Format("20060102150405"))
	openaiResp := converter.ConvertAnthropicToOpenAI(anthropicResp, anthropicReq.Model, openaiMessageId)

	ctx.Response.Header.SetContentType("application/json")
	ctx.Response.Header.Set("Access-Control-Allow-Origin", "*")

	respJson, err := sonic.Marshal(openaiResp)
	if err != nil {
		ctx.Error(fmt.Sprintf("序列化响应失败: %v", err), fasthttp.StatusInternalServerError)
		return
	}
	ctx.SetBody(respJson)
}

// handleOpenAIStreamRequest 处理OpenAI流式请求
func handleOpenAIStreamRequest(ctx *fasthttp.RequestCtx, anthropicReq types.AnthropicRequest, accessToken string) {
	ctx.Response.Header.Set("Content-Type", "text/event-stream")
	ctx.Response.Header.Set("Cache-Control", "no-cache")
	ctx.Response.Header.Set("Connection", "keep-alive")
	setCORSHeaders(ctx)

	messageId := fmt.Sprintf("chatcmpl-%s", time.Now().Format("20060102150405"))

	req, err := buildCodeWhispererRequest(anthropicReq, accessToken, true)
	if err != nil {
		sendOpenAIErrorEvent(ctx, "构建请求失败", err)
		return
	}
	defer fasthttp.ReleaseRequest(req)

	resp := fasthttp.AcquireResponse()
	defer fasthttp.ReleaseResponse(resp)

	if err := fasthttpClient.Do(req, resp); err != nil {
		sendOpenAIErrorEvent(ctx, "CodeWhisperer request error", fmt.Errorf("request error: %s", err.Error()))
		return
	}

	if handleCodeWhispererError(ctx, resp) {
		return
	}

	respBody := resp.Body()
	events := parser.ParseEvents(respBody)

	// 使用单一的SetBodyStreamWriter来处理所有流式输出
	ctx.Response.SetBodyStreamWriter(func(w *bufio.Writer) {
		if len(events) > 0 {
			// 发送初始流响应
			initialResp := types.OpenAIStreamResponse{
				ID:      messageId,
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
						FinishReason: nil,
					},
				},
			}
			if initialJson, err := sonic.Marshal(initialResp); err == nil {
				logger.Debug("发送OpenAI SSE事件", logger.String("data", string(initialJson)))
				fmt.Fprintf(w, "data: %s\n\n", string(initialJson))
				w.Flush()
			}

			// 处理内容事件
			for _, e := range events {
				if e.Event == "content_block_delta" {
					if dataMap, ok := e.Data.(map[string]any); ok {
						if delta, ok := dataMap["delta"]; ok {
							if deltaMap, ok := delta.(map[string]any); ok {
								if deltaMap["type"] == "text_delta" {
									if text, ok := deltaMap["text"]; ok {
										streamResp := types.OpenAIStreamResponse{
											ID:      messageId,
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
													FinishReason: nil,
												},
											},
										}
										if streamJson, err := sonic.Marshal(streamResp); err == nil {
											logger.Debug("发送OpenAI SSE事件", logger.String("data", string(streamJson)))
											fmt.Fprintf(w, "data: %s\n\n", string(streamJson))
											w.Flush()
										}
									}
								}
							}
						}
					}
				}
			}

			// 发送最终响应
			finishReason := "stop"
			finalResp := types.OpenAIStreamResponse{
				ID:      messageId,
				Object:  "chat.completion.chunk",
				Created: time.Now().Unix(),
				Model:   anthropicReq.Model,
				Choices: []types.OpenAIStreamChoice{
					{
						Index: 0,
						Delta: struct {
							Role    string `json:"role,omitempty"`
							Content string `json:"content,omitempty"`
						}{},
						FinishReason: &finishReason,
					},
				},
			}
			if finalJson, err := sonic.Marshal(finalResp); err == nil {
				logger.Debug("发送OpenAI SSE事件", logger.String("data", string(finalJson)))
				fmt.Fprintf(w, "data: %s\n\n", string(finalJson))
				w.Flush()
			}
		}

		// 发送 [DONE] 标记
		fmt.Fprintf(w, "data: [DONE]\n\n")
		w.Flush()
	})
}

// sendOpenAIErrorEvent 发送OpenAI格式的错误事件
func sendOpenAIErrorEvent(ctx *fasthttp.RequestCtx, message string, _ error) {
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

	ctx.Response.SetBodyStreamWriter(func(w *bufio.Writer) {
		fmt.Fprintf(w, "data: %s\n\n", string(jsonData))
		w.Flush()
	})
}
