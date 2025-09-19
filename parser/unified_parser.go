package parser

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"kiro2api/logger"
	"kiro2api/utils"
	"strings"
	"sync"
	"time"

	"github.com/bytedance/sonic"
)

// UnifiedParser 统一的事件流解析器，遵循KISS原则
// 合并所有解析功能到单一、清晰的实现中
type UnifiedParser struct {
	// 基础配置
	strictMode bool
	maxErrors  int
	timeout    time.Duration

	// 状态管理（使用对象池优化）
	toolStates   map[string]*ToolExecution
	activeBlocks map[int]*UnifiedBlockState
	mu           sync.RWMutex

	// 统计信息
	stats *ParseStats

	// 错误处理
	errorCount int
	lastError  error
}


// UnifiedBlockState 简化的块状态
type UnifiedBlockState struct {
	Index   int       `json:"index"`
	Type    string    `json:"type"`
	Started bool      `json:"started"`
	Stopped bool      `json:"stopped"`
	Content strings.Builder `json:"-"` // 不序列化，内容缓冲区
}

// ParseStats 解析统计信息
type ParseStats struct {
	EventsProcessed int           `json:"events_processed"`
	ToolsExecuted   int           `json:"tools_executed"`
	ParseDuration   time.Duration `json:"parse_duration"`
	ErrorCount      int           `json:"error_count"`
	BytesProcessed  int           `json:"bytes_processed"`
}


// NewUnifiedParser 创建统一解析器
func NewUnifiedParser(strictMode bool) *UnifiedParser {
	return &UnifiedParser{
		strictMode:   strictMode,
		maxErrors:    10, // 默认最大错误数
		timeout:      30 * time.Second,
		toolStates:   make(map[string]*ToolExecution),
		activeBlocks: make(map[int]*UnifiedBlockState),
		stats:        &ParseStats{},
	}
}

// ParseStream 解析事件流（简化版本，合并原有的多个解析方法）
func (up *UnifiedParser) ParseStream(data []byte) ([]SSEEvent, error) {
	start := time.Now()
	defer func() {
		up.stats.ParseDuration = time.Since(start)
		up.stats.BytesProcessed += len(data)
	}()

	if len(data) == 0 {
		return nil, nil
	}

	// 使用对象池优化内存分配
	events := utils.GetStringSlice()
	defer utils.PutStringSlice(events)

	// 简化的EventStream解析逻辑
	reader := bytes.NewReader(data)
	var results []SSEEvent

	for reader.Len() > 0 {
		event, err := up.parseNextEvent(reader)
		if err != nil {
			up.handleError(err)
			if up.strictMode || up.errorCount > up.maxErrors {
				return nil, fmt.Errorf("解析失败: %w", err)
			}
			continue
		}

		if event != nil {
			results = append(results, *event)
			up.stats.EventsProcessed++
		}
	}

	return results, nil
}

// parseNextEvent 解析下一个事件（简化的实现）
func (up *UnifiedParser) parseNextEvent(reader *bytes.Reader) (*SSEEvent, error) {
	// 简化的AWS EventStream格式解析
	// 原来分散在多个文件中的解析逻辑，现在统一到这里

	// 1. 读取消息长度（4字节，大端序）
	var messageLength uint32
	if err := binary.Read(reader, binary.BigEndian, &messageLength); err != nil {
		return nil, fmt.Errorf("读取消息长度失败: %w", err)
	}

	if messageLength > 1024*1024 { // 1MB限制
		return nil, fmt.Errorf("消息过大: %d bytes", messageLength)
	}

	// 2. 读取消息数据
	messageData := make([]byte, messageLength-4) // 减去长度字段本身
	if _, err := reader.Read(messageData); err != nil {
		return nil, fmt.Errorf("读取消息数据失败: %w", err)
	}

	// 3. 解析消息内容
	return up.parseEventMessage(messageData)
}

// parseEventMessage 解析事件消息内容
func (up *UnifiedParser) parseEventMessage(data []byte) (*SSEEvent, error) {
	// 简化的JSON事件解析
	var eventData map[string]interface{}
	if err := sonic.Unmarshal(data, &eventData); err != nil {
		// 尝试作为纯文本处理
		return &SSEEvent{
			Event: "text",
			Data:  string(data),
		}, nil
	}

	// 提取事件类型
	eventType, _ := eventData["type"].(string)
	if eventType == "" {
		eventType = "unknown"
	}

	// 处理特定事件类型
	switch eventType {
	case "content_block_start":
		up.handleContentBlockStart(eventData)
	case "content_block_delta":
		up.handleContentBlockDelta(eventData)
	case "content_block_stop":
		up.handleContentBlockStop(eventData)
	case "tool_use":
		up.handleToolUse(eventData)
	}

	return &SSEEvent{
		Event: eventType,
		Data:  eventData,
	}, nil
}

// handleContentBlockStart 处理内容块开始
func (up *UnifiedParser) handleContentBlockStart(data map[string]interface{}) {
	up.mu.Lock()
	defer up.mu.Unlock()

	index, _ := data["index"].(float64)
	blockIndex := int(index)

	up.activeBlocks[blockIndex] = &UnifiedBlockState{
		Index:   blockIndex,
		Type:    "content",
		Started: true,
	}

	logger.Debug("内容块开始", logger.Int("index", blockIndex))
}

// handleContentBlockDelta 处理内容块增量
func (up *UnifiedParser) handleContentBlockDelta(data map[string]interface{}) {
	up.mu.Lock()
	defer up.mu.Unlock()

	index, _ := data["index"].(float64)
	blockIndex := int(index)

	block, exists := up.activeBlocks[blockIndex]
	if !exists {
		logger.Warn("收到未知块的增量", logger.Int("index", blockIndex))
		return
	}

	// 处理文本增量
	if delta, ok := data["delta"].(map[string]interface{}); ok {
		if text, ok := delta["text"].(string); ok {
			block.Content.WriteString(text)
		}
	}
}

// handleContentBlockStop 处理内容块结束
func (up *UnifiedParser) handleContentBlockStop(data map[string]interface{}) {
	up.mu.Lock()
	defer up.mu.Unlock()

	index, _ := data["index"].(float64)
	blockIndex := int(index)

	if block, exists := up.activeBlocks[blockIndex]; exists {
		block.Stopped = true
		logger.Debug("内容块结束",
			logger.Int("index", blockIndex),
			logger.Int("content_length", block.Content.Len()))
	}
}

// handleToolUse 处理工具调用
func (up *UnifiedParser) handleToolUse(data map[string]interface{}) {
	up.mu.Lock()
	defer up.mu.Unlock()

	toolID, _ := data["id"].(string)
	toolName, _ := data["name"].(string)
	arguments, _ := data["input"].(map[string]interface{})

	if toolID == "" {
		logger.Warn("工具调用缺少ID")
		return
	}

	up.toolStates[toolID] = &ToolExecution{
		ID:        toolID,
		Name:      toolName,
		Status:    ToolStatusRunning, // 使用现有常量
		Arguments: arguments,
		StartTime: time.Now(),
	}

	up.stats.ToolsExecuted++
	logger.Debug("工具调用开始",
		logger.String("tool_id", toolID),
		logger.String("tool_name", toolName))
}

// handleError 处理错误
func (up *UnifiedParser) handleError(err error) {
	up.errorCount++
	up.lastError = err
	up.stats.ErrorCount++

	logger.Warn("解析器错误",
		logger.Err(err),
		logger.Int("error_count", up.errorCount))
}

// GetCompletionText 获取完整的文本内容
func (up *UnifiedParser) GetCompletionText() string {
	up.mu.RLock()
	defer up.mu.RUnlock()

	var result strings.Builder
	for _, block := range up.activeBlocks {
		result.WriteString(block.Content.String())
	}
	return result.String()
}

// GetToolStates 获取工具状态
func (up *UnifiedParser) GetToolStates() map[string]*ToolExecution {
	up.mu.RLock()
	defer up.mu.RUnlock()

	// 返回副本，避免并发问题
	result := make(map[string]*ToolExecution)
	for k, v := range up.toolStates {
		result[k] = v
	}
	return result
}

// GetStats 获取解析统计
func (up *UnifiedParser) GetStats() *ParseStats {
	return up.stats
}

// Reset 重置解析器状态
func (up *UnifiedParser) Reset() {
	up.mu.Lock()
	defer up.mu.Unlock()

	up.toolStates = make(map[string]*ToolExecution)
	up.activeBlocks = make(map[int]*UnifiedBlockState)
	up.errorCount = 0
	up.lastError = nil
	up.stats = &ParseStats{}
}

// SetStrictMode 设置严格模式
func (up *UnifiedParser) SetStrictMode(strict bool) {
	up.strictMode = strict
}

// SetMaxErrors 设置最大错误数
func (up *UnifiedParser) SetMaxErrors(maxErrors int) {
	up.maxErrors = maxErrors
}

// ParseResponse 解析完整响应（兼容现有接口）
func (up *UnifiedParser) ParseResponse(data []byte) (*ParseResult, error) {
	events, err := up.ParseStream(data)

	// 创建兼容的结果结构
	compatibleResult := &ParseResult{
		Messages:       []*EventStreamMessage{}, // 空消息列表
		Events:         events,
		ToolExecutions: up.GetToolStates(),
		ActiveTools:    up.GetActiveTools(),
		SessionInfo:    SessionInfo{}, // 空会话信息
		Summary: &ParseSummary{
			TotalMessages:    0, // 统一解析器不处理原始消息
			TotalEvents:      len(events),
			MessageTypes:     make(map[string]int),
			EventTypes:       make(map[string]int),
			HasToolCalls:     len(up.toolStates) > 0,
			HasCompletions:   true,
			HasErrors:        up.stats.ErrorCount > 0,
			HasSessionEvents: false,
			ToolSummary:      make(map[string]any),
		},
		Errors: []error{},
	}

	// 统计事件类型
	for _, event := range events {
		compatibleResult.Summary.EventTypes[event.Event]++
	}

	if err != nil {
		compatibleResult.Errors = append(compatibleResult.Errors, err)
	}

	return compatibleResult, err
}

// convertToCompatibleToolCalls 转换为兼容的工具调用格式
func (up *UnifiedParser) convertToCompatibleToolCalls() []*ToolExecution {
	up.mu.RLock()
	defer up.mu.RUnlock()

	var result []*ToolExecution
	for _, state := range up.toolStates {
		result = append(result, &ToolExecution{
			ID:        state.ID,
			Name:      state.Name,
			Status:    state.Status,
			Arguments: state.Arguments,
		})
	}
	return result
}

// GetCompletionBuffer 获取完成缓冲区（兼容现有接口）
func (up *UnifiedParser) GetCompletionBuffer() string {
	return up.GetCompletionText()
}

// GetToolManager 获取工具管理器（兼容现有接口）
func (up *UnifiedParser) GetToolManager() *ToolManager {
	return &ToolManager{
		activeTools:    up.GetActiveTools(),
		completedTools: up.GetCompletedTools(),
	}
}

// GetActiveTools 获取活跃工具（返回map格式兼容ParseResult）
func (up *UnifiedParser) GetActiveTools() map[string]*ToolExecution {
	up.mu.RLock()
	defer up.mu.RUnlock()

	result := make(map[string]*ToolExecution)
	for id, state := range up.toolStates {
		if state.Status == ToolStatusRunning {
			result[id] = state
		}
	}
	return result
}

// GetCompletedTools 获取已完成工具（返回map格式兼容ParseResult）
func (up *UnifiedParser) GetCompletedTools() map[string]*ToolExecution {
	up.mu.RLock()
	defer up.mu.RUnlock()

	result := make(map[string]*ToolExecution)
	for id, state := range up.toolStates {
		if state.Status == ToolStatusCompleted || state.Status == ToolStatusError {
			result[id] = state
		}
	}
	return result
}

// ToolManager 兼容性工具管理器
type ToolManager struct {
	activeTools    map[string]*ToolExecution
	completedTools map[string]*ToolExecution
}

func (tm *ToolManager) GetActiveTools() map[string]*ToolExecution {
	return tm.activeTools
}

func (tm *ToolManager) GetCompletedTools() map[string]*ToolExecution {
	return tm.completedTools
}

// NewUnifiedCompliantEventStreamParser 兼容性构造函数（避免名称冲突）
func NewUnifiedCompliantEventStreamParser(strictMode bool) *UnifiedParser {
	logger.Debug("创建统一解析器",
		logger.Bool("strict_mode", strictMode),
		logger.String("version", "unified_v1.0"))

	return NewUnifiedParser(strictMode)
}