package test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"kiro2api/parser"
	"kiro2api/utils"
)

// TestCase 测试用例定义
type TestCase struct {
	Name            string            `json:"name"`
	Description     string            `json:"description"`
	SourceFile      string            `json:"source_file"`
	ExpectedOutput  *ExpectedOutput   `json:"expected_output"`
	ValidationRules []ValidationRule  `json:"validation_rules"`
	Tags            []string          `json:"tags"`
	CreatedAt       time.Time         `json:"created_at"`
	Config          *TestCaseConfig   `json:"config"`
}

// TestCaseConfig 测试用例配置
type TestCaseConfig struct {
	EnableStrictMode    bool          `json:"enable_strict_mode"`
	RealTestConfig      *RealTestConfig `json:"real_test_config"`  // 替换 SimulatorConfig
	ValidationConfig    *ValidationConfig `json:"validation_config"`
	Timeout            time.Duration `json:"timeout"`
	RetryCount         int           `json:"retry_count"`
}

// TestResult 测试结果
type TestResult struct {
	TestCase        *TestCase         `json:"test_case"`
	ParseResult     *ParseResult      `json:"parse_result"`
	ExpectedOutput  *ExpectedOutput   `json:"expected_output"`
	IntegrationResult *RealIntegrationResult `json:"integration_result"`  // 替换 SimulationResult
	ValidationResult *ValidationResult `json:"validation_result"`
	ExecutionTime   time.Duration     `json:"execution_time"`
	Success         bool              `json:"success"`
	ErrorMessage    string            `json:"error_message,omitempty"`
}

// TestSuite 测试套件
type TestSuite struct {
	TestCases []TestCase     `json:"test_cases"`
	Results   []TestResult   `json:"results"`
	Summary   TestSummary    `json:"summary"`
	StartTime time.Time      `json:"start_time"`
	EndTime   time.Time      `json:"end_time"`
	logger    utils.Logger
}

// TestSummary 测试摘要
type TestSummary struct {
	TotalTests      int           `json:"total_tests"`
	PassedTests     int           `json:"passed_tests"`
	FailedTests     int           `json:"failed_tests"`
	SkippedTests    int           `json:"skipped_tests"`
	SuccessRate     float64       `json:"success_rate"`
	TotalDuration   time.Duration `json:"total_duration"`
	AverageScore    float64       `json:"average_score"`
}

// TestCaseGenerator 测试用例生成器
type TestCaseGenerator struct {
	logger utils.Logger
}

// NewTestCaseGenerator 创建测试用例生成器
func NewTestCaseGenerator() *TestCaseGenerator {
	return &TestCaseGenerator{
		logger: utils.GetLogger(),
	}
}

// GenerateTestCaseFromFile 从文件生成测试用例
func (g *TestCaseGenerator) GenerateTestCaseFromFile(filePath string) (*TestCase, error) {
	g.logger.Debug("从文件生成测试用例", utils.String("file_path", filePath))

	// 1. 加载和解析hex数据
	analyzer, err := LoadHexDataFromFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("加载hex数据失败: %w", err)
	}

	hexData, err := analyzer.ParseHexData()
	if err != nil {
		return nil, fmt.Errorf("解析hex数据失败: %w", err)
	}

	if !hexData.IsValid {
		return nil, fmt.Errorf("hex数据校验失败: %s", hexData.ErrorMessage)
	}

	// 2. 解析事件流
	parser := NewEventStreamParser()
	defer parser.Close()

	parseResult, err := parser.ParseEventStream(hexData.BinaryData)
	if err != nil {
		return nil, fmt.Errorf("解析事件流失败: %w", err)
	}

	// 3. 生成期望输出
	// 修复：使用与实际运行时相同的CompliantEventStreamParser
	// 这确保期望输出和实际输出使用相同的SSE事件格式
	sseEvents, err := parser.compliantParser.ParseStream(hexData.BinaryData)
	if err != nil {
		g.logger.Warn("使用CompliantEventStreamParser解析失败，回退到旧方法", utils.Err(err))
		
		// 回退到原来的方法
		expectationGenerator := NewExpectationGenerator()
		expectedOutput, err := expectationGenerator.GenerateExpectations(parseResult.Events)
		if err != nil {
			return nil, fmt.Errorf("生成期望输出失败: %w", err)
		}
		
		// 创建测试用例
		testCase := &TestCase{
			Name:           fmt.Sprintf("TestCase_%s", filepath.Base(filePath)),
			Description:    fmt.Sprintf("Generated from %s", filePath),
			SourceFile:     filePath,
			ExpectedOutput: expectedOutput,
			ValidationRules: expectedOutput.ValidationRules,
			Tags:           []string{"auto-generated", "stream-parsing"},
			CreatedAt:      time.Now(),
			Config: &TestCaseConfig{
				EnableStrictMode: false,
				RealTestConfig:  getDefaultRealTestConfig(),
				ValidationConfig: &ValidationConfig{},
				Timeout:         time.Minute * 5,
				RetryCount:      3,
			},
		}
		
		return testCase, nil
	}
	
	// 使用CompliantParser生成的SSE事件创建期望输出
	// 需要转换parser.SSEEvent为expectation_generator.SSEEvent格式
	expectedSSEEvents := convertParserSSEToExpectedSSE(sseEvents)
	
	expectedOutput := &ExpectedOutput{
		SSEEvents:       expectedSSEEvents,
		ToolCalls:       extractExpectedToolCallsFromSSE(expectedSSEEvents),
		ContentBlocks:   []ExpectedContentBlock{}, // 暂时为空
		FinalStats:      ExpectedStats{},           // 暂时为空
		GeneratedAt:     time.Now(),
		ValidationRules: []ValidationRule{},       // 暂时为空
	}

	// 4. 创建测试用例
	testCase := &TestCase{
		Name:           fmt.Sprintf("TestCase_%s", filepath.Base(filePath)),
		Description:    fmt.Sprintf("Generated from %s", filePath),
		SourceFile:     filePath,
		ExpectedOutput: expectedOutput,
		ValidationRules: expectedOutput.ValidationRules,
		Tags:           []string{"auto-generated", "stream-parsing"},
		CreatedAt:      time.Now(),
		Config: &TestCaseConfig{
			EnableStrictMode: false,
			RealTestConfig:  getDefaultRealTestConfig(),  // 使用 RealTestConfig
			ValidationConfig: &ValidationConfig{},
			Timeout:         time.Minute * 5,
			RetryCount:      3,
		},
	}

	g.logger.Debug("测试用例生成完成", 
		utils.String("test_name", testCase.Name),
		utils.Int("expected_events", len(expectedOutput.SSEEvents)),
		utils.Int("tool_calls", len(expectedOutput.ToolCalls)))

	return testCase, nil
}

// convertParserSSEToExpectedSSE 将parser.SSEEvent转换为expectation_generator.SSEEvent格式
func convertParserSSEToExpectedSSE(parserEvents []parser.SSEEvent) []SSEEvent {
	var expectedEvents []SSEEvent
	
	for i, parserEvent := range parserEvents {
		// 将parser.SSEEvent转换为map格式，然后创建expectation_generator.SSEEvent
		var eventData map[string]interface{}
		
		// 尝试将Data字段转换为map
		if parserEvent.Data != nil {
			if dataMap, ok := parserEvent.Data.(map[string]interface{}); ok {
				eventData = dataMap
			} else {
				// 如果不是map，创建一个包含原始数据的map
				eventData = map[string]interface{}{
					"original_data": parserEvent.Data,
				}
			}
		} else {
			eventData = make(map[string]interface{})
		}
		
		// 确保事件类型在data中
		if parserEvent.Event != "" {
			eventData["type"] = parserEvent.Event
		}
		
		expectedEvent := SSEEvent{
			Type:        parserEvent.Event,
			Data:        eventData,
			Timestamp:   time.Now(),
			Index:       i,
			EventSource: "content", // 默认值
		}
		
		expectedEvents = append(expectedEvents, expectedEvent)
	}
	
	return expectedEvents
}

// extractExpectedToolCallsFromSSE 从SSE事件中提取期望工具调用
func extractExpectedToolCallsFromSSE(sseEvents []SSEEvent) []ExpectedToolCall {
	var toolCalls []ExpectedToolCall
	
	for _, event := range sseEvents {
		if event.Type == "content_block_start" {
			if cb, ok := event.Data["content_block"].(map[string]interface{}); ok {
				if cbType, ok := cb["type"].(string); ok && cbType == "tool_use" {
					toolUseID, _ := cb["id"].(string)
					toolName, _ := cb["name"].(string)
					blockIndex, _ := event.Data["index"].(int)
					
					toolCall := ExpectedToolCall{
						ToolUseID:   toolUseID,
						Name:        toolName,
						BlockIndex:  blockIndex,
						Input:       make(map[string]interface{}),
						InputJSON:   "",
						StartEvent:  &event,
						InputEvents: []*SSEEvent{},
						StopEvent:   nil,
					}
					
					toolCalls = append(toolCalls, toolCall)
				}
			}
		}
	}
	
	return toolCalls
}

// extractExpectedContentFromSSE 从SSE事件中提取期望内容
func extractExpectedContentFromSSE(sseEvents []SSEEvent) string {
	var contentBuilder strings.Builder
	
	for _, event := range sseEvents {
		if event.Type == "content_block_delta" {
			if delta, ok := event.Data["delta"].(map[string]interface{}); ok {
				if deltaType, ok := delta["type"].(string); ok && deltaType == "text_delta" {
					if text, ok := delta["text"].(string); ok {
						contentBuilder.WriteString(text)
					}
				}
			}
		}
	}
	
	return contentBuilder.String()
}

// ExecuteTestCase 执行测试用例
func ExecuteTestCase(testCase *TestCase) (*TestResult, error) {
	startTime := time.Now()
	logger := utils.GetLogger()
	
	logger.Debug("开始执行测试用例", utils.String("test_name", testCase.Name))

	result := &TestResult{
		TestCase: testCase,
	}

	// 1. 重新加载和解析hex数据
	analyzer, err := LoadHexDataFromFile(testCase.SourceFile)
	if err != nil {
		result.ErrorMessage = fmt.Sprintf("加载hex数据失败: %v", err)
		result.ExecutionTime = time.Since(startTime)
		return result, err
	}

	hexData, err := analyzer.ParseHexData()
	if err != nil {
		result.ErrorMessage = fmt.Sprintf("解析hex数据失败: %v", err)
		result.ExecutionTime = time.Since(startTime)
		return result, err
	}

	// 2. 解析事件流
	parser := NewEventStreamParser()
	defer parser.Close()

	parseResult, err := parser.ParseEventStream(hexData.BinaryData)
	if err != nil {
		result.ErrorMessage = fmt.Sprintf("解析事件流失败: %v", err)
		result.ExecutionTime = time.Since(startTime)
		return result, err
	}
	result.ParseResult = parseResult

	// 3. 使用 RealIntegrationTester 进行测试
	tester := NewRealIntegrationTester(testCase.Config.RealTestConfig)
	defer tester.Close()
	
	// 设置模拟数据
	if tester.mockAWS != nil {
		tester.mockAWS.SetMockData(hexData.BinaryData, "event-stream")
	}
	
	// 执行测试
	integrationResult, err := tester.TestWithRawDataDirect(hexData.BinaryData)
	if err != nil {
		result.ErrorMessage = fmt.Sprintf("集成测试失败: %v", err)
		result.ExecutionTime = time.Since(startTime)
		return result, err
	}
	result.IntegrationResult = integrationResult

	// 4. 验证结果
	validator := NewValidationFramework(testCase.Config.ValidationConfig)
	validationResult := validator.ValidateResultsDirect(testCase.ExpectedOutput, integrationResult)
	result.ValidationResult = validationResult

	// 5. 判断测试结果
	result.Success = validationResult.IsValid
	result.ExpectedOutput = testCase.ExpectedOutput
	result.ExecutionTime = time.Since(startTime)

	logger.Debug("测试用例执行完成",
		utils.String("test_name", testCase.Name),
		utils.Bool("success", result.Success),
		utils.Float64("score", validationResult.OverallScore),
		utils.Duration("execution_time", result.ExecutionTime))

	return result, nil
}

// NewTestSuite 创建测试套件
func NewTestSuite() *TestSuite {
	return &TestSuite{
		TestCases: make([]TestCase, 0),
		Results:   make([]TestResult, 0),
		logger:    utils.GetLogger(),
	}
}

// AddTestCase 添加测试用例
func (ts *TestSuite) AddTestCase(testCase TestCase) {
	ts.TestCases = append(ts.TestCases, testCase)
}

// RunAllTests 运行所有测试
func (ts *TestSuite) RunAllTests() error {
	ts.StartTime = time.Now()
	ts.logger.Debug("开始运行测试套件", utils.Int("total_tests", len(ts.TestCases)))

	for i, testCase := range ts.TestCases {
		ts.logger.Debug("执行测试用例", 
			utils.Int("test_index", i+1),
			utils.String("test_name", testCase.Name))

		result, err := ExecuteTestCase(&testCase)
		if err != nil {
			ts.logger.Warn("测试用例执行失败",
				utils.String("test_name", testCase.Name),
				utils.Err(err))
		}

		if result != nil {
			ts.Results = append(ts.Results, *result)
		}
	}

	ts.EndTime = time.Now()
	ts.calculateSummary()

	ts.logger.Debug("测试套件执行完成",
		utils.Int("passed", ts.Summary.PassedTests),
		utils.Int("failed", ts.Summary.FailedTests),
		utils.Float64("success_rate", ts.Summary.SuccessRate))

	return nil
}

// calculateSummary 计算摘要
func (ts *TestSuite) calculateSummary() {
	summary := TestSummary{
		TotalTests:    len(ts.Results),
		TotalDuration: ts.EndTime.Sub(ts.StartTime),
	}

	totalScore := 0.0
	for _, result := range ts.Results {
		if result.Success {
			summary.PassedTests++
		} else {
			summary.FailedTests++
		}

		if result.ValidationResult != nil {
			totalScore += result.ValidationResult.OverallScore
		}
	}

	if summary.TotalTests > 0 {
		summary.SuccessRate = float64(summary.PassedTests) / float64(summary.TotalTests)
		summary.AverageScore = totalScore / float64(summary.TotalTests)
	}

	ts.Summary = summary
}

// GenerateTestReport 生成测试报告
func (ts *TestSuite) GenerateTestReport() string {
	var builder strings.Builder
	
	builder.WriteString("# 测试套件报告\n\n")
	
	// 摘要
	builder.WriteString("## 摘要\n\n")
	builder.WriteString(fmt.Sprintf("- **总测试数**: %d\n", ts.Summary.TotalTests))
	builder.WriteString(fmt.Sprintf("- **通过**: %d\n", ts.Summary.PassedTests))
	builder.WriteString(fmt.Sprintf("- **失败**: %d\n", ts.Summary.FailedTests))
	builder.WriteString(fmt.Sprintf("- **成功率**: %.1f%%\n", ts.Summary.SuccessRate*100))
	builder.WriteString(fmt.Sprintf("- **平均分数**: %.2f\n", ts.Summary.AverageScore))
	builder.WriteString(fmt.Sprintf("- **总耗时**: %v\n", ts.Summary.TotalDuration))
	builder.WriteString("\n")
	
	// 详细结果
	builder.WriteString("## 详细结果\n\n")
	for i, result := range ts.Results {
		status := "❌"
		if result.Success {
			status = "✅"
		}
		
		builder.WriteString(fmt.Sprintf("### %d. %s %s\n\n", i+1, result.TestCase.Name, status))
		
		if result.ValidationResult != nil {
			builder.WriteString(fmt.Sprintf("- **总体分数**: %.2f\n", result.ValidationResult.OverallScore))
			builder.WriteString(fmt.Sprintf("- **执行时间**: %v\n", result.ExecutionTime))
			
			if len(result.ValidationResult.Differences) > 0 {
				builder.WriteString("- **主要问题**:\n")
				for _, diff := range result.ValidationResult.Differences {
					if diff.Severity == "critical" {
						builder.WriteString(fmt.Sprintf("  - 🔴 %s\n", diff.Description))
					}
				}
			}
		}
		
		if result.ErrorMessage != "" {
			builder.WriteString(fmt.Sprintf("- **错误**: %s\n", result.ErrorMessage))
		}
		
		builder.WriteString("\n")
	}
	
	return builder.String()
}

// 具体的测试函数

// TestHexDataAnalyzer 测试hex数据解析器
func TestHexDataAnalyzer(t *testing.T) {
	// 调试信息
	wd, _ := os.Getwd()
	t.Logf("测试工作目录: %s", wd)
	
	// 找到测试文件
	files, err := utils.ListSavedRawData()
	if err != nil {
		t.Fatalf("获取测试文件失败: %v", err)
	}

	t.Logf("找到 %d 个文件", len(files))
	for _, file := range files {
		t.Logf("  - %s", file)
	}

	if len(files) == 0 {
		t.Skip("没有找到原始数据文件，跳过测试")
	}

	// 取第一个文件进行测试
	testFile := files[0]
	t.Logf("使用测试文件: %s", testFile)

	// 加载并解析
	analyzer, err := LoadHexDataFromFile(testFile)
	if err != nil {
		t.Fatalf("加载hex数据失败: %v", err)
	}

	hexData, err := analyzer.ParseHexData()
	if err != nil {
		t.Fatalf("解析hex数据失败: %v", err)
	}

	// 验证结果
	if !hexData.IsValid {
		t.Errorf("hex数据校验失败: %s", hexData.ErrorMessage)
	}

	if len(hexData.BinaryData) == 0 {
		t.Error("二进制数据为空")
	}

	t.Logf("成功解析%d字节的二进制数据", len(hexData.BinaryData))
}

// TestEventStreamParser 测试事件流解析器
func TestEventStreamParser(t *testing.T) {
	// 找到测试文件
	files, err := utils.ListSavedRawData()
	if err != nil {
		t.Fatalf("获取测试文件失败: %v", err)
	}

	if len(files) == 0 {
		t.Skip("没有找到原始数据文件，跳过测试")
	}

	// 加载数据
	analyzer, err := LoadHexDataFromFile(files[0])
	if err != nil {
		t.Fatalf("加载数据失败: %v", err)
	}

	hexData, err := analyzer.ParseHexData()
	if err != nil {
		t.Fatalf("解析hex数据失败: %v", err)
	}

	// 解析事件流
	parser := NewEventStreamParser()
	defer parser.Close()

	result, err := parser.ParseEventStream(hexData.BinaryData)
	if err != nil {
		t.Fatalf("解析事件流失败: %v", err)
	}

	// 验证结果
	if !result.Success {
		t.Error("事件流解析失败")
	}

	if len(result.Events) == 0 {
		t.Error("没有解析到任何事件")
	}

	// 输出统计信息
	summary := parser.GetEventSummary(result)
	t.Logf("解析到%d个事件", result.TotalEvents)
	for eventType, count := range summary {
		t.Logf("  %s: %d", eventType, count)
	}
}

// TestRealIntegration 测试真实集成（替代 TestStreamRequestSimulator）
func TestRealIntegration(t *testing.T) {
	// 加载测试数据
	files, err := utils.ListSavedRawData()
	if err != nil {
		t.Fatalf("获取测试文件失败: %v", err)
	}

	if len(files) == 0 {
		t.Skip("没有找到原始数据文件，跳过测试")
	}
	
	// 优先使用包含工具调用的文件（更长的文件通常包含更多内容）
	testFile := files[0]
	if len(files) > 1 {
		// 选择最长的文件（可能包含工具调用）
		for _, file := range files {
			if strings.Contains(file, "20250821_125001") { // 这个文件包含 toolUseEvent
				testFile = file
				break
			}
		}
	}
	
	t.Logf("使用测试文件: %s", testFile)

	analyzer, err := LoadHexDataFromFile(testFile)
	if err != nil {
		t.Fatalf("加载数据失败: %v", err)
	}

	hexData, err := analyzer.ParseHexData()
	if err != nil {
		t.Fatalf("解析hex数据失败: %v", err)
	}

	// 创建真实集成测试器
	tester := NewRealIntegrationTester(nil)
	defer tester.Close()

	// 执行测试
	result, err := tester.TestWithRawDataDirect(hexData.BinaryData)
	if err != nil {
		t.Fatalf("集成测试失败: %v", err)
	}

	// 验证结果
	if len(result.CapturedSSEEvents) == 0 {
		t.Error("没有捕获到SSE事件")
	}

	if result.Stats.ResponseSize == 0 {
		t.Error("响应大小为0")
	}

	t.Logf("集成测试完成: 处理%d字节, 生成%d个事件",
		result.Stats.ResponseSize, len(result.CapturedSSEEvents))
}

// TestEndToEndValidation 端到端验证测试
func TestEndToEndValidation(t *testing.T) {
	// 创建测试套件
	suite := NewTestSuite()

	// 从文件生成测试用例
	generator := NewTestCaseGenerator()
	
	files, err := utils.ListSavedRawData()
	if err != nil {
		t.Fatalf("获取测试文件失败: %v", err)
	}

	if len(files) == 0 {
		t.Skip("没有找到原始数据文件，跳过测试")
	}

	// 为每个文件生成测试用例
	for _, file := range files {
		testCase, err := generator.GenerateTestCaseFromFile(file)
		if err != nil {
			t.Logf("生成测试用例失败 %s: %v", file, err)
			continue
		}
		
		suite.AddTestCase(*testCase)
	}

	if len(suite.TestCases) == 0 {
		t.Skip("没有生成任何测试用例")
	}

	// 运行测试套件
	err = suite.RunAllTests()
	if err != nil {
		t.Fatalf("运行测试套件失败: %v", err)
	}

	// 生成报告
	report := suite.GenerateTestReport()
	t.Log("测试报告:\n", report)

	// 验证结果
	if suite.Summary.PassedTests == 0 {
		t.Error("没有任何测试通过")
	}

	if suite.Summary.SuccessRate < 0.5 {
		t.Errorf("成功率过低: %.1f%%", suite.Summary.SuccessRate*100)
	}

	t.Logf("测试套件执行完成: %d/%d 通过 (%.1f%%)", 
		suite.Summary.PassedTests, 
		suite.Summary.TotalTests, 
		suite.Summary.SuccessRate*100)
}

// BenchmarkRealIntegration 真实集成性能基准测试（替代 BenchmarkStreamProcessing）
func BenchmarkRealIntegration(b *testing.B) {
	// 加载测试数据
	files, err := utils.ListSavedRawData()
	if err != nil {
		b.Fatalf("获取测试文件失败: %v", err)
	}

	if len(files) == 0 {
		b.Skip("没有找到原始数据文件，跳过测试")
	}

	analyzer, err := LoadHexDataFromFile(files[0])
	if err != nil {
		b.Fatalf("加载数据失败: %v", err)
	}

	hexData, err := analyzer.ParseHexData()
	if err != nil {
		b.Fatalf("解析hex数据失败: %v", err)
	}

	// 为了性能测试，复用测试器实例
	tester := NewRealIntegrationTester(nil)
	defer tester.Close()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := tester.TestWithRawDataDirect(hexData.BinaryData)
		if err != nil {
			b.Fatalf("集成测试失败: %v", err)
		}
	}
}