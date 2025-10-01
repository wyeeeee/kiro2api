package server

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"kiro2api/auth"
	"kiro2api/logger"
	"kiro2api/parser"
	"kiro2api/types"
	"kiro2api/utils"

	"github.com/gin-gonic/gin"
)

// extractRelevantHeaders 提取相关的请求头信息
func extractRelevantHeaders(c *gin.Context) map[string]string {
	relevantHeaders := map[string]string{}

	// 提取关键的请求头
	headerKeys := []string{
		"Content-Type",
		"Authorization",
		"X-API-Key",
		"X-Request-ID",
		"X-Forwarded-For",
		"Accept",
		"Accept-Encoding",
	}

	for _, key := range headerKeys {
		if value := c.GetHeader(key); value != "" {
			// 对敏感信息进行脱敏处理
			if key == "Authorization" && len(value) > 20 {
				relevantHeaders[key] = value[:10] + "***" + value[len(value)-7:]
			} else if key == "X-API-Key" && len(value) > 10 {
				relevantHeaders[key] = value[:5] + "***" + value[len(value)-3:]
			} else {
				relevantHeaders[key] = value
			}
		}
	}

	return relevantHeaders
}

// handleStreamRequest 处理流式请求
func handleStreamRequest(c *gin.Context, anthropicReq types.AnthropicRequest, token types.TokenInfo) {
	// 转换为TokenWithUsage（简化版本）
	tokenWithUsage := &types.TokenWithUsage{
		TokenInfo:      token,
		AvailableCount: 100, // 默认可用次数
		LastUsageCheck: time.Now(),
	}
	sender := &AnthropicStreamSender{}
	handleGenericStreamRequest(c, anthropicReq, tokenWithUsage, sender, createAnthropicStreamEvents)
}

// isDebugMode 检查是否启用调试模式
func isDebugMode() bool {
	// 检查DEBUG环境变量
	if debug := os.Getenv("DEBUG"); debug == "true" || debug == "1" {
		return true
	}

	// 检查LOG_LEVEL是否为debug
	if logLevel := os.Getenv("LOG_LEVEL"); strings.ToLower(logLevel) == "debug" {
		return true
	}

	// 检查GIN_MODE是否为debug
	if ginMode := os.Getenv("GIN_MODE"); ginMode == "debug" {
		return true
	}

	return false
}

// containsToolResults 检测请求是否包含工具结果
func containsToolResults(anthropicReq types.AnthropicRequest) bool {
	for _, message := range anthropicReq.Messages {
		if message.Role == "user" {
			switch content := message.Content.(type) {
			case []any:
				for _, item := range content {
					if block, ok := item.(map[string]any); ok {
						if blockType, exists := block["type"]; exists {
							if typeStr, ok := blockType.(string); ok && typeStr == "tool_result" {
								return true
							}
						}
					}
				}
			case []types.ContentBlock:
				for _, block := range content {
					if block.Type == "tool_result" {
						return true
					}
				}
			}
		}
	}
	return false
}

// generateToolResultFollowUp 为工具结果提交生成后续内容
func generateToolResultFollowUp(anthropicReq types.AnthropicRequest) string {
	// 根据最近的工具结果生成相应的跟进内容
	var toolResults []string

	for _, message := range anthropicReq.Messages {
		if message.Role == "user" {
			switch content := message.Content.(type) {
			case []any:
				for _, item := range content {
					if block, ok := item.(map[string]any); ok {
						if blockType, exists := block["type"]; exists {
							if typeStr, ok := blockType.(string); ok && typeStr == "tool_result" {
								if toolContent, exists := block["content"]; exists {
									if contentStr, ok := toolContent.(string); ok && contentStr != "" {
										toolResults = append(toolResults, contentStr)
									}
								}
							}
						}
					}
				}
			case []types.ContentBlock:
				for _, block := range content {
					if block.Type == "tool_result" && block.Content != nil {
						if contentStr, ok := block.Content.(string); ok && contentStr != "" {
							toolResults = append(toolResults, contentStr)
						}
					}
				}
			}
		}
	}

	// 生成基于工具结果的跟进内容
	if len(toolResults) > 0 {
		// 检查是否是文件操作相关的工具结果
		for _, result := range toolResults {
			resultLower := strings.ToLower(result)
			if strings.Contains(resultLower, "文件") || strings.Contains(resultLower, "file") {
				if strings.Contains(resultLower, "成功") || strings.Contains(resultLower, "success") {
					return "文件操作已成功完成。"
				} else if strings.Contains(resultLower, "错误") || strings.Contains(resultLower, "error") {
					return "文件操作遇到了问题，我来帮您分析一下。"
				}
			}
			// 检查是否是命令执行结果
			if strings.Contains(resultLower, "command") || strings.Contains(resultLower, "执行") {
				return "命令执行完成。让我为您分析结果。"
			}
		}

		// 通用的工具执行完成消息
		return "工具执行完成，让我为您分析结果。"
	}

	// 默认的后续内容
	return "好的，基于您提供的信息，让我来帮您处理。"
}

// handleGenericStreamRequest 通用流式请求处理
func handleGenericStreamRequest(c *gin.Context, anthropicReq types.AnthropicRequest, token *types.TokenWithUsage, sender StreamEventSender, eventCreator func(string, string, string) []map[string]any) {
	// 创建SSE状态管理器，确保事件序列符合Claude规范
	sseStateManager := NewSSEStateManager(false) // 非严格模式，记录警告但不中断

	// 创建stop_reason管理器，确保符合Claude官方规范
	stopReasonManager := NewStopReasonManager(anthropicReq)

	// 创建token计算器
	tokenCalculator := utils.NewTokenCalculator()
	// 计算输入tokens
	inputTokens := tokenCalculator.CalculateInputTokens(anthropicReq)
	// 更完整的SSE响应头，禁用反向代理缓冲
	c.Header("Content-Type", "text/event-stream; charset=utf-8")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	// 确认底层Writer支持Flush
	if _, ok := c.Writer.(http.Flusher); !ok {
		err := sender.SendError(c, "连接不支持SSE刷新", fmt.Errorf("no flusher"))
		if err != nil {
			return
		}
		return
	}

	messageId := fmt.Sprintf("msg_%s", time.Now().Format("20060102150405"))
	// 注入 message_id 到上下文，便于统一日志会话标识
	c.Set("message_id", messageId)

	resp, err := execCWRequest(c, anthropicReq, token.TokenInfo, true)
	if err != nil {
		// 检查是否是模型未找到错误，如果是，则响应已经发送，不需要再次处理
		if _, ok := err.(*types.ModelNotFoundErrorType); ok {
			return
		}
		_ = sender.SendError(c, "构建请求失败", err)
		return
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)

	// 立即刷新响应头
	c.Writer.Flush()

	// 发送初始事件，处理包含图片的消息内容
	inputContent := ""
	if len(anthropicReq.Messages) > 0 {
		inputContent, _ = utils.GetMessageContent(anthropicReq.Messages[len(anthropicReq.Messages)-1].Content)
	}
	initialEvents := eventCreator(messageId, inputContent, anthropicReq.Model)
	for _, event := range initialEvents {
		// 更新流式事件中的input_tokens
		// event本身就是map[string]any类型，直接使用
		if message, exists := event["message"]; exists {
			if msgMap, ok := message.(map[string]any); ok {
				if usage, exists := msgMap["usage"]; exists {
					if usageMap, ok := usage.(map[string]any); ok {
						usageMap["input_tokens"] = inputTokens
					}
				}
			}
		}
		// 使用状态管理器发送事件，确保符合Claude规范
		err := sseStateManager.SendEvent(c, sender, event)
		if err != nil {
			logger.Error("初始SSE事件发送失败", logger.Err(err))
			return
		}
	}

	// 创建符合AWS规范的流式解析器
	compliantParser := parser.NewCompliantEventStreamParser(false) // 默认非严格模式

	// 统计输出token（以字符数近似）
	totalOutputChars := 0

	// 维护 tool_use 区块索引到 tool_use_id 的映射，确保仅在对应的 stop 时标记完成
	// 注意：由于这个map类型是map[int]string，而对象池提供的是map[string]any，直接使用make
	toolUseIdByBlockIndex := make(map[int]string)

	// 用于接收所有原始数据的字符串（使用对象池优化）
	rawDataBuffer := utils.GetStringBuilder()
	defer utils.PutStringBuilder(rawDataBuffer)

	// 使用对象池获取字节缓冲区，避免频繁分配
	buf := utils.GetByteSlice()
	defer utils.PutByteSlice(buf)
	buf = buf[:1024] // 限制为1024字节
	// 文本增量简单聚合，减少断句割裂：累计到中文标点/换行或达到阈值再下发
	pendingText := ""
	pendingIndex := 0
	hasPending := false
	// 新增：最小聚合阈值，避免极短片段导致颗粒化；默认10字符
	const minFlushChars = 10
	// 新增：简单去重，避免重复段落多次发送
	lastFlushedText := ""
	// 提取公共的文本冲刷逻辑，避免重复（DRY）
	flushPending := func() {
		if hasPending {
			trimmed := strings.TrimSpace(pendingText)
			if len([]rune(trimmed)) >= 2 && trimmed != strings.TrimSpace(lastFlushedText) {
				flush := map[string]any{
					"type":  "content_block_delta",
					"index": pendingIndex,
					"delta": map[string]any{
						"type": "text_delta",
						"text": pendingText,
					},
				}
				// 使用状态管理器发送事件，确保与主流程使用相同的发送机制
				if err := sseStateManager.SendEvent(c, sender, flush); err != nil {
					logger.Error("flushPending事件发送违规", logger.Err(err))
				}
				lastFlushedText = pendingText
				totalOutputChars += len(pendingText)
			} else {
				logger.Debug("跳过重复/过短文本片段A",
					addReqFields(c,
						logger.Int("len", len(pendingText)),
					)...)
			}
			hasPending = false
			pendingText = ""
		}
	}

	totalReadBytes := 0

	// 跟踪解析状态用于元数据收集
	var lastParseErr error
	totalProcessedEvents := 0

	for {
		n, err := resp.Body.Read(buf)
		totalReadBytes += n

		if n > 0 {
			// 将原始数据写入缓冲区
			rawDataBuffer.Write(buf[:n])

			// 使用符合规范的解析器解析流式数据
			events, parseErr := compliantParser.ParseStream(buf[:n])
			lastParseErr = parseErr // 保存最后的解析错误
			if parseErr != nil {
				logger.Warn("符合规范的解析器处理失败",
					addReqFields(c,
						logger.Err(parseErr),
						logger.Int("read_bytes", n),
						logger.String("direction", "upstream_response"),
					)...)
				// 在非严格模式下继续处理
			}

			totalProcessedEvents += len(events) // 累计处理的事件数量
			logger.Debug("解析到符合规范的CW事件批次",
				addReqFields(c,
					logger.String("direction", "upstream_response"),
					logger.Int("batch_events", len(events)),
					logger.Int("read_bytes", n),
					logger.Bool("has_parse_error", parseErr != nil),
				)...)
			for _, event := range events {

				// 增强调试：记录关键事件字段
				if dataMap, ok := event.Data.(map[string]any); ok {
					if t, ok := dataMap["type"].(string); ok {
						switch t {
						case "content_block_start":
							// 不在tool_use开始前冲刷文本，避免在tool_use之前插入额外的text_delta
							if cb, ok := dataMap["content_block"].(map[string]any); ok {
								if cbType, _ := cb["type"].(string); cbType == "tool_use" {
									logger.Debug("转发tool_use开始",
										logger.String("tool_use_id", func() string {
											if s, _ := cb["id"].(string); s != "" {
												return s
											}
											return ""
										}()),
										logger.String("tool_name", func() string {
											if s, _ := cb["name"].(string); s != "" {
												return s
											}
											return ""
										}()),
										logger.Int("index", func() int {
											if v, ok := dataMap["index"].(int); ok {
												return v
											}
											if f, ok := dataMap["index"].(float64); ok {
												return int(f)
											}
											return -1
										}()))

									// 记录索引到 tool_use_id 的映射，供 stop 时精确标记
									idx := func() int {
										if v, ok := dataMap["index"].(int); ok {
											return v
										}
										if f, ok := dataMap["index"].(float64); ok {
											return int(f)
										}
										return -1
									}()
									if idx >= 0 {
										if id, _ := cb["id"].(string); id != "" {
											toolUseIdByBlockIndex[idx] = id
											logger.Debug("建立tool_use索引映射",
												logger.Int("index", idx),
												logger.String("tool_use_id", id))
										}
									}
								}
							}
						case "content_block_delta":
							if delta, ok := dataMap["delta"].(map[string]any); ok {
								if dType, _ := delta["type"].(string); dType == "input_json_delta" {
									// 只打印前128字符
									if pj, ok := delta["partial_json"]; ok {
										var s string
										switch v := pj.(type) {
										case string:
											s = v
										case *string:
											if v != nil {
												s = *v
											}
										}
										if len(s) > 128 {
											s = s[:128] + "..."
										}
										logger.Debug("转发tool_use参数增量",
											logger.Int("index", func() int {
												if v, ok := dataMap["index"].(int); ok {
													return v
												}
												if f, ok := dataMap["index"].(float64); ok {
													return int(f)
												}
												return -1
											}()),
											logger.Int("partial_len", len(s)),
											logger.String("partial_preview", s))

										// 额外：尝试解析出 file_path 与 content 长度（用于快速验证）
										if s != "" {
											if strings.Contains(s, "file_path") || strings.Contains(s, "content") {
												logger.Debug("Write参数预览", logger.String("raw", s))
											}
										}
									}
								} else if dType == "text_delta" {
									if txt, ok := delta["text"].(string); ok {
										// 聚合逻辑：累计到中文句末标点/换行或长度阈值
										idx := 0
										if v, ok := dataMap["index"].(int); ok {
											idx = v
										} else if f, ok := dataMap["index"].(float64); ok {
											idx = int(f)
										}
										pendingIndex = idx
										pendingText += txt
										hasPending = true

										shouldFlush := false
										// 首先：达到基本长度阈值
										if len(pendingText) >= minFlushChars {
											shouldFlush = true
										}
										// 或者：遇到中文标点/换行
										if strings.ContainsAny(txt, "。！？；\n") || len(pendingText) >= 64 {
											shouldFlush = true
										}
										if shouldFlush {
											// 使用统一的flushPending函数，避免重复逻辑
											flushPending()
										}
										// 调试日志
										preview := txt
										if len(preview) > 64 {
											preview = preview[:64] + "..."
										}
										logger.Debug("转发文本增量",
											addReqFields(c,
												logger.Int("len", len(txt)),
												logger.String("preview", preview),
												logger.String("direction", "downstream_send"),
											)...)
										// 跳过原始事件的直接发送（因为已聚合发送或等待后续）
										continue
									}
								}
							}
						case "content_block_stop":
							// 在停止前冲刷挂起文本
							flushPending()

							// 仅在 tool_use 的对应索引 stop 时标记该工具完成，避免误标记
							idx := func() int {
								if v, ok := dataMap["index"].(int); ok {
									return v
								}
								if f, ok := dataMap["index"].(float64); ok {
									return int(f)
								}
								return -1
							}()
							if idx >= 0 {
								if toolId, exists := toolUseIdByBlockIndex[idx]; exists && toolId != "" {
									logger.Debug("工具执行完成",
										logger.String("tool_id", toolId),
										logger.Int("block_index", idx))
									// 清理映射，防止泄漏
									delete(toolUseIdByBlockIndex, idx)
								} else {
									logger.Debug("非tool_use或未知索引的内容块结束",
										logger.Int("block_index", idx))
								}
							}

							logger.Debug("转发内容块结束",
								logger.Int("index", idx))
						case "message_delta":
							if delta, ok := dataMap["delta"].(map[string]any); ok {
								if sr, _ := delta["stop_reason"].(string); sr != "" {
									logger.Debug("转发消息增量",
										logger.String("stop_reason", sr))
								}
							}
						}
					}
				}

				// 使用状态管理器发送事件，确保符合Claude规范
				if eventDataMap, ok := event.Data.(map[string]any); ok {
					if err := sseStateManager.SendEvent(c, sender, eventDataMap); err != nil {
						logger.Error("SSE事件发送违规", logger.Err(err))
						// 非严格模式下，违规事件被跳过但不中断流
					}
				} else {
					logger.Warn("事件数据类型不匹配，跳过", logger.String("event_type", event.Event))
				}

				if event.Event == "content_block_delta" {
					content, _ := utils.GetMessageContent(event.Data)
					// 如果该事件是我们自行聚合发出的，就不会走到这里；
					// 但若落入此处，说明直接转发了文本增量，也加入统计
					totalOutputChars += len(content)
				}
				c.Writer.Flush()
			}
		}
		if err != nil {
			if err == io.EOF {

				logger.Debug("响应流结束",
					addReqFields(c,
						logger.Int("total_read_bytes", totalReadBytes),
					)...)
			} else {
				logger.Error("读取响应流时发生错误",
					addReqFields(c,
						logger.Err(err),
						logger.Int("total_read_bytes", totalReadBytes),
						logger.String("direction", "upstream_response"),
					)...)
			}
			break
		}
	}

	// 发送结束事件
	// 冲刷可能遗留的文本
	if pendingText != "" && hasPending {
		// 冲刷尾部挂起内容
		flushPending()
	}

	// *** 关键修复：处理空响应流的情况 ***
	// 当CodeWhisperer返回空响应时，特别是工具结果提交场景，需要生成适当的内容
	if totalReadBytes == 0 && totalProcessedEvents == 0 {
		logger.Debug("检测到空响应流，分析处理策略",
			addReqFields(c,
				logger.Int("total_read_bytes", totalReadBytes),
				logger.Int("processed_events", totalProcessedEvents),
				logger.Bool("contains_tool_results", containsToolResults(anthropicReq)),
			)...)

		var compensationContent string

		// 检查是否是工具结果提交场景
		if containsToolResults(anthropicReq) {
			logger.Debug("检测到工具结果提交场景，生成补偿内容",
				addReqFields(c,
					logger.String("scenario", "tool_result_submission"),
				)...)
			compensationContent = generateToolResultFollowUp(anthropicReq)
		} else {
			// 尝试获取解析器的聚合内容
			aggregatedContent := compliantParser.GetCompletionBuffer()
			if aggregatedContent != "" {
				logger.Debug("发现解析器聚合内容，使用聚合内容",
					addReqFields(c,
						logger.String("content_preview", func() string {
							if len(aggregatedContent) > 100 {
								return aggregatedContent[:100] + "..."
							}
							return aggregatedContent
						}()),
						logger.Int("content_length", len(aggregatedContent)),
					)...)
				compensationContent = aggregatedContent
			} else {
				logger.Debug("未发现聚合内容，跳过补偿",
					addReqFields(c,
						logger.String("scenario", "empty_response_no_compensation"),
					)...)
			}
		}

		// 如果有补偿内容，生成content_block_delta事件
		if compensationContent != "" {
			contentBlockDelta := map[string]any{
				"type":  "content_block_delta",
				"index": 0,
				"delta": map[string]any{
					"type": "text_delta",
					"text": compensationContent,
				},
			}

			if err := sseStateManager.SendEvent(c, sender, contentBlockDelta); err == nil {
				totalOutputChars += len(compensationContent)
				logger.Debug("成功发送补偿内容事件",
					addReqFields(c,
						logger.Int("compensation_length", len(compensationContent)),
						logger.String("compensation_preview", func() string {
							if len(compensationContent) > 50 {
								return compensationContent[:50] + "..."
							}
							return compensationContent
						}()),
					)...)
			} else {
				logger.Error("发送补偿内容事件失败", logger.Err(err))
			}
		}
	}

	// *** 使用新的stop_reason管理器，确保符合Claude官方规范 ***
	// 更新工具调用状态
	stopReasonManager.UpdateToolCallStatus(
		len(toolUseIdByBlockIndex) > 0, // 有活跃工具
		len(toolUseIdByBlockIndex) > 0, // 有已完成工具
	)

	// 设置实际使用的tokens
	stopReasonManager.SetActualTokensUsed(tokenCalculator.CalculateOutputTokens(rawDataBuffer.String()[:utils.IntMin(totalOutputChars*4, rawDataBuffer.Len())], len(toolUseIdByBlockIndex) > 0))

	// 根据Claude官方规范确定stop_reason
	stopReason := stopReasonManager.DetermineStopReason()

	// 使用符合规范的stop_reason创建结束事件，包含完整的usage信息
	outputTokens := tokenCalculator.CalculateOutputTokens(rawDataBuffer.String()[:utils.IntMin(totalOutputChars*4, rawDataBuffer.Len())], len(toolUseIdByBlockIndex) > 0)
	finalEvents := createAnthropicFinalEvents(outputTokens, inputTokens, stopReason)

	logger.Debug("创建结束事件",
		logger.String("stop_reason", stopReason),
		logger.String("stop_reason_description", GetStopReasonDescription(stopReason)),
		logger.Int("output_tokens", outputTokens))

	// *** 关键修复：在发送message_delta之前，显式关闭所有未关闭的content_block ***
	// 确保符合Claude规范：所有content_block_stop必须在message_delta之前发送
	activeBlocks := sseStateManager.GetActiveBlocks()
	for index, block := range activeBlocks {
		if block.Started && !block.Stopped {
			stopEvent := map[string]any{
				"type":  "content_block_stop",
				"index": index,
			}
			logger.Debug("最终事件前关闭未关闭的content_block", logger.Int("index", index))
			if err := sseStateManager.SendEvent(c, sender, stopEvent); err != nil {
				logger.Error("关闭content_block失败", logger.Err(err), logger.Int("index", index))
			}
		}
	}

	for _, event := range finalEvents {
		// 使用状态管理器发送结束事件，确保符合Claude规范
		if err := sseStateManager.SendEvent(c, sender, event); err != nil {
			logger.Error("结束事件发送违规", logger.Err(err))
		}
	}

	// 输出接收到的所有原始数据，支持回放和测试
	rawData := rawDataBuffer.String()
	rawDataBytes := []byte(rawData)

	// 生成请求ID（如果messageId不够唯一，可以使用更复杂的生成方式）
	requestID := fmt.Sprintf("req_%s_%d", messageId, time.Now().Unix())

	// 收集元数据
	metadata := utils.Metadata{
		ClientIP:       c.ClientIP(),
		UserAgent:      c.GetHeader("User-Agent"),
		RequestHeaders: extractRelevantHeaders(c),
		ParseSuccess:   lastParseErr == nil, // 使用最后一次解析的错误状态
		EventsCount:    totalProcessedEvents,
	}

	// 仅在debug模式下保存原始数据以供回放和测试
	if isDebugMode() {
		if err := utils.SaveRawDataForReplay(rawDataBytes, requestID, messageId, anthropicReq.Model, true, metadata); err != nil {
			logger.Warn("保存原始数据失败", logger.Err(err))
		}
	}

	// 保留原有的调试日志
	logger.Debug("完整原始数据接收完成",
		addReqFields(c,
			logger.Int("total_bytes", len(rawData)),
			logger.String("request_id", requestID),
			logger.String("save_status", "saved_for_replay"),
		)...)
}

// createAnthropicStreamEvents 创建Anthropic流式初始事件
func createAnthropicStreamEvents(messageId, inputContent, model string) []map[string]any {
	// 创建token计算器来计算输入tokens
	tokenCalculator := utils.NewTokenCalculator()
	// 基于输入内容估算输入tokens
	inputTokens := tokenCalculator.EstimateTokensFromChars(len(inputContent))

	// 创建完整的初始事件序列，包括content_block_start
	// 这确保符合Claude API规范的完整SSE事件序列
	events := []map[string]any{
		{
			"type": "message_start",
			"message": map[string]any{
				"id":            messageId,
				"type":          "message",
				"role":          "assistant",
				"content":       []any{},
				"model":         model,
				"stop_reason":   nil,
				"stop_sequence": nil,
				"usage": map[string]any{
					"input_tokens":  inputTokens,
					"output_tokens": 1,
				},
			},
		},
		{
			"type": "ping",
		},
		{
			"type":  "content_block_start",
			"index": 0,
			"content_block": map[string]any{
				"type": "text",
				"text": "",
			},
		},
	}
	return events
}

// createAnthropicFinalEvents 创建Anthropic流式结束事件
func createAnthropicFinalEvents(outputTokens, inputTokens int, stopReason string) []map[string]any {
	// 构建符合Claude规范的完整usage信息
	usage := map[string]any{
		"output_tokens": outputTokens,
	}

	// 根据Claude规范，message_delta中的usage应包含完整的token统计
	// 包括输入tokens和可能的缓存相关tokens
	if inputTokens > 0 {
		// 注意：这里为了演示缓存机制，我们假设有一部分输入来自缓存
		// 实际实现中，这些值应该从真实的缓存系统中获取
		cacheThreshold := 100 // 假设超过100个输入token的部分可能来自缓存

		if inputTokens > cacheThreshold {
			// 模拟缓存使用情况：将部分输入token标记为缓存相关
			cacheTokens := utils.IntMin(32, inputTokens-cacheThreshold) // 最多32个缓存token
			regularInputTokens := inputTokens - cacheTokens

			usage["input_tokens"] = regularInputTokens
			usage["cache_read_input_tokens"] = cacheTokens
		} else {
			usage["input_tokens"] = inputTokens
		}
	}

	// 根据Claude API规范，确保包含必要的content_block_stop事件
	// 这是为了处理可能缺失的content_block_stop事件
	events := []map[string]any{
		{
			"type":  "content_block_stop",
			"index": 0,
		},
		{
			"type": "message_delta",
			"delta": map[string]any{
				"stop_reason":   stopReason,
				"stop_sequence": nil,
			},
			"usage": usage,
		},
		{
			"type": "message_stop",
		},
	}

	return events
}

// handleNonStreamRequest 处理非流式请求
func handleNonStreamRequest(c *gin.Context, anthropicReq types.AnthropicRequest, token types.TokenInfo) {
	// 创建token计算器
	tokenCalculator := utils.NewTokenCalculator()
	// 计算输入tokens
	inputTokens := tokenCalculator.CalculateInputTokens(anthropicReq)

	resp, err := executeCodeWhispererRequest(c, anthropicReq, token, false)
	if err != nil {
		return
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)

	// 读取响应体
	body, err := utils.ReadHTTPResponse(resp.Body)
	if err != nil {
		handleResponseReadError(c, err)
		return
	}

	// 使用新的符合AWS规范的解析器，但在非流式模式下增加超时保护
	compliantParser := parser.NewCompliantEventStreamParser(false) // 宽松模式
	compliantParser.SetMaxErrors(5)                                // 限制最大错误次数以防死循环

	// 为非流式解析添加超时保护
	result, err := func() (*parser.ParseResult, error) {
		done := make(chan struct{})
		var result *parser.ParseResult
		var err error

		go func() {
			defer func() {
				if r := recover(); r != nil {
					err = fmt.Errorf("解析器panic: %v", r)
				}
				close(done)
			}()
			result, err = compliantParser.ParseResponse(body)
		}()

		select {
		case <-done:
			return result, err
		case <-time.After(10 * time.Second): // 10秒超时
			logger.Error("非流式解析超时")
			return nil, fmt.Errorf("解析超时")
		}
	}()

	if err != nil {
		logger.Error("非流式解析失败",
			logger.Err(err),
			logger.String("model", anthropicReq.Model),
			logger.Int("response_size", len(body)))

		// 提供更详细的错误信息和建议
		errorResp := gin.H{
			"error":   "响应解析失败",
			"type":    "parsing_error",
			"message": "无法解析AWS CodeWhisperer响应格式",
		}

		// 根据错误类型提供不同的HTTP状态码
		statusCode := http.StatusInternalServerError
		if strings.Contains(err.Error(), "解析超时") {
			statusCode = http.StatusRequestTimeout
			errorResp["message"] = "请求处理超时，请稍后重试"
		} else if strings.Contains(err.Error(), "格式错误") {
			statusCode = http.StatusBadRequest
			errorResp["message"] = "请求格式不正确"
		}

		c.JSON(statusCode, errorResp)
		return
	}

	// 转换为Anthropic格式
	var contexts = []map[string]any{}
	textAgg := result.GetCompletionText()

	// 先获取工具管理器的所有工具，确保sawToolUse的判断基于实际工具
	toolManager := compliantParser.GetToolManager()
	allTools := make([]*parser.ToolExecution, 0)

	// 获取活跃工具
	for _, tool := range toolManager.GetActiveTools() {
		allTools = append(allTools, tool)
	}

	// 获取已完成工具
	for _, tool := range toolManager.GetCompletedTools() {
		allTools = append(allTools, tool)
	}

	// 基于实际工具数量判断是否包含工具调用
	sawToolUse := len(allTools) > 0

	logger.Debug("非流式响应处理完成",
		addReqFields(c,
			logger.String("text_content", textAgg[:utils.IntMin(100, len(textAgg))]),
			logger.Int("tool_calls_count", len(allTools)),
			logger.Bool("saw_tool_use", sawToolUse),
		)...)

	// 添加文本内容
	if textAgg != "" {
		contexts = append(contexts, map[string]any{
			"type": "text",
			"text": textAgg,
		})
	}

	// 添加工具调用
	// 工具已经在前面从toolManager获取到allTools中
	logger.Debug("从工具生命周期管理器获取工具调用",
		logger.Int("total_tools", len(allTools)),
		logger.Int("parse_result_tools", len(result.GetToolCalls())))

	for _, tool := range allTools {
		logger.Debug("添加工具调用到响应",
			logger.String("tool_id", tool.ID),
			logger.String("tool_name", tool.Name),
			logger.String("tool_status", tool.Status.String()),
			logger.Any("tool_arguments", tool.Arguments))

		// 创建标准的tool_use块，确保包含完整的状态信息
		toolUseBlock := map[string]any{
			"type":  "tool_use",
			"id":    tool.ID,
			"name":  tool.Name,
			"input": tool.Arguments,
		}

		// 如果工具参数为空或nil，确保为空对象而不是nil
		if tool.Arguments == nil {
			toolUseBlock["input"] = map[string]any{}
		}

		// 添加详细的调试日志，验证tool_use块格式
		if toolUseBlockJSON, err := utils.SafeMarshal(toolUseBlock); err == nil {
			logger.Debug("发送给Claude CLI的tool_use块详细结构",
				logger.String("tool_id", tool.ID),
				logger.String("tool_name", tool.Name),
				logger.String("tool_use_json", string(toolUseBlockJSON)),
				logger.String("input_type", fmt.Sprintf("%T", tool.Arguments)),
				logger.Any("arguments_value", tool.Arguments))
		}

		contexts = append(contexts, toolUseBlock)

		// 记录工具调用完成状态，帮助客户端识别工具调用已完成
		logger.Debug("工具调用已添加到响应",
			logger.String("tool_id", tool.ID),
			logger.String("tool_name", tool.Name))
	}

	// 使用新的stop_reason管理器，确保符合Claude官方规范
	stopReasonManager := NewStopReasonManager(anthropicReq)
	outputTokens := tokenCalculator.CalculateOutputTokens(textAgg, sawToolUse)
	stopReasonManager.SetActualTokensUsed(outputTokens)
	stopReasonManager.UpdateToolCallStatus(sawToolUse, sawToolUse)
	stopReason := stopReasonManager.DetermineStopReason()

	logger.Debug("非流式响应stop_reason决策",
		logger.String("stop_reason", stopReason),
		logger.String("description", GetStopReasonDescription(stopReason)),
		logger.Bool("saw_tool_use", sawToolUse),
		logger.Int("output_tokens", outputTokens))

	anthropicResp := map[string]any{
		"content":       contexts,
		"model":         anthropicReq.Model,
		"role":          "assistant",
		"stop_reason":   stopReason,
		"stop_sequence": nil,
		"type":          "message",
		"usage": map[string]any{
			"input_tokens":  inputTokens,
			"output_tokens": outputTokens,
		},
	}

	logger.Debug("非流式响应最终数据",
		logger.String("stop_reason", stopReason),
		logger.Int("content_blocks", len(contexts)))

	logger.Debug("下发非流式响应",
		addReqFields(c,
			logger.String("direction", "downstream_send"),
			logger.Bool("saw_tool_use", sawToolUse),
			logger.Int("content_count", len(contexts)),
		)...)
	c.JSON(http.StatusOK, anthropicResp)
}

// createTokenPreview 创建token预览显示格式 (***+后10位)
func createTokenPreview(token string) string {
	if len(token) <= 10 {
		// 如果token太短，全部用*代替
		return strings.Repeat("*", len(token))
	}

	// 3个*号 + 后10位
	suffix := token[len(token)-10:]
	return "***" + suffix
}

// handleTokenPoolAPI 处理Token池API请求 - 恢复多token显示
func handleTokenPoolAPI(c *gin.Context) {
	var tokenList []any
	var activeCount int

	// 从auth包获取配置信息
	configs, err := auth.GetConfigs()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "加载配置失败: " + err.Error(),
		})
		return
	}

	if len(configs) == 0 {
		c.JSON(http.StatusOK, gin.H{
			"timestamp":     time.Now().Format(time.RFC3339),
			"total_tokens":  0,
			"active_tokens": 0,
			"tokens":        []any{},
			"pool_stats": map[string]any{
				"total_tokens":  0,
				"active_tokens": 0,
			},
		})
		return
	}

	// 遍历所有配置
	for i, config := range configs {
		// 检查配置是否被禁用
		if config.Disabled {
			tokenData := map[string]any{
				"index":           i,
				"user_email":      "已禁用",
				"token_preview":   "***已禁用",
				"auth_type":       strings.ToLower(config.AuthType),
				"remaining_usage": 0,
				"expires_at":      time.Now().Add(time.Hour).Format(time.RFC3339),
				"last_used":       "未知",
				"status":          "disabled",
				"error":           "配置已禁用",
			}
			tokenList = append(tokenList, tokenData)
			continue
		}

		// 尝试获取token信息
		tokenInfo, err := refreshSingleTokenByConfig(config)
		if err != nil {
			tokenData := map[string]any{
				"index":           i,
				"user_email":      "获取失败",
				"token_preview":   createTokenPreview(config.RefreshToken),
				"auth_type":       strings.ToLower(config.AuthType),
				"remaining_usage": 0,
				"expires_at":      time.Now().Add(time.Hour).Format(time.RFC3339),
				"last_used":       "未知",
				"status":          "error",
				"error":           err.Error(),
			}
			tokenList = append(tokenList, tokenData)
			continue
		}

		// 检查使用限制
		var usageInfo *types.UsageLimits
		var available float64 = 100.0 // 默认值 (浮点数)
		var userEmail = "未知用户"

		checker := auth.NewUsageLimitsChecker()
		if usage, checkErr := checker.CheckUsageLimits(tokenInfo); checkErr == nil {
			usageInfo = usage
			available = auth.CalculateAvailableCount(usage)

			// 提取用户邮箱
			if usage.UserInfo.Email != "" {
				userEmail = usage.UserInfo.Email
			}
		}

		// 构建token数据
		tokenData := map[string]any{
			"index":           i,
			"user_email":      userEmail,
			"token_preview":   createTokenPreview(tokenInfo.AccessToken),
			"auth_type":       strings.ToLower(config.AuthType),
			"remaining_usage": available,
			"expires_at":      tokenInfo.ExpiresAt.Format(time.RFC3339),
			"last_used":       time.Now().Format(time.RFC3339),
			"status":          "active",
		}

		// 添加使用限制详细信息 (基于CREDIT资源类型)
		if usageInfo != nil {
			for _, breakdown := range usageInfo.UsageBreakdownList {
				if breakdown.ResourceType == "CREDIT" {
					var totalLimit float64
					var totalUsed float64

					// 基础额度
					totalLimit += breakdown.UsageLimitWithPrecision
					totalUsed += breakdown.CurrentUsageWithPrecision

					// 免费试用额度
					if breakdown.FreeTrialInfo != nil && breakdown.FreeTrialInfo.FreeTrialStatus == "ACTIVE" {
						totalLimit += breakdown.FreeTrialInfo.UsageLimitWithPrecision
						totalUsed += breakdown.FreeTrialInfo.CurrentUsageWithPrecision
					}

					tokenData["usage_limits"] = map[string]any{
						"total_limit":   totalLimit,   // 保留浮点精度
						"current_usage": totalUsed,    // 保留浮点精度
						"is_exceeded":   available <= 0,
					}
					break
				}
			}
		}

		// 如果token不可用，标记状态
		if available <= 0 {
			tokenData["status"] = "exhausted"
		} else {
			activeCount++
		}

		// 如果是 IdC 认证，显示额外信息
		if config.AuthType == auth.AuthMethodIdC && config.ClientID != "" {
			tokenData["client_id"] = func() string {
				if len(config.ClientID) > 10 {
					return config.ClientID[:5] + "***" + config.ClientID[len(config.ClientID)-3:]
				}
				return config.ClientID
			}()
		}

		tokenList = append(tokenList, tokenData)
	}

	// 返回多token数据
	c.JSON(http.StatusOK, gin.H{
		"timestamp":     time.Now().Format(time.RFC3339),
		"total_tokens":  len(tokenList),
		"active_tokens": activeCount,
		"tokens":        tokenList,
		"pool_stats": map[string]any{
			"total_tokens":  len(configs),
			"active_tokens": activeCount,
		},
	})
}

// refreshSingleTokenByConfig 根据配置刷新单个token
func refreshSingleTokenByConfig(config auth.AuthConfig) (types.TokenInfo, error) {
	switch config.AuthType {
	case auth.AuthMethodSocial:
		return auth.RefreshSocialToken(config.RefreshToken)
	case auth.AuthMethodIdC:
		return auth.RefreshIdCToken(config)
	default:
		return types.TokenInfo{}, fmt.Errorf("不支持的认证类型: %s", config.AuthType)
	}
}

// 已移除复杂的token数据收集函数，现在使用简单的内存数据读取
