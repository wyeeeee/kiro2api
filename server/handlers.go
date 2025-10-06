package server

import (
	"errors"
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

// handleGenericStreamRequest 通用流式请求处理
func handleGenericStreamRequest(c *gin.Context, anthropicReq types.AnthropicRequest, token *types.TokenWithUsage, sender StreamEventSender, eventCreator func(string, string, string) []map[string]any) {
	// 计算输入tokens
	estimator := utils.NewTokenEstimator()
	countReq := &types.CountTokensRequest{
		Model:    anthropicReq.Model,
		System:   anthropicReq.System,
		Messages: anthropicReq.Messages,
		Tools:    anthropicReq.Tools,
	}
	inputTokens := estimator.EstimateTokens(countReq)

	// 初始化SSE响应
	if err := initializeSSEResponse(c); err != nil {
		_ = sender.SendError(c, "连接不支持SSE刷新", err)
		return
	}

	// 生成消息ID并注入上下文
	messageID := fmt.Sprintf("msg_%s", time.Now().Format("20060102150405"))
	c.Set("message_id", messageID)

	// 执行CodeWhisperer请求
	resp, err := execCWRequest(c, anthropicReq, token.TokenInfo, true)
	if err != nil {
		var modelNotFoundErrorType *types.ModelNotFoundErrorType
		if errors.As(err, &modelNotFoundErrorType) {
			return
		}
		_ = sender.SendError(c, "构建请求失败", err)
		return
	}
	defer resp.Body.Close()

	// 创建流处理上下文
	ctx := NewStreamProcessorContext(c, anthropicReq, token, sender, messageID, inputTokens)
	defer ctx.Cleanup()

	// 发送初始事件
	if err := ctx.sendInitialEvents(eventCreator); err != nil {
		return
	}

	// 处理事件流
	processor := NewEventStreamProcessor(ctx)
	if err := processor.ProcessEventStream(resp.Body); err != nil {
		logger.Error("事件流处理失败", logger.Err(err))
		return
	}

	// 发送结束事件
	if err := ctx.sendFinalEvents(); err != nil {
		logger.Error("发送结束事件失败", logger.Err(err))
		return
	}

	// 保存原始数据用于调试
	ctx.saveRawDataForReplay()
}

// createAnthropicStreamEvents 创建Anthropic流式初始事件
func createAnthropicStreamEvents(messageId, inputContent, model string) []map[string]any {
	// 基于输入内容估算输入tokens（英文平均4字符/token）
	inputTokens := len(inputContent) / 4
	if inputTokens < 1 && len(inputContent) > 0 {
		inputTokens = 1
	}

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
		"input_tokens":  inputTokens,
	}

	// 根据Claude规范，message_delta中的usage应包含完整的token统计
	// 包括输入tokens和可能的缓存相关tokens

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
	// 计算输入tokens
	estimator := utils.NewTokenEstimator()
	countReq := &types.CountTokensRequest{
		Model:    anthropicReq.Model,
		System:   anthropicReq.System,
		Messages: anthropicReq.Messages,
		Tools:    anthropicReq.Tools,
	}
	inputTokens := estimator.EstimateTokens(countReq)

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
	var contexts []map[string]any
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

	// 计算输出tokens（使用TokenEstimator统一算法）
	baseTokens := estimator.EstimateTextTokens(textAgg)
	outputTokens := baseTokens
	if sawToolUse {
		outputTokens = int(float64(baseTokens) * 1.2) // 增加20%结构化开销
	}
	if outputTokens < 1 && len(textAgg) > 0 {
		outputTokens = 1
	}

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
	for i, authConfig := range configs {
		// 检查配置是否被禁用
		if authConfig.Disabled {
			tokenData := map[string]any{
				"index":           i,
				"user_email":      "已禁用",
				"token_preview":   "***已禁用",
				"auth_type":       strings.ToLower(authConfig.AuthType),
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
		tokenInfo, err := refreshSingleTokenByConfig(authConfig)
		if err != nil {
			tokenData := map[string]any{
				"index":           i,
				"user_email":      "获取失败",
				"token_preview":   createTokenPreview(authConfig.RefreshToken),
				"auth_type":       strings.ToLower(authConfig.AuthType),
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
		var available = 100.0 // 默认值 (浮点数)
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
			"auth_type":       strings.ToLower(authConfig.AuthType),
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
						"total_limit":   totalLimit, // 保留浮点精度
						"current_usage": totalUsed,  // 保留浮点精度
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
		if authConfig.AuthType == auth.AuthMethodIdC && authConfig.ClientID != "" {
			tokenData["client_id"] = func() string {
				if len(authConfig.ClientID) > 10 {
					return authConfig.ClientID[:5] + "***" + authConfig.ClientID[len(authConfig.ClientID)-3:]
				}
				return authConfig.ClientID
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
