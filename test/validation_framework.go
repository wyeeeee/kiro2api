package test

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"kiro2api/utils"
)

// 迁移的类型定义（从已删除的 stream_request_simulator.go）
// EventCapture 事件捕获
type EventCapture struct {
	Timestamp   time.Time              `json:"timestamp"`
	EventType   string                 `json:"event_type"`
	Data        map[string]interface{} `json:"data"`
	Source      string                 `json:"source"`
	Index       int                    `json:"index"`
}

// SimulationResult 模拟结果（保留用于兼容性）
type SimulationResult struct {
	CapturedSSEEvents []interface{}     `json:"captured_sse_events"`
	OutputText        string            `json:"output_text"`
	ProcessingTime    time.Duration     `json:"processing_time"`
	ErrorsEncountered []error           `json:"errors_encountered"`
	Stats             SimulationStats   `json:"stats"`
	RawOutput         string            `json:"raw_output"`
	EventSequence     []EventCapture    `json:"event_sequence"`
}

// SimulationStats 模拟统计（保留用于兼容性）
type SimulationStats struct {
	TotalBytesProcessed int               `json:"total_bytes_processed"`
	TotalEventsEmitted  int               `json:"total_events_emitted"`
	EventsByType        map[string]int    `json:"events_by_type"`
	ProcessingLatency   time.Duration     `json:"processing_latency"`
	ToolCallsDetected   int               `json:"tool_calls_detected"`
	ContentBlocksCount  int               `json:"content_blocks_count"`
	DeduplicationSkips  int               `json:"deduplication_skips"`
}

// ValidationFramework 验证框架
type ValidationFramework struct {
	tolerance Tolerance
	reporters []Reporter
	logger    utils.Logger
	config    *ValidationConfig
}

// ValidationConfig 验证配置
type ValidationConfig struct {
	StrictMode           bool    `json:"strict_mode"`
	TextSimilarityThreshold float64 `json:"text_similarity_threshold"`
	EventCountTolerance  int     `json:"event_count_tolerance"`
	TimingTolerance      time.Duration `json:"timing_tolerance"`
	EnableDeepComparison bool    `json:"enable_deep_comparison"`
	IgnoreFields         []string `json:"ignore_fields"`
	CustomValidators     map[string]string `json:"custom_validators"`
}

// Tolerance 容错配置
type Tolerance struct {
	EventCount    int     `json:"event_count"`
	TextSimilarity float64 `json:"text_similarity"`
	TimingVariance time.Duration `json:"timing_variance"`
	FieldMismatch int     `json:"field_mismatch"`
}

// Reporter 报告器接口
type Reporter interface {
	GenerateReport(result *ValidationResult) string
	GetFormat() string
}

// ValidationResult 验证结果
type ValidationResult struct {
	IsValid           bool                `json:"is_valid"`
	OverallScore      float64             `json:"overall_score"`
	Differences       []Difference        `json:"differences"`
	Matches           []Match             `json:"matches"`
	DetailedReport    *DetailedReport     `json:"detailed_report"`
	Recommendations   []string            `json:"recommendations"`
	ValidationTime    time.Duration       `json:"validation_time"`
	Summary           ValidationSummary   `json:"summary"`
}

// Difference 差异描述
type Difference struct {
	Type        string      `json:"type"`
	Field       string      `json:"field"`
	Path        string      `json:"path"`
	Expected    interface{} `json:"expected"`
	Actual      interface{} `json:"actual"`
	Severity    string      `json:"severity"` // "critical", "warning", "info"
	Description string      `json:"description"`
	Position    int         `json:"position"`
	Context     string      `json:"context"`
}

// Match 匹配描述
type Match struct {
	Type        string      `json:"type"`
	Field       string      `json:"field"`
	Path        string      `json:"path"`
	Value       interface{} `json:"value"`
	Score       float64     `json:"score"`
	Description string      `json:"description"`
}

// DetailedReport 详细报告
type DetailedReport struct {
	EventComparison     *EventComparisonReport     `json:"event_comparison"`
	ToolCallComparison  *ToolCallComparisonReport  `json:"tool_call_comparison"`
	ContentComparison   *ContentComparisonReport   `json:"content_comparison"`
	StatisticsComparison *StatisticsComparisonReport `json:"statistics_comparison"`
	PerformanceMetrics  *PerformanceMetrics        `json:"performance_metrics"`
}

// EventComparisonReport 事件对比报告
type EventComparisonReport struct {
	ExpectedCount int               `json:"expected_count"`
	ActualCount   int               `json:"actual_count"`
	MatchedEvents int               `json:"matched_events"`
	MissingEvents []EventMismatch   `json:"missing_events"`
	ExtraEvents   []EventMismatch   `json:"extra_events"`
	EventsByType  map[string]EventTypeComparison `json:"events_by_type"`
}

// EventMismatch 事件不匹配
type EventMismatch struct {
	Index       int                    `json:"index"`
	EventType   string                 `json:"event_type"`
	Data        map[string]interface{} `json:"data"`
	Reason      string                 `json:"reason"`
}

// EventTypeComparison 事件类型对比
type EventTypeComparison struct {
	Expected int `json:"expected"`
	Actual   int `json:"actual"`
	Matched  int `json:"matched"`
}

// ToolCallComparisonReport 工具调用对比报告
type ToolCallComparisonReport struct {
	ExpectedToolCalls []ExpectedToolCall    `json:"expected_tool_calls"`
	ActualToolCalls   []ActualToolCall      `json:"actual_tool_calls"`
	MatchedCalls      []ToolCallMatch       `json:"matched_calls"`
	MissingCalls      []ExpectedToolCall    `json:"missing_calls"`
	ExtraCalls        []ActualToolCall      `json:"extra_calls"`
}

// ActualToolCall 实际工具调用
type ActualToolCall struct {
	ToolUseID   string                 `json:"tool_use_id"`
	Name        string                 `json:"name"`
	Input       map[string]interface{} `json:"input"`
	InputJSON   string                 `json:"input_json"`
	BlockIndex  int                    `json:"block_index"`
	StartTime   time.Time              `json:"start_time"`
	EndTime     time.Time              `json:"end_time"`
}

// ToolCallMatch 工具调用匹配
type ToolCallMatch struct {
	Expected    ExpectedToolCall `json:"expected"`
	Actual      ActualToolCall   `json:"actual"`
	MatchScore  float64          `json:"match_score"`
	Differences []string         `json:"differences"`
}

// ContentComparisonReport 内容对比报告
type ContentComparisonReport struct {
	ExpectedContent string            `json:"expected_content"`
	ActualContent   string            `json:"actual_content"`
	SimilarityScore float64           `json:"similarity_score"`
	TextDifferences []TextDifference  `json:"text_differences"`
	ContentBlocks   []ContentBlockComparison `json:"content_blocks"`
}

// TextDifference 文本差异
type TextDifference struct {
	Type        string `json:"type"` // "insertion", "deletion", "substitution"
	Position    int    `json:"position"`
	Length      int    `json:"length"`
	ExpectedText string `json:"expected_text"`
	ActualText   string `json:"actual_text"`
	Context     string `json:"context"`
}

// ContentBlockComparison 内容块对比
type ContentBlockComparison struct {
	Index           int     `json:"index"`
	ExpectedType    string  `json:"expected_type"`
	ActualType      string  `json:"actual_type"`
	ExpectedContent string  `json:"expected_content"`
	ActualContent   string  `json:"actual_content"`
	SimilarityScore float64 `json:"similarity_score"`
}

// StatisticsComparisonReport 统计对比报告
type StatisticsComparisonReport struct {
	ExpectedStats ExpectedStats   `json:"expected_stats"`
	ActualStats   SimulationStats `json:"actual_stats"`
	Variances     map[string]float64 `json:"variances"`
}

// PerformanceMetrics 性能指标
type PerformanceMetrics struct {
	ValidationDuration  time.Duration `json:"validation_duration"`
	ComparisonCount     int           `json:"comparison_count"`
	MemoryUsage        int64         `json:"memory_usage"`
	PerformanceScore   float64       `json:"performance_score"`
}

// ValidationSummary 验证摘要
type ValidationSummary struct {
	TotalChecks    int     `json:"total_checks"`
	PassedChecks   int     `json:"passed_checks"`
	FailedChecks   int     `json:"failed_checks"`
	WarningChecks  int     `json:"warning_checks"`
	SuccessRate    float64 `json:"success_rate"`
	CriticalIssues int     `json:"critical_issues"`
	Warnings       int     `json:"warnings"`
}

// 报告器实现

// JSONReporter JSON格式报告器
type JSONReporter struct{}

// MarkdownReporter Markdown格式报告器
type MarkdownReporter struct{}

// TextReporter 文本格式报告器
type TextReporter struct{}

// NewValidationFramework 创建新的验证框架
func NewValidationFramework(config *ValidationConfig) *ValidationFramework {
	if config == nil {
		config = &ValidationConfig{}
	}
	
	return &ValidationFramework{
		tolerance: getDefaultTolerance(),
		reporters: []Reporter{
			&JSONReporter{},
			&MarkdownReporter{},
			&TextReporter{},
		},
		logger: utils.GetLogger(),
		config: config,
	}
}

// ValidateResults 验证结果（保留用于向后兼容）
func (v *ValidationFramework) ValidateResults(expected *ExpectedOutput, actual *SimulationResult) *ValidationResult {
	startTime := time.Now()
	
	v.logger.Debug("开始验证结果",
		utils.Int("expected_events", len(expected.SSEEvents)),
		utils.Int("actual_events", len(actual.CapturedSSEEvents)))

	result := &ValidationResult{
		Differences:     make([]Difference, 0),
		Matches:         make([]Match, 0),
		Recommendations: make([]string, 0),
		DetailedReport:  &DetailedReport{},
	}

	// 1. 事件对比
	eventComparison := v.compareSSEEvents(expected.SSEEvents, actual.CapturedSSEEvents)
	result.DetailedReport.EventComparison = eventComparison

	// 2. 工具调用对比
	toolCallComparison := v.compareToolCalls(expected.ToolCalls, actual)
	result.DetailedReport.ToolCallComparison = toolCallComparison

	// 3. 内容对比
	contentComparison := v.compareContent(expected.ContentBlocks, actual)
	result.DetailedReport.ContentComparison = contentComparison

	// 4. 统计对比
	statsComparison := v.compareStatistics(expected.FinalStats, actual.Stats)
	result.DetailedReport.StatisticsComparison = statsComparison

	// 5. 性能指标
	result.DetailedReport.PerformanceMetrics = &PerformanceMetrics{
		ValidationDuration: time.Since(startTime),
		ComparisonCount:    len(expected.SSEEvents) + len(actual.CapturedSSEEvents),
	}

	// 6. 计算总体分数
	result.OverallScore = v.calculateOverallScore(result.DetailedReport)

	// 7. 生成差异和匹配
	result.Differences = v.collectDifferences(result.DetailedReport)
	result.Matches = v.collectMatches(result.DetailedReport)

	// 8. 生成建议
	result.Recommendations = v.generateRecommendations(result.DetailedReport)

	// 9. 计算摘要
	result.Summary = v.calculateSummary(result)

	// 10. 判断是否有效
	result.IsValid = v.determineValidity(result)
	result.ValidationTime = time.Since(startTime)

	v.logger.Debug("验证完成",
		utils.Bool("is_valid", result.IsValid),
		utils.Float64("overall_score", result.OverallScore),
		utils.Duration("validation_time", result.ValidationTime))

	return result
}

// ValidateResultsDirect 直接使用 RealIntegrationResult 进行验证
func (v *ValidationFramework) ValidateResultsDirect(expected *ExpectedOutput, actual *RealIntegrationResult) *ValidationResult {
	startTime := time.Now()
	
	v.logger.Debug("开始验证结果（直接模式）",
		utils.Int("expected_events", len(expected.SSEEvents)),
		utils.Int("actual_events", len(actual.CapturedSSEEvents)))

	result := &ValidationResult{
		Differences:     make([]Difference, 0),
		Matches:         make([]Match, 0),
		Recommendations: make([]string, 0),
		DetailedReport:  &DetailedReport{},
	}

	// 1. 事件对比
	eventComparison := v.compareSSEEvents(expected.SSEEvents, actual.CapturedSSEEvents)
	result.DetailedReport.EventComparison = eventComparison

	// 2. 工具调用对比 - 使用 RealIntegrationResult 适配
	toolCallComparison := v.compareToolCallsDirect(expected.ToolCalls, actual)
	result.DetailedReport.ToolCallComparison = toolCallComparison

	// 3. 内容对比 - 使用 RealIntegrationResult 适配
	contentComparison := v.compareContentDirect(expected.ContentBlocks, actual)
	result.DetailedReport.ContentComparison = contentComparison

	// 4. 统计对比 - 转换 RealIntegrationStats 到 SimulationStats
	simulationStats := SimulationStats{
		TotalBytesProcessed: actual.Stats.ResponseSize,
		TotalEventsEmitted:  actual.Stats.TotalEvents,
		EventsByType:        actual.Stats.EventsByType,
		ToolCallsDetected:   0, // 需要从事件中计算
		ContentBlocksCount:  0, // 需要从事件中计算
	}
	
	// 从事件统计中获取工具调用和内容块数量
	for eventType, count := range actual.Stats.EventsByType {
		if eventType == "tool_use" {
			simulationStats.ToolCallsDetected += count
		}
		if eventType == "content_block_start" {
			simulationStats.ContentBlocksCount += count
		}
	}
	
	statsComparison := v.compareStatistics(expected.FinalStats, simulationStats)
	result.DetailedReport.StatisticsComparison = statsComparison

	// 5. 性能指标
	result.DetailedReport.PerformanceMetrics = &PerformanceMetrics{
		ValidationDuration: time.Since(startTime),
		ComparisonCount:    len(expected.SSEEvents) + len(actual.CapturedSSEEvents),
	}

	// 6. 计算总体分数
	result.OverallScore = v.calculateOverallScore(result.DetailedReport)

	// 7. 生成差异和匹配
	result.Differences = v.collectDifferences(result.DetailedReport)
	result.Matches = v.collectMatches(result.DetailedReport)

	// 8. 生成建议
	result.Recommendations = v.generateRecommendations(result.DetailedReport)

	// 9. 计算摘要
	result.Summary = v.calculateSummary(result)

	// 10. 判断是否有效
	result.IsValid = v.determineValidity(result)
	result.ValidationTime = time.Since(startTime)

	v.logger.Debug("验证完成（直接模式）",
		utils.Bool("is_valid", result.IsValid),
		utils.Float64("overall_score", result.OverallScore),
		utils.Duration("validation_time", result.ValidationTime))

	return result
}

// compareSSEEvents 对比SSE事件
func (v *ValidationFramework) compareSSEEvents(expected []SSEEvent, actual []interface{}) *EventComparisonReport {
	report := &EventComparisonReport{
		ExpectedCount: len(expected),
		ActualCount:   len(actual),
		EventsByType:  make(map[string]EventTypeComparison),
		MissingEvents: make([]EventMismatch, 0),
		ExtraEvents:   make([]EventMismatch, 0),
	}

	// 统计期望事件类型
	expectedByType := make(map[string]int)
	for _, event := range expected {
		expectedByType[event.Type]++
	}

	// 统计实际事件类型
	actualByType := make(map[string]int)
	for _, event := range actual {
		if eventMap, ok := event.(map[string]interface{}); ok {
			if eventType, ok := eventMap["type"].(string); ok {
				actualByType[eventType]++
			}
		}
	}

	// 对比事件类型统计
	allTypes := make(map[string]bool)
	for t := range expectedByType {
		allTypes[t] = true
	}
	for t := range actualByType {
		allTypes[t] = true
	}

	for eventType := range allTypes {
		expectedCount := expectedByType[eventType]
		actualCount := actualByType[eventType]
		
		comparison := EventTypeComparison{
			Expected: expectedCount,
			Actual:   actualCount,
			Matched:  int(math.Min(float64(expectedCount), float64(actualCount))),
		}
		
		report.EventsByType[eventType] = comparison
		report.MatchedEvents += comparison.Matched
	}

	// 检查缺失和多余的事件
	minLen := int(math.Min(float64(len(expected)), float64(len(actual))))
	
	// 逐个事件对比（简化版）
	for i := 0; i < minLen; i++ {
		if !v.eventsMatch(expected[i], actual[i]) {
			// 记录不匹配的事件
			v.logger.Debug("事件不匹配",
				utils.Int("index", i),
				utils.String("expected_type", expected[i].Type))
		}
	}

	// 处理数量不匹配
	if len(expected) > len(actual) {
		for i := len(actual); i < len(expected); i++ {
			report.MissingEvents = append(report.MissingEvents, EventMismatch{
				Index:     i,
				EventType: expected[i].Type,
				Data:      expected[i].Data,
				Reason:    "Expected event not found in actual output",
			})
		}
	} else if len(actual) > len(expected) {
		for i := len(expected); i < len(actual); i++ {
			if eventMap, ok := actual[i].(map[string]interface{}); ok {
				eventType, _ := eventMap["type"].(string)
				report.ExtraEvents = append(report.ExtraEvents, EventMismatch{
					Index:     i,
					EventType: eventType,
					Data:      eventMap,
					Reason:    "Unexpected event in actual output",
				})
			}
		}
	}

	return report
}

// eventsMatch 检查两个事件是否匹配
func (v *ValidationFramework) eventsMatch(expected SSEEvent, actual interface{}) bool {
	actualMap, ok := actual.(map[string]interface{})
	if !ok {
		return false
	}
	
	actualType, ok := actualMap["type"].(string)
	if !ok || actualType != expected.Type {
		return false
	}
	
	// 进一步的数据对比可以在这里实现
	return true
}

// compareToolCalls 对比工具调用
func (v *ValidationFramework) compareToolCalls(expectedCalls []ExpectedToolCall, actual *SimulationResult) *ToolCallComparisonReport {
	report := &ToolCallComparisonReport{
		ExpectedToolCalls: expectedCalls,
		ActualToolCalls:   v.extractActualToolCalls(actual),
		MatchedCalls:      make([]ToolCallMatch, 0),
		MissingCalls:      make([]ExpectedToolCall, 0),
		ExtraCalls:        make([]ActualToolCall, 0),
	}

	// 简化的工具调用匹配逻辑
	expectedMap := make(map[string]ExpectedToolCall)
	for _, expected := range expectedCalls {
		expectedMap[expected.ToolUseID] = expected
	}

	actualMap := make(map[string]ActualToolCall)
	for _, actual := range report.ActualToolCalls {
		actualMap[actual.ToolUseID] = actual
	}

	// 查找匹配的工具调用
	for id, expected := range expectedMap {
		if actual, exists := actualMap[id]; exists {
			match := ToolCallMatch{
				Expected:   expected,
				Actual:     actual,
				MatchScore: v.calculateToolCallMatchScore(expected, actual),
			}
			report.MatchedCalls = append(report.MatchedCalls, match)
			delete(actualMap, id)
		} else {
			report.MissingCalls = append(report.MissingCalls, expected)
		}
	}

	// 剩余的actual工具调用为额外的
	for _, actual := range actualMap {
		report.ExtraCalls = append(report.ExtraCalls, actual)
	}

	return report
}

// extractActualToolCalls 从模拟结果中提取实际工具调用
func (v *ValidationFramework) extractActualToolCalls(actual *SimulationResult) []ActualToolCall {
	toolCalls := make([]ActualToolCall, 0)
	
	// 从事件序列中提取工具调用信息
	toolCallsMap := make(map[string]*ActualToolCall)
	
	for _, eventCapture := range actual.EventSequence {
		if eventCapture.Data != nil {
			if eventType, ok := eventCapture.Data["type"].(string); ok {
				switch eventType {
				case "content_block_start":
					if cb, ok := eventCapture.Data["content_block"].(map[string]interface{}); ok {
						if cbType, ok := cb["type"].(string); ok && cbType == "tool_use" {
							if toolID, ok := cb["id"].(string); ok {
								toolCall := &ActualToolCall{
									ToolUseID:  toolID,
									StartTime:  eventCapture.Timestamp,
								}
								if name, ok := cb["name"].(string); ok {
									toolCall.Name = name
								}
								if index, ok := eventCapture.Data["index"].(int); ok {
									toolCall.BlockIndex = index
								}
								toolCallsMap[toolID] = toolCall
							}
						}
					}
				}
			}
		}
	}
	
	for _, toolCall := range toolCallsMap {
		toolCalls = append(toolCalls, *toolCall)
	}
	
	return toolCalls
}

// calculateToolCallMatchScore 计算工具调用匹配分数
func (v *ValidationFramework) calculateToolCallMatchScore(expected ExpectedToolCall, actual ActualToolCall) float64 {
	score := 0.0
	
	// 名称匹配
	if expected.Name == actual.Name {
		score += 0.5
	}
	
	// 参数匹配（简化）
	if len(expected.InputJSON) > 0 && len(actual.InputJSON) > 0 {
		if expected.InputJSON == actual.InputJSON {
			score += 0.5
		} else {
			// 计算文本相似度
			similarity := v.calculateTextSimilarity(expected.InputJSON, actual.InputJSON)
			score += 0.5 * similarity
		}
	}
	
	return score
}

// compareContent 对比内容
func (v *ValidationFramework) compareContent(expectedBlocks []ExpectedContentBlock, actual *SimulationResult) *ContentComparisonReport {
	report := &ContentComparisonReport{
		ContentBlocks: make([]ContentBlockComparison, 0),
	}

	// 提取实际内容
	actualContent := v.extractActualContent(actual)
	expectedContent := v.extractExpectedContent(expectedBlocks)

	report.ExpectedContent = expectedContent
	report.ActualContent = actualContent
	report.SimilarityScore = v.calculateTextSimilarity(expectedContent, actualContent)

	// 计算文本差异
	report.TextDifferences = v.calculateTextDifferences(expectedContent, actualContent)

	return report
}

// compareContentDirect 直接使用 RealIntegrationResult 对比内容
func (v *ValidationFramework) compareContentDirect(expectedBlocks []ExpectedContentBlock, actual *RealIntegrationResult) *ContentComparisonReport {
	report := &ContentComparisonReport{
		ContentBlocks: make([]ContentBlockComparison, 0),
	}

	// 提取实际内容 - 从 RealIntegrationResult
	actualContent := v.extractActualContentDirect(actual)
	expectedContent := v.extractExpectedContent(expectedBlocks)

	report.ExpectedContent = expectedContent
	report.ActualContent = actualContent
	report.SimilarityScore = v.calculateTextSimilarity(expectedContent, actualContent)

	// 计算文本差异
	report.TextDifferences = v.calculateTextDifferences(expectedContent, actualContent)

	return report
}

// compareToolCallsDirect 直接使用 RealIntegrationResult 对比工具调用
func (v *ValidationFramework) compareToolCallsDirect(expectedCalls []ExpectedToolCall, actual *RealIntegrationResult) *ToolCallComparisonReport {
	report := &ToolCallComparisonReport{
		ExpectedToolCalls: expectedCalls,
		ActualToolCalls:   v.extractActualToolCallsDirect(actual),
		MatchedCalls:      make([]ToolCallMatch, 0),
		MissingCalls:      make([]ExpectedToolCall, 0),
		ExtraCalls:        make([]ActualToolCall, 0),
	}

	// 调试：输出期望和实际的工具调用
	v.logger.Info("工具调用比较开始", 
		utils.Int("expected_calls", len(expectedCalls)),
		utils.Int("actual_calls", len(report.ActualToolCalls)))
	
	for i, expected := range expectedCalls {
		v.logger.Info("期望工具调用", 
			utils.Int("index", i),
			utils.String("tool_use_id", expected.ToolUseID),
			utils.String("name", expected.Name),
			utils.Int("block_index", expected.BlockIndex))
	}
	
	for i, actualCall := range report.ActualToolCalls {
		v.logger.Info("实际工具调用", 
			utils.Int("index", i),
			utils.String("tool_use_id", actualCall.ToolUseID),
			utils.String("name", actualCall.Name),
			utils.Int("block_index", actualCall.BlockIndex))
	}

	// 简化的工具调用匹配逻辑
	expectedMap := make(map[string]ExpectedToolCall)
	for _, expected := range expectedCalls {
		expectedMap[expected.ToolUseID] = expected
	}

	actualMap := make(map[string]ActualToolCall)
	for _, actual := range report.ActualToolCalls {
		actualMap[actual.ToolUseID] = actual
	}

	// 查找匹配的工具调用
	for id, expected := range expectedMap {
		if actual, exists := actualMap[id]; exists {
			match := ToolCallMatch{
				Expected:   expected,
				Actual:     actual,
				MatchScore: v.calculateToolCallMatchScore(expected, actual),
			}
			report.MatchedCalls = append(report.MatchedCalls, match)
			delete(actualMap, id)
			
			v.logger.Info("工具调用匹配成功", 
				utils.String("tool_use_id", id),
				utils.Float64("match_score", match.MatchScore))
		} else {
			report.MissingCalls = append(report.MissingCalls, expected)
			
			v.logger.Warn("工具调用缺失", 
				utils.String("expected_tool_use_id", expected.ToolUseID),
				utils.String("expected_name", expected.Name))
		}
	}

	// 剩余的actual工具调用为额外的
	for _, actual := range actualMap {
		report.ExtraCalls = append(report.ExtraCalls, actual)
		
		v.logger.Warn("额外的工具调用", 
			utils.String("actual_tool_use_id", actual.ToolUseID),
			utils.String("actual_name", actual.Name))
	}

	v.logger.Info("工具调用比较完成", 
		utils.Int("matched", len(report.MatchedCalls)),
		utils.Int("missing", len(report.MissingCalls)),
		utils.Int("extra", len(report.ExtraCalls)))

	return report
}

// extractActualContentDirect 从 RealIntegrationResult 提取实际内容
func (v *ValidationFramework) extractActualContentDirect(actual *RealIntegrationResult) string {
	var contentBuilder strings.Builder
	
	for _, event := range actual.CapturedSSEEvents {
		if eventMap, ok := event.(map[string]interface{}); ok {
			if eventType, ok := eventMap["type"].(string); ok && eventType == "content_block_delta" {
				if delta, ok := eventMap["delta"].(map[string]interface{}); ok {
					if text, ok := delta["text"].(string); ok {
						contentBuilder.WriteString(text)
					}
				}
			}
		}
	}
	
	return contentBuilder.String()
}

// extractActualToolCallsDirect 从 RealIntegrationResult 提取实际工具调用
func (v *ValidationFramework) extractActualToolCallsDirect(actual *RealIntegrationResult) []ActualToolCall {
	toolCalls := make([]ActualToolCall, 0)
	
	// 调试：输出EventSequence信息
	v.logger.Info("开始提取工具调用", 
		utils.Int("event_sequence_length", len(actual.EventSequence)),
		utils.Int("stream_events_length", len(actual.StreamEvents)))
	
	eventTypeCount := make(map[string]int)
	for _, eventCapture := range actual.EventSequence {
		eventTypeCount[eventCapture.EventType]++
	}
	
	v.logger.Info("EventSequence事件类型统计", utils.Any("event_types", eventTypeCount))
	
	// 从事件序列中提取工具调用信息
	toolCallsMap := make(map[string]*ActualToolCall)
	
	for i, eventCapture := range actual.EventSequence {
		if eventCapture.Data != nil {
			if eventType, ok := eventCapture.Data["type"].(string); ok {
				switch eventType {
				case "content_block_start":
					v.logger.Info("发现content_block_start事件", 
						utils.Int("event_index", i),
						utils.Any("event_data", eventCapture.Data))
					
					if cb, ok := eventCapture.Data["content_block"].(map[string]interface{}); ok {
						if cbType, ok := cb["type"].(string); ok && cbType == "tool_use" {
							v.logger.Info("发现tool_use类型的content_block", 
								utils.Any("content_block", cb))
							
							if toolID, ok := cb["id"].(string); ok {
								toolCall := &ActualToolCall{
									ToolUseID:  toolID,
									StartTime:  eventCapture.Timestamp,
								}
								if name, ok := cb["name"].(string); ok {
									toolCall.Name = name
								}
								if index, ok := eventCapture.Data["index"].(int); ok {
									toolCall.BlockIndex = index
								}
								toolCallsMap[toolID] = toolCall
								
								v.logger.Info("成功提取工具调用", 
									utils.String("tool_id", toolID),
									utils.String("tool_name", toolCall.Name),
									utils.Int("block_index", toolCall.BlockIndex))
							}
						}
					}
				}
			}
		}
	}
	
	for _, toolCall := range toolCallsMap {
		toolCalls = append(toolCalls, *toolCall)
	}
	
	v.logger.Info("工具调用提取完成", 
		utils.Int("extracted_tool_calls", len(toolCalls)))
	
	return toolCalls
}

// extractActualContent 提取实际内容
func (v *ValidationFramework) extractActualContent(actual *SimulationResult) string {
	var contentBuilder strings.Builder
	
	for _, event := range actual.CapturedSSEEvents {
		if eventMap, ok := event.(map[string]interface{}); ok {
			if eventType, ok := eventMap["type"].(string); ok && eventType == "content_block_delta" {
				if delta, ok := eventMap["delta"].(map[string]interface{}); ok {
					if text, ok := delta["text"].(string); ok {
						contentBuilder.WriteString(text)
					}
				}
			}
		}
	}
	
	return contentBuilder.String()
}

// extractExpectedContent 提取期望内容
func (v *ValidationFramework) extractExpectedContent(blocks []ExpectedContentBlock) string {
	var contentBuilder strings.Builder
	
	for _, block := range blocks {
		if block.Type == "text" {
			contentBuilder.WriteString(block.Content)
		}
	}
	
	return contentBuilder.String()
}

// compareStatistics 对比统计信息
func (v *ValidationFramework) compareStatistics(expected ExpectedStats, actual SimulationStats) *StatisticsComparisonReport {
	report := &StatisticsComparisonReport{
		ExpectedStats: expected,
		ActualStats:   actual,
		Variances:     make(map[string]float64),
	}

	// 计算各项指标的差异
	if expected.TotalEvents > 0 {
		report.Variances["total_events"] = float64(actual.TotalEventsEmitted-expected.TotalEvents) / float64(expected.TotalEvents)
	}
	
	if expected.ToolCalls > 0 {
		report.Variances["tool_calls"] = float64(actual.ToolCallsDetected-expected.ToolCalls) / float64(expected.ToolCalls)
	}
	
	if expected.ContentBlocks > 0 {
		report.Variances["content_blocks"] = float64(actual.ContentBlocksCount-expected.ContentBlocks) / float64(expected.ContentBlocks)
	}

	return report
}

// 计算和生成方法

// calculateOverallScore 计算总体分数
func (v *ValidationFramework) calculateOverallScore(report *DetailedReport) float64 {
	weights := map[string]float64{
		"events":     0.3,
		"tool_calls": 0.3,
		"content":    0.25,
		"statistics": 0.15,
	}

	scores := make(map[string]float64)

	// 事件分数
	if report.EventComparison != nil {
		if report.EventComparison.ExpectedCount > 0 {
			scores["events"] = float64(report.EventComparison.MatchedEvents) / float64(report.EventComparison.ExpectedCount)
		} else {
			scores["events"] = 1.0
		}
	} else {
		scores["events"] = 1.0 // 默认满分，当没有事件比较时
	}

	// 工具调用分数
	if report.ToolCallComparison != nil {
		expectedCount := len(report.ToolCallComparison.ExpectedToolCalls)
		if expectedCount > 0 {
			scores["tool_calls"] = float64(len(report.ToolCallComparison.MatchedCalls)) / float64(expectedCount)
		} else {
			scores["tool_calls"] = 1.0
		}
	} else {
		scores["tool_calls"] = 1.0 // 默认满分，当没有工具调用比较时
	}

	// 内容分数
	if report.ContentComparison != nil {
		scores["content"] = report.ContentComparison.SimilarityScore
	} else {
		scores["content"] = 1.0 // 默认满分，当没有内容比较时
	}

	// 统计分数（基于方差计算）
	if report.StatisticsComparison != nil && len(report.StatisticsComparison.Variances) > 0 {
		totalVariance := 0.0
		for _, variance := range report.StatisticsComparison.Variances {
			totalVariance += math.Abs(variance)
		}
		avgVariance := totalVariance / float64(len(report.StatisticsComparison.Variances))
		scores["statistics"] = math.Max(0, 1.0-avgVariance)
	} else {
		scores["statistics"] = 1.0 // 默认满分，当没有统计信息时
	}

	// 加权平均
	totalScore := 0.0
	for category, weight := range weights {
		if score, exists := scores[category]; exists {
			totalScore += score * weight
		}
	}

	return totalScore
}

// collectDifferences 收集差异
func (v *ValidationFramework) collectDifferences(report *DetailedReport) []Difference {
	differences := make([]Difference, 0)

	// 从事件对比中收集差异
	if report.EventComparison != nil {
		for _, missing := range report.EventComparison.MissingEvents {
			differences = append(differences, Difference{
				Type:        "missing_event",
				Field:       "events",
				Path:        fmt.Sprintf("events[%d]", missing.Index),
				Expected:    missing.Data,
				Actual:      nil,
				Severity:    "warning",
				Description: fmt.Sprintf("Missing %s event at position %d", missing.EventType, missing.Index),
				Position:    missing.Index,
			})
		}

		for _, extra := range report.EventComparison.ExtraEvents {
			differences = append(differences, Difference{
				Type:        "extra_event",
				Field:       "events",
				Path:        fmt.Sprintf("events[%d]", extra.Index),
				Expected:    nil,
				Actual:      extra.Data,
				Severity:    "warning",
				Description: fmt.Sprintf("Unexpected %s event at position %d", extra.EventType, extra.Index),
				Position:    extra.Index,
			})
		}
	}

	// 从工具调用对比中收集差异
	if report.ToolCallComparison != nil {
		for _, missing := range report.ToolCallComparison.MissingCalls {
			differences = append(differences, Difference{
				Type:        "missing_tool_call",
				Field:       "tool_calls",
				Path:        fmt.Sprintf("tool_calls[%s]", missing.ToolUseID),
				Expected:    missing.Name,
				Actual:      nil,
				Severity:    "critical",
				Description: fmt.Sprintf("Missing tool call: %s", missing.Name),
			})
		}

		for _, extra := range report.ToolCallComparison.ExtraCalls {
			differences = append(differences, Difference{
				Type:        "extra_tool_call",
				Field:       "tool_calls",
				Path:        fmt.Sprintf("tool_calls[%s]", extra.ToolUseID),
				Expected:    nil,
				Actual:      extra.Name,
				Severity:    "warning",
				Description: fmt.Sprintf("Unexpected tool call: %s", extra.Name),
			})
		}
	}

	return differences
}

// collectMatches 收集匹配项
func (v *ValidationFramework) collectMatches(report *DetailedReport) []Match {
	matches := make([]Match, 0)

	// 从工具调用对比中收集匹配
	if report.ToolCallComparison != nil {
		for _, match := range report.ToolCallComparison.MatchedCalls {
			matches = append(matches, Match{
				Type:        "tool_call_match",
				Field:       "tool_calls",
				Path:        fmt.Sprintf("tool_calls[%s]", match.Expected.ToolUseID),
				Value:       match.Expected.Name,
				Score:       match.MatchScore,
				Description: fmt.Sprintf("Tool call %s matched with score %.2f", match.Expected.Name, match.MatchScore),
			})
		}
	}

	return matches
}

// generateRecommendations 生成建议
func (v *ValidationFramework) generateRecommendations(report *DetailedReport) []string {
	recommendations := make([]string, 0)

	// 基于事件对比生成建议
	if report.EventComparison != nil {
		if len(report.EventComparison.MissingEvents) > 0 {
			recommendations = append(recommendations, 
				fmt.Sprintf("检查事件生成逻辑，有%d个期望事件未生成", len(report.EventComparison.MissingEvents)))
		}
		
		if len(report.EventComparison.ExtraEvents) > 0 {
			recommendations = append(recommendations, 
				fmt.Sprintf("检查去重逻辑，有%d个额外事件产生", len(report.EventComparison.ExtraEvents)))
		}
	}

	// 基于内容对比生成建议
	if report.ContentComparison != nil {
		if report.ContentComparison.SimilarityScore < 0.8 {
			recommendations = append(recommendations, 
				"内容相似度较低，检查文本聚合和处理逻辑")
		}
	}

	// 基于统计对比生成建议
	if report.StatisticsComparison != nil {
		for metric, variance := range report.StatisticsComparison.Variances {
			if math.Abs(variance) > 0.1 {
				recommendations = append(recommendations, 
					fmt.Sprintf("指标%s差异较大(%.1f%%)，检查相关处理逻辑", metric, variance*100))
			}
		}
	}

	return recommendations
}

// calculateSummary 计算摘要
func (v *ValidationFramework) calculateSummary(result *ValidationResult) ValidationSummary {
	summary := ValidationSummary{}

	// 统计检查项
	summary.TotalChecks = len(result.Differences) + len(result.Matches)
	summary.PassedChecks = len(result.Matches)
	summary.FailedChecks = len(result.Differences)

	// 按严重程度分类
	for _, diff := range result.Differences {
		switch diff.Severity {
		case "critical":
			summary.CriticalIssues++
		case "warning":
			summary.Warnings++
		}
	}

	// 计算成功率
	if summary.TotalChecks > 0 {
		summary.SuccessRate = float64(summary.PassedChecks) / float64(summary.TotalChecks)
	}

	return summary
}

// determineValidity 判断有效性
func (v *ValidationFramework) determineValidity(result *ValidationResult) bool {
	// 严格模式下不允许任何critical错误
	if v.config.StrictMode {
		for _, diff := range result.Differences {
			if diff.Severity == "critical" {
				return false
			}
		}
	}

	// 基于总体分数判断
	return result.OverallScore >= 0.7 && result.Summary.CriticalIssues == 0
}

// 工具方法

// calculateTextSimilarity 计算文本相似度
func (v *ValidationFramework) calculateTextSimilarity(text1, text2 string) float64 {
	if text1 == text2 {
		return 1.0
	}
	
	if len(text1) == 0 && len(text2) == 0 {
		return 1.0
	}
	
	if len(text1) == 0 || len(text2) == 0 {
		return 0.0
	}
	
	// 简化的Levenshtein距离计算
	distance := v.levenshteinDistance(text1, text2)
	maxLen := math.Max(float64(len(text1)), float64(len(text2)))
	
	return 1.0 - float64(distance)/maxLen
}

// levenshteinDistance 计算Levenshtein距离
func (v *ValidationFramework) levenshteinDistance(s1, s2 string) int {
	r1, r2 := []rune(s1), []rune(s2)
	
	matrix := make([][]int, len(r1)+1)
	for i := range matrix {
		matrix[i] = make([]int, len(r2)+1)
		matrix[i][0] = i
	}
	
	for j := range matrix[0] {
		matrix[0][j] = j
	}
	
	for i := 1; i <= len(r1); i++ {
		for j := 1; j <= len(r2); j++ {
			cost := 0
			if r1[i-1] != r2[j-1] {
				cost = 1
			}
			
			matrix[i][j] = int(math.Min(
				math.Min(
					float64(matrix[i-1][j]+1),     // deletion
					float64(matrix[i][j-1]+1)),    // insertion
				float64(matrix[i-1][j-1]+cost))) // substitution
		}
	}
	
	return matrix[len(r1)][len(r2)]
}

// calculateTextDifferences 计算文本差异
func (v *ValidationFramework) calculateTextDifferences(expected, actual string) []TextDifference {
	differences := make([]TextDifference, 0)
	
	// 简化的差异检测
	if expected != actual {
		differences = append(differences, TextDifference{
			Type:         "substitution",
			Position:     0,
			Length:       len(expected),
			ExpectedText: expected,
			ActualText:   actual,
			Context:      "Full text comparison",
		})
	}
	
	return differences
}

// 报告器实现

// GenerateReport 生成JSON报告
func (r *JSONReporter) GenerateReport(result *ValidationResult) string {
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fmt.Sprintf("Error generating JSON report: %v", err)
	}
	return string(data)
}

// GetFormat 获取格式
func (r *JSONReporter) GetFormat() string {
	return "json"
}

// GenerateReport 生成Markdown报告
func (r *MarkdownReporter) GenerateReport(result *ValidationResult) string {
	var builder strings.Builder
	
	builder.WriteString("# 验证报告\n\n")
	
	// 摘要部分
	builder.WriteString("## 摘要\n\n")
	builder.WriteString(fmt.Sprintf("- **总体分数**: %.2f\n", result.OverallScore))
	builder.WriteString(fmt.Sprintf("- **验证结果**: %s\n", map[bool]string{true: "✅ 通过", false: "❌ 失败"}[result.IsValid]))
	builder.WriteString(fmt.Sprintf("- **成功率**: %.1f%%\n", result.Summary.SuccessRate*100))
	builder.WriteString(fmt.Sprintf("- **严重问题**: %d\n", result.Summary.CriticalIssues))
	builder.WriteString(fmt.Sprintf("- **警告**: %d\n", result.Summary.Warnings))
	builder.WriteString("\n")
	
	// 差异部分
	if len(result.Differences) > 0 {
		builder.WriteString("## 差异列表\n\n")
		for i, diff := range result.Differences {
			severity := map[string]string{
				"critical": "🔴",
				"warning":  "🟡",
				"info":     "🔵",
			}[diff.Severity]
			
			builder.WriteString(fmt.Sprintf("%d. %s **%s** - %s\n", 
				i+1, severity, diff.Type, diff.Description))
			builder.WriteString(fmt.Sprintf("   - 路径: `%s`\n", diff.Path))
			if diff.Expected != nil {
				builder.WriteString(fmt.Sprintf("   - 期望: `%v`\n", diff.Expected))
			}
			if diff.Actual != nil {
				builder.WriteString(fmt.Sprintf("   - 实际: `%v`\n", diff.Actual))
			}
			builder.WriteString("\n")
		}
	}
	
	// 建议部分
	if len(result.Recommendations) > 0 {
		builder.WriteString("## 建议\n\n")
		for i, rec := range result.Recommendations {
			builder.WriteString(fmt.Sprintf("%d. %s\n", i+1, rec))
		}
		builder.WriteString("\n")
	}
	
	return builder.String()
}

// GetFormat 获取格式
func (r *MarkdownReporter) GetFormat() string {
	return "markdown"
}

// GenerateReport 生成文本报告
func (r *TextReporter) GenerateReport(result *ValidationResult) string {
	var builder strings.Builder
	
	builder.WriteString("=== 验证报告 ===\n\n")
	
	builder.WriteString(fmt.Sprintf("总体分数: %.2f\n", result.OverallScore))
	builder.WriteString(fmt.Sprintf("验证结果: %s\n", map[bool]string{true: "通过", false: "失败"}[result.IsValid]))
	builder.WriteString(fmt.Sprintf("成功率: %.1f%%\n", result.Summary.SuccessRate*100))
	
	if len(result.Differences) > 0 {
		builder.WriteString(fmt.Sprintf("\n发现 %d 个差异:\n", len(result.Differences)))
		for i, diff := range result.Differences {
			builder.WriteString(fmt.Sprintf("  %d. [%s] %s\n", i+1, diff.Severity, diff.Description))
		}
	}
	
	if len(result.Recommendations) > 0 {
		builder.WriteString(fmt.Sprintf("\n建议 (%d 项):\n", len(result.Recommendations)))
		for i, rec := range result.Recommendations {
			builder.WriteString(fmt.Sprintf("  %d. %s\n", i+1, rec))
		}
	}
	
	return builder.String()
}

// GetFormat 获取格式
func (r *TextReporter) GetFormat() string {
	return "text"
}

// GenerateReport 生成指定格式的报告
func (v *ValidationFramework) GenerateReport(result *ValidationResult, format string) string {
	for _, reporter := range v.reporters {
		if reporter.GetFormat() == format {
			return reporter.GenerateReport(result)
		}
	}
	
	// 默认返回JSON格式
	return v.reporters[0].GenerateReport(result)
}

// 配置函数

// getDefaultTolerance 获取默认容错配置
func getDefaultTolerance() Tolerance {
	return Tolerance{
		EventCount:     2,
		TextSimilarity: 0.1,
		TimingVariance: time.Second,
		FieldMismatch:  1,
	}
}