package converter

import (
	"fmt"
	"strings"

	"kiro2api/config"
	"kiro2api/logger"
	"kiro2api/types"
	"kiro2api/utils"
)

// ValidateAssistantResponseEvent 验证助手响应事件
// ConvertToAssistantResponseEvent 转换任意数据为标准的AssistantResponseEvent
// NormalizeAssistantResponseEvent 标准化助手响应事件（填充默认值等）
// normalizeWebLinks 标准化网页链接
// normalizeReferences 标准化引用
// CodeWhisperer格式转换器

// determineChatTriggerType 智能确定聊天触发类型 (SOLID-SRP: 单一责任)
func determineChatTriggerType(anthropicReq types.AnthropicRequest) string {
	// 如果有工具调用，通常是自动触发的
	if len(anthropicReq.Tools) > 0 {
		// 检查tool_choice是否强制要求使用工具
		if anthropicReq.ToolChoice != nil {
			if tc, ok := anthropicReq.ToolChoice.(*types.ToolChoice); ok && tc != nil {
				if tc.Type == "any" || tc.Type == "tool" {
					return "AUTO" // 自动工具调用
				}
			} else if tcMap, ok := anthropicReq.ToolChoice.(map[string]any); ok {
				if tcType, exists := tcMap["type"].(string); exists {
					if tcType == "any" || tcType == "tool" {
						return "AUTO" // 自动工具调用
					}
				}
			}
		}
	}

	// 默认为手动触发
	return "MANUAL"
}

// validateCodeWhispererRequest 验证CodeWhisperer请求的完整性 (SOLID-SRP: 单一责任验证)
func validateCodeWhispererRequest(cwReq *types.CodeWhispererRequest) error {
	// 验证必需字段
	if cwReq.ConversationState.CurrentMessage.UserInputMessage.ModelId == "" {
		return fmt.Errorf("ModelId不能为空")
	}

	if cwReq.ConversationState.ConversationId == "" {
		return fmt.Errorf("ConversationId不能为空")
	}

	// 验证内容完整性 (KISS: 简化内容验证)
	trimmedContent := strings.TrimSpace(cwReq.ConversationState.CurrentMessage.UserInputMessage.Content)
	hasImages := len(cwReq.ConversationState.CurrentMessage.UserInputMessage.Images) > 0
	hasTools := len(cwReq.ConversationState.CurrentMessage.UserInputMessage.UserInputMessageContext.Tools) > 0
	hasToolResults := len(cwReq.ConversationState.CurrentMessage.UserInputMessage.UserInputMessageContext.ToolResults) > 0

	// 如果有工具结果，允许内容为空（这是工具执行后的反馈请求）
	if hasToolResults {
		logger.Debug("检测到工具结果，允许内容为空",
			logger.String("conversation_id", cwReq.ConversationState.ConversationId),
			logger.Int("tool_results_count", len(cwReq.ConversationState.CurrentMessage.UserInputMessage.UserInputMessageContext.ToolResults)))
		return nil
	}

	// 如果没有内容但有工具，注入占位内容 (YAGNI: 只在需要时处理)
	if trimmedContent == "" && !hasImages && hasTools {
		placeholder := "执行工具任务"
		cwReq.ConversationState.CurrentMessage.UserInputMessage.Content = placeholder
		logger.Warn("注入占位内容以触发工具调用",
			logger.String("conversation_id", cwReq.ConversationState.ConversationId),
			logger.Int("tools_count", len(cwReq.ConversationState.CurrentMessage.UserInputMessage.UserInputMessageContext.Tools)))
		trimmedContent = placeholder
	}

	// 验证至少有内容或图片
	if trimmedContent == "" && !hasImages {
		return fmt.Errorf("用户消息内容和图片都为空")
	}

	return nil
}

// extractToolResultsFromMessage 从消息内容中提取工具结果
func extractToolResultsFromMessage(content any) []types.ToolResult {
	var toolResults []types.ToolResult

	switch v := content.(type) {
	case []any:
		for _, item := range v {
			if block, ok := item.(map[string]any); ok {
				if blockType, exists := block["type"]; exists {
					if typeStr, ok := blockType.(string); ok && typeStr == "tool_result" {
						toolResult := types.ToolResult{}

						// 提取 tool_use_id
						if toolUseId, ok := block["tool_use_id"].(string); ok {
							toolResult.ToolUseId = toolUseId
						}

						// 提取 content - 转换为数组格式
						if content, exists := block["content"]; exists {
							// 将 content 转换为 []map[string]interface{} 格式
							var contentArray []map[string]interface{}

							// 处理不同的 content 格式
							switch c := content.(type) {
							case string:
								// 如果是字符串，包装成标准格式
								contentArray = []map[string]interface{}{
									{"text": c},
								}
							case []interface{}:
								// 如果已经是数组，保持原样
								for _, item := range c {
									if m, ok := item.(map[string]interface{}); ok {
										contentArray = append(contentArray, m)
									}
								}
							case map[string]interface{}:
								// 如果是单个对象，包装成数组
								contentArray = []map[string]interface{}{c}
							default:
								// 其他格式，尝试转换为字符串
								contentArray = []map[string]interface{}{
									{"text": fmt.Sprintf("%v", c)},
								}
							}

							toolResult.Content = contentArray
						}

						// 提取 status (默认为 success)
						toolResult.Status = "success"
						if isError, ok := block["is_error"].(bool); ok && isError {
							toolResult.Status = "error"
							toolResult.IsError = true
						}

						toolResults = append(toolResults, toolResult)

						logger.Debug("提取到工具结果",
							logger.String("tool_use_id", toolResult.ToolUseId),
							logger.String("status", toolResult.Status),
							logger.Int("content_items", len(toolResult.Content)))
					}
				}
			}
		}
	case []types.ContentBlock:
		for _, block := range v {
			if block.Type == "tool_result" {
				toolResult := types.ToolResult{}

				if block.ToolUseId != nil {
					toolResult.ToolUseId = *block.ToolUseId
				}

				// 处理 content
				if block.Content != nil {
					var contentArray []map[string]interface{}

					switch c := block.Content.(type) {
					case string:
						contentArray = []map[string]interface{}{
							{"text": c},
						}
					case []interface{}:
						for _, item := range c {
							if m, ok := item.(map[string]interface{}); ok {
								contentArray = append(contentArray, m)
							}
						}
					case map[string]interface{}:
						contentArray = []map[string]interface{}{c}
					default:
						contentArray = []map[string]interface{}{
							{"text": fmt.Sprintf("%v", c)},
						}
					}

					toolResult.Content = contentArray
				}

				// 设置 status
				toolResult.Status = "success"
				if block.IsError != nil && *block.IsError {
					toolResult.Status = "error"
					toolResult.IsError = true
				}

				toolResults = append(toolResults, toolResult)
			}
		}
	}

	return toolResults
}

// BuildCodeWhispererRequest 构建 CodeWhisperer 请求
func BuildCodeWhispererRequest(anthropicReq types.AnthropicRequest, profileArn string) (types.CodeWhispererRequest, error) {
	// logger.Debug("构建CodeWhisperer请求", logger.String("profile_arn", profileArn))

	cwReq := types.CodeWhispererRequest{}

	// 智能设置ChatTriggerType (KISS: 简化逻辑但保持准确性)
	cwReq.ConversationState.ChatTriggerType = determineChatTriggerType(anthropicReq)
	cwReq.ConversationState.ConversationId = utils.GenerateUUID()

	// 处理最后一条消息，包括图片
	if len(anthropicReq.Messages) == 0 {
		return cwReq, fmt.Errorf("消息列表为空")
	}

	lastMessage := anthropicReq.Messages[len(anthropicReq.Messages)-1]

	// 调试：记录原始消息内容
	// logger.Debug("处理用户消息",
	// 	logger.String("role", lastMessage.Role),
	// 	logger.String("content_type", fmt.Sprintf("%T", lastMessage.Content)))

	textContent, images, err := processMessageContent(lastMessage.Content)
	if err != nil {
		return cwReq, fmt.Errorf("处理消息内容失败: %v", err)
	}

	cwReq.ConversationState.CurrentMessage.UserInputMessage.Content = textContent
	// 确保Images字段始终是数组，即使为空
	if len(images) > 0 {
		cwReq.ConversationState.CurrentMessage.UserInputMessage.Images = images
	} else {
		cwReq.ConversationState.CurrentMessage.UserInputMessage.Images = []types.CodeWhispererImage{}
	}

	// 新增：检查并处理 ToolResults
	if lastMessage.Role == "user" {
		toolResults := extractToolResultsFromMessage(lastMessage.Content)
		if len(toolResults) > 0 {
			cwReq.ConversationState.CurrentMessage.UserInputMessage.UserInputMessageContext.ToolResults = toolResults

			logger.Info("已添加工具结果到请求",
				logger.Int("tool_results_count", len(toolResults)),
				logger.String("conversation_id", cwReq.ConversationState.ConversationId))

			// 对于包含 tool_result 的请求，content 应该为空字符串（符合 req2.json 的格式）
			cwReq.ConversationState.CurrentMessage.UserInputMessage.Content = ""
			logger.Debug("工具结果请求，设置 content 为空字符串")
		}
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
	cwReq.ConversationState.CurrentMessage.UserInputMessage.Origin = "AI_EDITOR" // v0.4兼容性：固定使用AI_EDITOR

	// 处理 tools 信息 - 根据req.json实际结构优化工具转换
	if len(anthropicReq.Tools) > 0 {
		// logger.Debug("开始处理工具配置",
		// 	logger.Int("tools_count", len(anthropicReq.Tools)),
		// 	logger.String("conversation_id", cwReq.ConversationState.ConversationId))

		var tools []types.CodeWhispererTool
		for i, tool := range anthropicReq.Tools {
			// 验证工具定义的完整性 (SOLID-SRP: 单一责任验证)
			if tool.Name == "" {
				logger.Warn("跳过无名称的工具", logger.Int("tool_index", i))
				continue
			}

			// logger.Debug("转换工具定义",
			// 	logger.Int("tool_index", i),
			// 	logger.String("tool_name", tool.Name),
			// logger.String("tool_description", tool.Description)
			// )

			// 根据req.json的实际结构，确保JSON Schema完整性
			cwTool := types.CodeWhispererTool{}
			cwTool.ToolSpecification.Name = tool.Name
			cwTool.ToolSpecification.Description = tool.Description

			// 直接使用原始的InputSchema，避免过度处理 (恢复v0.4兼容性)
			cwTool.ToolSpecification.InputSchema = types.InputSchema{
				Json: tool.InputSchema,
			}
			tools = append(tools, cwTool)
		}

		// 工具配置放在 UserInputMessageContext.Tools 中 (符合req.json结构)
		cwReq.ConversationState.CurrentMessage.UserInputMessage.UserInputMessageContext.Tools = tools

		// logger.Debug("工具配置已添加到UserInputMessageContext",
		// 	logger.Int("converted_tools_count", len(tools)),
		// 	logger.String("conversation_id", cwReq.ConversationState.ConversationId))

		// 记录工具名称预览
		// var toolNames []string
		// for _, t := range tools {
		// 	if t.ToolSpecification.Name != "" {
		// 		toolNames = append(toolNames, t.ToolSpecification.Name)
		// 	}
		// }
		// logger.Debug("CW工具配置",
		// 	logger.Int("tools_count", len(tools)),
		// 	logger.String("tool_names", strings.Join(toolNames, ",")))
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

		// 如果有工具，添加简化的工具使用系统提示
		if len(anthropicReq.Tools) > 0 {
			toolSystemPrompt := generateToolSystemPrompt(anthropicReq.ToolChoice, anthropicReq.Tools)

			// 只有在有其他系统内容时才添加分隔符
			if systemContentBuilder.Len() > 0 && toolSystemPrompt != "" {
				systemContentBuilder.WriteString("\n\n")
			}
			if toolSystemPrompt != "" {
				systemContentBuilder.WriteString(toolSystemPrompt)
			}
		}

		// 如果有系统内容，添加到历史记录 (恢复v0.4结构化类型)
		if systemContentBuilder.Len() > 0 {
			userMsg := types.HistoryUserMessage{}
			userMsg.UserInputMessage.Content = strings.TrimSpace(systemContentBuilder.String())
			userMsg.UserInputMessage.ModelId = modelId
			userMsg.UserInputMessage.Origin = "AI_EDITOR" // v0.4兼容性：固定使用AI_EDITOR
			history = append(history, userMsg)

			assistantMsg := types.HistoryAssistantMessage{}
			assistantMsg.AssistantResponseMessage.Content = "OK"
			assistantMsg.AssistantResponseMessage.ToolUses = nil
			history = append(history, assistantMsg)
		}

		// 然后处理常规消息历史 (恢复v0.4结构化类型)
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

				// 检查用户消息中的工具结果
				toolResults := extractToolResultsFromMessage(anthropicReq.Messages[i].Content)
				if len(toolResults) > 0 {
					userMsg.UserInputMessage.UserInputMessageContext.ToolResults = toolResults
					logger.Debug("历史用户消息包含工具结果",
						logger.Int("tool_results_count", len(toolResults)))
				}

				userMsg.UserInputMessage.ModelId = modelId
				userMsg.UserInputMessage.Origin = "AI_EDITOR" // v0.4兼容性：固定使用AI_EDITOR
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

					// 提取助手消息中的工具调用
					toolUses := extractToolUsesFromMessage(anthropicReq.Messages[i+1].Content)
					if len(toolUses) > 0 {
						assistantMsg.AssistantResponseMessage.ToolUses = toolUses
						logger.Debug("历史助手消息包含工具调用",
							logger.Int("tool_uses_count", len(toolUses)))
					} else {
						assistantMsg.AssistantResponseMessage.ToolUses = nil
					}

					history = append(history, assistantMsg)
					i++ // 跳过已处理的助手消息
				}
			}
		}

		cwReq.ConversationState.History = history
	}

	// 最终验证请求完整性 (KISS: 简化验证逻辑)
	if err := validateCodeWhispererRequest(&cwReq); err != nil {
		return cwReq, fmt.Errorf("请求验证失败: %v", err)
	}

	// logger.Debug("构建CodeWhisperer请求完成",
	// 	logger.String("conversation_id", cwReq.ConversationState.ConversationId),
	// 	logger.String("model_id", cwReq.ConversationState.CurrentMessage.UserInputMessage.ModelId),
	// 	logger.String("content_preview", func() string {
	// 		content := cwReq.ConversationState.CurrentMessage.UserInputMessage.Content
	// 		if len(content) > 100 {
	// 			return content[:100] + "..."
	// 		}
	// 		return content
	// 	}()),
	// 	logger.Int("images_count", len(cwReq.ConversationState.CurrentMessage.UserInputMessage.Images)),
	// 	logger.Int("tools_count", len(cwReq.ConversationState.CurrentMessage.UserInputMessage.UserInputMessageContext.Tools)),
	// 	logger.Int("history_count", len(cwReq.ConversationState.History)),
	// 	logger.Bool("has_tools", len(cwReq.ConversationState.CurrentMessage.UserInputMessage.UserInputMessageContext.Tools) > 0))

	return cwReq, nil
}

// generateToolSystemPrompt 生成工具使用的系统提示（强化工具优先策略）
func generateToolSystemPrompt(toolChoice any, tools []types.AnthropicTool) string {
	var b strings.Builder

	b.WriteString("你是一个严格遵循工具优先策略的AI助手。\n")
	b.WriteString("- 当输入中明确要求使用某个工具，或当提供的工具可以完成用户请求时，必须调用工具，而不是给出自然语言回答。\n")

	// 根据tool_choice给出更强约束 (支持多种类型)
	if toolChoice != nil {
		// 尝试转换为ToolChoice结构体
		if tc, ok := toolChoice.(*types.ToolChoice); ok && tc != nil {
			switch tc.Type {
			case "any", "tool":
				b.WriteString("- 当前会话工具策略: 必须使用工具完成任务。\n")
			case "auto":
				b.WriteString("- 当前会话工具策略: 优先使用工具完成任务。即使信息不完整，也应先发起工具调用。\n")
			default:
				b.WriteString("- 当前会话工具策略: 优先使用工具完成任务。\n")
			}
		} else if tcMap, ok := toolChoice.(map[string]any); ok {
			// 处理map类型的tool_choice
			if tcType, exists := tcMap["type"].(string); exists {
				switch tcType {
				case "any", "tool":
					b.WriteString("- 当前会话工具策略: 必须使用工具完成任务。\n")
				case "auto":
					b.WriteString("- 当前会话工具策略: 优先使用工具完成任务。即使信息不完整，也应先发起工具调用。\n")
				default:
					b.WriteString("- 当前会话工具策略: 优先使用工具完成任务。\n")
				}
			} else {
				b.WriteString("- 当前会话工具策略: 优先使用工具完成任务。\n")
			}
		} else {
			b.WriteString("- 当前会话工具策略: 优先使用工具完成任务。\n")
		}
	} else {
		b.WriteString("- 当前会话工具策略: 优先使用工具完成任务。\n")
	}

	b.WriteString("- 参数缺失时：仍然先发起工具调用，缺失字段可暂时留空或使用合理占位值（例如空字符串或false），随后再向用户补充询问信息。\n")
	b.WriteString("- 触发工具时：避免额外解释性文本，直接给出工具调用。\n")

	// 列出可用工具及关键必填项，帮助模型映射参数
	if len(tools) > 0 {
		b.WriteString("- 可用工具与必填参数：\n")
		for _, t := range tools {
			b.WriteString("  • ")
			b.WriteString(t.Name)
			if t.Description != "" {
				b.WriteString("：")
				b.WriteString(t.Description)
			}
			// 提取必填字段
			required := extractRequiredFields(t.InputSchema)
			if len(required) > 0 {
				b.WriteString("（必填: ")
				b.WriteString(strings.Join(required, ", "))
				b.WriteString(")")
			}
			b.WriteString("\n")
		}
	}

	return b.String()
}

// extractRequiredFields 从输入schema中提取必填字段名称
// extractToolUsesFromMessage 从助手消息内容中提取工具调用
func extractToolUsesFromMessage(content any) []types.ToolUseEntry {
	var toolUses []types.ToolUseEntry

	switch v := content.(type) {
	case []any:
		for _, item := range v {
			if block, ok := item.(map[string]any); ok {
				if blockType, exists := block["type"]; exists {
					if typeStr, ok := blockType.(string); ok && typeStr == "tool_use" {
						toolUse := types.ToolUseEntry{}

						// 提取 id 作为 ToolUseId
						if id, ok := block["id"].(string); ok {
							toolUse.ToolUseId = id
						}

						// 提取 name
						if name, ok := block["name"].(string); ok {
							toolUse.Name = name
						}

						// 提取 input
						if input, ok := block["input"].(map[string]interface{}); ok {
							toolUse.Input = input
						} else if input != nil {
							// 如果 input 不是 map，尝试转换
							toolUse.Input = map[string]interface{}{
								"value": input,
							}
						} else {
							// 如果没有 input，设置为空对象
							toolUse.Input = map[string]interface{}{}
						}

						toolUses = append(toolUses, toolUse)

						logger.Debug("提取到历史工具调用",
							logger.String("tool_id", toolUse.ToolUseId),
							logger.String("tool_name", toolUse.Name))
					}
				}
			}
		}
	case []types.ContentBlock:
		for _, block := range v {
			if block.Type == "tool_use" {
				toolUse := types.ToolUseEntry{}

				if block.ID != nil {
					toolUse.ToolUseId = *block.ID
				}

				if block.Name != nil {
					toolUse.Name = *block.Name
				}

				if block.Input != nil {
					switch inp := (*block.Input).(type) {
					case map[string]interface{}:
						toolUse.Input = inp
					default:
						toolUse.Input = map[string]interface{}{
							"value": inp,
						}
					}
				} else {
					toolUse.Input = map[string]interface{}{}
				}

				toolUses = append(toolUses, toolUse)
			}
		}
	case string:
		// 如果是纯文本，不包含工具调用
		return nil
	}

	return toolUses
}

func extractRequiredFields(schema map[string]any) []string {
	if schema == nil {
		return nil
	}
	if reqAny, ok := schema["required"]; ok && reqAny != nil {
		if arr, ok := reqAny.([]any); ok {
			fields := make([]string, 0, len(arr))
			for _, v := range arr {
				if s, ok := v.(string); ok && s != "" {
					fields = append(fields, s)
				}
			}
			return fields
		}
	}
	return nil
}
