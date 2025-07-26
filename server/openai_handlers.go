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
	sender := &OpenAIStreamSender{}
	handleGenericStreamRequest(c, anthropicReq, accessToken, sender, createOpenAIStreamEvents)
}

// createOpenAIStreamEvents 创建OpenAI流式初始事件
func createOpenAIStreamEvents(messageId, inputContent, model string) []map[string]any {
	initialResp := map[string]any{
		"id":      messageId,
		"object":  "chat.completion.chunk",
		"created": time.Now().Unix(),
		"model":   model,
		"choices": []map[string]any{
			{
				"index": 0,
				"delta": map[string]any{
					"role": "assistant",
				},
			},
		},
	}
	return []map[string]any{initialResp}
}
