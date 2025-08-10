package converter

import (
	"fmt"
	"strings"

	"kiro2api/config"
	"kiro2api/logger"
	"kiro2api/types"
	"kiro2api/utils"
)

// CodeWhisperer格式转换器

// BuildCodeWhispererRequest 构建 CodeWhisperer 请求
func BuildCodeWhispererRequest(anthropicReq types.AnthropicRequest) (types.CodeWhispererRequest, error) {
	cwReq := types.CodeWhispererRequest{}

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

		logger.Debug("工具配置完成",
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
			toolSystemPrompt := generateToolSystemPrompt(anthropicReq.ToolChoice, anthropicReq.Tools)

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
			assistantMsg.AssistantResponseMessage.ToolUses = nil
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
					assistantMsg.AssistantResponseMessage.ToolUses = nil
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

// generateToolSystemPrompt 生成工具使用的系统提示（强化工具优先策略）
func generateToolSystemPrompt(toolChoice *types.ToolChoice, tools []types.AnthropicTool) string {
	var b strings.Builder

	b.WriteString("你是一个严格遵循工具优先策略的AI助手。\n")
	b.WriteString("- 当输入中明确要求使用某个工具，或当提供的工具可以完成用户请求时，必须调用工具，而不是给出自然语言回答。\n")

	// 根据tool_choice给出更强约束
	if toolChoice != nil {
		switch toolChoice.Type {
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
