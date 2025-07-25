package types

// Usage 表示API使用统计的通用结构
type Usage struct {
	PromptTokens     int `json:"prompt_tokens,omitempty"`
	CompletionTokens int `json:"completion_tokens,omitempty"`
	TotalTokens      int `json:"total_tokens,omitempty"`
	// Anthropic格式的兼容字段
	InputTokens  int `json:"input_tokens,omitempty"`
	OutputTokens int `json:"output_tokens,omitempty"`
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

// ToolSpec 表示工具规范的通用接口
type ToolSpec interface {
	GetName() string
	GetDescription() string
	GetInputSchema() map[string]any
}

// BaseTool 通用工具的基础结构
type BaseTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"input_schema,omitempty"`
}

// GetName 实现ToolSpec接口
func (t *BaseTool) GetName() string {
	return t.Name
}

// GetDescription 实现ToolSpec接口
func (t *BaseTool) GetDescription() string {
	return t.Description
}

// GetInputSchema 实现ToolSpec接口
func (t *BaseTool) GetInputSchema() map[string]any {
	return t.InputSchema
}

// ToAnthropicTool 转换为Anthropic工具格式
func (t *BaseTool) ToAnthropicTool() AnthropicTool {
	return AnthropicTool{
		Name:        t.Name,
		Description: t.Description,
		InputSchema: t.InputSchema,
	}
}

// ToOpenAITool 转换为OpenAI工具格式
func (t *BaseTool) ToOpenAITool() OpenAITool {
	return OpenAITool{
		Type: "function",
		Function: OpenAIFunction{
			Name:        t.Name,
			Description: t.Description,
			Parameters:  t.InputSchema,
		},
	}
}
