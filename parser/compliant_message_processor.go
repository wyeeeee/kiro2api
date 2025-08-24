package parser

import (
	"kiro2api/logger"
	"kiro2api/utils"
	"strings"
)

// CompliantMessageProcessor 符合规范的消息处理器
type CompliantMessageProcessor struct {
	sessionManager     *SessionManager
	toolManager        *ToolLifecycleManager
	toolFSM            *ToolCallFSM // 新增：工具调用状态机
	eventHandlers      map[string]EventHandler
	legacyHandlers     map[string]EventHandler
	completionBuffer   []string
	legacyToolState    *toolIndexState             // 添加旧格式事件的工具状态
	toolDataAggregator ToolDataAggregatorInterface // 统一的工具调用数据聚合器接口
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
		toolFSM:          NewToolCallFSM(),
		eventHandlers:    make(map[string]EventHandler),
		legacyHandlers:   make(map[string]EventHandler),
		completionBuffer: make([]string, 0, 16),
		startedTools:     make(map[string]bool),
		toolBlockIndex:   make(map[string]int),
	}

	// 添加状态机监听器
	processor.toolFSM.AddListener(func(toolID string, oldState, newState ToolCallState, state *ToolState) {
		logger.Debug("工具状态变化",
			logger.String("tool_id", toolID),
			logger.String("old_state", oldState.String()),
			logger.String("new_state", newState.String()))
	})

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
	cmp.toolFSM.Reset()
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
	// 移除非标准事件处理器：TOOL_CALL_RESULT, TOOL_EXECUTION_START, TOOL_EXECUTION_END
	cmp.eventHandlers[EventTypes.TOOL_CALL_ERROR] = &ToolCallErrorHandler{cmp.toolManager}
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
		if err := utils.FastUnmarshal([]byte(candidate), &testData); err == nil {
			jsonPayload = candidate
		}
	} else if strings.HasPrefix(payloadStr, "event{") && len(payloadStr) > 5 {
		// 验证移除前缀后是否为有效JSON
		candidate := payloadStr[5:] // 移除 "event" 前缀
		var testData map[string]interface{}
		if err := utils.FastUnmarshal([]byte(candidate), &testData); err == nil {
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
		if err := utils.FastUnmarshal(message.Payload, &errorData); err != nil {
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
		if err := utils.FastUnmarshal(message.Payload, &exceptionData); err != nil {
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

// === 事件处理器实现在 message_event_handlers.go 中 ===
