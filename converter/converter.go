package converter

import (
	"fmt"
	"strings"
	"time"

	"kiro2api/config"
	"kiro2api/logger"
	"kiro2api/types"
	"kiro2api/utils"
)

// ConvertOpenAIToAnthropic 将OpenAI请求转换为Anthropic请求
func ConvertOpenAIToAnthropic(openaiReq types.OpenAIRequest) types.AnthropicRequest {
	var anthropicMessages []types.AnthropicRequestMessage

	// 转换消息
	for _, msg := range openaiReq.Messages {
		// 转换消息内容格式
		convertedContent, err := convertOpenAIContentToAnthropic(msg.Content)
		if err != nil {
			// 如果转换失败，记录错误但使用原始内容继续处理
			convertedContent = msg.Content
		}

		anthropicMsg := types.AnthropicRequestMessage{
			Role:    msg.Role,
			Content: convertedContent,
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
									inputJson, _ := utils.SafeMarshal(input)
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
								inputJson, _ := utils.SafeMarshal(input)
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
func BuildCodeWhispererRequest(anthropicReq types.AnthropicRequest) (types.CodeWhispererRequest, error) {
	cwReq := types.CodeWhispererRequest{
		// ProfileArn: "arn:aws:codewhisperer:us-east-1:699475941385:profile/EHGA3GRVQMUK",
	}

	// // 验证必需字段
	// if cwReq.ProfileArn == "" {
	// 	return cwReq, fmt.Errorf("ProfileArn不能为空")
	// }
	cwReq.ConversationState.ChatTriggerType = "MANUAL"
	cwReq.ConversationState.ConversationId = utils.GenerateUUID()

	// 处理最后一条消息，包括图片
	if len(anthropicReq.Messages) == 0 {
		return cwReq, fmt.Errorf("消息列表为空")
	}

	lastMessage := anthropicReq.Messages[len(anthropicReq.Messages)-1]

	// 调试：记录原始消息内容
	logger.Debug("处理用户消息",
		logger.String("role", lastMessage.Role),
		logger.String("content_type", fmt.Sprintf("%T", lastMessage.Content)))

	textContent, images, err := processMessageContent(lastMessage.Content)
	if err != nil {
		return cwReq, fmt.Errorf("处理消息内容失败: %v", err)
	}

	cwReq.ConversationState.CurrentMessage.UserInputMessage.Content = textContent
	if len(images) > 0 {
		cwReq.ConversationState.CurrentMessage.UserInputMessage.Images = images
	}

	// 确保ModelId不为空，如果映射不存在则使用默认模型
	modelId := config.ModelMap[anthropicReq.Model]
	if modelId == "" {
		modelId = "CLAUDE_3_7_SONNET_20250219_V1_0" // 使用默认模型
		logger.Warn("使用默认模型，因为映射不存在",
			logger.String("requested_model", anthropicReq.Model),
			logger.String("default_model", modelId))
	}
	cwReq.ConversationState.CurrentMessage.UserInputMessage.ModelId = modelId
	cwReq.ConversationState.CurrentMessage.UserInputMessage.Origin = "AI_EDITOR"

	// 处理 tools 信息
	if len(anthropicReq.Tools) > 0 {
		logger.Debug("开始处理工具配置",
			logger.Int("tools_count", len(anthropicReq.Tools)),
			logger.String("conversation_id", cwReq.ConversationState.ConversationId))

		var tools []types.CodeWhispererTool
		for i, tool := range anthropicReq.Tools {
			logger.Debug("转换工具定义",
				logger.Int("tool_index", i),
				logger.String("tool_name", tool.Name),
				logger.String("tool_description", tool.Description))

			cwTool := types.CodeWhispererTool{}
			cwTool.ToolSpecification.Name = tool.Name
			cwTool.ToolSpecification.Description = tool.Description
			cwTool.ToolSpecification.InputSchema = types.InputSchema{
				Json: tool.InputSchema,
			}
			tools = append(tools, cwTool)
		}
		cwReq.ConversationState.CurrentMessage.UserInputMessage.UserInputMessageContext.Tools = tools

		logger.Info("工具配置完成",
			logger.Int("converted_tools_count", len(tools)),
			logger.String("conversation_id", cwReq.ConversationState.ConversationId))
	}

	// 构建历史消息
	if len(anthropicReq.System) > 0 || len(anthropicReq.Messages) > 1 || len(anthropicReq.Tools) > 0 {
		var history []any

		// 构建综合系统提示
		var systemContentBuilder strings.Builder

		// 添加原有的 system 消息
		if len(anthropicReq.System) > 0 {
			for _, sysMsg := range anthropicReq.System {
				content, err := utils.GetMessageContent(sysMsg)
				if err == nil {
					systemContentBuilder.WriteString(content)
					systemContentBuilder.WriteString("\n")
				}
			}
		}

		// 如果有工具，添加工具使用系统提示
		if len(anthropicReq.Tools) > 0 {
			toolSystemPrompt := generateToolSystemPrompt(anthropicReq.Tools)

			if systemContentBuilder.Len() > 0 {
				systemContentBuilder.WriteString("\n\n")
			}
			systemContentBuilder.WriteString(toolSystemPrompt)
		}

		// 如果有系统内容，添加到历史记录
		if systemContentBuilder.Len() > 0 {
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

				// 处理用户消息的内容和图片
				messageContent, messageImages, err := processMessageContent(anthropicReq.Messages[i].Content)
				if err == nil {
					userMsg.UserInputMessage.Content = messageContent
					if len(messageImages) > 0 {
						userMsg.UserInputMessage.Images = messageImages
					}
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

	// 最终验证请求完整性 - 恢复严格验证
	// 正常情况下内容不应该为空，如果为空说明处理过程有问题
	trimmedContent := strings.TrimSpace(cwReq.ConversationState.CurrentMessage.UserInputMessage.Content)
	if trimmedContent == "" && len(cwReq.ConversationState.CurrentMessage.UserInputMessage.Images) == 0 {
		logger.Error("用户消息内容和图片都为空，这不应该发生",
			logger.String("original_content", fmt.Sprintf("%q", cwReq.ConversationState.CurrentMessage.UserInputMessage.Content)),
			logger.Int("content_length", len(cwReq.ConversationState.CurrentMessage.UserInputMessage.Content)),
			logger.String("conversation_id", cwReq.ConversationState.ConversationId))
		return cwReq, fmt.Errorf("用户消息内容处理异常：内容和图片都为空，原始内容长度=%d", len(cwReq.ConversationState.CurrentMessage.UserInputMessage.Content))
	}

	if cwReq.ConversationState.CurrentMessage.UserInputMessage.ModelId == "" {
		return cwReq, fmt.Errorf("ModelId不能为空")
	}

	logger.Debug("构建CodeWhisperer请求完成",
		logger.String("conversation_id", cwReq.ConversationState.ConversationId),
		logger.String("model_id", cwReq.ConversationState.CurrentMessage.UserInputMessage.ModelId),
		logger.String("content_preview", func() string {
			content := cwReq.ConversationState.CurrentMessage.UserInputMessage.Content
			if len(content) > 100 {
				return content[:100] + "..."
			}
			return content
		}()),
		logger.Int("images_count", len(cwReq.ConversationState.CurrentMessage.UserInputMessage.Images)),
		logger.Int("tools_count", len(cwReq.ConversationState.CurrentMessage.UserInputMessage.UserInputMessageContext.Tools)),
		logger.Int("history_count", len(cwReq.ConversationState.History)))

	return cwReq, nil
}

// processMessageContent 处理消息内容，提取文本和图片
func processMessageContent(content any) (string, []types.CodeWhispererImage, error) {
	var textParts []string
	var images []types.CodeWhispererImage

	switch v := content.(type) {
	case string:
		// 简单字符串内容
		return v, nil, nil

	case []any:
		// 内容块数组
		for i, item := range v {
			if block, ok := item.(map[string]any); ok {
				contentBlock, err := parseContentBlock(block)
				if err != nil {
					logger.Warn("解析内容块失败，跳过", logger.Err(err), logger.Int("index", i))
					continue // 跳过无法解析的块
				}

				switch contentBlock.Type {
				case "text":
					if contentBlock.Text != nil {
						textParts = append(textParts, *contentBlock.Text)
					} else {
						logger.Warn("文本块的Text字段为nil")
					}
				case "image":
					// ... 图片处理保持不变
					if contentBlock.Source != nil {
						// 验证图片内容
						if err := utils.ValidateImageContent(contentBlock.Source); err != nil {
							return "", nil, fmt.Errorf("图片验证失败: %v", err)
						}

						// 转换为 CodeWhisperer 格式
						cwImage := utils.CreateCodeWhispererImage(contentBlock.Source)
						if cwImage != nil {
							images = append(images, *cwImage)
						}
					}
				case "tool_result":
					// 处理工具结果，支持复杂的内容结构
					if contentBlock.Content != nil {
						// 处理不同类型的tool_result内容
						switch content := contentBlock.Content.(type) {
						case string:
							// 简单字符串内容
							textParts = append(textParts, content)
						case []any:
							// 内容数组，可能包含text对象
							for _, item := range content {
								if textObj, ok := item.(map[string]any); ok {
									// 检查是否有text字段
									if text, hasText := textObj["text"].(string); hasText {
										textParts = append(textParts, text)
									}
								} else {
									// 如果不是map，直接尝试转换为字符串
									itemStr := fmt.Sprintf("%v", item)
									if itemStr != "" && itemStr != "<nil>" {
										textParts = append(textParts, itemStr)
									}
								}
							}
						case map[string]any:
							// 单个map对象，可能是嵌套的text结构
							if text, hasText := content["text"].(string); hasText {
								textParts = append(textParts, text)
							} else {
								// 尝试转换整个map为字符串
								textParts = append(textParts, fmt.Sprintf("%v", content))
							}
						default:
							// 其他类型，尝试转换为字符串
							textParts = append(textParts, fmt.Sprintf("%v", content))
						}
					}
				}
			} else {
				logger.Warn("内容块不是map[string]any类型",
					logger.Int("index", i),
					logger.String("actual_type", fmt.Sprintf("%T", item)))
			}
		}

	case []types.ContentBlock:
		// 结构化的内容块数组
		for _, block := range v {
			switch block.Type {
			case "text":
				if block.Text != nil {
					textParts = append(textParts, *block.Text)
				} else {
					logger.Warn("结构化文本块的Text字段为nil")
				}
			case "image":
				if block.Source != nil {
					// 验证图片内容
					if err := utils.ValidateImageContent(block.Source); err != nil {
						return "", nil, fmt.Errorf("图片验证失败: %v", err)
					}

					// 转换为 CodeWhisperer 格式
					cwImage := utils.CreateCodeWhispererImage(block.Source)
					if cwImage != nil {
						images = append(images, *cwImage)
					}
				}
			case "tool_result":
				// 处理工具结果，支持复杂的内容结构
				if block.Content != nil {
					// 处理不同类型的tool_result内容
					switch content := block.Content.(type) {
					case string:
						// 简单字符串内容
						textParts = append(textParts, content)
					case []any:
						// 内容数组，可能包含text对象
						for _, item := range content {
							if textObj, ok := item.(map[string]any); ok {
								// 检查是否有text字段
								if text, hasText := textObj["text"].(string); hasText {
									textParts = append(textParts, text)
								}
							} else {
								// 如果不是map，直接尝试转换为字符串
								itemStr := fmt.Sprintf("%v", item)
								if itemStr != "" && itemStr != "<nil>" {
									textParts = append(textParts, itemStr)
								}
							}
						}
					case map[string]any:
						// 单个map对象，可能是嵌套的text结构
						if text, hasText := content["text"].(string); hasText {
							textParts = append(textParts, text)
						} else {
							// 尝试转换整个map为字符串
							textParts = append(textParts, fmt.Sprintf("%v", content))
						}
					default:
						// 其他类型，尝试转换为字符串
						textParts = append(textParts, fmt.Sprintf("%v", content))
					}
				}
			}
		}

	default:
		// 尝试转换为字符串
		fallbackStr := fmt.Sprintf("%v", content)
		return fallbackStr, nil, nil
	}

	result := strings.Join(textParts, "")

	// 保留关键调试信息用于问题定位
	if result == "" && len(images) == 0 {
		logger.Debug("消息内容处理结果为空",
			logger.String("content_type", fmt.Sprintf("%T", content)),
			logger.Int("text_parts_count", len(textParts)),
			logger.Int("images_count", len(images)))
	}

	return result, images, nil
}

// parseContentBlock 解析内容块
func parseContentBlock(block map[string]any) (types.ContentBlock, error) {
	var contentBlock types.ContentBlock

	// 解析类型
	if blockType, ok := block["type"].(string); ok {
		contentBlock.Type = blockType
	} else {
		logger.Error("内容块缺少type字段或type不是字符串",
			logger.String("type_value", fmt.Sprintf("%v", block["type"])),
			logger.String("type_type", fmt.Sprintf("%T", block["type"])))
		return contentBlock, fmt.Errorf("缺少内容块类型")
	}

	// 根据类型解析不同字段
	switch contentBlock.Type {
	case "text":
		if text, ok := block["text"].(string); ok {
			contentBlock.Text = &text
		} else {
			logger.Warn("文本块缺少text字段或不是字符串",
				logger.String("text_value", fmt.Sprintf("%v", block["text"])),
				logger.String("text_type", fmt.Sprintf("%T", block["text"])))
		}

	case "image":
		if source, ok := block["source"].(map[string]any); ok {
			imageSource := &types.ImageSource{}

			if sourceType, ok := source["type"].(string); ok {
				imageSource.Type = sourceType
			}
			if mediaType, ok := source["media_type"].(string); ok {
				imageSource.MediaType = mediaType
			}
			if data, ok := source["data"].(string); ok {
				imageSource.Data = data
			}

			contentBlock.Source = imageSource
		}

	case "image_url":
		// 处理OpenAI格式的图片块，转换为Anthropic格式
		if imageURL, ok := block["image_url"].(map[string]any); ok {
			imageSource, err := utils.ConvertImageURLToImageSource(imageURL)
			if err != nil {
				return contentBlock, fmt.Errorf("转换image_url失败: %v", err)
			}
			// 将类型改为image并设置source
			contentBlock.Type = "image"
			contentBlock.Source = imageSource
		}

	case "tool_result":
		if toolUseId, ok := block["tool_use_id"].(string); ok {
			contentBlock.ToolUseId = &toolUseId
		}
		if content, ok := block["content"]; ok {
			contentBlock.Content = content
		}
		if isError, ok := block["is_error"].(bool); ok {
			contentBlock.IsError = &isError
		}

	case "tool_use":
		if id, ok := block["id"].(string); ok {
			contentBlock.ID = &id
		}
		if name, ok := block["name"].(string); ok {
			contentBlock.Name = &name
		}
		if input, ok := block["input"]; ok {
			contentBlock.Input = &input
		}
	}

	return contentBlock, nil
}

// generateToolSystemPrompt 生成工具使用的系统提示
func generateToolSystemPrompt(tools []types.AnthropicTool) string {
	prompt := "你是一个AI助手。如果用户的请求可以通过使用提供的工具来完成，你应该调用相应的工具。"
	return prompt
}

// getMapKeys 获取map的所有键，用于调试
func getMapKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
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
func cleanAndValidateToolParameters(params map[string]any, toolName string) (map[string]any, error) {
	if params == nil {
		return nil, fmt.Errorf("参数不能为nil")
	}

	// 深拷贝避免修改原始数据
	cleanedParams, _ := utils.SafeMarshal(params)
	var tempParams map[string]any
	if err := utils.SafeUnmarshal(cleanedParams, &tempParams); err != nil {
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

	// 处理超长参数名 - CodeWhisperer限制参数名长度
	if properties, ok := tempParams["properties"].(map[string]any); ok {
		cleanedProperties := make(map[string]any)
		for paramName, paramDef := range properties {
			cleanedName := paramName
			// 如果参数名超过64字符，进行简化
			if len(paramName) > 64 {
				// 保留前缀和后缀，中间用下划线连接
				if len(paramName) > 80 {
					cleanedName = paramName[:20] + "_" + paramName[len(paramName)-20:]
				} else {
					cleanedName = paramName[:30] + "_param"
				}
			}
			cleanedProperties[cleanedName] = paramDef
		}
		tempParams["properties"] = cleanedProperties

		// 同时更新required字段中的参数名
		if required, ok := tempParams["required"].([]any); ok {
			var cleanedRequired []any
			for _, req := range required {
				if reqStr, ok := req.(string); ok {
					if len(reqStr) > 64 {
						if len(reqStr) > 80 {
							cleanedRequired = append(cleanedRequired, reqStr[:20]+"_"+reqStr[len(reqStr)-20:])
						} else {
							cleanedRequired = append(cleanedRequired, reqStr[:30]+"_param")
						}
					} else {
						cleanedRequired = append(cleanedRequired, reqStr)
					}
				}
			}
			tempParams["required"] = cleanedRequired
		}
	}

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

// convertOpenAIContentToAnthropic 将OpenAI消息内容转换为Anthropic格式
func convertOpenAIContentToAnthropic(content any) (any, error) {
	switch v := content.(type) {
	case string:
		// 简单字符串内容，无需转换
		return v, nil

	case []any:
		// 内容块数组，需要转换格式
		var convertedBlocks []any

		for _, item := range v {
			if block, ok := item.(map[string]any); ok {
				convertedBlock, err := convertContentBlock(block)
				if err != nil {
					// 如果转换失败，跳过该块但继续处理其他块
					continue
				}
				convertedBlocks = append(convertedBlocks, convertedBlock)
			} else {
				// 非map类型的项目，直接保留
				convertedBlocks = append(convertedBlocks, item)
			}
		}

		return convertedBlocks, nil

	default:
		// 其他类型，直接返回
		return content, nil
	}
}

// convertContentBlock 转换单个内容块
func convertContentBlock(block map[string]any) (map[string]any, error) {
	blockType, exists := block["type"]
	if !exists {
		return block, fmt.Errorf("内容块缺少type字段")
	}

	switch blockType {
	case "text":
		// 文本块无需转换
		return block, nil

	case "image_url":
		// 将OpenAI的image_url格式转换为Anthropic的image格式
		imageURL, exists := block["image_url"]
		if !exists {
			return nil, fmt.Errorf("image_url块缺少image_url字段")
		}

		imageURLMap, ok := imageURL.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("image_url字段必须是对象")
		}

		// 使用utils包中的转换函数
		imageSource, err := utils.ConvertImageURLToImageSource(imageURLMap)
		if err != nil {
			return nil, fmt.Errorf("转换图片格式失败: %v", err)
		}

		// 构建Anthropic格式的图片块，确保source为map[string]any类型
		sourceMap := map[string]any{
			"type":       imageSource.Type,
			"media_type": imageSource.MediaType,
			"data":       imageSource.Data,
		}

		convertedBlock := map[string]any{
			"type":   "image",
			"source": sourceMap,
		}

		return convertedBlock, nil

	case "image":
		// 已经是Anthropic格式，无需转换
		return block, nil

	case "tool_result", "tool_use":
		// 工具相关块，无需转换
		return block, nil

	default:
		// 未知类型，直接返回
		return block, nil
	}
}
