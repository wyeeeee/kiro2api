package parser

import (
	"fmt"
	"strings"
	"time"
)

// MessageStartEndRule 消息开始结束验证规则
type MessageStartEndRule struct {
	hasMessageStart bool
	hasMessageStop  bool
}

func (r *MessageStartEndRule) Name() string {
	return "message_start_end"
}

func (r *MessageStartEndRule) Validate(session *ValidationSession, event SSEEvent) *ValidationError {
	switch event.Event {
	case "message_start":
		if r.hasMessageStart {
			return &ValidationError{
				Timestamp: time.Now(),
				ErrorType: "duplicate_message_start",
				Message:   "检测到重复的 message_start 事件",
				EventData: event,
				Severity:  SeverityError,
			}
		}
		r.hasMessageStart = true

	case "message_stop":
		if !r.hasMessageStart {
			return &ValidationError{
				Timestamp: time.Now(),
				ErrorType: "message_stop_without_start",
				Message:   "收到 message_stop 但未收到 message_start",
				EventData: event,
				Severity:  SeverityError,
			}
		}
		if r.hasMessageStop {
			return &ValidationError{
				Timestamp: time.Now(),
				ErrorType: "duplicate_message_stop",
				Message:   "检测到重复的 message_stop 事件",
				EventData: event,
				Severity:  SeverityError,
			}
		}
		r.hasMessageStop = true
	}

	return nil
}

// ContentBlockIntegrityRule 内容块完整性验证规则
type ContentBlockIntegrityRule struct {
	activeBlocks    map[int]string // index -> block_type
	completedBlocks map[int]bool   // index -> completed
}

func (r *ContentBlockIntegrityRule) Name() string {
	return "content_block_integrity"
}

func (r *ContentBlockIntegrityRule) Validate(session *ValidationSession, event SSEEvent) *ValidationError {
	if r.activeBlocks == nil {
		r.activeBlocks = make(map[int]string)
		r.completedBlocks = make(map[int]bool)
	}

	if dataMap, ok := event.Data.(map[string]any); ok {
		switch event.Event {
		case "content_block_start":
			if index, ok := r.getBlockIndex(dataMap); ok {
				if _, exists := r.activeBlocks[index]; exists {
					return &ValidationError{
						Timestamp: time.Now(),
						ErrorType: "duplicate_content_block_start",
						Message:   fmt.Sprintf("内容块索引 %d 重复开始", index),
						EventData: event,
						Severity:  SeverityWarning,
					}
				}

				blockType := "text" // 默认
				if cb, ok := dataMap["content_block"].(map[string]any); ok {
					if bt, ok := cb["type"].(string); ok {
						blockType = bt
					}
				}
				r.activeBlocks[index] = blockType

				// 统计工具调用
				if blockType == "tool_use" {
					session.toolCallCount++
				}
			}

		case "content_block_stop":
			if index, ok := r.getBlockIndex(dataMap); ok {
				if _, exists := r.activeBlocks[index]; !exists {
					return &ValidationError{
						Timestamp: time.Now(),
						ErrorType: "content_block_stop_without_start",
						Message:   fmt.Sprintf("内容块索引 %d 未开始就结束", index),
						EventData: event,
						Severity:  SeverityError,
					}
				}

				if r.completedBlocks[index] {
					return &ValidationError{
						Timestamp: time.Now(),
						ErrorType: "duplicate_content_block_stop",
						Message:   fmt.Sprintf("内容块索引 %d 重复结束", index),
						EventData: event,
						Severity:  SeverityWarning,
					}
				}

				r.completedBlocks[index] = true
			}

		case "content_block_delta":
			if index, ok := r.getBlockIndex(dataMap); ok {
				if _, exists := r.activeBlocks[index]; !exists {
					return &ValidationError{
						Timestamp: time.Now(),
						ErrorType: "content_block_delta_without_start",
						Message:   fmt.Sprintf("内容块索引 %d 未开始就有增量", index),
						EventData: event,
						Severity:  SeverityError,
					}
				}

				// 统计文本内容长度
				if delta, ok := dataMap["delta"].(map[string]any); ok {
					if text, ok := delta["text"].(string); ok {
						session.textContentLength += len(text)
					}
				}
			}
		}
	}

	return nil
}

func (r *ContentBlockIntegrityRule) getBlockIndex(dataMap map[string]any) (int, bool) {
	if index, ok := dataMap["index"].(int); ok {
		return index, true
	}
	if indexFloat, ok := dataMap["index"].(float64); ok {
		return int(indexFloat), true
	}
	return 0, false
}

// ToolExecutionFlowRule 工具执行流程验证规则
type ToolExecutionFlowRule struct {
	toolExecutions map[string]*ToolExecutionState
}

type ToolExecutionState struct {
	toolUseId  string
	toolName   string
	hasStart   bool
	hasStop    bool
	deltaCount int
	startTime  time.Time
}

func (r *ToolExecutionFlowRule) Name() string {
	return "tool_execution_flow"
}

func (r *ToolExecutionFlowRule) Validate(session *ValidationSession, event SSEEvent) *ValidationError {
	if r.toolExecutions == nil {
		r.toolExecutions = make(map[string]*ToolExecutionState)
	}

	if dataMap, ok := event.Data.(map[string]any); ok {
		switch event.Event {
		case "content_block_start":
			if cb, ok := dataMap["content_block"].(map[string]any); ok {
				if blockType, ok := cb["type"].(string); ok && blockType == "tool_use" {
					if toolUseId, ok := cb["id"].(string); ok && toolUseId != "" {
						if state, exists := r.toolExecutions[toolUseId]; exists {
							if state.hasStart {
								return &ValidationError{
									Timestamp: time.Now(),
									ErrorType: "duplicate_tool_start",
									Message:   fmt.Sprintf("工具 %s 重复开始", toolUseId),
									EventData: event,
									Severity:  SeverityWarning,
								}
							}
						} else {
							r.toolExecutions[toolUseId] = &ToolExecutionState{
								toolUseId: toolUseId,
								startTime: time.Now(),
							}
						}

						state := r.toolExecutions[toolUseId]
						state.hasStart = true
						if toolName, ok := cb["name"].(string); ok {
							state.toolName = toolName
						}
					}
				}
			}

		case "content_block_stop":
			// 检查是否为工具执行结束
			if _, ok := r.getBlockIndex(dataMap); ok {
				// 需要通过索引反查工具ID，这里简化处理
				for toolUseId, state := range r.toolExecutions {
					if state.hasStart && !state.hasStop {
						state.hasStop = true

						if state.deltaCount == 0 {
							return &ValidationError{
								Timestamp: time.Now(),
								ErrorType: "tool_without_parameters",
								Message:   fmt.Sprintf("工具 %s 没有参数增量", toolUseId),
								EventData: event,
								Severity:  SeverityWarning,
							}
						}
						break
					}
				}
			}

		case "content_block_delta":
			if delta, ok := dataMap["delta"].(map[string]any); ok {
				if deltaType, ok := delta["type"].(string); ok && deltaType == "input_json_delta" {
					// 这是工具参数增量，寻找对应的工具执行
					for _, state := range r.toolExecutions {
						if state.hasStart && !state.hasStop {
							state.deltaCount++
							break
						}
					}
				}
			}
		}
	}

	return nil
}

func (r *ToolExecutionFlowRule) getBlockIndex(dataMap map[string]any) (int, bool) {
	if index, ok := dataMap["index"].(int); ok {
		return index, true
	}
	if indexFloat, ok := dataMap["index"].(float64); ok {
		return int(indexFloat), true
	}
	return 0, false
}

// StreamingTimeoutRule 流式超时验证规则
type StreamingTimeoutRule struct {
	timeout   time.Duration
	lastEvent time.Time
}

func (r *StreamingTimeoutRule) Name() string {
	return "streaming_timeout"
}

func (r *StreamingTimeoutRule) Validate(session *ValidationSession, event SSEEvent) *ValidationError {
	now := time.Now()

	if !r.lastEvent.IsZero() {
		timeSinceLastEvent := now.Sub(r.lastEvent)
		if timeSinceLastEvent > r.timeout {
			return &ValidationError{
				Timestamp: now,
				ErrorType: "streaming_timeout",
				Message:   fmt.Sprintf("流式响应超时: %v", timeSinceLastEvent),
				EventData: event,
				Severity:  SeverityError,
			}
		}
	}

	r.lastEvent = now
	return nil
}

// DuplicateEventRule 重复事件检测规则
type DuplicateEventRule struct {
	eventHistory []EventFingerprint
	maxHistory   int
}

type EventFingerprint struct {
	EventType string
	Timestamp time.Time
	DataHash  string
}

func (r *DuplicateEventRule) Name() string {
	return "duplicate_event"
}

func (r *DuplicateEventRule) Validate(session *ValidationSession, event SSEEvent) *ValidationError {
	if r.maxHistory == 0 {
		r.maxHistory = 100
	}

	// 创建事件指纹
	fingerprint := EventFingerprint{
		EventType: event.Event,
		Timestamp: time.Now(),
		DataHash:  r.generateDataHash(event.Data),
	}

	// 检查是否有重复
	for _, existing := range r.eventHistory {
		if existing.EventType == fingerprint.EventType &&
			existing.DataHash == fingerprint.DataHash &&
			fingerprint.Timestamp.Sub(existing.Timestamp) < 100*time.Millisecond {
			return &ValidationError{
				Timestamp: fingerprint.Timestamp,
				ErrorType: "duplicate_event",
				Message:   fmt.Sprintf("检测到重复事件: %s", event.Event),
				EventData: event,
				Severity:  SeverityWarning,
			}
		}
	}

	// 添加到历史记录
	r.eventHistory = append(r.eventHistory, fingerprint)

	// 限制历史记录大小
	if len(r.eventHistory) > r.maxHistory {
		r.eventHistory = r.eventHistory[len(r.eventHistory)-r.maxHistory:]
	}

	return nil
}

func (r *DuplicateEventRule) generateDataHash(data interface{}) string {
	// 简化的数据哈希生成
	if dataMap, ok := data.(map[string]any); ok {
		var parts []string
		for key, value := range dataMap {
			if key == "index" || key == "type" {
				parts = append(parts, fmt.Sprintf("%s:%v", key, value))
			}
		}
		return strings.Join(parts, "|")
	}
	return ""
}
