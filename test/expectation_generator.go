package test

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"kiro2api/utils"
)

// ExpectationGenerator 期望值生成器
type ExpectationGenerator struct {
	protocolSpec *ProtocolSpecification
	dedupRules   *DeduplicationRules
	logger       utils.Logger
}

// ExpectedOutput 期望输出
type ExpectedOutput struct {
	SSEEvents       []SSEEvent           `json:"sse_events"`
	ToolCalls       []ExpectedToolCall   `json:"tool_calls"`
	ContentBlocks   []ExpectedContentBlock `json:"content_blocks"`
	FinalStats      ExpectedStats        `json:"final_stats"`
	GeneratedAt     time.Time            `json:"generated_at"`
	ValidationRules []ValidationRule     `json:"validation_rules"`
}

// SSEEvent SSE事件定义
type SSEEvent struct {
	Type        string                 `json:"type"`
	Data        map[string]interface{} `json:"data"`
	Timestamp   time.Time              `json:"timestamp"`
	Index       int                    `json:"index"`
	EventSource string                 `json:"event_source"` // "content", "tool_use", "message"
}

// ExpectedToolCall 期望的工具调用
type ExpectedToolCall struct {
	ToolUseID    string                 `json:"tool_use_id"`
	Name         string                 `json:"name"`
	Input        map[string]interface{} `json:"input"`
	InputJSON    string                 `json:"input_json"`
	BlockIndex   int                    `json:"block_index"`
	StartEvent   *SSEEvent              `json:"start_event"`
	InputEvents  []*SSEEvent            `json:"input_events"`
	StopEvent    *SSEEvent              `json:"stop_event"`
}

// ExpectedContentBlock 期望的内容块
type ExpectedContentBlock struct {
	Index       int         `json:"index"`
	Type        string      `json:"type"` // "text", "tool_use"
	Content     string      `json:"content"`
	TextDeltas  []*SSEEvent `json:"text_deltas"`
	StartEvent  *SSEEvent   `json:"start_event"`
	StopEvent   *SSEEvent   `json:"stop_event"`
}

// ExpectedStats 期望的统计信息
type ExpectedStats struct {
	TotalEvents      int            `json:"total_events"`
	ContentBlocks    int            `json:"content_blocks"`
	ToolCalls        int            `json:"tool_calls"`
	OutputCharacters int            `json:"output_characters"`
	EventsByType     map[string]int `json:"events_by_type"`
}

// ValidationRule 验证规则
type ValidationRule struct {
	Type        string      `json:"type"`
	Field       string      `json:"field"`
	Expected    interface{} `json:"expected"`
	Tolerance   float64     `json:"tolerance,omitempty"`
	Description string      `json:"description"`
}

// ProtocolSpecification 协议规范
type ProtocolSpecification struct {
	EventTypes      map[string]EventTypeSpec    `json:"event_types"`
	HeaderFormat    HeaderFormatSpec           `json:"header_format"`
	PayloadFormats  map[string]PayloadFormatSpec `json:"payload_formats"`
	ValidationRules []ProtocolValidationRule   `json:"validation_rules"`
}

// EventTypeSpec 事件类型规范
type EventTypeSpec struct {
	Name         string            `json:"name"`
	RequiredFields []string        `json:"required_fields"`
	OptionalFields []string        `json:"optional_fields"`
	PayloadType    string          `json:"payload_type"`
	Examples       []interface{}   `json:"examples"`
}

// HeaderFormatSpec 头部格式规范
type HeaderFormatSpec struct {
	RequiredHeaders []string `json:"required_headers"`
	OptionalHeaders []string `json:"optional_headers"`
}

// PayloadFormatSpec 载荷格式规范
type PayloadFormatSpec struct {
	ContentType    string      `json:"content_type"`
	Schema         interface{} `json:"schema"`
	ValidationFunc string      `json:"validation_func"`
}

// ProtocolValidationRule 协议验证规则
type ProtocolValidationRule struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Rule        string `json:"rule"`
	Severity    string `json:"severity"` // "error", "warning", "info"
}

// DeduplicationRules 去重规则
type DeduplicationRules struct {
	ToolCallRules    []ToolCallDeduplicationRule `json:"tool_call_rules"`
	ContentRules     []ContentDeduplicationRule  `json:"content_rules"`
	TimingRules      []TimingDeduplicationRule   `json:"timing_rules"`
}

// ToolCallDeduplicationRule 工具调用去重规则
type ToolCallDeduplicationRule struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Rule        string `json:"rule"`
	Action      string `json:"action"` // "skip", "merge", "warn"
}

// ContentDeduplicationRule 内容去重规则
type ContentDeduplicationRule struct {
	Name        string  `json:"name"`
	Description string  `json:"description"`
	MinLength   int     `json:"min_length"`
	Similarity  float64 `json:"similarity"`
	Action      string  `json:"action"`
}

// TimingDeduplicationRule 时序去重规则
type TimingDeduplicationRule struct {
	Name        string        `json:"name"`
	Description string        `json:"description"`
	TimeWindow  time.Duration `json:"time_window"`
	Action      string        `json:"action"`
}

// NewExpectationGenerator 创建新的期望值生成器
func NewExpectationGenerator() *ExpectationGenerator {
	return &ExpectationGenerator{
		protocolSpec: getDefaultProtocolSpec(),
		dedupRules:   getDefaultDeduplicationRules(),
		logger:       utils.GetLogger(),
	}
}

// GenerateExpectations 生成期望输出
func (g *ExpectationGenerator) GenerateExpectations(events []*ParsedEvent) (*ExpectedOutput, error) {
	g.logger.Debug("开始生成期望输出",
		utils.Int("input_events", len(events)))

	// 应用去重规则
	filteredEvents := g.ApplyDeduplicationRules(events)
	
	// 生成SSE事件序列
	sseEvents := g.generateSSEEvents(filteredEvents)
	
	// 提取工具调用
	toolCalls := g.extractToolCalls(sseEvents)
	
	// 提取内容块
	contentBlocks := g.extractContentBlocks(sseEvents)
	
	// 计算统计信息
	stats := g.calculateStats(sseEvents, toolCalls, contentBlocks)
	
	// 生成验证规则
	validationRules := g.generateValidationRules(sseEvents, toolCalls, contentBlocks)

	expectedOutput := &ExpectedOutput{
		SSEEvents:       sseEvents,
		ToolCalls:       toolCalls,
		ContentBlocks:   contentBlocks,
		FinalStats:      stats,
		GeneratedAt:     time.Now(),
		ValidationRules: validationRules,
	}

	g.logger.Debug("期望输出生成完成",
		utils.Int("sse_events", len(sseEvents)),
		utils.Int("tool_calls", len(toolCalls)),
		utils.Int("content_blocks", len(contentBlocks)))

	return expectedOutput, nil
}

// ApplyDeduplicationRules 应用去重规则
func (g *ExpectationGenerator) ApplyDeduplicationRules(events []*ParsedEvent) []*ParsedEvent {
	g.logger.Debug("应用去重规则", utils.Int("original_events", len(events)))
	
	filtered := make([]*ParsedEvent, 0, len(events))
	seenToolCalls := make(map[string]bool)
	lastContent := ""
	
	for _, event := range events {
		shouldSkip := false
		
		// 工具调用去重
		if event.EventType == "toolUseEvent" {
			if toolID := g.extractToolUseID(event); toolID != "" {
				if seenToolCalls[toolID] {
					g.logger.Debug("跳过重复的工具调用",
						utils.String("tool_use_id", toolID))
					shouldSkip = true
				} else {
					seenToolCalls[toolID] = true
				}
			}
		}
		
		// 内容去重
		if event.EventType == "assistantResponseEvent" {
			if content := g.extractTextContent(event); content != "" {
				if strings.TrimSpace(content) == strings.TrimSpace(lastContent) && content != "" {
					g.logger.Debug("跳过重复的文本内容",
						utils.String("content_preview", g.truncateString(content, 50)))
					shouldSkip = true
				} else {
					lastContent = content
				}
			}
		}
		
		if !shouldSkip {
			filtered = append(filtered, event)
		}
	}
	
	g.logger.Debug("去重完成",
		utils.Int("filtered_events", len(filtered)),
		utils.Int("removed_events", len(events)-len(filtered)))
	
	return filtered
}

// generateSSEEvents 生成SSE事件序列
func (g *ExpectationGenerator) generateSSEEvents(events []*ParsedEvent) []SSEEvent {
	var sseEvents []SSEEvent
	currentIndex := 0
	
	for _, event := range events {
		switch event.EventType {
		case "assistantResponseEvent":
			if newEvents := g.convertAssistantResponseEvent(event, &currentIndex); len(newEvents) > 0 {
				sseEvents = append(sseEvents, newEvents...)
			}
			
		case "toolUseEvent":
			if newEvents := g.convertToolUseEvent(event, &currentIndex); len(newEvents) > 0 {
				sseEvents = append(sseEvents, newEvents...)
			}
		}
	}
	
	// 添加最终的message_stop事件
	sseEvents = append(sseEvents, SSEEvent{
		Type:        "message_stop",
		Data:        map[string]interface{}{},
		Timestamp:   time.Now(),
		Index:       currentIndex,
		EventSource: "message",
	})
	
	return sseEvents
}

// convertAssistantResponseEvent 转换助手响应事件
func (g *ExpectationGenerator) convertAssistantResponseEvent(event *ParsedEvent, currentIndex *int) []SSEEvent {
	var events []SSEEvent
	
	if dataMap, ok := event.Payload.(map[string]interface{}); ok {
		eventType, _ := dataMap["type"].(string)
		
		switch eventType {
		case "content_block_delta":
			if delta, ok := dataMap["delta"].(map[string]interface{}); ok {
				if deltaType, _ := delta["type"].(string); deltaType == "text_delta" {
					if text, ok := delta["text"].(string); ok && text != "" {
						events = append(events, SSEEvent{
							Type: "content_block_delta",
							Data: map[string]interface{}{
								"index": dataMap["index"],
								"delta": map[string]interface{}{
									"type": "text_delta",
									"text": text,
								},
							},
							Timestamp:   time.Now(),
							Index:       *currentIndex,
							EventSource: "content",
						})
						*currentIndex++
					}
				}
			}
			
		case "content_block_start":
			events = append(events, SSEEvent{
				Type:        "content_block_start",
				Data:        dataMap,
				Timestamp:   time.Now(),
				Index:       *currentIndex,
				EventSource: "content",
			})
			*currentIndex++
			
		case "content_block_stop":
			events = append(events, SSEEvent{
				Type:        "content_block_stop",
				Data:        dataMap,
				Timestamp:   time.Now(),
				Index:       *currentIndex,
				EventSource: "content",
			})
			*currentIndex++
		}
	}
	
	return events
}

// convertToolUseEvent 转换工具使用事件
func (g *ExpectationGenerator) convertToolUseEvent(event *ParsedEvent, currentIndex *int) []SSEEvent {
	var events []SSEEvent
	
	// 解析工具调用信息
	toolCall := g.parseToolCallFromEvent(event)
	if toolCall == nil {
		return events
	}
	
	// 生成tool_use开始事件
	startEvent := SSEEvent{
		Type: "content_block_start",
		Data: map[string]interface{}{
			"index": toolCall.BlockIndex,
			"content_block": map[string]interface{}{
				"type": "tool_use",
				"id":   toolCall.ToolUseID,
				"name": toolCall.Name,
			},
		},
		Timestamp:   time.Now(),
		Index:       *currentIndex,
		EventSource: "tool_use",
	}
	events = append(events, startEvent)
	*currentIndex++
	
	// 生成输入JSON增量事件
	if toolCall.InputJSON != "" {
		chunks := g.chunkInputJSON(toolCall.InputJSON)
		for _, chunk := range chunks {
			inputEvent := SSEEvent{
				Type: "content_block_delta",
				Data: map[string]interface{}{
					"index": toolCall.BlockIndex,
					"delta": map[string]interface{}{
						"type":         "input_json_delta",
						"partial_json": chunk,
					},
				},
				Timestamp:   time.Now(),
				Index:       *currentIndex,
				EventSource: "tool_use",
			}
			events = append(events, inputEvent)
			*currentIndex++
		}
	}
	
	// 生成tool_use结束事件
	stopEvent := SSEEvent{
		Type: "content_block_stop",
		Data: map[string]interface{}{
			"index": toolCall.BlockIndex,
		},
		Timestamp:   time.Now(),
		Index:       *currentIndex,
		EventSource: "tool_use",
	}
	events = append(events, stopEvent)
	*currentIndex++
	
	return events
}

// extractToolCalls 提取工具调用
func (g *ExpectationGenerator) extractToolCalls(sseEvents []SSEEvent) []ExpectedToolCall {
	var toolCalls []ExpectedToolCall
	toolCallMap := make(map[string]*ExpectedToolCall)
	
	for i := range sseEvents {
		event := &sseEvents[i]
		
		// 修复：支持两种SSE事件格式
		// 1. 新格式：使用 Event 字段（来自 parser/event_stream_types.go）
		// 2. 旧格式：使用 Type 字段（本文件定义）
		eventType := event.Type
		if eventType == "" {
			// 尝试从 event 结构中获取事件类型
			if eventInterface, ok := interface{}(event).(map[string]interface{}); ok {
				if et, ok := eventInterface["event"].(string); ok {
					eventType = et
				}
			}
		}
		
		// 处理来自 parser 的真实事件数据
		var eventData map[string]interface{}
		if event.Data != nil {
			eventData = event.Data
		} else {
			// 如果 Data 字段为空，尝试获取整个事件作为数据
			if fullEvent, ok := interface{}(event).(map[string]interface{}); ok {
				if data, ok := fullEvent["data"].(map[string]interface{}); ok {
					eventData = data
					if et, ok := fullEvent["event"].(string); ok {
						eventType = et
					}
				}
			}
		}
		
		g.logger.Debug("处理事件", 
			utils.String("event_type", eventType),
			utils.Any("event_data", eventData))
		
		if eventType == "content_block_start" && eventData != nil {
			if cb, ok := eventData["content_block"].(map[string]interface{}); ok {
				if cbType, _ := cb["type"].(string); cbType == "tool_use" {
					toolUseID, _ := cb["id"].(string)
					toolName, _ := cb["name"].(string)
					blockIndex, _ := eventData["index"].(int)
					
					g.logger.Info("发现期望工具调用", 
						utils.String("tool_use_id", toolUseID),
						utils.String("tool_name", toolName),
						utils.Int("block_index", blockIndex))
					
					toolCall := &ExpectedToolCall{
						ToolUseID:   toolUseID,
						Name:        toolName,
						BlockIndex:  blockIndex,
						StartEvent:  event,
						InputEvents: make([]*SSEEvent, 0),
					}
					
					toolCallMap[toolUseID] = toolCall
				}
			}
		} else if eventType == "content_block_delta" && eventData != nil {
			if delta, ok := event.Data["delta"].(map[string]interface{}); ok {
				if deltaType, _ := delta["type"].(string); deltaType == "input_json_delta" {
					blockIndex, _ := event.Data["index"].(int)
					
					// 找到对应的工具调用
					for _, toolCall := range toolCallMap {
						if toolCall.BlockIndex == blockIndex {
							toolCall.InputEvents = append(toolCall.InputEvents, event)
							break
						}
					}
				}
			}
		} else if event.Type == "content_block_stop" {
			blockIndex, _ := event.Data["index"].(int)
			
			// 找到对应的工具调用并设置结束事件
			for _, toolCall := range toolCallMap {
				if toolCall.BlockIndex == blockIndex && toolCall.StopEvent == nil {
					toolCall.StopEvent = event
					break
				}
			}
		}
	}
	
	// 构建完整的输入JSON
	for _, toolCall := range toolCallMap {
		var inputJSONBuilder strings.Builder
		for _, inputEvent := range toolCall.InputEvents {
			if delta, ok := inputEvent.Data["delta"].(map[string]interface{}); ok {
				if partialJSON, ok := delta["partial_json"].(string); ok {
					inputJSONBuilder.WriteString(partialJSON)
				}
			}
		}
		
		toolCall.InputJSON = inputJSONBuilder.String()
		
		// 尝试解析输入JSON
		if toolCall.InputJSON != "" {
			var input map[string]interface{}
			if err := json.Unmarshal([]byte(toolCall.InputJSON), &input); err == nil {
				toolCall.Input = input
			}
		}
		
		toolCalls = append(toolCalls, *toolCall)
	}
	
	return toolCalls
}

// extractContentBlocks 提取内容块
func (g *ExpectationGenerator) extractContentBlocks(sseEvents []SSEEvent) []ExpectedContentBlock {
	var contentBlocks []ExpectedContentBlock
	blockMap := make(map[int]*ExpectedContentBlock)
	
	for i := range sseEvents {
		event := &sseEvents[i]
		
		if event.Type == "content_block_start" {
			blockIndex, _ := event.Data["index"].(int)
			
			block := &ExpectedContentBlock{
				Index:      blockIndex,
				StartEvent: event,
				TextDeltas: make([]*SSEEvent, 0),
			}
			
			if cb, ok := event.Data["content_block"].(map[string]interface{}); ok {
				block.Type, _ = cb["type"].(string)
			}
			
			blockMap[blockIndex] = block
			
		} else if event.Type == "content_block_delta" {
			blockIndex, _ := event.Data["index"].(int)
			
			if block, exists := blockMap[blockIndex]; exists {
				if delta, ok := event.Data["delta"].(map[string]interface{}); ok {
					if deltaType, _ := delta["type"].(string); deltaType == "text_delta" {
						block.TextDeltas = append(block.TextDeltas, event)
						if text, ok := delta["text"].(string); ok {
							block.Content += text
						}
					}
				}
			}
			
		} else if event.Type == "content_block_stop" {
			blockIndex, _ := event.Data["index"].(int)
			
			if block, exists := blockMap[blockIndex]; exists {
				block.StopEvent = event
			}
		}
	}
	
	// 转换为切片并排序
	for _, block := range blockMap {
		contentBlocks = append(contentBlocks, *block)
	}
	
	return contentBlocks
}

// calculateStats 计算统计信息
func (g *ExpectationGenerator) calculateStats(sseEvents []SSEEvent, toolCalls []ExpectedToolCall, contentBlocks []ExpectedContentBlock) ExpectedStats {
	eventsByType := make(map[string]int)
	totalChars := 0
	
	for _, event := range sseEvents {
		eventsByType[event.Type]++
	}
	
	for _, block := range contentBlocks {
		totalChars += len(block.Content)
	}
	
	return ExpectedStats{
		TotalEvents:      len(sseEvents),
		ContentBlocks:    len(contentBlocks),
		ToolCalls:        len(toolCalls),
		OutputCharacters: totalChars,
		EventsByType:     eventsByType,
	}
}

// generateValidationRules 生成验证规则
func (g *ExpectationGenerator) generateValidationRules(sseEvents []SSEEvent, toolCalls []ExpectedToolCall, contentBlocks []ExpectedContentBlock) []ValidationRule {
	rules := []ValidationRule{
		{
			Type:        "count",
			Field:       "sse_events",
			Expected:    len(sseEvents),
			Description: "SSE事件总数验证",
		},
		{
			Type:        "count",
			Field:       "tool_calls",
			Expected:    len(toolCalls),
			Description: "工具调用总数验证",
		},
		{
			Type:        "count",
			Field:       "content_blocks",
			Expected:    len(contentBlocks),
			Description: "内容块总数验证",
		},
	}
	
	// 为每个工具调用生成验证规则
	for _, toolCall := range toolCalls {
		rules = append(rules, ValidationRule{
			Type:        "tool_call",
			Field:       fmt.Sprintf("tool_call[%s].name", toolCall.ToolUseID),
			Expected:    toolCall.Name,
			Description: fmt.Sprintf("工具调用 %s 名称验证", toolCall.ToolUseID),
		})
	}
	
	return rules
}

// 辅助函数

// extractToolUseID 提取工具使用ID
func (g *ExpectationGenerator) extractToolUseID(event *ParsedEvent) string {
	if dataMap, ok := event.Payload.(map[string]interface{}); ok {
		if toolUseID, ok := dataMap["toolUseId"].(string); ok {
			return toolUseID
		}
		if id, ok := dataMap["id"].(string); ok {
			return id
		}
	}
	return ""
}

// extractTextContent 提取文本内容
func (g *ExpectationGenerator) extractTextContent(event *ParsedEvent) string {
	if dataMap, ok := event.Payload.(map[string]interface{}); ok {
		if content, ok := dataMap["content"].(string); ok {
			return content
		}
		if delta, ok := dataMap["delta"].(map[string]interface{}); ok {
			if text, ok := delta["text"].(string); ok {
				return text
			}
		}
	}
	return ""
}

// parseToolCallFromEvent 从事件中解析工具调用
func (g *ExpectationGenerator) parseToolCallFromEvent(event *ParsedEvent) *ExpectedToolCall {
	if dataMap, ok := event.Payload.(map[string]interface{}); ok {
		toolCall := &ExpectedToolCall{
			Input: make(map[string]interface{}),
		}
		
		if toolUseID, ok := dataMap["toolUseId"].(string); ok {
			toolCall.ToolUseID = toolUseID
		}
		
		if name, ok := dataMap["name"].(string); ok {
			toolCall.Name = name
		}
		
		if input, ok := dataMap["input"].(string); ok {
			toolCall.InputJSON = input
			// 尝试解析JSON
			var inputMap map[string]interface{}
			if err := json.Unmarshal([]byte(input), &inputMap); err == nil {
				toolCall.Input = inputMap
			}
		}
		
		return toolCall
	}
	return nil
}

// chunkInputJSON 将输入JSON分块
func (g *ExpectationGenerator) chunkInputJSON(inputJSON string) []string {
	// 简单的分块策略，实际实现可能更复杂
	chunkSize := 50
	var chunks []string
	
	for i := 0; i < len(inputJSON); i += chunkSize {
		end := i + chunkSize
		if end > len(inputJSON) {
			end = len(inputJSON)
		}
		chunks = append(chunks, inputJSON[i:end])
	}
	
	return chunks
}

// truncateString 截断字符串
func (g *ExpectationGenerator) truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// getDefaultProtocolSpec 获取默认协议规范
func getDefaultProtocolSpec() *ProtocolSpecification {
	return &ProtocolSpecification{
		EventTypes: map[string]EventTypeSpec{
			"assistantResponseEvent": {
				Name: "assistantResponseEvent",
				RequiredFields: []string{"content"},
				OptionalFields: []string{"contentType", "messageStatus"},
				PayloadType: "json",
			},
			"toolUseEvent": {
				Name: "toolUseEvent",
				RequiredFields: []string{"name", "toolUseId"},
				OptionalFields: []string{"input"},
				PayloadType: "json",
			},
		},
	}
}

// getDefaultDeduplicationRules 获取默认去重规则
func getDefaultDeduplicationRules() *DeduplicationRules {
	return &DeduplicationRules{
		ToolCallRules: []ToolCallDeduplicationRule{
			{
				Name:        "duplicate_tool_use_id",
				Description: "跳过重复的工具使用ID",
				Rule:        "same_tool_use_id",
				Action:      "skip",
			},
		},
		ContentRules: []ContentDeduplicationRule{
			{
				Name:        "duplicate_text_content",
				Description: "跳过重复的文本内容",
				MinLength:   5,
				Similarity:  0.95,
				Action:      "skip",
			},
		},
	}
}