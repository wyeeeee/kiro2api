package utils

// ToolDedupManager 管理工具调用去重的结构
// 使用 tool_use_id 进行去重，符合 Anthropic 标准
type ToolDedupManager struct {
	processedIds map[string]bool
}

// NewToolDedupManager 创建新的工具去重管理器
func NewToolDedupManager() *ToolDedupManager {
	return &ToolDedupManager{
		processedIds: make(map[string]bool),
	}
}

// IsToolProcessed 检查工具是否已被处理（基于 tool_use_id）
func (m *ToolDedupManager) IsToolProcessed(toolUseId string) bool {
	if toolUseId == "" {
		return false
	}
	return m.processedIds[toolUseId]
}

// MarkToolProcessed 标记工具为已处理（基于 tool_use_id）
func (m *ToolDedupManager) MarkToolProcessed(toolUseId string) {
	if toolUseId != "" {
		m.processedIds[toolUseId] = true
	}
}

// Reset 重置去重管理器状态
func (m *ToolDedupManager) Reset() {
	m.processedIds = make(map[string]bool)
}
