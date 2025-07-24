package server

import (
	"bufio"
	"fmt"
	"strings"
	"time"

	"kiro2api/logger"
	"kiro2api/parser"
	"kiro2api/types"

	"github.com/bytedance/sonic"
	"github.com/valyala/fasthttp"
)

// handleStreamRequest 处理流式请求
func handleStreamRequest(ctx *fasthttp.RequestCtx, anthropicReq types.AnthropicRequest, accessToken string) {
	ctx.Response.Header.Set("Content-Type", "text/event-stream")
	ctx.Response.Header.Set("Cache-Control", "no-cache")
	ctx.Response.Header.Set("Connection", "keep-alive")
	setCORSHeaders(ctx)

	messageId := fmt.Sprintf("msg_%s", time.Now().Format("20060102150405"))

	req, err := buildCodeWhispererRequest(anthropicReq, accessToken, true)
	if err != nil {
		sendErrorEvent(ctx, "构建请求失败", err)
		return
	}
	defer fasthttp.ReleaseRequest(req)

	resp := fasthttp.AcquireResponse()
	defer fasthttp.ReleaseResponse(resp)

	if err := fasthttpClient.Do(req, resp); err != nil {
		sendErrorEvent(ctx, "CodeWhisperer request error", fmt.Errorf("request error: %s", err.Error()))
		return
	}

	if handleCodeWhispererError(ctx, resp) {
		return
	}

	// 先读取整个响应体
	respBody := resp.Body()

	// 使用新的CodeWhisperer解析器
	events := parser.ParseEvents(respBody)

	if len(events) > 0 {
		// 发送开始事件
		messageStart := map[string]any{
			"type": "message_start",
			"message": map[string]any{
				"id":            messageId,
				"type":          "message",
				"role":          "assistant",
				"content":       []any{},
				"model":         anthropicReq.Model,
				"stop_reason":   nil,
				"stop_sequence": nil,
				"usage": map[string]any{
					"input_tokens":  len(getMessageContent(anthropicReq.Messages[0].Content)),
					"output_tokens": 1,
				},
			},
		}
		sendSSEEvent(ctx, "message_start", messageStart)
		sendSSEEvent(ctx, "ping", map[string]string{
			"type": "ping",
		})

		contentBlockStart := map[string]any{
			"content_block": map[string]any{
				"text": "",
				"type": "text"},
			"index": 0, "type": "content_block_start",
		}

		sendSSEEvent(ctx, "content_block_start", contentBlockStart)

		outputTokens := 0
		for _, e := range events {
			sendSSEEvent(ctx, e.Event, e.Data)

			if e.Event == "content_block_delta" {
				outputTokens = len(getMessageContent(e.Data))
			}
		}

		contentBlockStop := map[string]any{
			"index": 0,
			"type":  "content_block_stop",
		}
		sendSSEEvent(ctx, "content_block_stop", contentBlockStop)

		contentBlockStopReason := map[string]any{
			"type": "message_delta", "delta": map[string]any{"stop_reason": "end_turn", "stop_sequence": nil}, "usage": map[string]any{
				"output_tokens": outputTokens,
			},
		}
		sendSSEEvent(ctx, "message_delta", contentBlockStopReason)

		messageStop := map[string]any{
			"type": "message_stop",
		}
		sendSSEEvent(ctx, "message_stop", messageStop)
	}
}

// handleNonStreamRequest 处理非流式请求
func handleNonStreamRequest(ctx *fasthttp.RequestCtx, anthropicReq types.AnthropicRequest, accessToken string) {
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
	respBodyStr := string(cwRespBody)
	events := parser.ParseEvents(cwRespBody)

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
								toolUseId = deltaMap["id"].(string)
								toolName = deltaMap["name"].(string)
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
	if strings.Contains(string(cwRespBody), "Improperly formed request.") {
		logger.Error("CodeWhisperer返回格式错误", logger.String("response", respBodyStr))
		ctx.Error(fmt.Sprintf("请求格式错误: %s", respBodyStr), fasthttp.StatusBadRequest)
		return
	}
	anthropicResp := map[string]any{
		"content":       contexts,
		"model":         anthropicReq.Model,
		"role":          "assistant",
		"stop_reason":   "end_turn",
		"stop_sequence": nil,
		"type":          "message",
		"usage": map[string]any{
			"input_tokens":  len(getMessageContent(anthropicReq.Messages[0].Content)),
			"output_tokens": len(context),
		},
	}
	ctx.Response.Header.SetContentType("application/json")
	respJson, err := sonic.Marshal(anthropicResp)
	if err != nil {
		ctx.Error(fmt.Sprintf("序列化响应失败: %v", err), fasthttp.StatusInternalServerError)
		return
	}
	ctx.SetBody(respJson)
}

// sendSSEEvent 发送 SSE 事件
func sendSSEEvent(ctx *fasthttp.RequestCtx, eventType string, data any) {
	json, err := sonic.Marshal(data)
	if err != nil {
		return
	}

	logger.Debug("发送SSE事件",
		logger.String("event", eventType),
		logger.String("data", string(json)))

	ctx.Response.SetBodyStreamWriter(func(w *bufio.Writer) {
		fmt.Fprintf(w, "event: %s\n", eventType)
		fmt.Fprintf(w, "data: %s\n\n", string(json))
		w.Flush()
	})
}

// sendErrorEvent 发送错误事件
func sendErrorEvent(ctx *fasthttp.RequestCtx, message string, _ error) {
	errorResp := map[string]any{
		"type": "error",
		"error": map[string]any{
			"type":    "overloaded_error",
			"message": message,
		},
	}
	sendSSEEvent(ctx, "error", errorResp)
}
