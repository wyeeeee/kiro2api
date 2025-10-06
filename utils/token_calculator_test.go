package utils

import (
	"kiro2api/types"
	"testing"
)

// TestTokenCalculatorBasic 测试基本功能
func TestTokenCalculatorBasic(t *testing.T) {
	calc := NewTokenCalculator()

	// 测试1: 简单英文文本
	req1 := types.AnthropicRequest{
		Model: "claude-sonnet-4-20250514",
		Messages: []types.AnthropicRequestMessage{
			{
				Role:    "user",
				Content: "Hello",
			},
		},
	}
	tokens1 := calc.CalculateInputTokens(req1)
	if tokens1 < 10 || tokens1 > 30 {
		t.Errorf("简单英文文本token数异常: got %d, expected 10-30", tokens1)
	}
	t.Logf("简单英文文本 'Hello': %d tokens", tokens1)

	// 测试2: 中文文本
	req2 := types.AnthropicRequest{
		Model: "claude-sonnet-4-20250514",
		Messages: []types.AnthropicRequestMessage{
			{
				Role:    "user",
				Content: "你好，今天天气怎么样？",
			},
		},
	}
	tokens2 := calc.CalculateInputTokens(req2)
	if tokens2 < 15 || tokens2 > 35 {
		t.Errorf("中文文本token数异常: got %d, expected 15-35", tokens2)
	}
	t.Logf("中文文本 '你好，今天天气怎么样？': %d tokens", tokens2)

	// 测试3: 包含系统提示词
	req3 := types.AnthropicRequest{
		Model: "claude-sonnet-4-20250514",
		System: []types.AnthropicSystemMessage{
			{Text: "You are a helpful assistant"},
		},
		Messages: []types.AnthropicRequestMessage{
			{
				Role:    "user",
				Content: "Hello",
			},
		},
	}
	tokens3 := calc.CalculateInputTokens(req3)
	if tokens3 < 20 || tokens3 > 50 {
		t.Errorf("包含系统提示词token数异常: got %d, expected 20-50", tokens3)
	}
	t.Logf("包含系统提示词: %d tokens", tokens3)
}

// TestTokenCalculatorWithTools 测试工具定义计算（关键测试）
func TestTokenCalculatorWithTools(t *testing.T) {
	calc := NewTokenCalculator()

	// 测试单个工具定义（应该约516 tokens，使用TokenEstimator的精确算法）
	req := types.AnthropicRequest{
		Model: "claude-sonnet-4-20250514",
		Messages: []types.AnthropicRequestMessage{
			{
				Role:    "user",
				Content: "Hello",
			},
		},
		Tools: []types.AnthropicTool{
			{
				Name:        "get_weather",
				Description: "Get the current weather in a given location",
				InputSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"location": map[string]any{
							"type":        "string",
							"description": "The city and state, e.g. San Francisco, CA",
						},
						"unit": map[string]any{
							"type": "string",
							"enum": []string{"celsius", "fahrenheit"},
						},
					},
					"required": []string{"location"},
				},
			},
		},
	}

	tokens := calc.CalculateInputTokens(req)

	// 使用TokenEstimator算法后，单工具应该在450-550范围内
	if tokens < 450 || tokens > 600 {
		t.Errorf("单工具定义token数异常: got %d, expected 450-600 (实测约516)", tokens)
	}
	t.Logf("单工具定义: %d tokens (预期约516)", tokens)
}

// TestTokenCalculatorOutputTokens 测试输出token计算
func TestTokenCalculatorOutputTokens(t *testing.T) {
	calc := NewTokenCalculator()

	// 测试1: 空内容
	tokens1 := calc.CalculateOutputTokens("", false)
	if tokens1 != 1 {
		t.Errorf("空内容应返回1 token, got %d", tokens1)
	}

	// 测试2: 简单文本
	tokens2 := calc.CalculateOutputTokens("Hello, how are you?", false)
	if tokens2 < 3 || tokens2 > 10 {
		t.Errorf("简单文本token数异常: got %d, expected 3-10", tokens2)
	}
	t.Logf("简单输出文本: %d tokens", tokens2)

	// 测试3: 包含工具调用（增加20%开销）- 使用更长的文本以确保能看到差异
	longText := "This is a longer response that includes tool usage information and should have enough tokens to show the 20% overhead clearly."
	tokens3Base := calc.CalculateOutputTokens(longText, false)
	tokens3WithTools := calc.CalculateOutputTokens(longText, true)
	if tokens3WithTools <= tokens3Base {
		t.Errorf("包含工具调用应该有更多tokens: got %d, expected > %d", tokens3WithTools, tokens3Base)
	}
	t.Logf("包含工具调用的输出: %d tokens (基础: %d, 增加: %.1f%%)", tokens3WithTools, tokens3Base, float64(tokens3WithTools-tokens3Base)/float64(tokens3Base)*100)
}

// TestTokenCalculatorEstimateFromChars 测试字符数估算
func TestTokenCalculatorEstimateFromChars(t *testing.T) {
	calc := NewTokenCalculator()

	// 测试1: 100字符
	tokens1 := calc.EstimateTokensFromChars(100)
	if tokens1 != 25 {
		t.Errorf("100字符应约25 tokens, got %d", tokens1)
	}

	// 测试2: 0字符
	tokens2 := calc.EstimateTokensFromChars(0)
	if tokens2 != 0 {
		t.Errorf("0字符应返回0 tokens, got %d", tokens2)
	}

	// 测试3: 1字符
	tokens3 := calc.EstimateTokensFromChars(1)
	if tokens3 != 1 {
		t.Errorf("1字符应返回1 token, got %d", tokens3)
	}
}

// TestTokenCalculatorAccuracy 精确度对比测试
func TestTokenCalculatorAccuracy(t *testing.T) {
	calc := NewTokenCalculator()
	estimator := NewTokenEstimator()

	// 创建相同的请求
	req := types.AnthropicRequest{
		Model: "claude-sonnet-4-20250514",
		System: []types.AnthropicSystemMessage{
			{Text: "You are a helpful assistant"},
		},
		Messages: []types.AnthropicRequestMessage{
			{
				Role:    "user",
				Content: "你好，今天天气怎么样？",
			},
		},
		Tools: []types.AnthropicTool{
			{
				Name:        "get_weather",
				Description: "Get the current weather",
				InputSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"location": map[string]any{
							"type": "string",
						},
					},
				},
			},
		},
	}

	// TokenCalculator 结果
	calcTokens := calc.CalculateInputTokens(req)

	// TokenEstimator 结果
	countReq := &types.CountTokensRequest{
		Model:    req.Model,
		System:   req.System,
		Messages: req.Messages,
		Tools:    req.Tools,
	}
	estimatorTokens := estimator.EstimateTokens(countReq)

	// 两者应该完全一致（因为TokenCalculator内部使用TokenEstimator）
	if calcTokens != estimatorTokens {
		t.Errorf("TokenCalculator和TokenEstimator结果不一致: calc=%d, estimator=%d", calcTokens, estimatorTokens)
	}

	t.Logf("✅ TokenCalculator和TokenEstimator结果一致: %d tokens", calcTokens)
}
