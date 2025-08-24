package utils

import (
	"time"
)

// ToolDedupManager 管理工具调用去重的结构
// 使用 tool_use_id 进行去重，符合 Anthropic 标准
type ToolDedupManager struct {
	// 请求级别独立实例 - 无需并发保护
	processedIds map[string]bool
	executing    map[string]bool      
	lastAccess   map[string]time.Time 
	// 移除所有锁 - 每个HTTP请求有独立实例
}

// NewToolDedupManager 创建新的工具去重管理器
func NewToolDedupManager() *ToolDedupManager {
	return &ToolDedupManager{
		processedIds: make(map[string]bool),
		executing:    make(map[string]bool),
		lastAccess:   make(map[string]time.Time),
		// 无锁设计 - 请求级别独立实例
	}
}

// IsToolProcessed 检查工具是否已被处理（基于 tool_use_id）
func (m *ToolDedupManager) IsToolProcessed(toolUseId string) bool {
	if toolUseId == "" {
		return false
	}
	// 无锁访问 - 请求级别独立实例
	return m.processedIds[toolUseId]
}

// IsToolExecuting 检查工具是否正在执行
func (m *ToolDedupManager) IsToolExecuting(toolUseId string) bool {
	if toolUseId == "" {
		return false
	}
	// 无锁访问 - 请求级别独立实例
	return m.executing[toolUseId]
}

// StartToolExecution 标记工具开始执行
func (m *ToolDedupManager) StartToolExecution(toolUseId string) bool {
	if toolUseId == "" {
		return false
	}

	// 无锁检查和设置 - 请求级别独立实例
	if m.executing[toolUseId] {
		return false // 已经在执行
	}

	// 标记为正在执行
	m.executing[toolUseId] = true
	m.lastAccess[toolUseId] = time.Now()
	return true
}

// MarkToolProcessed 标记工具为已处理（基于 tool_use_id）
func (m *ToolDedupManager) MarkToolProcessed(toolUseId string) {
	if toolUseId != "" {
		// 无锁操作 - 请求级别独立实例
		m.processedIds[toolUseId] = true
		// 清除执行状态
		delete(m.executing, toolUseId)
		m.lastAccess[toolUseId] = time.Now()
	}
}

// Reset 重置去重管理器状态
func (m *ToolDedupManager) Reset() {
	// 无锁重置 - 请求级别独立实例  
	m.processedIds = make(map[string]bool)
	m.executing = make(map[string]bool)
	m.lastAccess = make(map[string]time.Time)
}

// CleanupExpiredTools 清理过期的工具状态（超过指定时间的工具）
func (m *ToolDedupManager) CleanupExpiredTools(expireDuration time.Duration) {
	// 无锁清理 - 请求级别独立实例，无需保护
	now := time.Now()
	for toolId, lastTime := range m.lastAccess {
		if now.Sub(lastTime) > expireDuration {
			delete(m.processedIds, toolId)
			delete(m.executing, toolId)
			delete(m.lastAccess, toolId)
		}
	}
}
