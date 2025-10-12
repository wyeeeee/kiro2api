package server

import (
	"fmt"
	"io"
	"strings"
	"time"

	"kiro2api/config"
	"kiro2api/logger"
	"kiro2api/parser"
	"kiro2api/types"
	"kiro2api/utils"

	"github.com/gin-gonic/gin"
)

// StreamProcessorContext 流处理上下文，封装所有流处理状态
// 遵循单一职责原则：专注于流式数据处理
type StreamProcessorContext struct {
	// 请求上下文
	c           *gin.Context
	req         types.AnthropicRequest
	token       *types.TokenWithUsage
	sender      StreamEventSender
	messageID   string
	inputTokens int

	// 状态管理器
	sseStateManager   *SSEStateManager
	stopReasonManager *StopReasonManager
	tokenEstimator    *utils.TokenEstimator

	// 流解析器
	compliantParser *parser.CompliantEventStreamParser

	// 统计信息
	totalOutputChars     int
	totalReadBytes       int
	totalProcessedEvents int
	lastParseErr         error

	// 文本聚合状态
	pendingText     string
	lastFlushedText string

	// 工具调用跟踪
	toolUseIdByBlockIndex map[int]string
	completedToolUseIds   map[string]bool // 已完成的工具ID集合（用于stop_reason判断）

	// 原始数据缓冲
	rawDataBuffer *strings.Builder
}

// NewStreamProcessorContext 创建流处理上下文
func NewStreamProcessorContext(
	c *gin.Context,
	req types.AnthropicRequest,
	token *types.TokenWithUsage,
	sender StreamEventSender,
	messageID string,
	inputTokens int,
) *StreamProcessorContext {
	return &StreamProcessorContext{
		c:                     c,
		req:                   req,
		token:                 token,
		sender:                sender,
		messageID:             messageID,
		inputTokens:           inputTokens,
		sseStateManager:       NewSSEStateManager(false),
		stopReasonManager:     NewStopReasonManager(req),
		tokenEstimator:        utils.NewTokenEstimator(),
		compliantParser:       parser.NewCompliantEventStreamParser(false),
		toolUseIdByBlockIndex: make(map[int]string),
		completedToolUseIds:   make(map[string]bool),
		rawDataBuffer:         utils.GetStringBuilder(),
	}
}

// Cleanup 清理资源
// 完整清理所有状态，防止内存泄漏
func (ctx *StreamProcessorContext) Cleanup() {
	// 清理字符串构建器（对象池）
	if ctx.rawDataBuffer != nil {
		utils.PutStringBuilder(ctx.rawDataBuffer)
		ctx.rawDataBuffer = nil
	}

	// 重置解析器状态
	if ctx.compliantParser != nil {
		ctx.compliantParser.Reset()
	}

	// 清理工具调用映射
	if ctx.toolUseIdByBlockIndex != nil {
		// 清空map，释放内存
		for k := range ctx.toolUseIdByBlockIndex {
			delete(ctx.toolUseIdByBlockIndex, k)
		}
		ctx.toolUseIdByBlockIndex = nil
	}

	// 清理已完成工具集合
	if ctx.completedToolUseIds != nil {
		for k := range ctx.completedToolUseIds {
			delete(ctx.completedToolUseIds, k)
		}
		ctx.completedToolUseIds = nil
	}

	// 清空文本聚合状态
	ctx.pendingText = ""
	ctx.lastFlushedText = ""

	// 清理管理器引用，帮助GC
	ctx.sseStateManager = nil
	ctx.stopReasonManager = nil
	ctx.tokenEstimator = nil
}

// TextAggregator 文本聚合器，负责文本增量的智能聚合
// 遵循 KISS 原则：简单高效的聚合逻辑
type TextAggregator struct {
	pendingText     string
	pendingIndex    int
	hasPending      bool
	lastFlushedText string
	minFlushChars   int
	maxFlushChars   int
}

// NewTextAggregator 创建文本聚合器
func NewTextAggregator() *TextAggregator {
	return &TextAggregator{
		minFlushChars: config.MinTextFlushChars,
		maxFlushChars: config.TextFlushMaxChars,
	}
}

// AddText 添加文本片段
func (ta *TextAggregator) AddText(index int, text string) {
	ta.pendingIndex = index
	ta.pendingText += text
	ta.hasPending = true
}

// ShouldFlush 判断是否应该冲刷文本
func (ta *TextAggregator) ShouldFlush() bool {
	if !ta.hasPending {
		return false
	}

	// 达到基本长度阈值
	if len(ta.pendingText) >= ta.minFlushChars {
		return true
	}

	// 遇到中文或英文标点、换行时立即冲刷
	// 包含：句号、感叹号、问号、分号、冒号、逗号、顿号、换行
	if strings.ContainsAny(ta.pendingText, "。！？；：，、\n.!?;:,") {
		return true
	}

	// 达到最大长度
	if len(ta.pendingText) >= ta.maxFlushChars {
		return true
	}

	return false
}

// Flush 冲刷待处理文本，返回要发送的事件
func (ta *TextAggregator) Flush() (map[string]any, bool) {
	if !ta.hasPending {
		return nil, false
	}

	trimmed := strings.TrimSpace(ta.pendingText)
	// 去重检查
	if len([]rune(trimmed)) < config.MinTextLength || trimmed == strings.TrimSpace(ta.lastFlushedText) {
		ta.hasPending = false
		ta.pendingText = ""
		return nil, false
	}

	event := map[string]any{
		"type":  "content_block_delta",
		"index": ta.pendingIndex,
		"delta": map[string]any{
			"type": "text_delta",
			"text": ta.pendingText,
		},
	}

	ta.lastFlushedText = ta.pendingText
	ta.hasPending = false
	ta.pendingText = ""

	return event, true
}

// GetPendingTextLength 获取待处理文本长度
func (ta *TextAggregator) GetPendingTextLength() int {
	return len(ta.pendingText)
}

// initializeSSEResponse 初始化SSE响应头
func initializeSSEResponse(c *gin.Context) error {
	// 设置SSE响应头，禁用反向代理缓冲
	c.Header("Content-Type", "text/event-stream; charset=utf-8")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	// 确认底层Writer支持Flush
	if _, ok := c.Writer.(io.Writer); !ok {
		return fmt.Errorf("writer不支持SSE刷新")
	}

	c.Writer.Flush()
	return nil
}

// sendInitialEvents 发送初始事件
func (ctx *StreamProcessorContext) sendInitialEvents(eventCreator func(string, int, string) []map[string]any) error {
	// 直接使用上下文中的 inputTokens（已经通过 TokenEstimator 精确计算）
	initialEvents := eventCreator(ctx.messageID, ctx.inputTokens, ctx.req.Model)

	// 注意：初始事件现在只包含 message_start 和 ping
	// content_block_start 会在收到实际内容时由 sse_state_manager 自动生成
	// 这避免了发送空内容块（如果上游只返回 tool_use 而没有文本）
	for _, event := range initialEvents {
		// 使用状态管理器发送事件
		if err := ctx.sseStateManager.SendEvent(ctx.c, ctx.sender, event); err != nil {
			logger.Error("初始SSE事件发送失败", logger.Err(err))
			return err
		}
	}

	return nil
}

// processToolUseStart 处理工具使用开始事件
func (ctx *StreamProcessorContext) processToolUseStart(dataMap map[string]any) {
	cb, ok := dataMap["content_block"].(map[string]any)
	if !ok {
		return
	}

	cbType, _ := cb["type"].(string)
	if cbType != "tool_use" {
		return
	}

	// 提取索引
	idx := extractIndex(dataMap)
	if idx < 0 {
		return
	}

	// 提取tool_use_id
	id, _ := cb["id"].(string)
	if id == "" {
		return
	}

	// 记录索引到tool_use_id的映射
	ctx.toolUseIdByBlockIndex[idx] = id

	logger.Debug("转发tool_use开始",
		logger.String("tool_use_id", id),
		logger.String("tool_name", getStringField(cb, "name")),
		logger.Int("index", idx))
}

// processToolUseStop 处理工具使用结束事件
func (ctx *StreamProcessorContext) processToolUseStop(dataMap map[string]any) {
	idx := extractIndex(dataMap)
	if idx < 0 {
		return
	}

	if toolId, exists := ctx.toolUseIdByBlockIndex[idx]; exists && toolId != "" {
		// *** 关键修复：在删除前先记录到已完成工具集合 ***
		// 问题：直接删除导致sendFinalEvents()中len(toolUseIdByBlockIndex)==0
		// 结果：stop_reason错误判断为end_turn而非tool_use
		// 解决：先添加到completedToolUseIds，保持工具调用的证据
		ctx.completedToolUseIds[toolId] = true

		logger.Debug("工具执行完成",
			logger.String("tool_id", toolId),
			logger.Int("block_index", idx))
		delete(ctx.toolUseIdByBlockIndex, idx)
	} else {
		logger.Debug("非tool_use或未知索引的内容块结束",
			logger.Int("block_index", idx))
	}
}

// processTextDelta 处理文本增量事件
func (ctx *StreamProcessorContext) processTextDelta(dataMap map[string]any, aggregator *TextAggregator) bool {
	delta, ok := dataMap["delta"].(map[string]any)
	if !ok {
		return false
	}

	dType, _ := delta["type"].(string)
	if dType != "text_delta" {
		return false
	}

	txt, ok := delta["text"].(string)
	if !ok {
		return false
	}

	idx := extractIndex(dataMap)
	aggregator.AddText(idx, txt)

	// 调试日志
	preview := txt
	if len(preview) > config.DebugPayloadPreviewLength {
		preview = preview[:config.DebugPayloadPreviewLength] + "..."
	}
	logger.Debug("转发文本增量",
		addReqFields(ctx.c,
			logger.Int("len", len(txt)),
			logger.String("preview", preview),
			logger.String("direction", "downstream_send"),
		)...)

	return true // 返回true表示已处理，不需要转发原始事件
}

// processToolInputDelta 处理工具参数增量
func processToolInputDelta(dataMap map[string]any) {
	delta, ok := dataMap["delta"].(map[string]any)
	if !ok {
		return
	}

	dType, _ := delta["type"].(string)
	if dType != "input_json_delta" {
		return
	}

	partialJSON := getStringField(delta, "partial_json")
	if partialJSON == "" {
		return
	}

	// 只打印前N字符
	preview := partialJSON
	if len(preview) > config.ToolInputPreviewLength {
		preview = preview[:config.ToolInputPreviewLength] + "..."
	}

	idx := extractIndex(dataMap)
	logger.Debug("转发tool_use参数增量",
		logger.Int("index", idx),
		logger.Int("partial_len", len(preview)),
		logger.String("partial_preview", preview))

	// 额外：尝试解析出file_path与content长度
	if strings.Contains(partialJSON, "file_path") || strings.Contains(partialJSON, "content") {
		logger.Debug("Write参数预览", logger.String("raw", preview))
	}
}

// sendFinalEvents 发送结束事件
func (ctx *StreamProcessorContext) sendFinalEvents() error {
	// 关闭所有未关闭的content_block
	activeBlocks := ctx.sseStateManager.GetActiveBlocks()
	for index, block := range activeBlocks {
		if block.Started && !block.Stopped {
			stopEvent := map[string]any{
				"type":  "content_block_stop",
				"index": index,
			}
			logger.Debug("最终事件前关闭未关闭的content_block", logger.Int("index", index))
			if err := ctx.sseStateManager.SendEvent(ctx.c, ctx.sender, stopEvent); err != nil {
				logger.Error("关闭content_block失败", logger.Err(err), logger.Int("index", index))
			}
		}
	}

	// 更新工具调用状态
	// 使用已完成工具集合来判断，因为toolUseIdByBlockIndex在stop时已被清空
	hasActiveTools := len(ctx.toolUseIdByBlockIndex) > 0
	hasCompletedTools := len(ctx.completedToolUseIds) > 0

	logger.Debug("更新工具调用状态",
		logger.Bool("has_active_tools", hasActiveTools),
		logger.Bool("has_completed_tools", hasCompletedTools),
		logger.Int("active_count", len(ctx.toolUseIdByBlockIndex)),
		logger.Int("completed_count", len(ctx.completedToolUseIds)))

	ctx.stopReasonManager.UpdateToolCallStatus(hasActiveTools, hasCompletedTools)

	// 计算输出tokens（使用TokenEstimator统一算法）
	content := ctx.rawDataBuffer.String()[:utils.IntMin(ctx.totalOutputChars*4, ctx.rawDataBuffer.Len())]
	baseTokens := ctx.tokenEstimator.EstimateTextTokens(content)

	// 如果包含工具调用，增加结构化开销
	outputTokens := baseTokens
	if len(ctx.toolUseIdByBlockIndex) > 0 {
		outputTokens = int(float64(baseTokens) * config.ToolCallTokenOverhead)
	}
	if outputTokens < config.MinOutputTokens && len(content) > 0 {
		outputTokens = config.MinOutputTokens
	}

	// 设置实际使用的tokens
	ctx.stopReasonManager.SetActualTokensUsed(outputTokens)

	// 确定stop_reason
	stopReason := ctx.stopReasonManager.DetermineStopReason()

	logger.Debug("创建结束事件",
		logger.String("stop_reason", stopReason),
		logger.String("stop_reason_description", GetStopReasonDescription(stopReason)),
		logger.Int("output_tokens", outputTokens))

	// 创建并发送结束事件
	finalEvents := createAnthropicFinalEvents(outputTokens, ctx.inputTokens, stopReason)
	for _, event := range finalEvents {
		if err := ctx.sseStateManager.SendEvent(ctx.c, ctx.sender, event); err != nil {
			logger.Error("结束事件发送违规", logger.Err(err))
		}
	}

	return nil
}

// saveRawDataForReplay 保存原始数据用于调试
func (ctx *StreamProcessorContext) saveRawDataForReplay() {
	if !config.IsSaveRawDataEnabled() {
		return
	}

	rawData := ctx.rawDataBuffer.String()
	rawDataBytes := []byte(rawData)
	requestID := fmt.Sprintf("req_%s_%d", ctx.messageID, time.Now().Unix())

	metadata := utils.Metadata{
		ClientIP:       ctx.c.ClientIP(),
		UserAgent:      ctx.c.GetHeader("User-Agent"),
		RequestHeaders: extractRelevantHeaders(ctx.c),
		ParseSuccess:   ctx.lastParseErr == nil,
		EventsCount:    ctx.totalProcessedEvents,
	}

	if err := utils.SaveRawDataForReplay(rawDataBytes, requestID, ctx.messageID, ctx.req.Model, true, metadata); err != nil {
		logger.Warn("保存原始数据失败", logger.Err(err))
	}

	logger.Debug("完整原始数据接收完成",
		addReqFields(ctx.c,
			logger.Int("total_bytes", len(rawData)),
			logger.String("request_id", requestID),
			logger.String("save_status", "saved_for_replay"),
		)...)
}

// 辅助函数

// extractIndex 从数据映射中提取索引
func extractIndex(dataMap map[string]any) int {
	if v, ok := dataMap["index"].(int); ok {
		return v
	}
	if f, ok := dataMap["index"].(float64); ok {
		return int(f)
	}
	return -1
}

// getStringField 从映射中安全提取字符串字段
func getStringField(m map[string]any, key string) string {
	if s, ok := m[key].(string); ok {
		return s
	}
	return ""
}

// EventStreamProcessor 事件流处理器
// 遵循单一职责原则：专注于处理事件流
type EventStreamProcessor struct {
	ctx        *StreamProcessorContext
	aggregator *TextAggregator
}

// NewEventStreamProcessor 创建事件流处理器
func NewEventStreamProcessor(ctx *StreamProcessorContext) *EventStreamProcessor {
	return &EventStreamProcessor{
		ctx:        ctx,
		aggregator: NewTextAggregator(),
	}
}

// ProcessEventStream 处理事件流的主循环
func (esp *EventStreamProcessor) ProcessEventStream(reader io.Reader) error {
	buf := utils.GetByteSlice()
	defer utils.PutByteSlice(buf)
	buf = buf[:1024] // 限制为1024字节

	for {
		n, err := reader.Read(buf)
		esp.ctx.totalReadBytes += n

		if n > 0 {
			// 写入原始数据缓冲区
			esp.ctx.rawDataBuffer.Write(buf[:n])

			// 解析事件流
			events, parseErr := esp.ctx.compliantParser.ParseStream(buf[:n])
			esp.ctx.lastParseErr = parseErr

			if parseErr != nil {
				logger.Warn("符合规范的解析器处理失败",
					addReqFields(esp.ctx.c,
						logger.Err(parseErr),
						logger.Int("read_bytes", n),
						logger.String("direction", "upstream_response"),
					)...)
			}

			esp.ctx.totalProcessedEvents += len(events)
			logger.Debug("解析到符合规范的CW事件批次",
				addReqFields(esp.ctx.c,
					logger.String("direction", "upstream_response"),
					logger.Int("batch_events", len(events)),
					logger.Int("read_bytes", n),
					logger.Bool("has_parse_error", parseErr != nil),
				)...)

			// 处理每个事件
			for _, event := range events {
				if err := esp.processEvent(event); err != nil {
					return err
				}
			}
		}

		if err != nil {
			if err == io.EOF {
				logger.Debug("响应流结束",
					addReqFields(esp.ctx.c,
						logger.Int("total_read_bytes", esp.ctx.totalReadBytes),
					)...)
			} else {
				logger.Error("读取响应流时发生错误",
					addReqFields(esp.ctx.c,
						logger.Err(err),
						logger.Int("total_read_bytes", esp.ctx.totalReadBytes),
						logger.String("direction", "upstream_response"),
					)...)
			}
			break
		}
	}

	// 冲刷剩余文本
	return esp.flushRemainingText()
}

// processEvent 处理单个事件
func (esp *EventStreamProcessor) processEvent(event parser.SSEEvent) error {
	dataMap, ok := event.Data.(map[string]any)
	if !ok {
		logger.Warn("事件数据类型不匹配，跳过", logger.String("event_type", event.Event))
		return nil
	}

	eventType, _ := dataMap["type"].(string)

	// 处理不同类型的事件
	switch eventType {
	case "content_block_start":
		// 在启动新块前，先冲刷待处理文本（防止自动关闭文本块时丢失文本）
		if err := esp.flushPendingText(); err != nil {
			return err
		}
		esp.ctx.processToolUseStart(dataMap)

	case "content_block_delta":
		// 处理文本增量（聚合发送）
		if esp.processContentBlockDelta(dataMap) {
			return nil // 已聚合，不转发原始事件
		}

	case "content_block_stop":
		// 冲刷待处理文本
		if err := esp.flushPendingText(); err != nil {
			return err
		}
		esp.ctx.processToolUseStop(dataMap)
		logger.Debug("转发内容块结束", logger.Int("index", extractIndex(dataMap)))

	case "message_delta":
		if delta, ok := dataMap["delta"].(map[string]any); ok {
			if sr, _ := delta["stop_reason"].(string); sr != "" {
				logger.Debug("转发消息增量", logger.String("stop_reason", sr))
			}
		}

	case "exception":
		// 处理上游异常事件，检查是否需要映射为max_tokens
		if esp.handleExceptionEvent(dataMap) {
			return nil // 已转换并发送，不转发原始exception事件
		}
	}

	// 使用状态管理器发送事件
	if err := esp.ctx.sseStateManager.SendEvent(esp.ctx.c, esp.ctx.sender, dataMap); err != nil {
		logger.Error("SSE事件发送违规", logger.Err(err))
		// 非严格模式下，违规事件被跳过但不中断流
	}

	// 更新输出字符统计
	if event.Event == "content_block_delta" {
		content, _ := utils.GetMessageContent(event.Data)
		esp.ctx.totalOutputChars += len(content)
	}

	esp.ctx.c.Writer.Flush()
	return nil
}

// processContentBlockDelta 处理content_block_delta事件
// 返回true表示已处理（聚合），不需要转发原始事件
func (esp *EventStreamProcessor) processContentBlockDelta(dataMap map[string]any) bool {
	delta, ok := dataMap["delta"].(map[string]any)
	if !ok {
		return false
	}

	deltaType, _ := delta["type"].(string)

	switch deltaType {
	case "text_delta":
		// 文本增量：聚合处理
		if esp.ctx.processTextDelta(dataMap, esp.aggregator) {
			// 检查是否需要冲刷
			if esp.aggregator.ShouldFlush() {
				_ = esp.flushPendingText()
			}
			return true // 已聚合，跳过原始事件
		}

	case "input_json_delta":
		// 工具参数增量：直接记录日志
		processToolInputDelta(dataMap)
	}

	return false
}

// handleExceptionEvent 处理上游异常事件，检查是否需要映射为max_tokens
// 返回true表示已处理并转换，不需要转发原始exception事件
func (esp *EventStreamProcessor) handleExceptionEvent(dataMap map[string]any) bool {
	// 提取异常类型
	exceptionType, _ := dataMap["exception_type"].(string)

	// 检查是否为内容长度超限异常
	if exceptionType == "ContentLengthExceededException" ||
		strings.Contains(exceptionType, "CONTENT_LENGTH_EXCEEDS") {

		logger.Info("检测到内容长度超限异常，映射为max_tokens stop_reason",
			addReqFields(esp.ctx.c,
				logger.String("exception_type", exceptionType),
				logger.String("claude_stop_reason", "max_tokens"))...)

		// 先冲刷待处理文本
		_ = esp.flushPendingText()

		// 关闭所有活跃的content_block
		activeBlocks := esp.ctx.sseStateManager.GetActiveBlocks()
		for index, block := range activeBlocks {
			if block.Started && !block.Stopped {
				stopEvent := map[string]any{
					"type":  "content_block_stop",
					"index": index,
				}
				_ = esp.ctx.sseStateManager.SendEvent(esp.ctx.c, esp.ctx.sender, stopEvent)
			}
		}

		// 构造符合Claude规范的max_tokens响应
		maxTokensEvent := map[string]any{
			"type": "message_delta",
			"delta": map[string]any{
				"stop_reason":   "max_tokens",
				"stop_sequence": nil,
			},
			"usage": map[string]any{
				"input_tokens":  esp.ctx.inputTokens,
				"output_tokens": esp.ctx.totalOutputChars / config.TokenEstimationRatio, // 简单估算
			},
		}

		// 发送max_tokens事件
		if err := esp.ctx.sseStateManager.SendEvent(esp.ctx.c, esp.ctx.sender, maxTokensEvent); err != nil {
			logger.Error("发送max_tokens响应失败", logger.Err(err))
			return false
		}

		// 发送message_stop事件
		stopEvent := map[string]any{
			"type": "message_stop",
		}
		if err := esp.ctx.sseStateManager.SendEvent(esp.ctx.c, esp.ctx.sender, stopEvent); err != nil {
			logger.Error("发送message_stop失败", logger.Err(err))
			return false
		}

		esp.ctx.c.Writer.Flush()

		return true // 已转换并发送，不转发原始exception
	}

	// 其他类型的异常，正常转发
	return false
}

// flushPendingText 冲刷待处理文本
func (esp *EventStreamProcessor) flushPendingText() error {
	event, ok := esp.aggregator.Flush()
	if !ok {
		return nil
	}

	if err := esp.ctx.sseStateManager.SendEvent(esp.ctx.c, esp.ctx.sender, event); err != nil {
		logger.Error("flushPending事件发送违规", logger.Err(err))
		return err
	}

	// 更新统计
	if delta, ok := event["delta"].(map[string]any); ok {
		if text, ok := delta["text"].(string); ok {
			esp.ctx.totalOutputChars += len(text)
		}
	}

	return nil
}

// flushRemainingText 冲刷剩余文本
func (esp *EventStreamProcessor) flushRemainingText() error {
	if esp.aggregator.GetPendingTextLength() > 0 {
		return esp.flushPendingText()
	}
	return nil
}
