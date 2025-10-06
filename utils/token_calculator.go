package utils

import (
	"kiro2api/types"
)

// TokenCalculator 使用 TokenEstimator 的精确算法计算 tokens
// 设计原则：
// - DRY: 复用 TokenEstimator 的精确算法，避免重复代码
// - 接口兼容: 保持原有接口签名，确保向后兼容
// - 代码重用: 内部委托给 TokenEstimator，统一算法实现
type TokenCalculator struct {
	estimator *TokenEstimator // 复用 TokenEstimator 的精确算法
}

// NewTokenCalculator 创建token计算器实例
// 内部使用 TokenEstimator 的精确算法
func NewTokenCalculator() *TokenCalculator {
	return &TokenCalculator{
		estimator: NewTokenEstimator(),
	}
}

// CalculateInputTokens 计算输入tokens
// 委托给 TokenEstimator.EstimateTokens() 实现精确计算
// 包含：系统消息 + 用户消息 + 工具定义
func (tc *TokenCalculator) CalculateInputTokens(req types.AnthropicRequest) int {
	// 转换为 CountTokensRequest 格式
	countReq := &types.CountTokensRequest{
		Model:    req.Model,
		System:   req.System,
		Messages: req.Messages,
		Tools:    req.Tools,
	}

	// 使用 TokenEstimator 的精确算法
	return tc.estimator.EstimateTokens(countReq)
}

// CalculateOutputTokens 计算输出tokens
// 使用 TokenEstimator 的文本估算算法
// 基于响应内容的字符数和结构复杂度
func (tc *TokenCalculator) CalculateOutputTokens(content string, hasTools bool) int {
	if content == "" {
		return 1 // Anthropic API最小返回1个token
	}

	// 使用 TokenEstimator 的精确文本估算算法
	baseTokens := tc.estimator.estimateTextTokens(content)

	// 如果包含工具调用，增加结构化开销
	if hasTools {
		baseTokens = int(float64(baseTokens) * 1.2) // 增加20%结构化开销
	}

	// 最小值为1
	if baseTokens < 1 {
		return 1
	}

	return baseTokens
}

// EstimateTokensFromChars 从字符数快速估算tokens（向后兼容）
// 委托给 TokenEstimator 的文本估算算法
func (tc *TokenCalculator) EstimateTokensFromChars(charCount int) int {
	// 使用简单的字符数估算（英文平均4字符/token）
	tokens := charCount / 4
	if tokens < 1 && charCount > 0 {
		return 1
	}
	return tokens
}
