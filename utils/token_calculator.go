package utils

import (
	"encoding/json"
	"regexp"
	"unicode/utf8"

	"kiro2api/types"
)

// TokenCalculator 专用于计算input和output tokens
// 遵循KISS原则，使用字符长度估算而非复杂tokenizer
type TokenCalculator struct {
	// 不同类型内容的token权重系数
	textWeight  float64 // 普通文本权重
	imageWeight float64 // 图片内容权重
	toolWeight  float64 // 工具定义权重
	jsonWeight  float64 // JSON结构权重
	baseTokens  int     // 基础token开销
}

// NewTokenCalculator 创建token计算器实例
func NewTokenCalculator() *TokenCalculator {
	return &TokenCalculator{
		textWeight:  0.25,  // 约4字符=1token
		imageWeight: 100.0, // 图片基础token较高
		toolWeight:  0.2,   // 工具定义相对紧凑
		jsonWeight:  0.3,   // JSON结构稍复杂
		baseTokens:  10,    // 每个请求的基础开销
	}
}

// CalculateInputTokens 计算输入tokens
// 包含：系统消息 + 用户消息 + 工具定义
func (tc *TokenCalculator) CalculateInputTokens(req types.AnthropicRequest) int {
	totalTokens := tc.baseTokens

	// 1. 计算系统消息tokens
	for _, sysMsg := range req.System {
		totalTokens += tc.estimateTextTokens(sysMsg.Text)
	}

	// 2. 计算所有消息tokens
	for _, msg := range req.Messages {
		totalTokens += tc.calculateMessageTokens(msg)
	}

	// 3. 计算工具定义tokens
	for _, tool := range req.Tools {
		totalTokens += tc.calculateToolTokens(tool)
	}

	// 4. 模型名称开销
	totalTokens += len(req.Model) / 10

	return totalTokens
}

// CalculateOutputTokens 计算输出tokens
// 基于响应内容的字符数和结构复杂度
func (tc *TokenCalculator) CalculateOutputTokens(content string, hasTools bool) int {
	if content == "" {
		return 1 // Anthropic API最小返回1个token
	}

	baseTokens := tc.estimateTextTokens(content)

	// 如果包含工具调用，增加结构化开销
	if hasTools || tc.containsToolStructure(content) {
		baseTokens = int(float64(baseTokens) * 1.2) // 增加20%结构化开销
	}

	// 最小值为1
	if baseTokens < 1 {
		return 1
	}

	return baseTokens
}

// calculateMessageTokens 计算单条消息的tokens
func (tc *TokenCalculator) calculateMessageTokens(msg types.AnthropicRequestMessage) int {
	tokens := 0

	switch content := msg.Content.(type) {
	case string:
		tokens += tc.estimateTextTokens(content)
	case []any:
		for _, block := range content {
			tokens += tc.calculateContentBlockTokens(block)
		}
	case []types.ContentBlock:
		for _, block := range content {
			tokens += tc.calculateTypedContentBlockTokens(block)
		}
	default:
		// 尝试JSON序列化估算
		if jsonBytes, err := json.Marshal(content); err == nil {
			tokens += int(float64(len(jsonBytes)) * tc.jsonWeight)
		}
	}

	// 角色标识开销
	tokens += 2

	return tokens
}

// calculateContentBlockTokens 计算动态内容块tokens
func (tc *TokenCalculator) calculateContentBlockTokens(block any) int {
	blockMap, ok := block.(map[string]any)
	if !ok {
		return 0
	}

	blockType, hasType := blockMap["type"].(string)
	if !hasType {
		return 0
	}

	switch blockType {
	case "text":
		if text, ok := blockMap["text"].(string); ok {
			return tc.estimateTextTokens(text)
		}
	case "image":
		return int(tc.imageWeight) // 固定图片token开销
	case "tool_use":
		return tc.calculateToolUseTokens(blockMap)
	case "tool_result":
		return tc.calculateToolResultTokens(blockMap)
	}

	return 0
}

// calculateTypedContentBlockTokens 计算类型化内容块tokens
func (tc *TokenCalculator) calculateTypedContentBlockTokens(block types.ContentBlock) int {
	switch block.Type {
	case "text":
		if block.Text != nil {
			return tc.estimateTextTokens(*block.Text)
		}
	case "image":
		return int(tc.imageWeight)
	case "tool_use":
		tokens := 10 // 基础结构开销
		if block.Name != nil {
			tokens += len(*block.Name) / 4
		}
		if block.Input != nil {
			if jsonBytes, err := json.Marshal(*block.Input); err == nil {
				tokens += int(float64(len(jsonBytes)) * tc.jsonWeight)
			}
		}
		return tokens
	case "tool_result":
		tokens := 5 // 基础结构开销
		switch content := block.Content.(type) {
		case string:
			tokens += tc.estimateTextTokens(content)
		case []any:
			if jsonBytes, err := json.Marshal(content); err == nil {
				tokens += int(float64(len(jsonBytes)) * tc.jsonWeight)
			}
		}
		return tokens
	}

	return 0
}

// calculateToolUseTokens 计算tool_use块的tokens
func (tc *TokenCalculator) calculateToolUseTokens(toolUse map[string]any) int {
	tokens := 10 // 基础结构开销

	if name, ok := toolUse["name"].(string); ok {
		tokens += len(name) / 4
	}

	if input, ok := toolUse["input"]; ok {
		if jsonBytes, err := json.Marshal(input); err == nil {
			tokens += int(float64(len(jsonBytes)) * tc.jsonWeight)
		}
	}

	return tokens
}

// calculateToolResultTokens 计算tool_result块的tokens
func (tc *TokenCalculator) calculateToolResultTokens(toolResult map[string]any) int {
	tokens := 5 // 基础结构开销

	if content, ok := toolResult["content"]; ok {
		switch c := content.(type) {
		case string:
			tokens += tc.estimateTextTokens(c)
		case []any:
			if jsonBytes, err := json.Marshal(c); err == nil {
				tokens += int(float64(len(jsonBytes)) * tc.jsonWeight)
			}
		}
	}

	return tokens
}

// calculateToolTokens 计算工具定义的tokens
func (tc *TokenCalculator) calculateToolTokens(tool types.AnthropicTool) int {
	tokens := 0

	// 工具名称和描述
	tokens += int(float64(len(tool.Name)+len(tool.Description)) * tc.toolWeight)

	// 输入schema
	if schemaBytes, err := json.Marshal(tool.InputSchema); err == nil {
		tokens += int(float64(len(schemaBytes)) * tc.jsonWeight)
	}

	return tokens
}

// estimateTextTokens 基于字符数估算文本tokens
// 考虑中英文混合的情况
func (tc *TokenCalculator) estimateTextTokens(text string) int {
	if text == "" {
		return 0
	}

	// 统计中文字符数（通常1个中文字符=1.5个token）
	chineseCount := tc.countChineseChars(text)
	// 英文字符数（通常4个字符=1个token）
	englishCount := utf8.RuneCountInString(text) - chineseCount

	// 计算混合权重
	chineseTokens := int(float64(chineseCount) * 1.5)
	englishTokens := int(float64(englishCount) * tc.textWeight)

	total := chineseTokens + englishTokens
	if total < 1 && len(text) > 0 {
		return 1
	}

	return total
}

// countChineseChars 统计中文字符数量
func (tc *TokenCalculator) countChineseChars(text string) int {
	chineseRegex := regexp.MustCompile(`[\x{4e00}-\x{9fa5}]`)
	return len(chineseRegex.FindAllString(text, -1))
}

// containsToolStructure 检查文本是否包含工具调用结构
func (tc *TokenCalculator) containsToolStructure(content string) bool {
	toolPatterns := []string{
		`"type":\s*"tool_use"`,
		`"tool_calls"`,
		`<tool_use>`,
		`</tool_use>`,
	}

	for _, pattern := range toolPatterns {
		if matched, _ := regexp.MatchString(pattern, content); matched {
			return true
		}
	}

	return false
}

// EstimateTokensFromChars 从字符数快速估算tokens（向后兼容）
func (tc *TokenCalculator) EstimateTokensFromChars(charCount int) int {
	return int(float64(charCount) * tc.textWeight)
}
