package converter

import (
	"kiro2api/types"
	"strings"
)

// SystemPromptGenerator 系统提示生成器
type SystemPromptGenerator struct{}

// NewSystemPromptGenerator 创建系统提示生成器
func NewSystemPromptGenerator() *SystemPromptGenerator {
	return &SystemPromptGenerator{}
}

// GenerateToolSystemPrompt 生成工具系统提示
func (spg *SystemPromptGenerator) GenerateToolSystemPrompt(toolChoice any, tools []types.AnthropicTool) string {
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
			required := spg.ExtractRequiredFields(t.InputSchema)
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

// ExtractRequiredFields 从输入模式中提取必填字段
func (spg *SystemPromptGenerator) ExtractRequiredFields(inputSchema map[string]interface{}) []string {
	var required []string

	if req, ok := inputSchema["required"]; ok {
		if reqSlice, ok := req.([]interface{}); ok {
			for _, item := range reqSlice {
				if str, ok := item.(string); ok {
					required = append(required, str)
				}
			}
		}
	}

	return required
}
