package test

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"kiro2api/types"
	"kiro2api/utils"
)

// RealIntegrationTester 真实集成测试器
type RealIntegrationTester struct {
	server     *httptest.Server
	router     *gin.Engine
	mockAWS    *MockAWSServer
	logger     utils.Logger
	config     *RealTestConfig
}

// RealTestConfig 真实测试配置
type RealTestConfig struct {
	ServerTimeout    time.Duration `json:"server_timeout"`
	EnableMockAWS    bool          `json:"enable_mock_aws"`
	MockAWSPort      int           `json:"mock_aws_port"`
	EnableAuth       bool          `json:"enable_auth"`
	TestToken        string        `json:"test_token"`
	DebugMode        bool          `json:"debug_mode"`
}

// MockAWSServer 模拟AWS服务器
type MockAWSServer struct {
	server       *httptest.Server
	mockData     []byte
	responseType string
	logger       utils.Logger
}

// RealIntegrationResult 真实集成测试结果
type RealIntegrationResult struct {
	HTTPStatusCode   int                    `json:"http_status_code"`
	ResponseHeaders  map[string]string      `json:"response_headers"`
	StreamEvents     []interface{}          `json:"stream_events"`
	ResponseBody     string                 `json:"response_body"`
	ProcessingTime   time.Duration          `json:"processing_time"`
	Success          bool                   `json:"success"`
	ErrorMessage     string                 `json:"error_message,omitempty"`
	ValidationResult *ValidationResult      `json:"validation_result,omitempty"`
	Stats            RealIntegrationStats   `json:"stats"`
	
	// 兼容 SimulationResult 的字段
	CapturedSSEEvents []interface{}     `json:"captured_sse_events"`
	OutputText        string            `json:"output_text"`
	ErrorsEncountered []error           `json:"errors_encountered"`
	EventSequence     []EventCapture    `json:"event_sequence"`
}

// EventCapture 类型现已在 validation_framework.go 中定义

// RealIntegrationStats 真实集成统计
type RealIntegrationStats struct {
	RequestSize         int           `json:"request_size"`
	ResponseSize        int           `json:"response_size"`
	TotalEvents         int           `json:"total_events"`
	EventsByType        map[string]int `json:"events_by_type"`
	FirstEventTime      time.Duration `json:"first_event_time"`
	LastEventTime       time.Duration `json:"last_event_time"`
	MiddlewareLatency   time.Duration `json:"middleware_latency"`
	HandlerLatency      time.Duration `json:"handler_latency"`
}

// NewRealIntegrationTester 创建真实集成测试器
func NewRealIntegrationTester(config *RealTestConfig) *RealIntegrationTester {
	if config == nil {
		config = getDefaultRealTestConfig()
	}

	// 设置Gin为测试模式
	gin.SetMode(gin.TestMode)

	tester := &RealIntegrationTester{
		config: config,
		logger: utils.GetLogger(),
	}
	
	// 为测试强制设置Debug日志
	tester.logger.Info("RealIntegrationTester初始化", utils.Bool("enable_mock_aws", config.EnableMockAWS))

	// 初始化模拟AWS服务器
	if config.EnableMockAWS {
		tester.mockAWS = NewMockAWSServer()
	}

	// 初始化HTTP服务器
	tester.setupHTTPServer()

	return tester
}

// TestWithRawData 使用原始数据进行真实测试
func (r *RealIntegrationTester) TestWithRawData(rawDataFile string) (*RealIntegrationResult, error) {
	startTime := time.Now()

	r.logger.Debug("开始真实集成测试", utils.String("raw_data_file", rawDataFile))

	// 1. 加载原始数据
	analyzer, err := LoadHexDataFromFile(rawDataFile)
	if err != nil {
		return nil, fmt.Errorf("加载原始数据失败: %w", err)
	}

	hexData, err := analyzer.ParseHexData()
	if err != nil {
		return nil, fmt.Errorf("解析hex数据失败: %w", err)
	}

	// 2. 生成期望输出
	expectationGenerator := NewExpectationGenerator()
	parser := NewEventStreamParser()
	defer parser.Close()

	parseResult, err := parser.ParseEventStream(hexData.BinaryData)
	if err != nil {
		return nil, fmt.Errorf("解析事件流失败: %w", err)
	}

	expectedOutput, err := expectationGenerator.GenerateExpectations(parseResult.Events)
	if err != nil {
		return nil, fmt.Errorf("生成期望输出失败: %w", err)
	}

	// 3. 配置模拟AWS响应
	if r.mockAWS != nil {
		r.mockAWS.SetMockData(hexData.BinaryData, "event-stream")
	}

	// 4. 创建真实的HTTP请求
	request := r.createAnthropicRequest()
	httpReq, err := r.buildHTTPRequest(request)
	if err != nil {
		return nil, fmt.Errorf("构建HTTP请求失败: %w", err)
	}

	// 5. 执行真实的HTTP请求
	result, err := r.executeHTTPRequest(httpReq, expectedOutput, startTime)
	if err != nil {
		return nil, fmt.Errorf("执行HTTP请求失败: %w", err)
	}

	// 6. 填充兼容 SimulationResult 的字段
	result.CapturedSSEEvents = result.StreamEvents
	result.OutputText = result.ResponseBody
	result.ErrorsEncountered = []error{}
	if result.ErrorMessage != "" {
		result.ErrorsEncountered = append(result.ErrorsEncountered, fmt.Errorf(result.ErrorMessage))
	}
	
	// 从流事件中构建 EventSequence
	result.EventSequence = r.buildEventSequence(result.StreamEvents)
	
	// 调试：检查EventSequence中的事件类型
	r.logger.Info("EventSequence构建完成", 
		utils.Int("total_events", len(result.EventSequence)))
	
	eventTypeCount := make(map[string]int)
	hasContentBlockStart := false
	hasToolUseEvents := false
	
	for _, eventCapture := range result.EventSequence {
		eventTypeCount[eventCapture.EventType]++
		if eventCapture.EventType == "content_block_start" {
			hasContentBlockStart = true
			// 检查是否包含tool_use
			if cb, ok := eventCapture.Data["content_block"].(map[string]interface{}); ok {
				if cbType, ok := cb["type"].(string); ok && cbType == "tool_use" {
					hasToolUseEvents = true
					r.logger.Info("发现tool_use事件", 
						utils.String("tool_id", fmt.Sprintf("%v", cb["id"])),
						utils.String("tool_name", fmt.Sprintf("%v", cb["name"])))
				}
			}
		}
	}
	
	r.logger.Info("事件类型统计", 
		utils.Any("event_types", eventTypeCount),
		utils.Bool("has_content_block_start", hasContentBlockStart),
		utils.Bool("has_tool_use_events", hasToolUseEvents))

	r.logger.Debug("真实集成测试完成",
		utils.Duration("processing_time", result.ProcessingTime),
		utils.Bool("success", result.Success),
		utils.Int("events_captured", len(result.StreamEvents)))

	return result, nil
}

// TestWithRawDataDirect 使用原始二进制数据进行测试（取代 SimulateStreamProcessing）
func (r *RealIntegrationTester) TestWithRawDataDirect(binaryData []byte) (*RealIntegrationResult, error) {
	startTime := time.Now()

	r.logger.Debug("开始直接测试", utils.Int("data_size", len(binaryData)))

	// 1. 配置模拟AWS响应
	if r.mockAWS != nil {
		r.mockAWS.SetMockData(binaryData, "event-stream")
	}

	// 2. 创建真实的HTTP请求
	request := r.createAnthropicRequest()
	httpReq, err := r.buildHTTPRequest(request)
	if err != nil {
		return nil, fmt.Errorf("构建HTTP请求失败: %w", err)
	}

	// 3. 执行真实的HTTP请求
	result, err := r.executeHTTPRequest(httpReq, nil, startTime)
	if err != nil {
		return nil, fmt.Errorf("执行HTTP请求失败: %w", err)
	}

	// 4. 填充兼容字段
	result.CapturedSSEEvents = result.StreamEvents
	result.OutputText = result.ResponseBody
	result.ErrorsEncountered = []error{}
	if result.ErrorMessage != "" {
		result.ErrorsEncountered = append(result.ErrorsEncountered, fmt.Errorf(result.ErrorMessage))
	}
	
	// 从流事件中构建 EventSequence
	result.EventSequence = r.buildEventSequence(result.StreamEvents)

	r.logger.Debug("直接测试完成",
		utils.Duration("processing_time", result.ProcessingTime),
		utils.Bool("success", result.Success),
		utils.Int("events_captured", len(result.StreamEvents)))

	return result, nil
}

// buildEventSequence 从流事件构建事件序列
func (r *RealIntegrationTester) buildEventSequence(streamEvents []interface{}) []EventCapture {
	eventSequence := make([]EventCapture, 0, len(streamEvents))
	
	for i, event := range streamEvents {
		capture := EventCapture{
			Timestamp: time.Now(),
			Index:     i,
			Source:    "http_response",
		}
		
		if eventMap, ok := event.(map[string]interface{}); ok {
			capture.Data = eventMap
			if eventType, ok := eventMap["type"].(string); ok {
				capture.EventType = eventType
			}
		}
		
		eventSequence = append(eventSequence, capture)
	}
	
	return eventSequence
}

// setupHTTPServer 设置HTTP服务器
func (r *RealIntegrationTester) setupHTTPServer() {
	// 创建路由器
	r.router = gin.New()

	// 如果启用调试模式，添加日志中间件
	if r.config.DebugMode {
		r.router.Use(gin.Logger())
	}

	// 手动设置路由 - 模拟server包中的路由
	r.setupTestRoutes()

	// 创建测试服务器
	r.server = httptest.NewServer(r.router)

	r.logger.Debug("HTTP测试服务器已启动", utils.String("url", r.server.URL))
}

// setupTestRoutes 设置测试路由
func (r *RealIntegrationTester) setupTestRoutes() {
	// CORS中间件
	r.router.Use(func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization, x-api-key")
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(200)
			return
		}
		c.Next()
	})

	// 认证中间件（可选）
	if r.config.EnableAuth {
		r.router.Use(r.mockAuthMiddleware())
	}

	// GET /v1/models 端点
	r.router.GET("/v1/models", func(c *gin.Context) {
		models := []map[string]interface{}{
			{
				"id":           "claude-sonnet-4-20250514",
				"object":       "model",
				"created":      1234567890,
				"owned_by":     "anthropic",
				"display_name": "claude-sonnet-4-20250514",
			},
		}
		c.JSON(200, map[string]interface{}{
			"object": "list",
			"data":   models,
		})
	})

	// POST /v1/messages 端点 - 核心测试目标
	r.router.POST("/v1/messages", r.handleMessagesEndpoint)
}

// mockAuthMiddleware 模拟认证中间件
func (r *RealIntegrationTester) mockAuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		auth := c.GetHeader("Authorization")
		apiKey := c.GetHeader("x-api-key")
		
		expectedToken := "Bearer " + r.config.TestToken
		
		if auth != expectedToken && apiKey != r.config.TestToken {
			c.JSON(401, map[string]interface{}{
				"error": "unauthorized",
				"message": "Invalid API key",
			})
			c.Abort()
			return
		}
		c.Next()
	}
}

// handleMessagesEndpoint 处理messages端点 - 这是核心测试逻辑
func (r *RealIntegrationTester) handleMessagesEndpoint(c *gin.Context) {
	// 读取请求体
	body, err := c.GetRawData()
	if err != nil {
		c.JSON(400, map[string]interface{}{
			"error": "bad_request",
			"message": "Failed to read request body",
		})
		return
	}

	// 解析请求
	var anthropicReq types.AnthropicRequest
	if err := utils.FastUnmarshal(body, &anthropicReq); err != nil {
		c.JSON(400, map[string]interface{}{
			"error": "bad_request", 
			"message": "Failed to parse request body",
		})
		return
	}

	// 验证请求
	if len(anthropicReq.Messages) == 0 {
		c.JSON(400, map[string]interface{}{
			"error": "bad_request",
			"message": "Messages array cannot be empty",
		})
		return
	}

	// 如果是流式请求
	if anthropicReq.Stream {
		r.handleStreamResponse(c, anthropicReq)
		return
	}

	// 非流式响应
	r.handleNonStreamResponse(c, anthropicReq)
}

// handleStreamResponse 处理流式响应 - 使用模拟AWS数据
func (r *RealIntegrationTester) handleStreamResponse(c *gin.Context, req types.AnthropicRequest) {
	// 设置SSE响应头
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")

	// 如果有mock AWS数据，使用它
	if r.mockAWS != nil && r.mockAWS.mockData != nil {
		// 使用真实的流解析器处理模拟数据
		parser := NewEventStreamParser()
		defer parser.Close()

		r.logger.Debug("开始处理模拟AWS数据", utils.Int("data_size", len(r.mockAWS.mockData)))

		// *** 关键修复：使用完整的消息处理管道 ***
		// 不能直接发送原始事件载荷，必须通过CompliantMessageProcessor处理
		
		// 直接使用 CompliantEventStreamParser 的 ParseStream 方法
		// 这会同时解析消息并通过消息处理器处理，返回正确的SSE事件
		sseEvents, processErr := parser.compliantParser.ParseStream(r.mockAWS.mockData)
		if processErr != nil {
			r.logger.Warn("解析和处理事件流失败", utils.Err(processErr))
			// 在非严格模式下继续处理可能的部分结果
		}

		r.logger.Debug("处理完成", utils.Int("generated_sse_events", len(sseEvents)))

		// 发送处理后的SSE事件
		for i, sseEvent := range sseEvents {
			r.logger.Debug("发送SSE事件", 
				utils.Int("event_index", i),
				utils.String("event_type", sseEvent.Event))
			
			if sseEventData, err := utils.FastMarshal(sseEvent.Data); err == nil {
				// 如果事件名称为空，使用默认的data事件
				eventName := sseEvent.Event
				if eventName == "" {
					eventName = "data"
				}
				c.SSEvent(eventName, string(sseEventData))
				c.Writer.Flush()
			} else {
				r.logger.Warn("序列化SSE事件数据失败", utils.Err(err), utils.Int("event_index", i))
			}
		}
	} else {
		// 发送默认的模拟流式响应
		r.sendDefaultStreamResponse(c, req)
	}
}

// sendDefaultStreamResponse 发送默认的流式响应
func (r *RealIntegrationTester) sendDefaultStreamResponse(c *gin.Context, req types.AnthropicRequest) {
	// 模拟的响应事件
	events := []map[string]interface{}{
		{
			"type": "message_start",
			"message": map[string]interface{}{
				"id":      "msg_test_123",
				"type":    "message",
				"role":    "assistant",
				"model":   req.Model,
				"content": []interface{}{},
				"usage": map[string]interface{}{
					"input_tokens":  0,
					"output_tokens": 0,
				},
			},
		},
		{
			"type":  "content_block_start",
			"index": 0,
			"content_block": map[string]interface{}{
				"type": "text",
				"text": "",
			},
		},
		{
			"type":  "content_block_delta",
			"index": 0,
			"delta": map[string]interface{}{
				"type": "text_delta",
				"text": "这是一个测试响应。",
			},
		},
		{
			"type":  "content_block_stop",
			"index": 0,
		},
		{
			"type": "message_delta",
			"delta": map[string]interface{}{
				"stop_reason": "end_turn",
				"usage": map[string]interface{}{
					"output_tokens": 10,
				},
			},
		},
		{
			"type": "message_stop",
		},
	}

	for _, event := range events {
		if jsonData, err := utils.FastMarshal(event); err == nil {
			c.SSEvent("", string(jsonData))
			c.Writer.Flush()
			// 添加小延迟模拟真实流式传输
			time.Sleep(50 * time.Millisecond)
		}
	}
}

// handleNonStreamResponse 处理非流式响应
func (r *RealIntegrationTester) handleNonStreamResponse(c *gin.Context, req types.AnthropicRequest) {
	response := map[string]interface{}{
		"id":    "msg_test_123",
		"type":  "message", 
		"role":  "assistant",
		"model": req.Model,
		"content": []interface{}{
			map[string]interface{}{
				"type": "text",
				"text": "这是一个测试响应。",
			},
		},
		"stop_reason": "end_turn",
		"usage": map[string]interface{}{
			"input_tokens":  10,
			"output_tokens": 10,
		},
	}

	c.JSON(200, response)
}

// createAnthropicRequest 创建Anthropic请求
func (r *RealIntegrationTester) createAnthropicRequest() types.AnthropicRequest {
	return types.AnthropicRequest{
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 1000,
		Stream:    true,
		Messages: []types.AnthropicRequestMessage{
			{
				Role:    "user",
				Content: "这是一个集成测试请求，用于验证流式处理功能",
			},
		},
	}
}

// buildHTTPRequest 构建HTTP请求
func (r *RealIntegrationTester) buildHTTPRequest(anthropicReq types.AnthropicRequest) (*http.Request, error) {
	// 序列化请求体
	reqBody, err := utils.FastMarshal(anthropicReq)
	if err != nil {
		return nil, fmt.Errorf("序列化请求体失败: %w", err)
	}

	// 创建HTTP请求
	req, err := http.NewRequest("POST", r.server.URL+"/v1/messages", bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("创建HTTP请求失败: %w", err)
	}

	// 设置请求头
	req.Header.Set("Content-Type", "application/json")
	if r.config.EnableAuth {
		req.Header.Set("Authorization", "Bearer "+r.config.TestToken)
	}
	req.Header.Set("Accept", "text/event-stream")

	return req, nil
}

// executeHTTPRequest 执行HTTP请求
func (r *RealIntegrationTester) executeHTTPRequest(
	req *http.Request,
	expectedOutput *ExpectedOutput,
	startTime time.Time,
) (*RealIntegrationResult, error) {

	result := &RealIntegrationResult{
		StreamEvents:    make([]interface{}, 0),
		ResponseHeaders: make(map[string]string),
		Stats: RealIntegrationStats{
			EventsByType: make(map[string]int),
		},
	}

	// 创建HTTP客户端
	client := &http.Client{
		Timeout: r.config.ServerTimeout,
	}

	// 执行请求
	resp, err := client.Do(req)
	if err != nil {
		result.ErrorMessage = fmt.Sprintf("HTTP请求失败: %v", err)
		result.ProcessingTime = time.Since(startTime)
		return result, err
	}
	defer resp.Body.Close()

	// 记录响应信息
	result.HTTPStatusCode = resp.StatusCode
	result.Stats.RequestSize = int(req.ContentLength)

	// 复制响应头
	for key, values := range resp.Header {
		if len(values) > 0 {
			result.ResponseHeaders[key] = values[0]
		}
	}

	// 处理流式响应
	if resp.Header.Get("Content-Type") == "text/event-stream" {
		err = r.processStreamResponse(resp.Body, result, startTime)
	} else {
		err = r.processRegularResponse(resp.Body, result)
	}

	if err != nil {
		result.ErrorMessage = fmt.Sprintf("处理响应失败: %v", err)
	}

	result.ProcessingTime = time.Since(startTime)

	// 验证结果
	if expectedOutput != nil {
		validator := NewValidationFramework(nil)
		
		// 直接使用 RealIntegrationResult 进行验证（因为现在包含了兼容字段）
		result.CapturedSSEEvents = result.StreamEvents
		result.OutputText = result.ResponseBody
		
		result.ValidationResult = validator.ValidateResultsDirect(expectedOutput, result)
		result.Success = result.ValidationResult.IsValid && result.HTTPStatusCode == 200
	} else {
		result.Success = result.HTTPStatusCode == 200 && len(result.StreamEvents) > 0
	}

	return result, nil
}

// processStreamResponse 处理流式响应
func (r *RealIntegrationTester) processStreamResponse(
	body io.Reader,
	result *RealIntegrationResult,
	startTime time.Time,
) error {

	var responseBuilder strings.Builder
	buffer := make([]byte, 1024)
	eventIndex := 0
	firstEventRecorded := false

	for {
		n, err := body.Read(buffer)
		if n > 0 {
			chunk := string(buffer[:n])
			responseBuilder.WriteString(chunk)
			result.Stats.ResponseSize += n
			
			r.logger.Debug("读取到数据块", utils.Int("chunk_size", n))
		}

		if err != nil {
			if err == io.EOF {
				break
			}
			return fmt.Errorf("读取流式响应失败: %w", err)
		}
	}

	// 读取完所有数据后，解析完整的SSE流
	fullResponse := responseBuilder.String()
	result.ResponseBody = fullResponse
	
	r.logger.Debug("完整响应读取完成", utils.Int("total_size", len(fullResponse)))
	
	// 解析完整的SSE流
	events := r.parseSSEEvents(fullResponse)
	r.logger.Debug("解析SSE事件完成", utils.Int("parsed_events", len(events)))
	
	for _, event := range events {
		// 记录第一个事件的时间
		if !firstEventRecorded {
			result.Stats.FirstEventTime = time.Since(startTime)
			firstEventRecorded = true
		}

		result.StreamEvents = append(result.StreamEvents, event)
		result.Stats.TotalEvents++

		// 统计事件类型
		if eventData, ok := event.(map[string]interface{}); ok {
			if eventType, ok := eventData["type"].(string); ok {
				result.Stats.EventsByType[eventType]++
			}
		}

		eventIndex++
	}

	// 记录最后事件时间
	if len(events) > 0 {
		result.Stats.LastEventTime = time.Since(startTime)
	}

	return nil
}

// processRegularResponse 处理常规响应
func (r *RealIntegrationTester) processRegularResponse(body io.Reader, result *RealIntegrationResult) error {
	responseBytes, err := io.ReadAll(body)
	if err != nil {
		return fmt.Errorf("读取响应体失败: %w", err)
	}

	result.ResponseBody = string(responseBytes)
	result.Stats.ResponseSize = len(responseBytes)

	// 尝试解析JSON响应
	var jsonResponse interface{}
	if err := utils.FastUnmarshal(responseBytes, &jsonResponse); err == nil {
		result.StreamEvents = []interface{}{jsonResponse}
		result.Stats.TotalEvents = 1
	}

	return nil
}

// parseSSEEvents 解析SSE事件
func (r *RealIntegrationTester) parseSSEEvents(chunk string) []interface{} {
	events := make([]interface{}, 0)
	lines := strings.Split(chunk, "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		
		// 修复：标准SSE格式没有空格 - "data:" 而不是 "data: "
		if strings.HasPrefix(line, "data:") {
			dataStr := strings.TrimPrefix(line, "data:")
			if strings.TrimSpace(dataStr) == "" || dataStr == "[DONE]" {
				continue
			}

			var eventData interface{}
			if err := utils.FastUnmarshal([]byte(dataStr), &eventData); err == nil {
				events = append(events, eventData)
			}
		}
	}

	return events
}

// Close 关闭测试器
func (r *RealIntegrationTester) Close() {
	if r.server != nil {
		r.server.Close()
		r.logger.Debug("HTTP测试服务器已关闭")
	}

	if r.mockAWS != nil {
		r.mockAWS.Close()
		r.logger.Debug("Mock AWS服务器已关闭")
	}
}

// MockAWSServer 实现

// NewMockAWSServer 创建模拟AWS服务器
func NewMockAWSServer() *MockAWSServer {
	mock := &MockAWSServer{
		logger: utils.GetLogger(),
	}

	// 创建HTTP处理器
	mux := http.NewServeMux()
	mux.HandleFunc("/", mock.handleRequest)

	// 创建测试服务器
	mock.server = httptest.NewServer(mux)

	mock.logger.Debug("Mock AWS服务器已启动", utils.String("url", mock.server.URL))

	return mock
}

// SetMockData 设置模拟数据
func (m *MockAWSServer) SetMockData(data []byte, responseType string) {
	m.mockData = data
	m.responseType = responseType
	m.logger.Debug("Mock AWS数据已设置", 
		utils.Int("data_size", len(data)),
		utils.String("response_type", responseType))
}

// handleRequest 处理请求
func (m *MockAWSServer) handleRequest(w http.ResponseWriter, r *http.Request) {
	m.logger.Debug("Mock AWS收到请求", 
		utils.String("method", r.Method),
		utils.String("path", r.URL.Path))

	// 设置响应头
	switch m.responseType {
	case "event-stream":
		w.Header().Set("Content-Type", "application/vnd.amazon.eventstream")
		w.Header().Set("Cache-Control", "no-cache")
	default:
		w.Header().Set("Content-Type", "application/json")
	}

	// 写入模拟数据
	if m.mockData != nil {
		// 模拟流式响应的延迟
		if m.responseType == "event-stream" {
			chunkSize := 1024
			for i := 0; i < len(m.mockData); i += chunkSize {
				end := i + chunkSize
				if end > len(m.mockData) {
					end = len(m.mockData)
				}

				w.Write(m.mockData[i:end])
				if f, ok := w.(http.Flusher); ok {
					f.Flush()
				}

				// 添加小延迟模拟真实网络
				time.Sleep(time.Millisecond * 10)
			}
		} else {
			w.Write(m.mockData)
		}
	} else {
		// 默认响应
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"message": "mock response"}`))
	}
}

// Close 关闭模拟服务器
func (m *MockAWSServer) Close() {
	if m.server != nil {
		m.server.Close()
	}
}

// 配置函数

// minInt 返回两个整数中的最小值
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// getDefaultRealTestConfig 获取默认真实测试配置
func getDefaultRealTestConfig() *RealTestConfig {
	return &RealTestConfig{
		ServerTimeout: time.Minute * 5,
		EnableMockAWS: true,
		MockAWSPort:   0, // 使用随机端口
		EnableAuth:    true,
		TestToken:     "test_token_123",
		DebugMode:     false,
	}
}

// CreateRealTestConfigWithDefaults 创建带默认值的真实测试配置
func CreateRealTestConfigWithDefaults(overrides map[string]interface{}) *RealTestConfig {
	config := getDefaultRealTestConfig()

	// 应用覆盖配置
	if timeout, ok := overrides["server_timeout"].(time.Duration); ok {
		config.ServerTimeout = timeout
	}
	if enableMock, ok := overrides["enable_mock_aws"].(bool); ok {
		config.EnableMockAWS = enableMock
	}
	if enableAuth, ok := overrides["enable_auth"].(bool); ok {
		config.EnableAuth = enableAuth
	}
	if token, ok := overrides["test_token"].(string); ok {
		config.TestToken = token
	}
	if debug, ok := overrides["debug_mode"].(bool); ok {
		config.DebugMode = debug
	}

	return config
}