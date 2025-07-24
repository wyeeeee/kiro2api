package converter

import (
	"strings"
	"time"

	"kiro2api/config"
	"kiro2api/types"
	"kiro2api/utils"

	"github.com/bytedance/sonic"
)

// getMessageContent 从消息中提取文本内容
func getMessageContent(content any) string {
	switch v := content.(type) {
	case string:
		if len(v) == 0 {
			return "answer for user question"
		}
		return v
	case []any:
		var texts []string
		for _, block := range v {
			if m, ok := block.(map[string]any); ok {
				var cb types.ContentBlock
				if data, err := sonic.Marshal(m); err == nil {
					if err := sonic.Unmarshal(data, &cb); err == nil {
						switch cb.Type {
						case "tool_result":
							texts = append(texts, *cb.Content)
						case "text":
							texts = append(texts, *cb.Text)
						}
					}
				}
			}
		}
		if len(texts) == 0 {
			return "answer for user question"
		}
		return strings.Join(texts, "\n")
	default:
		return "answer for user question"
	}
}

// ConvertOpenAIToAnthropic 将OpenAI请求转换为Anthropic请求
func ConvertOpenAIToAnthropic(openaiReq types.OpenAIRequest) types.AnthropicRequest {
	var anthropicMessages []types.AnthropicRequestMessage

	// 转换消息
	for _, msg := range openaiReq.Messages {
		anthropicMsg := types.AnthropicRequestMessage{
			Role:    msg.Role,
			Content: msg.Content,
		}
		anthropicMessages = append(anthropicMessages, anthropicMsg)
	}

	// 设置默认值
	maxTokens := 16384
	if openaiReq.MaxTokens != nil {
		maxTokens = *openaiReq.MaxTokens
	}

	stream := false
	if openaiReq.Stream != nil {
		stream = *openaiReq.Stream
	}

	anthropicReq := types.AnthropicRequest{
		Model:     openaiReq.Model,
		MaxTokens: maxTokens,
		Messages:  anthropicMessages,
		Stream:    stream,
	}

	if openaiReq.Temperature != nil {
		anthropicReq.Temperature = openaiReq.Temperature
	}

	// 转换 tools
	if len(openaiReq.Tools) > 0 {
		var anthropicTools []types.AnthropicTool
		for _, tool := range openaiReq.Tools {
			if tool.Type == "function" {
				// 清理 OpenAI 特有的字段，避免 CodeWhisperer 拒绝请求
				cleanedParams := make(map[string]any)
				for k, v := range tool.Function.Parameters {
					// 过滤掉 OpenAI 特有的字段
					if k != "additionalProperties" && k != "strict" {
						cleanedParams[k] = v
					}
				}

				anthropicTool := types.AnthropicTool{
					Name:        tool.Function.Name,
					Description: tool.Function.Description,
					InputSchema: cleanedParams,
				}
				anthropicTools = append(anthropicTools, anthropicTool)
			}
		}
		anthropicReq.Tools = anthropicTools
	}

	return anthropicReq
}

// ConvertAnthropicToOpenAI 将Anthropic响应转换为OpenAI响应
func ConvertAnthropicToOpenAI(anthropicResp map[string]any, model string, messageId string) types.OpenAIResponse {
	content := ""
	var toolCalls []types.OpenAIToolCall
	finishReason := "stop"

	// 首先尝试[]any类型断言
	if contentArray, ok := anthropicResp["content"].([]any); ok && len(contentArray) > 0 {
		// 遍历所有content blocks
		var textParts []string
		for _, block := range contentArray {
			if textBlock, ok := block.(map[string]any); ok {
				if blockType, ok := textBlock["type"].(string); ok {
					switch blockType {
					case "text":
						if text, ok := textBlock["text"].(string); ok {
							textParts = append(textParts, text)
						}
					case "tool_use":
						finishReason = "tool_calls"
						if toolUseId, ok := textBlock["id"].(string); ok {
							if toolName, ok := textBlock["name"].(string); ok {
								if input, ok := textBlock["input"]; ok {
									inputJson, _ := sonic.Marshal(input)
									toolCall := types.OpenAIToolCall{
										ID:   toolUseId,
										Type: "function",
										Function: types.OpenAIToolFunction{
											Name:      toolName,
											Arguments: string(inputJson),
										},
									}
									toolCalls = append(toolCalls, toolCall)
								}
							}
						}
					}
				}
			}
		}
		content = strings.Join(textParts, "")
	} else if contentSlice, ok := anthropicResp["content"].([]map[string]any); ok && len(contentSlice) > 0 {
		// 尝试[]map[string]any类型断言
		var textParts []string
		for _, textBlock := range contentSlice {
			if blockType, ok := textBlock["type"].(string); ok {
				switch blockType {
				case "text":
					if text, ok := textBlock["text"].(string); ok {
						textParts = append(textParts, text)
					}
				case "tool_use":
					finishReason = "tool_calls"
					if toolUseId, ok := textBlock["id"].(string); ok {
						if toolName, ok := textBlock["name"].(string); ok {
							if input, ok := textBlock["input"]; ok {
								inputJson, _ := sonic.Marshal(input)
								toolCall := types.OpenAIToolCall{
									ID:   toolUseId,
									Type: "function",
									Function: types.OpenAIToolFunction{
										Name:      toolName,
										Arguments: string(inputJson),
									},
								}
								toolCalls = append(toolCalls, toolCall)
							}
						}
					}
				}
			}
		}
		content = strings.Join(textParts, "")
	}

	// 计算token使用量
	promptTokens := 0
	completionTokens := len(content) / 4 // 简单估算
	if usage, ok := anthropicResp["usage"].(map[string]any); ok {
		if inputTokens, ok := usage["input_tokens"].(int); ok {
			promptTokens = inputTokens
		}
		if outputTokens, ok := usage["output_tokens"].(int); ok {
			completionTokens = outputTokens
		}
	}

	message := types.OpenAIMessage{
		Role:    "assistant",
		Content: content,
	}

	// 只有当有tool_calls时才添加ToolCalls字段
	if len(toolCalls) > 0 {
		message.ToolCalls = toolCalls
	}

	return types.OpenAIResponse{
		ID:      messageId,
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   model,
		Choices: []types.OpenAIChoice{
			{
				Index:        0,
				Message:      message,
				FinishReason: finishReason,
			},
		},
		Usage: types.OpenAIUsage{
			PromptTokens:     promptTokens,
			CompletionTokens: completionTokens,
			TotalTokens:      promptTokens + completionTokens,
		},
	}
}

// BuildCodeWhispererRequest 构建 CodeWhisperer 请求
func BuildCodeWhispererRequest(anthropicReq types.AnthropicRequest) types.CodeWhispererRequest {
	cwReq := types.CodeWhispererRequest{
		ProfileArn: "arn:aws:codewhisperer:us-east-1:699475941385:profile/EHGA3GRVQMUK",
	}
	cwReq.ConversationState.ChatTriggerType = "MANUAL"
	cwReq.ConversationState.ConversationId = utils.GenerateUUID()
	cwReq.ConversationState.CurrentMessage.UserInputMessage.Content = getMessageContent(anthropicReq.Messages[len(anthropicReq.Messages)-1].Content)
	cwReq.ConversationState.CurrentMessage.UserInputMessage.ModelId = config.ModelMap[anthropicReq.Model]
	cwReq.ConversationState.CurrentMessage.UserInputMessage.Origin = "AI_EDITOR"

	// 处理 tools 信息
	if len(anthropicReq.Tools) > 0 {
		var tools []types.CodeWhispererTool
		for _, tool := range anthropicReq.Tools {
			cwTool := types.CodeWhispererTool{}
			cwTool.ToolSpecification.Name = tool.Name
			cwTool.ToolSpecification.Description = tool.Description
			cwTool.ToolSpecification.InputSchema = types.InputSchema{
				Json: tool.InputSchema,
			}
			tools = append(tools, cwTool)
		}
		cwReq.ConversationState.CurrentMessage.UserInputMessage.UserInputMessageContext.Tools = tools
	}

	// 构建历史消息
	// 先处理 system 消息或者常规历史消息
	if len(anthropicReq.System) > 0 || len(anthropicReq.Messages) > 1 {
		var history []any

		// 首先添加每个 system 消息作为独立的历史记录项
		assistantDefaultMsg := types.HistoryAssistantMessage{}
		assistantDefaultMsg.AssistantResponseMessage.Content = getMessageContent("I will follow these instructions")
		assistantDefaultMsg.AssistantResponseMessage.ToolUses = make([]any, 0)

		if len(anthropicReq.System) > 0 {
			for _, sysMsg := range anthropicReq.System {
				userMsg := types.HistoryUserMessage{}
				userMsg.UserInputMessage.Content = sysMsg.Text
				userMsg.UserInputMessage.ModelId = config.ModelMap[anthropicReq.Model]
				userMsg.UserInputMessage.Origin = "AI_EDITOR"
				history = append(history, userMsg)
				history = append(history, assistantDefaultMsg)
			}
		}

		// 然后处理常规消息历史
		for i := 0; i < len(anthropicReq.Messages)-1; i++ {
			if anthropicReq.Messages[i].Role == "user" {
				userMsg := types.HistoryUserMessage{}
				userMsg.UserInputMessage.Content = getMessageContent(anthropicReq.Messages[i].Content)
				userMsg.UserInputMessage.ModelId = config.ModelMap[anthropicReq.Model]
				userMsg.UserInputMessage.Origin = "AI_EDITOR"
				history = append(history, userMsg)

				// 检查下一条消息是否是助手回复
				if i+1 < len(anthropicReq.Messages)-1 && anthropicReq.Messages[i+1].Role == "assistant" {
					assistantMsg := types.HistoryAssistantMessage{}
					assistantMsg.AssistantResponseMessage.Content = getMessageContent(anthropicReq.Messages[i+1].Content)
					assistantMsg.AssistantResponseMessage.ToolUses = make([]any, 0)
					history = append(history, assistantMsg)
					i++ // 跳过已处理的助手消息
				}
			}
		}

		cwReq.ConversationState.History = history
	}

	return cwReq
}
