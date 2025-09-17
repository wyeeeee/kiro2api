package types

import (
	"io"
)

// Usage 表示API使用统计的通用结构
type Usage struct {
	PromptTokens     int `json:"prompt_tokens,omitempty"`
	CompletionTokens int `json:"completion_tokens,omitempty"`
	TotalTokens      int `json:"total_tokens,omitempty"`
	// Anthropic格式的兼容字段
	InputTokens  int `json:"input_tokens,omitempty"`
	OutputTokens int `json:"output_tokens,omitempty"`
}

// HTTPRequest HTTP请求封装
type HTTPRequest struct {
	Method  string
	URL     string
	Headers map[string]string
	Body    io.Reader
}

// HTTPResponse HTTP响应封装
type HTTPResponse struct {
	StatusCode int
	Headers    map[string][]string
	Body       io.ReadCloser
}

// ToAnthropicFormat 转换为Anthropic格式
func (u *Usage) ToAnthropicFormat() Usage {
	return Usage{
		InputTokens:  u.PromptTokens,
		OutputTokens: u.CompletionTokens,
	}
}

// ToOpenAIFormat 转换为OpenAI格式
func (u *Usage) ToOpenAIFormat() Usage {
	total := u.PromptTokens + u.CompletionTokens
	if total == 0 {
		total = u.InputTokens + u.OutputTokens
	}
	return Usage{
		PromptTokens:     u.PromptTokens + u.InputTokens,
		CompletionTokens: u.CompletionTokens + u.OutputTokens,
		TotalTokens:      total,
	}
}
