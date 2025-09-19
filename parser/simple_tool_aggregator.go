package parser

import (
	"bytes"
	"fmt"
	"kiro2api/logger"
	"sync"
	"time"
)

// SimpleToolState 简化的工具状态 - 只有3个必要状态
type SimpleToolState int

const (
	ToolStarted SimpleToolState = iota
	ToolCollecting
	ToolCompleted
)

func (s SimpleToolState) String() string {
	states := []string{"Started", "Collecting", "Completed"}
	if int(s) < len(states) {
		return states[s]
	}
	return "Unknown"
}

// ToolData 工具数据容器 - 替代复杂的FSM状态
type ToolData struct {
	ID         string
	Name       string
	State      SimpleToolState
	Arguments  map[string]any
	StartTime  time.Time
	BlockIndex int
	Buffer     *bytes.Buffer // 用于增量数据聚合
	Error      error
}

// SimpleToolAggregator 简化的工具聚合器 - 替代ToolCallFSM
// 遵循KISS原则：只保留核心的数据聚合功能，移除复杂的状态转换逻辑
type SimpleToolAggregator struct {
	mu        sync.RWMutex
	tools     map[string]*ToolData
	nextIndex int
}

// NewSimpleToolAggregator 创建简化的工具聚合器
func NewSimpleToolAggregator() *SimpleToolAggregator {
	return &SimpleToolAggregator{
		tools: make(map[string]*ToolData),
	}
}

// StartTool 开始工具调用 - 简化版本，无复杂状态转换
func (sta *SimpleToolAggregator) StartTool(id, name string) *ToolData {
	sta.mu.Lock()
	defer sta.mu.Unlock()

	tool := &ToolData{
		ID:         id,
		Name:       name,
		State:      ToolStarted,
		Arguments:  make(map[string]any),
		StartTime:  time.Now(),
		BlockIndex: sta.nextIndex,
		Buffer:     &bytes.Buffer{},
	}

	sta.tools[id] = tool
	sta.nextIndex++

	logger.Debug("工具调用开始",
		logger.String("tool_id", id),
		logger.String("tool_name", name),
		logger.Int("block_index", tool.BlockIndex))

	return tool
}

// AddData 添加工具数据 - 顺序聚合数据，无状态验证开销
func (sta *SimpleToolAggregator) AddData(id string, data []byte) error {
	sta.mu.Lock()
	defer sta.mu.Unlock()

	tool, exists := sta.tools[id]
	if !exists {
		return fmt.Errorf("工具不存在: %s", id)
	}

	// 简单状态更新：Started -> Collecting
	if tool.State == ToolStarted {
		tool.State = ToolCollecting
	}

	// 直接聚合数据，无复杂缓冲区限制检查
	tool.Buffer.Write(data)

	logger.Debug("工具数据聚合",
		logger.String("tool_id", id),
		logger.Int("data_bytes", len(data)),
		logger.Int("total_bytes", tool.Buffer.Len()))

	return nil
}

// AddStringData 添加字符串数据 - 便捷方法
func (sta *SimpleToolAggregator) AddStringData(id string, data string) error {
	return sta.AddData(id, []byte(data))
}

// CompleteTool 完成工具调用 - 简单状态更新
func (sta *SimpleToolAggregator) CompleteTool(id string) *ToolData {
	sta.mu.Lock()
	defer sta.mu.Unlock()

	tool, exists := sta.tools[id]
	if !exists {
		return nil
	}

	tool.State = ToolCompleted
	duration := time.Since(tool.StartTime)

	logger.Info("工具调用完成",
		logger.String("tool_id", id),
		logger.String("tool_name", tool.Name),
		logger.Duration("duration", duration),
		logger.Int("total_data", tool.Buffer.Len()))

	return tool
}

// GetTool 获取工具数据
func (sta *SimpleToolAggregator) GetTool(id string) (*ToolData, bool) {
	sta.mu.RLock()
	defer sta.mu.RUnlock()

	tool, exists := sta.tools[id]
	return tool, exists
}

// GetAllTools 获取所有工具数据
func (sta *SimpleToolAggregator) GetAllTools() map[string]*ToolData {
	sta.mu.RLock()
	defer sta.mu.RUnlock()

	result := make(map[string]*ToolData)
	for id, tool := range sta.tools {
		result[id] = tool
	}
	return result
}

// GetToolsByState 按状态获取工具
func (sta *SimpleToolAggregator) GetToolsByState(state SimpleToolState) []*ToolData {
	sta.mu.RLock()
	defer sta.mu.RUnlock()

	var result []*ToolData
	for _, tool := range sta.tools {
		if tool.State == state {
			result = append(result, tool)
		}
	}
	return result
}

// UpdateArguments 更新工具参数
func (sta *SimpleToolAggregator) UpdateArguments(id string, args map[string]any) {
	sta.mu.Lock()
	defer sta.mu.Unlock()

	if tool, exists := sta.tools[id]; exists {
		for key, value := range args {
			tool.Arguments[key] = value
		}

		logger.Debug("工具参数更新",
			logger.String("tool_id", id),
			logger.Int("args_count", len(args)))
	}
}

// SetError 设置工具错误
func (sta *SimpleToolAggregator) SetError(id string, err error) {
	sta.mu.Lock()
	defer sta.mu.Unlock()

	if tool, exists := sta.tools[id]; exists {
		tool.Error = err
		tool.State = ToolCompleted // 错误也算完成状态

		logger.Error("工具调用错误",
			logger.String("tool_id", id),
			logger.String("tool_name", tool.Name),
			logger.Err(err))
	}
}

// Reset 重置聚合器 - 简单清理
func (sta *SimpleToolAggregator) Reset() {
	sta.mu.Lock()
	defer sta.mu.Unlock()

	sta.tools = make(map[string]*ToolData)
	sta.nextIndex = 0

	logger.Debug("工具聚合器已重置")
}

// GetAggregatedData 获取工具的聚合数据
func (sta *SimpleToolAggregator) GetAggregatedData(id string) ([]byte, error) {
	sta.mu.RLock()
	defer sta.mu.RUnlock()

	tool, exists := sta.tools[id]
	if !exists {
		return nil, fmt.Errorf("工具不存在: %s", id)
	}

	return tool.Buffer.Bytes(), nil
}

// GetStats 获取简单统计信息 - 无复杂指标系统
func (sta *SimpleToolAggregator) GetStats() map[string]any {
	sta.mu.RLock()
	defer sta.mu.RUnlock()

	stats := map[string]any{
		"total_tools": len(sta.tools),
		"started":     0,
		"collecting":  0,
		"completed":   0,
	}

	for _, tool := range sta.tools {
		switch tool.State {
		case ToolStarted:
			stats["started"] = stats["started"].(int) + 1
		case ToolCollecting:
			stats["collecting"] = stats["collecting"].(int) + 1
		case ToolCompleted:
			stats["completed"] = stats["completed"].(int) + 1
		}
	}

	return stats
}

// HasActiveTool 检查是否有活跃工具 - 替代复杂的状态查询
func (sta *SimpleToolAggregator) HasActiveTool() bool {
	sta.mu.RLock()
	defer sta.mu.RUnlock()

	for _, tool := range sta.tools {
		if tool.State != ToolCompleted {
			return true
		}
	}
	return false
}

// GetBlockIndex 获取工具的块索引
func (sta *SimpleToolAggregator) GetBlockIndex(id string) int {
	sta.mu.RLock()
	defer sta.mu.RUnlock()

	if tool, exists := sta.tools[id]; exists {
		return tool.BlockIndex
	}
	return -1
}
