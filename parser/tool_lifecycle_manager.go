package parser

import (
	"fmt"
	"kiro2api/logger"
	"kiro2api/utils"
	"strings"
	"time"
)

// ToolLifecycleManager 工具调用生命周期管理器
type ToolLifecycleManager struct {
	activeTools       map[string]*ToolExecution
	completedTools    map[string]*ToolExecution
	blockIndexMap     map[string]int
	nextBlockIndex    int
	textIntroGenerated bool // 跟踪是否已生成文本介绍
}

// validateRequiredArguments 针对常见工具进行必填参数校验
// 支持各种工具名称格式，避免空参数触发 CLI 报错
func validateRequiredArguments(toolName string, args map[string]interface{}) (bool, string) {
	toolNameLower := strings.ToLower(toolName)
	switch toolNameLower {
	case "bash":
		if v, ok := args["command"].(string); !ok || strings.TrimSpace(v) == "" {
			return false, "Bash 缺少必填参数: command"
		}
	case "write":
		fp, fpo := args["file_path"].(string)
		ct, cto := args["content"].(string)
		if !fpo || strings.TrimSpace(fp) == "" {
			return false, "Write 缺少必填参数: file_path"
		}
		if !cto || strings.TrimSpace(ct) == "" {
			return false, "Write 缺少必填参数: content"
		}
	case "read":
		if fp, ok := args["file_path"].(string); !ok || strings.TrimSpace(fp) == "" {
			return false, "Read 缺少必填参数: file_path"
		}
	case "edit":
		fp, fpo := args["file_path"].(string)
		os, oso := args["old_string"].(string)
		_, nso := args["new_string"].(string)
		if !fpo || strings.TrimSpace(fp) == "" {
			return false, "Edit 缺少必填参数: file_path"
		}
		if !oso || strings.TrimSpace(os) == "" {
			return false, "Edit 缺少必填参数: old_string"
		}
		if !nso {
			return false, "Edit 缺少必填参数: new_string"
		}
	}
	return true, ""
}

// validateToolCallFormat 验证工具调用的基本格式
func (tlm *ToolLifecycleManager) validateToolCallFormat(toolCall ToolCall) error {
	// 验证基本字段
	if strings.TrimSpace(toolCall.ID) == "" {
		return fmt.Errorf("工具调用ID不能为空")
	}

	if strings.TrimSpace(toolCall.Function.Name) == "" {
		return fmt.Errorf("工具名称不能为空")
	}

	if toolCall.Type != "function" && toolCall.Type != "" {
		return fmt.Errorf("不支持的工具类型: %s", toolCall.Type)
	}

	// 验证参数JSON格式
	if toolCall.Function.Arguments != "" {
		var args map[string]interface{}
		if err := utils.FastUnmarshal([]byte(toolCall.Function.Arguments), &args); err != nil {
			return fmt.Errorf("工具参数JSON格式无效: %w", err)
		}
	}

	return nil
}

// NewToolLifecycleManager 创建工具生命周期管理器
func NewToolLifecycleManager() *ToolLifecycleManager {
	return &ToolLifecycleManager{
		activeTools:    make(map[string]*ToolExecution),
		completedTools: make(map[string]*ToolExecution),
		blockIndexMap:  make(map[string]int),
		nextBlockIndex: 1, // 索引0预留给文本内容
	}
}

// Reset 重置管理器状态
func (tlm *ToolLifecycleManager) Reset() {
	tlm.activeTools = make(map[string]*ToolExecution)
	tlm.completedTools = make(map[string]*ToolExecution)
	tlm.blockIndexMap = make(map[string]int)
	tlm.nextBlockIndex = 1
	tlm.textIntroGenerated = false // 重置文本介绍生成状态
}

// HandleToolCallRequest 处理工具调用请求
// HandleToolCallRequest 处理工具调用请求（增强参数验证）
func (tlm *ToolLifecycleManager) HandleToolCallRequest(request ToolCallRequest) []SSEEvent {
	events := make([]SSEEvent, 0, len(request.ToolCalls)*3) // 调整预分配容量，包含文本介绍

	// *** 关键修复：根据Claude规范，在第一个工具调用前自动生成文本介绍（index:0） ***
	if !tlm.textIntroGenerated && len(request.ToolCalls) > 0 {
		// 生成符合Claude规范的文本介绍事件序列
		textIntroEvents := tlm.generateTextIntroduction(request.ToolCalls[0])
		events = append(events, textIntroEvents...)
		tlm.textIntroGenerated = true

		logger.Debug("自动生成工具调用文本介绍",
			logger.Int("intro_events", len(textIntroEvents)),
			logger.String("first_tool", request.ToolCalls[0].Function.Name))
	}

	for _, toolCall := range request.ToolCalls {
		// 严格验证工具调用基本格式
		if err := tlm.validateToolCallFormat(toolCall); err != nil {
			logger.Warn("工具调用格式验证失败",
				logger.String("tool_id", toolCall.ID),
				logger.String("tool_name", toolCall.Function.Name),
				logger.Err(err))

			// 发送错误事件
			events = append(events, SSEEvent{
				Event: "error",
				Data: map[string]interface{}{
					"type": "invalid_request_error",
					"error": map[string]interface{}{
						"type":      "invalid_tool_parameters",
						"message":   err.Error(),
						"tool_id":   toolCall.ID,
						"tool_name": toolCall.Function.Name,
					},
				},
			})
			continue
		}

		// 检查工具是否已存在，避免重复创建
		if existing, exists := tlm.activeTools[toolCall.ID]; exists {
			logger.Debug("工具已存在，更新参数",
				logger.String("tool_id", toolCall.ID),
				logger.String("tool_name", toolCall.Function.Name),
				logger.String("existing_status", existing.Status.String()))

			// 解析工具调用参数
			var arguments map[string]interface{}
			if err := utils.SafeUnmarshal([]byte(toolCall.Function.Arguments), &arguments); err != nil {
				logger.Warn("解析工具调用参数失败",
					logger.String("tool_id", toolCall.ID),
					logger.String("tool_name", toolCall.Function.Name),
					logger.Err(err))
				arguments = make(map[string]interface{})
			}

			// 更新现有工具的参数
			if len(arguments) > 0 {
				existing.Arguments = arguments
			}
			continue
		}

		// 解析工具调用参数
		var arguments map[string]interface{}
		if err := utils.SafeUnmarshal([]byte(toolCall.Function.Arguments), &arguments); err != nil {
			logger.Warn("解析工具调用参数失败",
				logger.String("tool_id", toolCall.ID),
				logger.String("tool_name", toolCall.Function.Name),
				logger.Err(err))
			arguments = make(map[string]interface{})
		}

		// 针对常见工具做必填参数校验：仅记录告警，不阻断工具起始事件，遵循 Anthropic 流式协议
		if ok, msg := validateRequiredArguments(toolCall.Function.Name, arguments); !ok {
			logger.Warn("工具调用参数可能不完整（仅记录，不阻断）",
				logger.String("tool_id", toolCall.ID),
				logger.String("tool_name", toolCall.Function.Name),
				logger.String("reason", msg))
		}

		execution := &ToolExecution{
			ID:         toolCall.ID,
			Name:       toolCall.Function.Name,
			StartTime:  time.Now(),
			Status:     ToolStatusPending,
			Arguments:  arguments,
			BlockIndex: tlm.getOrAssignBlockIndex(toolCall.ID),
		}

		tlm.activeTools[toolCall.ID] = execution

		logger.Debug("开始处理工具调用",
			logger.String("tool_id", toolCall.ID),
			logger.String("tool_name", toolCall.Function.Name),
			logger.Int("block_index", execution.BlockIndex))

		// 1. 生成标准的 content_block_start 事件（符合Anthropic规范）
		// 这替代了原来的 TOOL_EXECUTION_START 非标准事件
		events = append(events, SSEEvent{
			Event: "content_block_start",
			Data: map[string]interface{}{
				"type":  "content_block_start",
				"index": execution.BlockIndex,
				"content_block": map[string]interface{}{
					"type":  "tool_use",
					"id":    toolCall.ID,
					"name":  toolCall.Function.Name,
					"input": arguments, // 使用解析后的参数而不是空对象
				},
			},
		})

		// 2. 如果有参数，生成参数输入增量事件
		if len(arguments) > 0 {
			argsJSON, _ := utils.SafeMarshal(arguments)
			events = append(events, SSEEvent{
				Event: "content_block_delta",
				Data: map[string]interface{}{
					"type":  "content_block_delta",
					"index": execution.BlockIndex,
					"delta": map[string]interface{}{
						"type":         "input_json_delta",
						"partial_json": string(argsJSON),
					},
				},
			})
		}

		execution.Status = ToolStatusRunning
	}

	return events
}

// HandleToolCallResult 处理工具调用结果
func (tlm *ToolLifecycleManager) HandleToolCallResult(result ToolCallResult) []SSEEvent {
	events := make([]SSEEvent, 0, 3) // 增加预分配容量以支持更多事件

	execution, exists := tlm.activeTools[result.ToolCallID]
	if !exists {
		logger.Warn("收到未知工具调用的结果",
			logger.String("tool_call_id", result.ToolCallID))
		return events
	}

	now := time.Now()
	execution.EndTime = &now
	execution.Result = result.Result
	execution.Status = ToolStatusCompleted

	// 计算执行时间
	executionTime := now.Sub(execution.StartTime).Milliseconds()
	if result.ExecutionTime > 0 {
		executionTime = result.ExecutionTime
	}

	logger.Debug("工具调用完成",
		logger.String("tool_id", result.ToolCallID),
		logger.String("tool_name", execution.Name),
		logger.Int64("execution_time", executionTime))

	// 1. 生成标准的 content_block_stop 事件（符合Anthropic规范）
	// 这替代了原来的 TOOL_CALL_RESULT 和 TOOL_EXECUTION_END 非标准事件
	events = append(events, SSEEvent{
		Event: "content_block_stop",
		Data: map[string]interface{}{
			"type":  "content_block_stop",
			"index": execution.BlockIndex,
		},
	})

	// 移动到已完成工具列表
	tlm.completedTools[result.ToolCallID] = execution
	delete(tlm.activeTools, result.ToolCallID)

	// 2. 如果所有工具都完成了，生成 message_delta 事件（符合Anthropic规范）
	// 这提供了工具执行完成的状态信息，替代了 TOOL_EXECUTION_END 的功能
	if tlm.allToolsCompleted() {
		events = append(events, SSEEvent{
			Event: "message_delta",
			Data: map[string]interface{}{
				"type": "message_delta",
				"delta": map[string]interface{}{
					"stop_reason":   "tool_use",
					"stop_sequence": nil,
				},
				"usage": map[string]interface{}{
					"output_tokens": 0,
				},
			},
		})

		// 注意：不在此处生成 message_stop 事件，避免与主流式处理的结束事件重复
		// message_stop 事件由 handlers.go 中的主流式处理逻辑统一管理
	}

	return events
}

// HandleToolCallError 处理工具调用错误
func (tlm *ToolLifecycleManager) HandleToolCallError(errorInfo ToolCallError) []SSEEvent {
	events := make([]SSEEvent, 0, 3) // 增加预分配容量

	execution, exists := tlm.activeTools[errorInfo.ToolCallID]
	if !exists {
		logger.Warn("收到未知工具调用的错误",
			logger.String("tool_call_id", errorInfo.ToolCallID))
		return events
	}

	now := time.Now()
	execution.EndTime = &now
	execution.Error = errorInfo.Error
	execution.Status = ToolStatusError

	executionTime := now.Sub(execution.StartTime).Milliseconds()

	logger.Warn("工具调用失败",
		logger.String("tool_id", errorInfo.ToolCallID),
		logger.String("tool_name", execution.Name),
		logger.String("error", errorInfo.Error),
		logger.Int64("execution_time", executionTime))

	// 1. 生成标准的错误事件（符合Anthropic规范）
	// 这替代了原来的 TOOL_CALL_ERROR 非标准事件
	events = append(events, SSEEvent{
		Event: "error",
		Data: map[string]interface{}{
			"type": "error",
			"error": map[string]interface{}{
				"type":         "tool_error",
				"message":      errorInfo.Error,
				"tool_call_id": errorInfo.ToolCallID,
			},
		},
	})

	// 2. 生成标准的 content_block_stop 事件（符合Anthropic规范）
	// 即使出错也要正确结束内容块
	events = append(events, SSEEvent{
		Event: "content_block_stop",
		Data: map[string]interface{}{
			"type":  "content_block_stop",
			"index": execution.BlockIndex,
		},
	})

	// 3. 生成 message_delta 事件表示消息因错误而停止
	// 这替代了 TOOL_EXECUTION_END 的功能，提供错误状态信息
	events = append(events, SSEEvent{
		Event: "message_delta",
		Data: map[string]interface{}{
			"type": "message_delta",
			"delta": map[string]interface{}{
				"stop_reason":   "tool_error",
				"stop_sequence": nil,
			},
			"usage": map[string]interface{}{
				"output_tokens": 0,
			},
		},
	})

	// 移动到已完成工具列表
	tlm.completedTools[errorInfo.ToolCallID] = execution
	delete(tlm.activeTools, errorInfo.ToolCallID)

	return events
}

// GetToolExecution 获取工具执行信息
func (tlm *ToolLifecycleManager) GetToolExecution(toolID string) *ToolExecution {
	if tool, exists := tlm.activeTools[toolID]; exists {
		return tool
	}
	if tool, exists := tlm.completedTools[toolID]; exists {
		return tool
	}
	return nil
}

// GetActiveTools 获取所有活跃的工具
func (tlm *ToolLifecycleManager) GetActiveTools() map[string]*ToolExecution {
	result := make(map[string]*ToolExecution)
	for id, tool := range tlm.activeTools {
		result[id] = tool
	}
	return result
}

// GetCompletedTools 获取所有已完成的工具
func (tlm *ToolLifecycleManager) GetCompletedTools() map[string]*ToolExecution {
	result := make(map[string]*ToolExecution)
	for id, tool := range tlm.completedTools {
		result[id] = tool
	}
	return result
}

// allToolsCompleted 检查是否所有工具都已完成
func (tlm *ToolLifecycleManager) allToolsCompleted() bool {
	return len(tlm.activeTools) == 0
}

// getOrAssignBlockIndex 获取或分配块索引
func (tlm *ToolLifecycleManager) getOrAssignBlockIndex(toolID string) int {
	if index, exists := tlm.blockIndexMap[toolID]; exists {
		return index
	}

	index := tlm.nextBlockIndex
	tlm.blockIndexMap[toolID] = index
	tlm.nextBlockIndex++
	return index
}

// GetBlockIndex 获取工具的块索引
func (tlm *ToolLifecycleManager) GetBlockIndex(toolID string) int {
	if index, exists := tlm.blockIndexMap[toolID]; exists {
		return index
	}
	return -1
}

// generateTextIntroduction 生成符合Claude规范的文本介绍事件序列
// 根据Claude官方示例，工具调用前应有文本介绍，如："Okay, let's check the weather for San Francisco, CA:"
func (tlm *ToolLifecycleManager) generateTextIntroduction(firstTool ToolCall) []SSEEvent {
	// 根据工具类型生成合适的介绍文本
	introText := tlm.generateIntroText(firstTool.Function.Name, firstTool.Function.Arguments)

	return []SSEEvent{
		// 1. content_block_start for text (index:0)
		{
			Event: "content_block_start",
			Data: map[string]interface{}{
				"type":  "content_block_start",
				"index": 0,
				"content_block": map[string]interface{}{
					"type": "text",
					"text": "",
				},
			},
		},
		// 2. content_block_delta for text introduction
		{
			Event: "content_block_delta",
			Data: map[string]interface{}{
				"type":  "content_block_delta",
				"index": 0,
				"delta": map[string]interface{}{
					"type": "text_delta",
					"text": introText,
				},
			},
		},
		// 3. content_block_stop for text (index:0)
		{
			Event: "content_block_stop",
			Data: map[string]interface{}{
				"type":  "content_block_stop",
				"index": 0,
			},
		},
	}
}

// generateIntroText 根据工具类型生成合适的介绍文本
func (tlm *ToolLifecycleManager) generateIntroText(toolName string, arguments string) string {
	switch strings.ToLower(toolName) {
	case "get_weather":
		// 尝试从参数中提取城市名
		if city := tlm.extractCityFromArgs(arguments); city != "" {
			return fmt.Sprintf("好的，让我为您查询%s的天气信息。", city)
		}
		return "好的，让我为您查询天气信息。"
	case "search", "web_search":
		return "好的，让我为您搜索相关信息。"
	case "calculator", "calc":
		return "好的，让我为您进行计算。"
	case "todowrite":
		return "好的，让我为您更新任务列表。"
	default:
		return fmt.Sprintf("好的，让我使用%s工具来帮助您。", toolName)
	}
}

// extractCityFromArgs 从工具参数中提取城市名
func (tlm *ToolLifecycleManager) extractCityFromArgs(arguments string) string {
	// 简单的JSON解析来提取city字段
	if strings.Contains(arguments, "city") {
		// 使用简单的字符串匹配来提取city值
		start := strings.Index(arguments, "\"city\"")
		if start >= 0 {
			substr := arguments[start:]
			valueStart := strings.Index(substr, ":")
			if valueStart >= 0 {
				valueStr := substr[valueStart+1:]
				valueStr = strings.TrimSpace(valueStr)
				if strings.HasPrefix(valueStr, "\"") {
					end := strings.Index(valueStr[1:], "\"")
					if end >= 0 {
						return valueStr[1 : end+1]
					}
				}
			}
		}
	}
	return ""
}

// GenerateToolSummary 生成工具执行摘要
func (tlm *ToolLifecycleManager) GenerateToolSummary() map[string]interface{} {
	activeCount := len(tlm.activeTools)
	completedCount := len(tlm.completedTools)
	errorCount := 0
	totalExecutionTime := int64(0)

	for _, tool := range tlm.completedTools {
		if tool.Status == ToolStatusError {
			errorCount++
		}
		if tool.EndTime != nil {
			totalExecutionTime += tool.EndTime.Sub(tool.StartTime).Milliseconds()
		}
	}

	return map[string]interface{}{
		"active_tools":         activeCount,
		"completed_tools":      completedCount,
		"error_tools":          errorCount,
		"total_execution_time": totalExecutionTime,
		"success_rate":         float64(completedCount-errorCount) / float64(completedCount+activeCount),
	}
}

// ParseToolCallFromLegacyEvent 从旧格式事件中解析工具调用
func (tlm *ToolLifecycleManager) ParseToolCallFromLegacyEvent(evt assistantResponseEvent, state *toolIndexState) []SSEEvent {
	// 对于工具调用聚合完成的情况，需要先注册工具调用，再处理结果
	if evt.ToolUseId != "" && evt.Name != "" && evt.Stop {
		// 检查工具调用是否已注册
		if _, exists := tlm.activeTools[evt.ToolUseId]; !exists {
			logger.Debug("工具调用聚合完成，先注册工具调用",
				logger.String("toolUseId", evt.ToolUseId),
				logger.String("name", evt.Name))

			// 先注册工具调用
			toolCall := ToolCall{
				ID:   evt.ToolUseId,
				Type: "function",
				Function: ToolCallFunction{
					Name:      evt.Name,
					Arguments: "{}",
				},
			}

			if evt.Input != nil && *evt.Input != "" {
				toolCall.Function.Arguments = *evt.Input
			}

			request := ToolCallRequest{
				ToolCalls: []ToolCall{toolCall},
			}

			// 注册工具调用请求
			requestEvents := tlm.HandleToolCallRequest(request)

			// 然后处理工具调用结果
			result := ToolCallResult{
				ToolCallID: evt.ToolUseId,
				Result:     evt.Content,
			}

			resultEvents := tlm.HandleToolCallResult(result)

			// 合并事件
			allEvents := make([]SSEEvent, 0, len(requestEvents)+len(resultEvents))
			allEvents = append(allEvents, requestEvents...)
			allEvents = append(allEvents, resultEvents...)

			return allEvents
		} else {
			// 工具调用已注册，直接处理结果
			result := ToolCallResult{
				ToolCallID: evt.ToolUseId,
				Result:     evt.Content,
			}

			return tlm.HandleToolCallResult(result)
		}
	}

	// 处理工具使用：开始与输入增量（原有逻辑保持不变）
	if evt.ToolUseId != "" && evt.Name != "" && !evt.Stop {
		toolCall := ToolCall{
			ID:   evt.ToolUseId,
			Type: "function",
			Function: ToolCallFunction{
				Name:      evt.Name,
				Arguments: "{}",
			},
		}

		if evt.Input != nil && *evt.Input != "" {
			toolCall.Function.Arguments = *evt.Input
		}

		request := ToolCallRequest{
			ToolCalls: []ToolCall{toolCall},
		}

		return tlm.HandleToolCallRequest(request)
	}

	return nil
}

// UpdateToolArguments 更新工具调用的参数
func (tlm *ToolLifecycleManager) UpdateToolArguments(toolID string, arguments map[string]interface{}) {
	logger.Debug("更新工具调用参数",
		logger.String("tool_id", toolID),
		logger.Any("arguments", arguments))

	// 检查活跃工具
	if execution, exists := tlm.activeTools[toolID]; exists {
		execution.Arguments = arguments
		logger.Debug("已更新活跃工具的参数",
			logger.String("tool_id", toolID),
			logger.String("tool_name", execution.Name))
		return
	}

	// 检查已完成工具
	if execution, exists := tlm.completedTools[toolID]; exists {
		execution.Arguments = arguments
		logger.Debug("已更新已完成工具的参数",
			logger.String("tool_id", toolID),
			logger.String("tool_name", execution.Name))
		return
	}

	logger.Warn("未找到要更新参数的工具",
		logger.String("tool_id", toolID))
}

// UpdateToolArgumentsFromJSON 从JSON字符串更新工具调用参数
func (tlm *ToolLifecycleManager) UpdateToolArgumentsFromJSON(toolID string, jsonArgs string) {
	var arguments map[string]interface{}
	if err := utils.SafeUnmarshal([]byte(jsonArgs), &arguments); err != nil {
		logger.Warn("解析工具参数JSON失败",
			logger.String("tool_id", toolID),
			logger.String("json", jsonArgs),
			logger.Err(err))
		return
	}

	tlm.UpdateToolArguments(toolID, arguments)
}
