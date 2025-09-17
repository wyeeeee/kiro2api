package utils

import (
	"testing"

	"kiro2api/types"
)

func TestTokenCalculator_EstimateTextTokens(t *testing.T) {
	calc := NewTokenCalculator()

	tests := []struct {
		name     string
		text     string
		expected int
		delta    int // 允许的误差范围
	}{
		{
			name:     "空文本",
			text:     "",
			expected: 0,
			delta:    0,
		},
		{
			name:     "纯英文文本",
			text:     "Hello world",
			expected: 3, // 约11字符 / 4 = 2.75 ≈ 3
			delta:    1,
		},
		{
			name:     "纯中文文本",
			text:     "你好世界",
			expected: 6, // 4个中文字符 * 1.5 = 6
			delta:    1,
		},
		{
			name:     "中英混合",
			text:     "Hello 你好",
			expected: 4, // "Hello " (6字符*0.25=1.5≈2) + "你好" (2中文*1.5=3) = 5
			delta:    2,
		},
		{
			name:     "长英文文本",
			text:     "This is a long English text for testing token calculation accuracy.",
			expected: 16, // 约65字符 / 4 = 16.25 ≈ 16
			delta:    2,
		},
		{
			name:     "技术文档样本",
			text:     "在 /v1/messages 流式和非流返回中增加 input_tokens 和 output_tokens 的计算",
			expected: 25, // 混合中英文技术文档
			delta:    5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := calc.estimateTextTokens(tt.text)
			if abs(result-tt.expected) > tt.delta {
				t.Errorf("estimateTextTokens() = %v, expected %v (±%v)", result, tt.expected, tt.delta)
			}
			t.Logf("Text: %q, Tokens: %d, Expected: %d±%d", tt.text, result, tt.expected, tt.delta)
		})
	}
}

func TestTokenCalculator_CalculateInputTokens(t *testing.T) {
	calc := NewTokenCalculator()

	tests := []struct {
		name     string
		req      types.AnthropicRequest
		expected int
		delta    int
	}{
		{
			name: "简单文本请求",
			req: types.AnthropicRequest{
				Model:     "claude-sonnet-4-20250514",
				MaxTokens: 100,
				Messages: []types.AnthropicRequestMessage{
					{
						Role:    "user",
						Content: "你好",
					},
				},
			},
			expected: 15, // 基础开销10 + 角色2 + 中文3 ≈ 15
			delta:    5,
		},
		{
			name: "包含系统消息的请求",
			req: types.AnthropicRequest{
				Model:     "claude-sonnet-4-20250514",
				MaxTokens: 200,
				System: []types.AnthropicSystemMessage{
					{
						Type: "text",
						Text: "You are a helpful assistant.",
					},
				},
				Messages: []types.AnthropicRequestMessage{
					{
						Role:    "user",
						Content: "Hello world",
					},
				},
			},
			expected: 25, // 基础10 + 系统消息7 + 用户消息5 ≈ 22
			delta:    8,
		},
		{
			name: "包含工具的请求",
			req: types.AnthropicRequest{
				Model:     "claude-sonnet-4-20250514",
				MaxTokens: 300,
				Messages: []types.AnthropicRequestMessage{
					{
						Role:    "user",
						Content: "请帮我写代码",
					},
				},
				Tools: []types.AnthropicTool{
					{
						Name:        "Write",
						Description: "Write content to a file",
						InputSchema: map[string]any{
							"type": "object",
							"properties": map[string]any{
								"file_path": map[string]any{"type": "string"},
								"content":   map[string]any{"type": "string"},
							},
						},
					},
				},
			},
			expected: 35, // 基础开销 + 消息 + 工具定义
			delta:    10,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := calc.CalculateInputTokens(tt.req)
			if abs(result-tt.expected) > tt.delta {
				t.Errorf("CalculateInputTokens() = %v, expected %v (±%v)", result, tt.expected, tt.delta)
			}
			t.Logf("Input tokens: %d, Expected: %d±%d", result, tt.expected, tt.delta)
		})
	}
}

func TestTokenCalculator_CalculateOutputTokens(t *testing.T) {
	calc := NewTokenCalculator()

	tests := []struct {
		name     string
		content  string
		hasTools bool
		expected int
		delta    int
	}{
		{
			name:     "空响应",
			content:  "",
			hasTools: false,
			expected: 1, // 最小值
			delta:    0,
		},
		{
			name:     "简单文本响应",
			content:  "好的，我来帮你完成这个任务。",
			hasTools: false,
			expected: 15, // 约15个中文字符 * 1.5 = 22.5
			delta:    5,
		},
		{
			name:     "包含工具调用的响应",
			content:  "我将使用Write工具来创建文件。",
			hasTools: true,
			expected: 20, // 文本token + 20%工具开销
			delta:    8,
		},
		{
			name:     "长文本响应",
			content:  "根据你的需求，我建议使用以下方案来实现token计算功能：首先创建TokenCalculator结构体，然后实现输入和输出token的精确计算逻辑，最后集成到现有的API响应中。",
			hasTools: false,
			expected: 85, // 长中文文本
			delta:    15,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := calc.CalculateOutputTokens(tt.content, tt.hasTools)
			if abs(result-tt.expected) > tt.delta {
				t.Errorf("CalculateOutputTokens() = %v, expected %v (±%v)", result, tt.expected, tt.delta)
			}
			t.Logf("Content: %q, Tokens: %d, Expected: %d±%d", tt.content, result, tt.expected, tt.delta)
		})
	}
}

func TestTokenCalculator_ContentBlockTokens(t *testing.T) {
	calc := NewTokenCalculator()

	// 测试不同类型的内容块
	textBlock := map[string]any{
		"type": "text",
		"text": "这是一个文本块",
	}

	toolUseBlock := map[string]any{
		"type": "tool_use",
		"name": "Write",
		"input": map[string]any{
			"file_path": "/tmp/test.txt",
			"content":   "Hello world",
		},
	}

	imageBlock := map[string]any{
		"type": "image",
	}

	tests := []struct {
		name     string
		block    any
		expected int
		delta    int
	}{
		{
			name:     "文本块",
			block:    textBlock,
			expected: 11, // 约7个中文字符 * 1.5 = 10.5
			delta:    3,
		},
		{
			name:     "工具使用块",
			block:    toolUseBlock,
			expected: 25, // 基础10 + 工具名1 + JSON输入约14
			delta:    8,
		},
		{
			name:     "图片块",
			block:    imageBlock,
			expected: 100, // 固定图片权重
			delta:    0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := calc.calculateContentBlockTokens(tt.block)
			if abs(result-tt.expected) > tt.delta {
				t.Errorf("calculateContentBlockTokens() = %v, expected %v (±%v)", result, tt.expected, tt.delta)
			}
			t.Logf("Block tokens: %d, Expected: %d±%d", result, tt.expected, tt.delta)
		})
	}
}

func TestTokenCalculator_ChineseCharCounting(t *testing.T) {
	calc := NewTokenCalculator()

	tests := []struct {
		name     string
		text     string
		expected int
	}{
		{
			name:     "纯英文",
			text:     "Hello World",
			expected: 0,
		},
		{
			name:     "纯中文",
			text:     "你好世界",
			expected: 4,
		},
		{
			name:     "中英混合",
			text:     "Hello 你好 World",
			expected: 2,
		},
		{
			name:     "包含标点符号",
			text:     "你好，世界！",
			expected: 4, // 中文标点不算中文字符
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := calc.countChineseChars(tt.text)
			if result != tt.expected {
				t.Errorf("countChineseChars() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

func TestTokenCalculator_ToolStructureDetection(t *testing.T) {
	calc := NewTokenCalculator()

	tests := []struct {
		name     string
		content  string
		expected bool
	}{
		{
			name:     "无工具结构",
			content:  "这是一个普通的文本响应。",
			expected: false,
		},
		{
			name:     "包含JSON工具调用",
			content:  `{"type": "tool_use", "name": "Write"}`,
			expected: true,
		},
		{
			name:     "包含XML工具调用",
			content:  "<tool_use>Write</tool_use>",
			expected: true,
		},
		{
			name:     "包含OpenAI格式",
			content:  `{"tool_calls": []}`,
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := calc.containsToolStructure(tt.content)
			if result != tt.expected {
				t.Errorf("containsToolStructure() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

// 辅助函数：计算绝对值
func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// 基准测试
func BenchmarkTokenCalculator_EstimateTextTokens(b *testing.B) {
	calc := NewTokenCalculator()
	text := "这是一个用于测试token计算性能的中英文混合文本 This is a mixed Chinese-English text for testing token calculation performance."

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		calc.estimateTextTokens(text)
	}
}

func BenchmarkTokenCalculator_CalculateInputTokens(b *testing.B) {
	calc := NewTokenCalculator()
	req := types.AnthropicRequest{
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 1000,
		Messages: []types.AnthropicRequestMessage{
			{
				Role:    "user",
				Content: "请帮我实现一个token计算器，需要支持中英文混合文本的精确计算。",
			},
		},
		Tools: []types.AnthropicTool{
			{
				Name:        "Write",
				Description: "Write content to a file",
				InputSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"file_path": map[string]any{"type": "string"},
						"content":   map[string]any{"type": "string"},
					},
				},
			},
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		calc.CalculateInputTokens(req)
	}
}
