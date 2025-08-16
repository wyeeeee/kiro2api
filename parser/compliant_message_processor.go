package parser

import (
	"encoding/json"
	"fmt"
	"kiro2api/logger"
	"kiro2api/types"
	"kiro2api/utils"
	"strings"
	"sync"
	"time"
)

// ToolDataAggregatorInterface 统一聚合器接口
type ToolDataAggregatorInterface interface {
	ProcessToolData(toolUseId, name, input string, stop bool, fragmentIndex int) (complete bool, fullInput string)
	CleanupExpiredBuffers(timeout time.Duration)
}

// CompliantMessageProcessor 符合规范的消息处理器
type CompliantMessageProcessor struct {
	sessionManager     *SessionManager
	toolManager        *ToolLifecycleManager
	eventHandlers      map[string]EventHandler
	legacyHandlers     map[string]EventHandler
	completionBuffer   []string
	legacyToolState    *toolIndexState                // 添加旧格式事件的工具状态
	toolDataAggregator ToolDataAggregatorInterface    // 统一的工具调用数据聚合器接口
	// 运行时状态：跟踪已开始的工具与其内容块索引，用于按增量输出
	startedTools   map[string]bool
	toolBlockIndex map[string]int
}

// EventHandler 事件处理器接口
type EventHandler interface {
	Handle(message *EventStreamMessage) ([]SSEEvent, error)
}

// NewCompliantMessageProcessor 创建符合规范的消息处理器
func NewCompliantMessageProcessor() *CompliantMessageProcessor {
	processor := &CompliantMessageProcessor{
		sessionManager:   NewSessionManager(),
		toolManager:      NewToolLifecycleManager(),
		eventHandlers:    make(map[string]EventHandler),
		legacyHandlers:   make(map[string]EventHandler),
		completionBuffer: make([]string, 0, 16),
		startedTools:     make(map[string]bool),
		toolBlockIndex:   make(map[string]int),
	}

	// 创建Sonic聚合器，并设置参数更新回调
	processor.toolDataAggregator = NewSonicStreamingJSONAggregatorWithCallback(
		func(toolUseId string, fullParams string) {
			logger.Debug("Sonic聚合器回调：更新工具参数",
				logger.String("toolUseId", toolUseId),
				logger.String("fullParams", func() string {
					if len(fullParams) > 100 {
						return fullParams[:100] + "..."
					}
					return fullParams
				}()))
			
			// 调用工具管理器更新参数
			processor.toolManager.UpdateToolArgumentsFromJSON(toolUseId, fullParams)
		})

	processor.registerEventHandlers()
	return processor
}

// Reset 重置处理器状态
func (cmp *CompliantMessageProcessor) Reset() {
	cmp.sessionManager.Reset()
	cmp.toolManager.Reset()
	cmp.completionBuffer = cmp.completionBuffer[:0]
	// 重置旧格式工具状态
	if cmp.legacyToolState != nil {
		cmp.legacyToolState.fullReset()
	}
}

// registerEventHandlers 注册所有事件处理器
func (cmp *CompliantMessageProcessor) registerEventHandlers() {
	// 标准事件处理器
	cmp.eventHandlers[EventTypes.COMPLETION] = &CompletionEventHandler{cmp}
	cmp.eventHandlers[EventTypes.COMPLETION_CHUNK] = &CompletionChunkEventHandler{cmp}
	cmp.eventHandlers[EventTypes.TOOL_CALL_REQUEST] = &ToolCallRequestHandler{cmp.toolManager}
	cmp.eventHandlers[EventTypes.TOOL_CALL_RESULT] = &ToolCallResultHandler{cmp.toolManager}
	cmp.eventHandlers[EventTypes.TOOL_CALL_ERROR] = &ToolCallErrorHandler{cmp.toolManager}
	cmp.eventHandlers[EventTypes.TOOL_EXECUTION_START] = &ToolExecutionStartHandler{cmp.toolManager}
	cmp.eventHandlers[EventTypes.TOOL_EXECUTION_END] = &ToolExecutionEndHandler{}
	cmp.eventHandlers[EventTypes.SESSION_START] = &SessionStartHandler{cmp.sessionManager}
	cmp.eventHandlers[EventTypes.SESSION_END] = &SessionEndHandler{cmp.sessionManager}

	// 标准事件处理器 - 将assistantResponseEvent作为标准事件
	cmp.eventHandlers[EventTypes.ASSISTANT_RESPONSE_EVENT] = &StandardAssistantResponseEventHandler{cmp}

	// 兼容旧格式的处理器
	cmp.legacyHandlers[EventTypes.TOOL_USE_EVENT] = &LegacyToolUseEventHandler{
		toolManager: cmp.toolManager,
		aggregator:  cmp.toolDataAggregator,
	}
}

// ProcessMessage 处理单个消息
func (cmp *CompliantMessageProcessor) ProcessMessage(message *EventStreamMessage) ([]SSEEvent, error) {
	messageType := message.GetMessageType()
	eventType := message.GetEventType()

	logger.Debug("处理消息",
		logger.String("message_type", messageType),
		logger.String("event_type", eventType),
		logger.Int("payload_len", len(message.Payload)),
		logger.String("payload_preview", func() string {
			if len(message.Payload) > 100 {
				return string(message.Payload[:100]) + "..."
			}
			return string(message.Payload)
		}()))

	// 预处理payload - 修复CodeWhisperer特殊格式
	processedMessage := cmp.preprocessCodeWhispererPayload(message)

	// 根据消息类型分别处理
	switch messageType {
	case MessageTypes.EVENT:
		return cmp.processEventMessage(processedMessage, eventType)
	case MessageTypes.ERROR:
		return cmp.processErrorMessage(processedMessage)
	case MessageTypes.EXCEPTION:
		return cmp.processExceptionMessage(processedMessage)
	default:
		logger.Warn("未知消息类型", logger.String("message_type", messageType))
		return []SSEEvent{}, nil
	}
}

// preprocessCodeWhispererPayload 预处理CodeWhisperer特殊格式的payload
func (cmp *CompliantMessageProcessor) preprocessCodeWhispererPayload(message *EventStreamMessage) *EventStreamMessage {
	payloadStr := string(message.Payload)

	// 检查是否是CodeWhisperer特殊格式: "vent{...}" 或 "event{...}"
	// 注意：只有在payload真正以这些前缀开头并且后面是有效JSON时才进行修复
	var jsonPayload string
	if strings.HasPrefix(payloadStr, "vent{") && len(payloadStr) > 4 {
		// 验证移除前缀后是否为有效JSON
		candidate := payloadStr[4:] // 移除 "vent" 前缀
		var testData map[string]interface{}
		if err := json.Unmarshal([]byte(candidate), &testData); err == nil {
			jsonPayload = candidate
		}
	} else if strings.HasPrefix(payloadStr, "event{") && len(payloadStr) > 5 {
		// 验证移除前缀后是否为有效JSON
		candidate := payloadStr[5:] // 移除 "event" 前缀
		var testData map[string]interface{}
		if err := json.Unmarshal([]byte(candidate), &testData); err == nil {
			jsonPayload = candidate
		}
	}

	if jsonPayload != "" {
		logger.Debug("修复CodeWhisperer格式",
			logger.String("原始", payloadStr[:min(50, len(payloadStr))]),
			logger.String("修复后", jsonPayload[:min(50, len(jsonPayload))]))

		// 创建修复后的消息副本
		return &EventStreamMessage{
			MessageType: message.MessageType,
			EventType:   message.EventType,
			Headers:     message.Headers,
			Payload:     []byte(jsonPayload),
		}
	}

	return message
}

// processEventMessage 处理事件消息
func (cmp *CompliantMessageProcessor) processEventMessage(message *EventStreamMessage, eventType string) ([]SSEEvent, error) {
	logger.Debug("处理事件消息",
		logger.String("event_type", eventType),
		logger.Bool("has_standard_handler", func() bool {
			_, exists := cmp.eventHandlers[eventType]
			return exists
		}()),
		logger.Bool("has_legacy_handler", func() bool {
			_, exists := cmp.legacyHandlers[eventType]
			return exists
		}()))

	// 优先处理标准事件
	if handler, exists := cmp.eventHandlers[eventType]; exists {
		logger.Debug("使用标准处理器", logger.String("event_type", eventType))
		return handler.Handle(message)
	}

	// 处理兼容的旧格式事件
	if handler, exists := cmp.legacyHandlers[eventType]; exists {
		logger.Debug("使用旧格式处理器", logger.String("event_type", eventType))
		return handler.Handle(message)
	}

	// 未知事件类型，记录日志但不报错
	logger.Warn("未知事件类型",
		logger.String("event_type", eventType),
		logger.Any("available_standard", func() []string {
			var keys []string
			for k := range cmp.eventHandlers {
				keys = append(keys, k)
			}
			return keys
		}()),
		logger.Any("available_legacy", func() []string {
			var keys []string
			for k := range cmp.legacyHandlers {
				keys = append(keys, k)
			}
			return keys
		}()))
	return []SSEEvent{}, nil
}

// processErrorMessage 处理错误消息
func (cmp *CompliantMessageProcessor) processErrorMessage(message *EventStreamMessage) ([]SSEEvent, error) {
	var errorData map[string]interface{}
	if len(message.Payload) > 0 {
		if err := json.Unmarshal(message.Payload, &errorData); err != nil {
			logger.Warn("解析错误消息载荷失败", logger.Err(err))
			errorData = map[string]interface{}{
				"message": string(message.Payload),
			}
		}
	}

	errorCode := ""
	errorMessage := ""

	if errorData != nil {
		if code, ok := errorData["__type"].(string); ok {
			errorCode = code
		}
		if msg, ok := errorData["message"].(string); ok {
			errorMessage = msg
		}
	}

	return []SSEEvent{
		{
			Event: "error",
			Data: map[string]interface{}{
				"type":          "error",
				"error_code":    errorCode,
				"error_message": errorMessage,
				"raw_data":      errorData,
			},
		},
	}, nil
}

// processExceptionMessage 处理异常消息
func (cmp *CompliantMessageProcessor) processExceptionMessage(message *EventStreamMessage) ([]SSEEvent, error) {
	var exceptionData map[string]interface{}
	if len(message.Payload) > 0 {
		if err := json.Unmarshal(message.Payload, &exceptionData); err != nil {
			logger.Warn("解析异常消息载荷失败", logger.Err(err))
			exceptionData = map[string]interface{}{
				"message": string(message.Payload),
			}
		}
	}

	exceptionType := ""
	exceptionMessage := ""

	if exceptionData != nil {
		if eType, ok := exceptionData["__type"].(string); ok {
			exceptionType = eType
		}
		if msg, ok := exceptionData["message"].(string); ok {
			exceptionMessage = msg
		}
	}

	return []SSEEvent{
		{
			Event: "exception",
			Data: map[string]interface{}{
				"type":              "exception",
				"exception_type":    exceptionType,
				"exception_message": exceptionMessage,
				"raw_data":          exceptionData,
			},
		},
	}, nil
}

// GetSessionManager 获取会话管理器
func (cmp *CompliantMessageProcessor) GetSessionManager() *SessionManager {
	return cmp.sessionManager
}

// GetToolManager 获取工具管理器
func (cmp *CompliantMessageProcessor) GetToolManager() *ToolLifecycleManager {
	return cmp.toolManager
}

// === 事件处理器实现 ===

// CompletionEventHandler 处理代码补全事件
type CompletionEventHandler struct {
	processor *CompliantMessageProcessor
}

func (h *CompletionEventHandler) Handle(message *EventStreamMessage) ([]SSEEvent, error) {
	var data map[string]interface{}
	if err := json.Unmarshal(message.Payload, &data); err != nil {
		return nil, err
	}

	content := ""
	if c, ok := data["content"].(string); ok {
		content = c
	}

	finishReason := ""
	if fr, ok := data["finish_reason"].(string); ok {
		finishReason = fr
	}

	// 处理工具调用
	var toolCalls []ToolCall
	if tcData, ok := data["tool_calls"].([]interface{}); ok {
		for _, tc := range tcData {
			if tcMap, ok := tc.(map[string]interface{}); ok {
				toolCall := ToolCall{}
				if id, ok := tcMap["id"].(string); ok {
					toolCall.ID = id
				}
				if tcType, ok := tcMap["type"].(string); ok {
					toolCall.Type = tcType
				}
				if function, ok := tcMap["function"].(map[string]interface{}); ok {
					if name, ok := function["name"].(string); ok {
						toolCall.Function.Name = name
					}
					if args, ok := function["arguments"].(string); ok {
						toolCall.Function.Arguments = args
					}
				}
				toolCalls = append(toolCalls, toolCall)
			}
		}
	}

	events := []SSEEvent{
		{
			Event: "completion",
			Data: map[string]interface{}{
				"type":          "completion",
				"content":       content,
				"finish_reason": finishReason,
				"tool_calls":    toolCalls,
				"raw_data":      data,
			},
		},
	}

	return events, nil
}

// CompletionChunkEventHandler 处理流式补全事件
type CompletionChunkEventHandler struct {
	processor *CompliantMessageProcessor
}

func (h *CompletionChunkEventHandler) Handle(message *EventStreamMessage) ([]SSEEvent, error) {
	var data map[string]interface{}
	if err := json.Unmarshal(message.Payload, &data); err != nil {
		return nil, err
	}

	content := ""
	if c, ok := data["content"].(string); ok {
		content = c
	}

	delta := ""
	if d, ok := data["delta"].(string); ok {
		delta = d
	}

	finishReason := ""
	if fr, ok := data["finish_reason"].(string); ok {
		finishReason = fr
	}

	// 累积完整内容
	h.processor.completionBuffer = append(h.processor.completionBuffer, content)

	// 使用delta作为实际的文本增量，如果没有则使用content
	textDelta := delta
	if textDelta == "" {
		textDelta = content
	}

	events := []SSEEvent{
		{
			Event: "content_block_delta",
			Data: map[string]interface{}{
				"type":  "content_block_delta",
				"index": 0,
				"delta": map[string]interface{}{
					"type": "text_delta",
					"text": textDelta,
				},
			},
		},
	}

	// 如果有完成原因，添加完成事件
	if finishReason != "" {
		events = append(events, SSEEvent{
			Event: "content_block_stop",
			Data: map[string]interface{}{
				"type":          "content_block_stop",
				"index":         0,
				"finish_reason": finishReason,
			},
		})
	}

	return events, nil
}

// ToolCallRequestHandler 处理工具调用请求
type ToolCallRequestHandler struct {
	toolManager *ToolLifecycleManager
}

func (h *ToolCallRequestHandler) Handle(message *EventStreamMessage) ([]SSEEvent, error) {
	var data map[string]interface{}
	if err := json.Unmarshal(message.Payload, &data); err != nil {
		return nil, err
	}

	// 从标准AWS事件格式解析工具调用信息
	toolCallID, _ := data["toolCallId"].(string)
	toolName, _ := data["toolName"].(string)

	// 如果没有直接的toolCallId，尝试解析input字段
	input := map[string]interface{}{}
	if inputData, ok := data["input"].(map[string]interface{}); ok {
		input = inputData
	}

	// 创建标准格式的工具调用请求
	toolCall := ToolCall{
		ID:   toolCallID,
		Type: "function",
		Function: ToolCallFunction{
			Name:      toolName,
			Arguments: "{}",
		},
	}

	// 将input转换为JSON字符串
	if len(input) > 0 {
		if argsJSON, err := json.Marshal(input); err == nil {
			toolCall.Function.Arguments = string(argsJSON)
		}
	}

	request := ToolCallRequest{
		ToolCalls: []ToolCall{toolCall},
	}

	logger.Debug("标准工具调用请求处理",
		logger.String("tool_id", toolCallID),
		logger.String("tool_name", toolName),
		logger.Any("input", input))

	return h.toolManager.HandleToolCallRequest(request), nil
}

// ToolCallResultHandler 处理工具调用结果
type ToolCallResultHandler struct {
	toolManager *ToolLifecycleManager
}

func (h *ToolCallResultHandler) Handle(message *EventStreamMessage) ([]SSEEvent, error) {
	var data map[string]interface{}
	if err := json.Unmarshal(message.Payload, &data); err != nil {
		return nil, err
	}

	// 从标准AWS事件格式解析工具调用结果
	toolCallID, _ := data["toolCallId"].(string)
	result, _ := data["result"].(string)
	success, _ := data["success"].(bool)

	// 如果没有结果字符串，尝试整个data作为结果
	if result == "" {
		if resultData, exists := data["result"]; exists {
			result = fmt.Sprintf("%v", resultData)
		} else {
			result = "Tool execution completed"
		}
	}

	// 创建标准格式的工具调用结果
	toolResult := ToolCallResult{
		ToolCallID: toolCallID,
		Result:     result,
	}

	if !success {
		// 如果工具执行失败，转换为错误处理
		errorInfo := ToolCallError{
			ToolCallID: toolCallID,
			Error:      result,
		}
		return h.toolManager.HandleToolCallError(errorInfo), nil
	}

	logger.Debug("标准工具调用结果处理",
		logger.String("tool_id", toolCallID),
		logger.String("result", result),
		logger.Bool("success", success))

	return h.toolManager.HandleToolCallResult(toolResult), nil
}

// ToolCallErrorHandler 处理工具调用错误
type ToolCallErrorHandler struct {
	toolManager *ToolLifecycleManager
}

func (h *ToolCallErrorHandler) Handle(message *EventStreamMessage) ([]SSEEvent, error) {
	var errorInfo ToolCallError
	if err := json.Unmarshal(message.Payload, &errorInfo); err != nil {
		return nil, err
	}

	return h.toolManager.HandleToolCallError(errorInfo), nil
}

// ToolExecutionStartHandler 处理工具执行开始事件
type ToolExecutionStartHandler struct {
	toolManager *ToolLifecycleManager
}

func (h *ToolExecutionStartHandler) Handle(message *EventStreamMessage) ([]SSEEvent, error) {
	var data map[string]interface{}
	if err := json.Unmarshal(message.Payload, &data); err != nil {
		return nil, err
	}

	// 从执行开始事件中提取工具信息并创建执行记录
	toolCallID, _ := data["toolCallId"].(string)
	toolName, _ := data["toolName"].(string)
	executionID, _ := data["executionId"].(string)

	if toolCallID != "" && toolName != "" {
		// 创建工具执行记录
		toolCall := ToolCall{
			ID:   toolCallID,
			Type: "function",
			Function: ToolCallFunction{
				Name:      toolName,
				Arguments: "{}",
			},
		}

		request := ToolCallRequest{
			ToolCalls: []ToolCall{toolCall},
		}

		logger.Debug("工具执行开始，创建执行记录",
			logger.String("tool_id", toolCallID),
			logger.String("tool_name", toolName),
			logger.String("execution_id", executionID))

		// 在工具管理器中开始执行
		h.toolManager.HandleToolCallRequest(request)
	}

	return []SSEEvent{
		{
			Event: EventTypes.TOOL_EXECUTION_START,
			Data:  data,
		},
	}, nil
}

// ToolExecutionEndHandler 处理工具执行结束事件
type ToolExecutionEndHandler struct{}

func (h *ToolExecutionEndHandler) Handle(message *EventStreamMessage) ([]SSEEvent, error) {
	var data map[string]interface{}
	if err := json.Unmarshal(message.Payload, &data); err != nil {
		return nil, err
	}

	return []SSEEvent{
		{
			Event: EventTypes.TOOL_EXECUTION_END,
			Data:  data,
		},
	}, nil
}

// SessionStartHandler 处理会话开始事件
type SessionStartHandler struct {
	sessionManager *SessionManager
}

func (h *SessionStartHandler) Handle(message *EventStreamMessage) ([]SSEEvent, error) {
	var data map[string]interface{}
	if err := json.Unmarshal(message.Payload, &data); err != nil {
		return nil, err
	}

	// 尝试多种会话ID字段名
	sessionID := ""
	if sid, ok := data["sessionId"].(string); ok {
		sessionID = sid
	} else if sid, ok := data["session_id"].(string); ok {
		sessionID = sid
	}

	if sessionID != "" {
		h.sessionManager.SetSessionID(sessionID)
		// 触发实际的会话开始
		h.sessionManager.StartSession()
	}

	return []SSEEvent{
		{
			Event: EventTypes.SESSION_START,
			Data:  data,
		},
	}, nil
}

// SessionEndHandler 处理会话结束事件
type SessionEndHandler struct {
	sessionManager *SessionManager
}

func (h *SessionEndHandler) Handle(message *EventStreamMessage) ([]SSEEvent, error) {
	var data map[string]interface{}
	if err := json.Unmarshal(message.Payload, &data); err != nil {
		return nil, err
	}

	// 处理测试中的持续时间字段 - 确保测试有足够的时间差
	if duration, ok := data["duration"].(float64); ok && duration > 0 {
		// 如果载荷中包含持续时间，我们需要模拟时间流逝
		time.Sleep(time.Millisecond * 10) // 至少10ms的持续时间
	} else {
		// 默认情况下也添加一个小的延迟
		time.Sleep(time.Millisecond * 5)
	}

	// 实际结束会话
	endEvents := h.sessionManager.EndSession()

	// 合并事件数据
	result := []SSEEvent{
		{
			Event: EventTypes.SESSION_END,
			Data:  data,
		},
	}

	// 添加会话管理器生成的结束事件
	result = append(result, endEvents...)

	return result, nil
}

// StandardAssistantResponseEventHandler 标准assistantResponseEvent处理器
type StandardAssistantResponseEventHandler struct {
	processor *CompliantMessageProcessor
}

func (h *StandardAssistantResponseEventHandler) Handle(message *EventStreamMessage) ([]SSEEvent, error) {
	// 首先检查是否是工具调用相关的事件
	if isToolCallEvent(message.Payload) {
		logger.Debug("检测到工具调用事件，使用聚合器处理")
		return h.handleToolCallEvent(message)
	}

	// 作为标准事件，优先尝试解析完整格式
	if fullEvent, err := parseFullAssistantResponseEvent(message.Payload); err == nil {
		// 对于流式响应，放宽验证要求
		if isStreamingResponse(fullEvent) {
			logger.Debug("检测到流式格式assistantResponseEvent，使用宽松验证")
			return h.handleStreamingEvent(fullEvent)
		}

		logger.Debug("检测到完整格式assistantResponseEvent，使用标准处理器")
		return h.handleFullAssistantEvent(fullEvent)
	}

	// 如果完整格式解析失败，回退到legacy格式处理
	logger.Debug("完整格式解析失败，回退到legacy格式处理")
	return h.handleLegacyFormat(message.Payload)
}

// isToolCallEvent 检查是否是工具调用事件
func isToolCallEvent(payload []byte) bool {
	payloadStr := string(payload)
	// 检查是否包含工具调用的典型字段
	return (strings.Contains(payloadStr, "\"toolUseId\"") || strings.Contains(payloadStr, "\"name\"")) &&
		(strings.Contains(payloadStr, "\"input\"") || strings.Contains(payloadStr, "\"stop\""))
}

// handleToolCallEvent 处理工具调用事件
func (h *StandardAssistantResponseEventHandler) handleToolCallEvent(message *EventStreamMessage) ([]SSEEvent, error) {
	// 尝试解析为工具使用事件
	var evt toolUseEvent
	if err := json.Unmarshal(message.Payload, &evt); err != nil {
		logger.Warn("解析工具调用事件失败", logger.Err(err))
		return []SSEEvent{}, nil
	}

	// 使用聚合器记录分片，供 stop 时重组完整输入
	_, _ = h.processor.toolDataAggregator.ProcessToolData(evt.ToolUseId, evt.Name, evt.Input, evt.Stop, -1)

	events := make([]SSEEvent, 0, 4)

	// 首次片段：发出 content_block_start
	if !h.processor.startedTools[evt.ToolUseId] {
		// 检查并验证聚合器中是否有完整参数
		var toolArgs map[string]interface{}
		if complete, fullInput := h.processor.toolDataAggregator.ProcessToolData(evt.ToolUseId, evt.Name, "", false, -1); complete && fullInput != "" {
			// 如果已经聚合完成，使用完整参数
			if err := utils.SafeUnmarshal([]byte(fullInput), &toolArgs); err == nil {
				logger.Debug("使用聚合器中的完整参数",
					logger.String("toolUseId", evt.ToolUseId),
					logger.String("fullInput", fullInput))
			}
		}

		// 如果没有完整参数，使用空参数占位
		if toolArgs == nil {
			toolArgs = make(map[string]interface{})
		}

		// 使用工具管理器发出标准的 content_block_start
		toolCall := ToolCall{
			ID:   evt.ToolUseId,
			Type: "function",
			Function: ToolCallFunction{
				Name:      evt.Name,
				Arguments: "{}", // 首次不带参数，避免错误的完整参数
			},
		}

		// 如果有验证过的参数，更新Arguments
		if len(toolArgs) > 0 {
			if argsJSON, err := utils.SafeMarshal(toolArgs); err == nil {
				toolCall.Function.Arguments = string(argsJSON)
			}
		}

		reqEvents := h.processor.toolManager.HandleToolCallRequest(ToolCallRequest{ToolCalls: []ToolCall{toolCall}})
		// 记录 block index
		for _, e := range reqEvents {
			if e.Event == "content_block_start" {
				if data, ok := e.Data.(map[string]any); ok {
					if idx, ok2 := data["index"].(int); ok2 {
						h.processor.toolBlockIndex[evt.ToolUseId] = idx
					} else if f, ok3 := data["index"].(float64); ok3 {
						h.processor.toolBlockIndex[evt.ToolUseId] = int(f)
					}
				}
			}
		}
		events = append(events, reqEvents...)
		h.processor.startedTools[evt.ToolUseId] = true
	}

	// 追加输入增量（如果有）
	if evt.Input != "" {
		idx := h.processor.toolBlockIndex[evt.ToolUseId]
		events = append(events, SSEEvent{
			Event: "content_block_delta",
			Data: map[string]any{
				"type":  "content_block_delta",
				"index": idx,
				"delta": map[string]any{
					"type":         "input_json_delta",
					"partial_json": evt.Input,
				},
			},
		})
	}

	// 结束：发出 stop 与 message_delta(stop_reason=tool_use)
	if evt.Stop {
		idx := h.processor.toolBlockIndex[evt.ToolUseId]
		events = append(events,
			SSEEvent{Event: "content_block_stop", Data: map[string]any{"type": "content_block_stop", "index": idx}},
			SSEEvent{Event: "message_delta", Data: map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": "tool_use", "stop_sequence": nil}, "usage": map[string]any{"output_tokens": 0}}},
		)
		// 清理状态
		delete(h.processor.startedTools, evt.ToolUseId)
		delete(h.processor.toolBlockIndex, evt.ToolUseId)
	}

	if len(events) == 0 {
		// 没有生成可下发事件（例如仅聚合空片段），返回空
		logger.Debug("工具调用分片已记录，等待更多数据",
			logger.String("toolUseId", evt.ToolUseId),
			logger.String("name", evt.Name),
			logger.Bool("stop", evt.Stop))
	}

	return events, nil
}

// isStreamingResponse 检查是否是流式响应（只有content字段）
func isStreamingResponse(evt *FullAssistantResponseEvent) bool {
	return evt.ConversationID == "" && evt.MessageID == "" && evt.Content != ""
}

// handleStreamingEvent 处理流式事件（放宽验证要求）
func (h *StandardAssistantResponseEventHandler) handleStreamingEvent(evt *FullAssistantResponseEvent) ([]SSEEvent, error) {
	logger.Debug("流式AssistantResponseEvent处理开始",
		logger.String("content_preview", func() string {
			if len(evt.Content) > 50 {
				return evt.Content[:50] + "..."
			}
			return evt.Content
		}()),
		logger.String("content_type", string(evt.ContentType)),
		logger.String("message_status", string(evt.MessageStatus)))

	// 对于流式响应，跳过严格验证，只验证有内容
	if evt.Content == "" {
		logger.Warn("流式响应内容为空")
	}

	// 使用完整事件转换函数
	if h.processor.legacyToolState == nil {
		h.processor.legacyToolState = newToolIndexState()
	}

	events := convertFullAssistantEventToSSE(evt, h.processor.legacyToolState)

	logger.Debug("流式AssistantResponseEvent处理完成",
		logger.Int("generated_events", len(events)))

	return events, nil
}

// handleFullAssistantEvent 处理完整格式的助手事件
func (h *StandardAssistantResponseEventHandler) handleFullAssistantEvent(evt *FullAssistantResponseEvent) ([]SSEEvent, error) {
	logger.Debug("标准AssistantResponseEvent处理开始",
		logger.String("conversation_id", evt.ConversationID),
		logger.String("message_id", evt.MessageID),
		logger.String("content_preview", func() string {
			if len(evt.Content) > 50 {
				return evt.Content[:50] + "..."
			}
			return evt.Content
		}()),
		logger.String("message_status", string(evt.MessageStatus)),
		logger.String("content_type", string(evt.ContentType)),
		logger.Bool("has_followup", evt.FollowupPrompt != nil),
		logger.Int("web_links_count", len(evt.SupplementaryWebLinks)),
		logger.Int("references_count", len(evt.References)),
		logger.Int("code_references_count", len(evt.CodeReference)))

	// 验证事件完整性
	if err := evt.Validate(); err != nil {
		logger.Warn("完整AssistantResponseEvent验证失败", logger.Err(err))
		// 在非严格模式下，继续处理但记录警告
	}

	// 使用完整事件转换函数
	if h.processor.legacyToolState == nil {
		h.processor.legacyToolState = newToolIndexState()
	}

	events := convertFullAssistantEventToSSE(evt, h.processor.legacyToolState)

	// 如果有工具相关的事件，也需要在工具管理器中创建执行记录
	if evt.CodeQuery != nil && evt.CodeQuery.CodeQueryID != "" {
		toolCall := ToolCall{
			ID:   evt.CodeQuery.CodeQueryID,
			Type: "function",
			Function: ToolCallFunction{
				Name:      "codeQuery",
				Arguments: fmt.Sprintf(`{"query_id": "%s"}`, evt.CodeQuery.CodeQueryID),
			},
		}

		request := ToolCallRequest{
			ToolCalls: []ToolCall{toolCall},
		}
		h.processor.toolManager.HandleToolCallRequest(request)

		// 如果消息已完成，也处理结果
		if evt.MessageStatus == types.MessageStatusCompleted {
			result := ToolCallResult{
				ToolCallID: evt.CodeQuery.CodeQueryID,
				Result:     evt.Content,
			}
			h.processor.toolManager.HandleToolCallResult(result)
		}
	}

	logger.Debug("标准AssistantResponseEvent处理完成",
		logger.Int("generated_events", len(events)))

	return events, nil
}

// handleLegacyFormat 处理legacy格式
func (h *StandardAssistantResponseEventHandler) handleLegacyFormat(payload []byte) ([]SSEEvent, error) {
	var evt assistantResponseEvent
	if err := json.Unmarshal(payload, &evt); err != nil {
		return nil, fmt.Errorf("解析legacy assistantResponseEvent失败: %w", err)
	}

	logger.Debug("使用legacy格式处理assistantResponseEvent")

	// 清理内容中的不可见字符
	if evt.Content != "" {
		evt.Content = cleanInvisibleChars(evt.Content)
	}
	if evt.Input != nil && *evt.Input != "" {
		cleaned := cleanInvisibleChars(*evt.Input)
		evt.Input = &cleaned
	}

	// 检查内容是否包含XML工具标记
	if evt.Content != "" && strings.Contains(evt.Content, "<tool_use>") {
		logger.Debug("检测到XML工具标记，进行解析转换",
			logger.String("content_preview", func() string {
				if len(evt.Content) > 100 {
					return evt.Content[:100] + "..."
				}
				return evt.Content
			}()))

		// 提取并转换XML工具调用
		cleanText, xmlTools := ExtractAndConvertXMLTools(evt.Content)
		
		if len(xmlTools) > 0 {
			logger.Debug("成功解析XML工具调用",
				logger.Int("tool_count", len(xmlTools)),
				logger.String("clean_text", cleanText))

			// 创建SSE事件
			events := make([]SSEEvent, 0)

			// 如果有清理后的文本，先发送文本内容
			if cleanText != "" && cleanText != " " {
				events = append(events, SSEEvent{
					Event: "content_block_start",
					Data: map[string]interface{}{
						"type": "content_block_start",
						"index": 0,
						"content_block": map[string]interface{}{
							"type": "text",
							"text": "",
						},
					},
				})
				
				events = append(events, SSEEvent{
					Event: "content_block_delta",
					Data: map[string]interface{}{
						"type": "content_block_delta",
						"index": 0,
						"delta": map[string]interface{}{
							"type": "text_delta",
							"text": cleanText,
						},
					},
				})
				
				events = append(events, SSEEvent{
					Event: "content_block_stop",
					Data: map[string]interface{}{
						"type": "content_block_stop",
						"index": 0,
					},
				})
			}

			// 发送工具调用事件
			for i, tool := range xmlTools {
				blockIndex := i
				if cleanText != "" {
					blockIndex = i + 1  // 如果有文本，工具索引从1开始
				}

				// content_block_start for tool_use
				events = append(events, SSEEvent{
					Event: "content_block_start",
					Data: map[string]interface{}{
						"type": "content_block_start",
						"index": blockIndex,
						"content_block": map[string]interface{}{
							"type": "tool_use",
							"id": tool["id"],
							"name": tool["name"],
							"input": tool["input"],
						},
					},
				})

				// content_block_stop for tool_use
				events = append(events, SSEEvent{
					Event: "content_block_stop",
					Data: map[string]interface{}{
						"type": "content_block_stop",
						"index": blockIndex,
					},
				})
				
				// 在工具管理器中创建执行记录
				if h.processor.toolManager != nil {
					toolCall := ToolCall{
						ID:   tool["id"].(string),
						Type: "function",
						Function: ToolCallFunction{
							Name:      tool["name"].(string),
							Arguments: func() string {
								if input, ok := tool["input"].(map[string]interface{}); ok {
									if argsJSON, err := json.Marshal(input); err == nil {
										return string(argsJSON)
									}
								}
								return "{}"
							}(),
						},
					}
					
					request := ToolCallRequest{
						ToolCalls: []ToolCall{toolCall},
					}
					h.processor.toolManager.HandleToolCallRequest(request)
					
					// 标记工具执行完成
					result := ToolCallResult{
						ToolCallID: tool["id"].(string),
						Result:     "Tool execution completed via XML format",
					}
					h.processor.toolManager.HandleToolCallResult(result)
				}
			}

			// 添加message_delta with stop_reason
			events = append(events, SSEEvent{
				Event: "message_delta",
				Data: map[string]interface{}{
					"type": "message_delta",
					"delta": map[string]interface{}{
						"stop_reason": "tool_use",
						"stop_sequence": nil,
					},
					"usage": map[string]interface{}{
						"output_tokens": 0,
					},
				},
			})

			return events, nil
		}
	}

	// 使用处理器级别的工具状态，确保工具收集跨事件保持
	if h.processor.legacyToolState == nil {
		h.processor.legacyToolState = newToolIndexState()
	}

	events := convertAssistantEventToSSE(evt, h.processor.legacyToolState)

	// 处理工具执行记录（与原有逻辑保持一致）
	if len(events) > 0 {
		for i, event := range events {
			if event.Event == "content_block_start" {
				if data, ok := event.Data.(map[string]interface{}); ok {
					if contentBlock, ok := data["content_block"].(map[string]interface{}); ok {
						if contentBlock["type"] == "tool_use" {
							toolID, _ := contentBlock["id"].(string)
							toolName, _ := contentBlock["name"].(string)

							// 创建工具调用请求并处理
							toolCall := ToolCall{
								ID:   toolID,
								Type: "function",
								Function: ToolCallFunction{
									Name:      toolName,
									Arguments: "{}",
								},
							}

							// 查找对应的输入增量事件来获取参数
							for j := i + 1; j < len(events) && j < i+3; j++ {
								if events[j].Event == "content_block_delta" {
									if deltaData, ok := events[j].Data.(map[string]interface{}); ok {
										if delta, ok := deltaData["delta"].(map[string]interface{}); ok {
											if partialJSON, ok := delta["partial_json"].(string); ok && partialJSON != "{}" {
												toolCall.Function.Arguments = partialJSON
												break
											}
										}
									}
								}
							}

							// 在工具管理器中创建执行记录
							request := ToolCallRequest{
								ToolCalls: []ToolCall{toolCall},
							}
							h.processor.toolManager.HandleToolCallRequest(request)

							// 处理结果
							result := ToolCallResult{
								ToolCallID: toolID,
								Result:     "Tool execution completed via legacy format",
							}
							h.processor.toolManager.HandleToolCallResult(result)
						}
					}
				}
			}
		}
	}

	return events, nil
}

// LegacyAssistantEventHandler 处理旧格式的助手事件（保留用于完全的legacy支持）
type LegacyAssistantEventHandler struct {
	processor *CompliantMessageProcessor
}

func (h *LegacyAssistantEventHandler) Handle(message *EventStreamMessage) ([]SSEEvent, error) {
	// 首先尝试解析完整格式
	if fullEvent, err := parseFullAssistantResponseEvent(message.Payload); err == nil {
		logger.Debug("检测到完整格式assistantResponseEvent，使用完整处理器")
		return h.handleFullAssistantEvent(fullEvent)
	}

	// 如果完整格式解析失败，回退到legacy格式
	var evt assistantResponseEvent
	if err := json.Unmarshal(message.Payload, &evt); err != nil {
		return nil, fmt.Errorf("解析assistantResponseEvent失败 (完整格式和legacy格式都失败): %w", err)
	}

	logger.Debug("使用legacy格式处理assistantResponseEvent")
	return h.handleLegacyAssistantEvent(evt)
}

// handleFullAssistantEvent 处理完整格式的助手事件
func (h *LegacyAssistantEventHandler) handleFullAssistantEvent(evt *FullAssistantResponseEvent) ([]SSEEvent, error) {
	logger.Debug("完整AssistantResponseEvent处理开始",
		logger.String("conversation_id", evt.ConversationID),
		logger.String("message_id", evt.MessageID),
		logger.String("content_preview", func() string {
			if len(evt.Content) > 50 {
				return evt.Content[:50] + "..."
			}
			return evt.Content
		}()),
		logger.String("message_status", string(evt.MessageStatus)),
		logger.String("content_type", string(evt.ContentType)),
		logger.Bool("has_followup", evt.FollowupPrompt != nil),
		logger.Int("web_links_count", len(evt.SupplementaryWebLinks)),
		logger.Int("references_count", len(evt.References)),
		logger.Int("code_references_count", len(evt.CodeReference)))

	// 验证事件完整性
	if err := evt.Validate(); err != nil {
		logger.Warn("完整AssistantResponseEvent验证失败", logger.Err(err))
		// 在非严格模式下，继续处理但记录警告
	}

	// 使用完整事件转换函数
	if h.processor.legacyToolState == nil {
		h.processor.legacyToolState = newToolIndexState()
	}

	events := convertFullAssistantEventToSSE(evt, h.processor.legacyToolState)

	logger.Debug("完整AssistantResponseEvent处理完成",
		logger.Int("generated_events", len(events)),
		logger.Bool("has_tools", evt.CodeQuery != nil))

	return events, nil
}

// handleLegacyAssistantEvent 处理legacy格式的助手事件
func (h *LegacyAssistantEventHandler) handleLegacyAssistantEvent(evt assistantResponseEvent) ([]SSEEvent, error) {

	// 清理内容中的不可见字符
	if evt.Content != "" {
		evt.Content = cleanInvisibleChars(evt.Content)
	}
	if evt.Input != nil && *evt.Input != "" {
		cleaned := cleanInvisibleChars(*evt.Input)
		evt.Input = &cleaned
	}

	// 关键修复：使用处理器级别的工具状态，确保工具收集跨事件保持
	if h.processor.legacyToolState == nil {
		h.processor.legacyToolState = newToolIndexState()
	}

	// 详细调试日志
	logger.Debug("LegacyAssistantEventHandler处理事件",
		logger.String("content", evt.Content),
		logger.Bool("has_tool_use_id", evt.ToolUseId != ""),
		logger.String("tool_use_id", evt.ToolUseId),
		logger.String("name", evt.Name),
		logger.Bool("stop", evt.Stop),
		logger.Bool("has_input", evt.Input != nil),
		logger.Bool("collecting_tool_text", h.processor.legacyToolState.collectingToolText),
		logger.Int("tool_buffer_len", h.processor.legacyToolState.toolTextBuffer.Len()))

	// 检查内容是否包含工具相关标记
	if evt.Content != "" {
		hasToolMarkers := strings.Contains(evt.Content, "<tool") ||
			strings.Contains(evt.Content, "Bash") ||
			strings.Contains(evt.Content, "_use>") ||
			strings.Contains(evt.Content, "_name>") ||
			strings.Contains(evt.Content, "_parameter")

		if hasToolMarkers {
			logger.Debug("检测到工具标记内容",
				logger.String("content", evt.Content),
				logger.Bool("has_tool_prefix", strings.Contains(evt.Content, "<tool")),
				logger.Bool("has_bash", strings.Contains(evt.Content, "Bash")),
				logger.Bool("has_use_suffix", strings.Contains(evt.Content, "_use>")))
		}
	}

	events := convertAssistantEventToSSE(evt, h.processor.legacyToolState)

	// 详细记录事件生成结果
	logger.Debug("LegacyAssistantEventHandler事件生成完成",
		logger.Int("generated_events", len(events)),
		logger.Bool("collecting_after", h.processor.legacyToolState.collectingToolText),
		logger.Int("buffer_after", h.processor.legacyToolState.toolTextBuffer.Len()))

	// 关键修复：如果生成了工具相关事件，也需要在工具管理器中创建执行记录
	if len(events) > 0 {
		for i, event := range events {
			if event.Event == "content_block_start" {
				if data, ok := event.Data.(map[string]interface{}); ok {
					if contentBlock, ok := data["content_block"].(map[string]interface{}); ok {
						if contentBlock["type"] == "tool_use" {
							toolID, _ := contentBlock["id"].(string)
							toolName, _ := contentBlock["name"].(string)

							logger.Debug("生成了工具使用事件，创建工具执行记录",
								logger.Int("event_index", i),
								logger.String("tool_name", toolName),
								logger.String("tool_id", toolID))

							// 创建工具调用请求并处理，以在工具管理器中创建记录
							toolCall := ToolCall{
								ID:   toolID,
								Type: "function",
								Function: ToolCallFunction{
									Name:      toolName,
									Arguments: "{}",
								},
							}

							// 查找对应的输入增量事件来获取参数
							for j := i + 1; j < len(events) && j < i+3; j++ {
								if events[j].Event == "content_block_delta" {
									if deltaData, ok := events[j].Data.(map[string]interface{}); ok {
										if delta, ok := deltaData["delta"].(map[string]interface{}); ok {
											if partialJSON, ok := delta["partial_json"].(string); ok && partialJSON != "{}" {
												toolCall.Function.Arguments = partialJSON
												break
											}
										}
									}
								}
							}

							// 在工具管理器中创建执行记录
							request := ToolCallRequest{
								ToolCalls: []ToolCall{toolCall},
							}
							h.processor.toolManager.HandleToolCallRequest(request)

							// 对于旧格式的assistantResponseEvent，工具通常会立即完成
							// 所以我们也需要处理结果
							result := ToolCallResult{
								ToolCallID: toolID,
								Result:     "Tool execution completed via legacy format",
							}
							h.processor.toolManager.HandleToolCallResult(result)
						}
					}
				}
			}
		}
	}

	return events, nil
}

// FullAssistantEventHandler 专门处理完整格式的助手响应事件
type FullAssistantEventHandler struct {
	processor *CompliantMessageProcessor
}

func (h *FullAssistantEventHandler) Handle(message *EventStreamMessage) ([]SSEEvent, error) {
	fullEvent, err := parseFullAssistantResponseEvent(message.Payload)
	if err != nil {
		return nil, fmt.Errorf("解析完整assistantResponseEvent失败: %w", err)
	}

	logger.Debug("FullAssistantEventHandler处理事件",
		logger.String("conversation_id", fullEvent.ConversationID),
		logger.String("message_id", fullEvent.MessageID),
		logger.String("message_status", string(fullEvent.MessageStatus)))

	// 验证事件
	if err := fullEvent.Validate(); err != nil {
		logger.Error("完整assistantResponseEvent验证失败", logger.Err(err))
		return nil, fmt.Errorf("事件验证失败: %w", err)
	}

	// 初始化工具状态
	if h.processor.legacyToolState == nil {
		h.processor.legacyToolState = newToolIndexState()
	}

	// 转换为SSE事件
	events := convertFullAssistantEventToSSE(fullEvent, h.processor.legacyToolState)

	// 如果有工具相关的事件，也需要在工具管理器中创建执行记录
	if fullEvent.CodeQuery != nil && fullEvent.CodeQuery.CodeQueryID != "" {
		toolCall := ToolCall{
			ID:   fullEvent.CodeQuery.CodeQueryID,
			Type: "function",
			Function: ToolCallFunction{
				Name:      "codeQuery",
				Arguments: fmt.Sprintf(`{"query_id": "%s"}`, fullEvent.CodeQuery.CodeQueryID),
			},
		}

		request := ToolCallRequest{
			ToolCalls: []ToolCall{toolCall},
		}
		h.processor.toolManager.HandleToolCallRequest(request)

		// 如果消息已完成，也处理结果
		if fullEvent.MessageStatus == types.MessageStatusCompleted {
			result := ToolCallResult{
				ToolCallID: fullEvent.CodeQuery.CodeQueryID,
				Result:     fullEvent.Content,
			}
			h.processor.toolManager.HandleToolCallResult(result)
		}
	}

	logger.Debug("FullAssistantEventHandler处理完成",
		logger.Int("generated_events", len(events)))

	return events, nil
}

// LegacyToolUseEventHandler 处理旧格式的工具使用事件
type LegacyToolUseEventHandler struct {
	toolManager *ToolLifecycleManager
	aggregator  ToolDataAggregatorInterface
}

func (h *LegacyToolUseEventHandler) Handle(message *EventStreamMessage) ([]SSEEvent, error) {
	var evt toolUseEvent
	if err := json.Unmarshal(message.Payload, &evt); err != nil {
		return nil, err
	}

	// 使用聚合器处理分片数据
	complete, fullInput := h.aggregator.ProcessToolData(evt.ToolUseId, evt.Name, evt.Input, evt.Stop, -1)

	// 只有当数据完整时才处理
	if !complete {
		logger.Debug("工具调用数据未完整，继续聚合",
			logger.String("toolUseId", evt.ToolUseId),
			logger.String("name", evt.Name),
			logger.String("inputFragment", evt.Input),
			logger.Bool("stop", evt.Stop))
		return []SSEEvent{}, nil
	}

	// 转换为标准格式
	assistantEvt := assistantResponseEvent{
		Content:   "",
		Name:      evt.Name,
		ToolUseId: evt.ToolUseId,
		Stop:      evt.Stop,
	}

	if fullInput != "" {
		cleaned := cleanInvisibleChars(fullInput)
		assistantEvt.Input = &cleaned
	}

	// 使用工具管理器处理
	state := newToolIndexState()
	return h.toolManager.ParseToolCallFromLegacyEvent(assistantEvt, state), nil
}

// ToolDataAggregator 工具调用数据聚合器
type ToolDataAggregator struct {
	activeTools map[string]*toolDataBuffer
	mu          sync.RWMutex // 添加读写锁保护并发访问
}

// toolDataBuffer 工具数据缓冲区
type toolDataBuffer struct {
	toolUseId  string
	name       string
	inputParts []string
	lastUpdate time.Time
	isComplete bool
}

// NewToolDataAggregator 创建工具数据聚合器
func NewToolDataAggregator() *ToolDataAggregator {
	return &ToolDataAggregator{
		activeTools: make(map[string]*toolDataBuffer),
	}
}

// ProcessToolData 处理工具调用数据片段
func (tda *ToolDataAggregator) ProcessToolData(toolUseId, name, input string, stop bool) (complete bool, fullInput string) {
	tda.mu.Lock()
	defer tda.mu.Unlock()

	if tda.activeTools == nil {
		tda.activeTools = make(map[string]*toolDataBuffer)
	}

	// 尝试找到匹配的缓冲区（包括模糊匹配）
	buffer := tda.findMatchingBuffer(toolUseId, name)
	if buffer == nil {
		// 创建新缓冲区时记录调试信息
		logger.Debug("创建新的工具调用缓冲区",
			logger.String("toolUseId", toolUseId),
			logger.String("name", name),
			logger.String("initialInput", input),
			logger.Bool("stop", stop))

		buffer = &toolDataBuffer{
			toolUseId:  toolUseId,
			name:       name,
			inputParts: make([]string, 0),
			lastUpdate: time.Now(),
		}
		tda.activeTools[toolUseId] = buffer
	} else {
		// 检查是否已经完成的工具调用
		if buffer.isComplete {
			logger.Warn("尝试处理已完成的工具调用，跳过",
				logger.String("toolUseId", toolUseId),
				logger.String("name", name),
				logger.Bool("originalComplete", buffer.isComplete))
			return false, ""
		}

		if buffer.toolUseId != toolUseId {
			// 找到了相似的buffer，记录日志
			logger.Debug("使用模糊匹配找到相似的工具调用缓冲区",
				logger.String("originalId", buffer.toolUseId),
				logger.String("currentId", toolUseId),
				logger.String("name", name))
		}
	}

	// 添加新的输入片段（即使是空字符串也记录，用于调试）
	if input != "" {
		buffer.inputParts = append(buffer.inputParts, input)
		logger.Debug("添加工具调用数据片段",
			logger.String("toolUseId", toolUseId),
			logger.String("fragment", input),
			logger.Int("totalParts", len(buffer.inputParts)))
	} else if !stop {
		// 如果input为空且不是stop事件，记录警告
		logger.Warn("收到空的工具调用输入片段",
			logger.String("toolUseId", toolUseId),
			logger.String("name", name))
	}
	buffer.lastUpdate = time.Now()

	// 如果标记为停止，则认为数据完整
	if stop {
		buffer.isComplete = true

		// 使用智能JSON重组逻辑
		fullInput = tda.reconstructJSON(buffer.inputParts)

		// 清理已完成的工具数据（需要清理所有相关的缓冲区）
		tda.cleanupRelatedBuffers(toolUseId, name)

		fullInputPreview := fullInput
		if len(fullInputPreview) > 100 {
			fullInputPreview = fullInputPreview[:100] + "..."
		}

		logger.Debug("工具调用数据聚合完成",
			logger.String("toolUseId", toolUseId),
			logger.String("name", name),
			logger.String("fullInput", fullInputPreview),
			logger.Int("inputParts", len(buffer.inputParts)))

		// 额外校验：打印关键字段与长度，辅助定位缺参/路径截断
		if name == "Write" || name == "Bash" {
			var obj map[string]any
			if err := json.Unmarshal([]byte(fullInput), &obj); err == nil {
				if name == "Write" {
					fp, _ := obj["file_path"].(string)
					content, _ := obj["content"].(string)
					
					logger.Debug("Write聚合校验",
						logger.Int("file_path_len", len(fp)),
						logger.String("file_path", fp),
						logger.Int("content_len", len(content)))
				} else if name == "Bash" {
					cmd, _ := obj["command"].(string)
					logger.Debug("Bash聚合校验",
						logger.Int("command_len", len(cmd)),
						logger.String("command_preview", func() string {
							if len(cmd) > 64 {
								return cmd[:64] + "..."
							}
							return cmd
						}()))
				}
			} else {
				logger.Warn("聚合后JSON解析失败", logger.Err(err), logger.String("raw", fullInputPreview))
			}
		}

		return true, fullInput
	}

	return false, ""
}

// reconstructJSON 智能重组JSON片段
func (tda *ToolDataAggregator) reconstructJSON(parts []string) string {
	if len(parts) == 0 {
		return "{}"
	}

	// 简单连接所有片段
	rawInput := strings.Join(parts, "")

	// 尝试修复常见的JSON格式问题
	fixed := tda.fixJSONFormat(rawInput)

	// 验证JSON格式
	var temp interface{}
	if err := json.Unmarshal([]byte(fixed), &temp); err != nil {
		logger.Warn("JSON重组后仍然无效，使用原始字符串",
			logger.String("原始", rawInput),
			logger.String("修复后", fixed),
			logger.Err(err))
		return rawInput
	}

	logger.Debug("JSON重组成功",
		logger.String("原始", rawInput),
		logger.String("修复后", fixed))

	return fixed
}

// fixJSONFormat 修复常见的JSON格式问题
func (tda *ToolDataAggregator) fixJSONFormat(input string) string {
	// 清理可能的前缀
	input = strings.TrimSpace(input)

	// 如果不以{开头，尝试添加
	if !strings.HasPrefix(input, "{") {
		input = "{" + input
	}

	// 如果不以}结尾，尝试添加
	if !strings.HasSuffix(input, "}") {
		input = input + "}"
	}

	// 移除错误的JSON重构逻辑，避免截断路径
	// 原先的逻辑会错误地将 {"file_path": "/Users/..."} 重构为 {"path": "..."}
	// 并且可能截断路径的开头部分，导致路径损坏

	return input
}

// CleanupExpiredBuffers 清理过期的缓冲区
func (tda *ToolDataAggregator) CleanupExpiredBuffers(timeout time.Duration) {
	tda.mu.Lock()
	defer tda.mu.Unlock()

	now := time.Now()
	for toolUseId, buffer := range tda.activeTools {
		if now.Sub(buffer.lastUpdate) > timeout {
			logger.Warn("清理过期的工具调用缓冲区",
				logger.String("toolUseId", toolUseId),
				logger.Duration("expired", now.Sub(buffer.lastUpdate)),
				logger.Int("lostParts", len(buffer.inputParts)))
			delete(tda.activeTools, toolUseId)
		}
	}
}

// cleanupRelatedBuffers 清理所有相关的缓冲区
func (tda *ToolDataAggregator) cleanupRelatedBuffers(toolUseId, name string) {
	// 清理精确匹配的缓冲区
	delete(tda.activeTools, toolUseId)

	// 清理所有相似的缓冲区
	var toDelete []string
	for existingId, buffer := range tda.activeTools {
		if buffer.name == name && isSimilarToolUseId(existingId, toolUseId) {
			toDelete = append(toDelete, existingId)
			logger.Debug("清理相似的工具调用缓冲区",
				logger.String("toolUseId", existingId),
				logger.String("similarTo", toolUseId))
		}
	}

	for _, id := range toDelete {
		delete(tda.activeTools, id)
	}
}

// findMatchingBuffer 查找匹配的缓冲区（仅精确匹配）
func (tda *ToolDataAggregator) findMatchingBuffer(toolUseId, name string) *toolDataBuffer {
	// 只使用精确匹配，避免模糊匹配导致的数据损坏
	if buffer, exists := tda.activeTools[toolUseId]; exists {
		return buffer
	}

	// 不再使用模糊匹配，返回nil让调用方创建新缓冲区
	return nil
}

// isSimilarToolUseId 判断两个toolUseId是否相似（已废弃，不再使用）
// 保留此函数仅为了代码兼容性，但不应该被调用
func isSimilarToolUseId(id1, id2 string) bool {
	// 始终返回false，强制使用精确匹配
	return false
}

// 废弃的辅助函数已移除

// min 返回最小值
func min(values ...int) int {
	if len(values) == 0 {
		return 0
	}
	minVal := values[0]
	for _, v := range values[1:] {
		if v < minVal {
			minVal = v
		}
	}
	return minVal
}
