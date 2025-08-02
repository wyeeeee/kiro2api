package converter

import (
	"fmt"
	"strings"
	"time"

	"kiro2api/config"
	"kiro2api/types"
	"kiro2api/utils"

	"github.com/bytedance/sonic"
)

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

	// 为了增强兼容性，当stream未设置时默认为false（非流式响应）
	// 这样可以避免客户端在处理函数调用时的解析问题
	stream := false
	if openaiReq.Stream != nil {
		stream = *openaiReq.Stream
	}

	// 如果环境变量禁用了流式传输，则强制设置为false
	if config.IsStreamDisabled() {
		stream = false
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
		anthropicTools, err := validateAndProcessTools(openaiReq.Tools)
		if err != nil {
			// 记录警告但不中断处理，允许部分工具失败
			// 可以考虑返回错误，取决于业务需求
			// 这里参考server.py的做法，记录错误但继续处理有效的工具
		}
		anthropicReq.Tools = anthropicTools
	}

	// 转换 tool_choice
	if openaiReq.ToolChoice != nil {
		anthropicReq.ToolChoice = convertOpenAIToolChoiceToAnthropic(openaiReq.ToolChoice)
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
		Usage: types.Usage{
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
	content, err := utils.GetMessageContent(anthropicReq.Messages[len(anthropicReq.Messages)-1].Content)
	if err != nil {
		// 错误处理: 可以选择记录日志、返回错误或设置默认内容
		content = ""
	}
	cwReq.ConversationState.CurrentMessage.UserInputMessage.Content = content

	// 确保ModelId不为空，如果映射不存在则使用默认模型
	modelId := config.ModelMap[anthropicReq.Model]
	if modelId == "" {
		modelId = "CLAUDE_3_7_SONNET_20250219_V1_0" // 使用默认模型
	}
	cwReq.ConversationState.CurrentMessage.UserInputMessage.ModelId = modelId
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
	if len(anthropicReq.System) > 0 || len(anthropicReq.Messages) > 1 {
		var history []any

		// 正确处理 system 消息
		if len(anthropicReq.System) > 0 {
			// 将 system prompt 作为独立的 user/assistant 消息对添加到历史记录
			// AWS Q 的后端会将其识别为 system prompt
			var systemContentBuilder strings.Builder
			for _, sysMsg := range anthropicReq.System {
				content, err := utils.GetMessageContent(sysMsg)
				if err == nil {
					systemContentBuilder.WriteString(content)
					systemContentBuilder.WriteString("\n")
				}
			}

			userMsg := types.HistoryUserMessage{}
			userMsg.UserInputMessage.Content = strings.TrimSpace(systemContentBuilder.String())
			userMsg.UserInputMessage.ModelId = modelId
			userMsg.UserInputMessage.Origin = "AI_EDITOR"
			history = append(history, userMsg)

			assistantMsg := types.HistoryAssistantMessage{}
			assistantMsg.AssistantResponseMessage.Content = "OK"
			assistantMsg.AssistantResponseMessage.ToolUses = make([]any, 0)
			history = append(history, assistantMsg)
		}

		// 然后处理常规消息历史
		for i := 0; i < len(anthropicReq.Messages)-1; i++ {
			if anthropicReq.Messages[i].Role == "user" {
				userMsg := types.HistoryUserMessage{}
				content, err := utils.GetMessageContent(anthropicReq.Messages[i].Content)
				if err == nil {
					userMsg.UserInputMessage.Content = content
				} else {
					userMsg.UserInputMessage.Content = ""
				}
				userMsg.UserInputMessage.ModelId = modelId // 使用验证后的modelId
				userMsg.UserInputMessage.Origin = "AI_EDITOR"
				history = append(history, userMsg)

				// 检查下一条消息是否是助手回复
				if i+1 < len(anthropicReq.Messages)-1 && anthropicReq.Messages[i+1].Role == "assistant" {
					assistantMsg := types.HistoryAssistantMessage{}
					assistantContent, err := utils.GetMessageContent(anthropicReq.Messages[i+1].Content)
					if err == nil {
						assistantMsg.AssistantResponseMessage.Content = assistantContent
					} else {
						assistantMsg.AssistantResponseMessage.Content = ""
					}
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

// validateAndProcessTools 验证和处理工具定义
// 参考server.py中的clean_gemini_schema函数以及Anthropic官方文档
func validateAndProcessTools(tools []types.OpenAITool) ([]types.AnthropicTool, error) {
	if len(tools) == 0 {
		return nil, nil
	}

	var anthropicTools []types.AnthropicTool
	var validationErrors []string

	for i, tool := range tools {
		if tool.Type != "function" {
			validationErrors = append(validationErrors, fmt.Sprintf("tool[%d]: 不支持的工具类型 '%s'，仅支持 'function'", i, tool.Type))
			continue
		}

		// 验证函数名称
		if tool.Function.Name == "" {
			validationErrors = append(validationErrors, fmt.Sprintf("tool[%d]: 函数名称不能为空", i))
			continue
		}

		// 验证参数schema
		if tool.Function.Parameters == nil {
			validationErrors = append(validationErrors, fmt.Sprintf("tool[%d]: 参数schema不能为空", i))
			continue
		}

		// 清理和验证参数
		cleanedParams, err := cleanAndValidateToolParameters(tool.Function.Parameters, tool.Function.Name)
		if err != nil {
			validationErrors = append(validationErrors, fmt.Sprintf("tool[%d] (%s): %v", i, tool.Function.Name, err))
			continue
		}

		anthropicTool := types.AnthropicTool{
			Name:        tool.Function.Name,
			Description: tool.Function.Description,
			InputSchema: cleanedParams,
		}
		anthropicTools = append(anthropicTools, anthropicTool)
	}

	if len(validationErrors) > 0 {
		return anthropicTools, fmt.Errorf("工具验证失败: %s", strings.Join(validationErrors, "; "))
	}

	return anthropicTools, nil
}

// cleanAndValidateToolParameters 清理和验证工具参数
func cleanAndValidateToolParameters(params map[string]any, _ string) (map[string]any, error) {
	if params == nil {
		return nil, fmt.Errorf("参数不能为nil")
	}

	// 深拷贝避免修改原始数据
	cleanedParams, _ := sonic.Marshal(params)
	var tempParams map[string]any
	if err := sonic.Unmarshal(cleanedParams, &tempParams); err != nil {
		return nil, fmt.Errorf("参数序列化失败: %v", err)
	}

	// 移除不支持的顶级字段
	delete(tempParams, "additionalProperties")
	delete(tempParams, "strict")
	delete(tempParams, "$schema")
	delete(tempParams, "$id")
	delete(tempParams, "$ref")
	delete(tempParams, "definitions")
	delete(tempParams, "$defs")

	// 验证必需的字段
	if schemaType, exists := tempParams["type"]; exists {
		if typeStr, ok := schemaType.(string); ok && typeStr == "object" {
			// 对象类型应该有properties字段
			if _, hasProps := tempParams["properties"]; !hasProps {
				return nil, fmt.Errorf("对象类型缺少properties字段")
			}
		}
	}

	return tempParams, nil
}

// convertOpenAIToolChoiceToAnthropic 将OpenAI的tool_choice转换为Anthropic格式
// 参考server.py中的转换逻辑以及Anthropic官方文档
func convertOpenAIToolChoiceToAnthropic(openaiToolChoice any) *types.ToolChoice {
	if openaiToolChoice == nil {
		return nil
	}

	switch choice := openaiToolChoice.(type) {
	case string:
		// 处理字符串类型："auto", "none", "required"
		switch choice {
		case "auto":
			return &types.ToolChoice{Type: "auto"}
		case "required", "any":
			return &types.ToolChoice{Type: "any"}
		case "none":
			// Anthropic没有"none"选项，返回nil表示不强制使用工具
			return nil
		default:
			// 未知字符串，默认为auto
			return &types.ToolChoice{Type: "auto"}
		}

	case map[string]any:
		// 处理对象类型：{"type": "function", "function": {"name": "tool_name"}}
		if choiceType, ok := choice["type"].(string); ok && choiceType == "function" {
			if functionObj, ok := choice["function"].(map[string]any); ok {
				if name, ok := functionObj["name"].(string); ok {
					return &types.ToolChoice{
						Type: "tool",
						Name: name,
					}
				}
			}
		}
		// 如果无法解析，返回auto
		return &types.ToolChoice{Type: "auto"}

	case types.OpenAIToolChoice:
		// 处理结构化的OpenAIToolChoice类型
		if choice.Type == "function" && choice.Function != nil {
			return &types.ToolChoice{
				Type: "tool",
				Name: choice.Function.Name,
			}
		}
		return &types.ToolChoice{Type: "auto"}

	default:
		// 未知类型，默认为auto
		return &types.ToolChoice{Type: "auto"}
	}
}
