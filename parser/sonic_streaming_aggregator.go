package parser

import (
	"bytes"
	"kiro2api/logger"
	"kiro2api/utils"
	"strings"
	"sync"
	"time"
)

// SonicStreamingJSONAggregator 基于Sonic的高性能流式JSON聚合器
type SonicStreamingJSONAggregator struct {
	activeStreamers map[string]*SonicJSONStreamer
	mu              sync.RWMutex
	updateCallback  ToolParamsUpdateCallback
}

// SonicJSONStreamer 单个工具调用的Sonic流式解析器
type SonicJSONStreamer struct {
	toolUseId     string
	toolName      string
	buffer        *bytes.Buffer
	state         SonicParseState
	lastUpdate    time.Time
	isComplete    bool
	result        map[string]interface{}
	fragmentCount int
	totalBytes    int
}

// SonicParseState Sonic JSON解析状态
type SonicParseState struct {
	hasValidJSON    bool
	isPartialJSON   bool
	expectingValue  bool
	isValueFragment bool
}

// NewSonicStreamingJSONAggregator 创建基于Sonic的流式JSON聚合器
func NewSonicStreamingJSONAggregator() *SonicStreamingJSONAggregator {
	return &SonicStreamingJSONAggregator{
		activeStreamers: make(map[string]*SonicJSONStreamer),
	}
}

// NewSonicStreamingJSONAggregatorWithCallback 创建带回调的Sonic流式JSON聚合器
func NewSonicStreamingJSONAggregatorWithCallback(callback ToolParamsUpdateCallback) *SonicStreamingJSONAggregator {
	logger.Debug("创建Sonic流式JSON聚合器",
		logger.Bool("has_callback", callback != nil))

	return &SonicStreamingJSONAggregator{
		activeStreamers: make(map[string]*SonicJSONStreamer),
		updateCallback:  callback,
	}
}

// SetUpdateCallback 设置更新回调
func (ssja *SonicStreamingJSONAggregator) SetUpdateCallback(callback ToolParamsUpdateCallback) {
	ssja.mu.Lock()
	defer ssja.mu.Unlock()
	ssja.updateCallback = callback
}

// ProcessToolData 处理工具调用数据片段（Sonic版本）
func (ssja *SonicStreamingJSONAggregator) ProcessToolData(toolUseId, name, input string, stop bool, fragmentIndex int) (complete bool, fullInput string) {
	ssja.mu.Lock()
	defer ssja.mu.Unlock()

	// 获取或创建Sonic流式解析器
	streamer, exists := ssja.activeStreamers[toolUseId]
	if !exists {
		streamer = ssja.createSonicJSONStreamer(toolUseId, name)
		ssja.activeStreamers[toolUseId] = streamer

		logger.Debug("创建Sonic JSON流式解析器",
			logger.String("toolUseId", toolUseId),
			logger.String("toolName", name))
	}

	// 处理输入片段
	if input != "" {
		if err := streamer.appendFragment(input); err != nil {
			logger.Warn("追加JSON片段到Sonic解析器失败",
				logger.String("toolUseId", toolUseId),
				logger.String("fragment", input),
				logger.Err(err))
		}
	}

	// 使用Sonic尝试解析当前缓冲区
	parseResult := streamer.tryParseWithSonic()

	logger.Debug("Sonic流式JSON解析进度",
		logger.String("toolUseId", toolUseId),
		logger.String("fragment", input),
		logger.Bool("hasValidJSON", streamer.state.hasValidJSON),
		logger.Bool("isPartialJSON", streamer.state.isPartialJSON),
		logger.Bool("stop", stop),
		logger.String("parseStatus", parseResult),
		logger.Int("fragmentCount", streamer.fragmentCount),
		logger.Int("totalBytes", streamer.totalBytes))

	// 如果收到停止信号
	if stop {
		streamer.isComplete = true

		// 最终尝试解析或生成基础JSON
		if !streamer.state.hasValidJSON {
			logger.Debug("停止时JSON未完整，尝试Sonic智能补全",
				logger.String("toolUseId", toolUseId),
				logger.Int("bufferSize", streamer.buffer.Len()))

			result := ssja.sonicIntelligentComplete(streamer)
			if result != nil {
				streamer.result = result
				streamer.state.hasValidJSON = true
			}
		}

		if streamer.state.hasValidJSON && streamer.result != nil {
			// 使用Sonic序列化结果
			if jsonBytes, err := utils.FastMarshal(streamer.result); err == nil {
				fullInput = string(jsonBytes)
			} else {
				logger.Warn("Sonic序列化失败，使用fallback",
					logger.Err(err))
				fullInput = ssja.generateFallbackJSON(streamer.toolName)
			}
		} else {
			fullInput = ssja.generateFallbackJSON(streamer.toolName)
		}

		// 清理完成的流式解析器
		delete(ssja.activeStreamers, toolUseId)

		// 触发回调
		ssja.onAggregationComplete(toolUseId, fullInput)

		logger.Debug("Sonic流式JSON聚合完成",
			logger.String("toolUseId", toolUseId),
			logger.String("toolName", name),
			logger.String("result", func() string {
				if len(fullInput) > 100 {
					return fullInput[:100] + "..."
				}
				return fullInput
			}()),
			logger.Int("totalFragments", streamer.fragmentCount),
			logger.Int("totalBytes", streamer.totalBytes))

		return true, fullInput
	}

	return false, ""
}

// createSonicJSONStreamer 创建Sonic JSON流式解析器
func (ssja *SonicStreamingJSONAggregator) createSonicJSONStreamer(toolUseId, toolName string) *SonicJSONStreamer {
	buffer := &bytes.Buffer{}

	return &SonicJSONStreamer{
		toolUseId:  toolUseId,
		toolName:   toolName,
		buffer:     buffer,
		lastUpdate: time.Now(),
		state: SonicParseState{
			expectingValue: true,
		},
		result: make(map[string]interface{}),
	}
}

// appendFragment 追加JSON片段
func (sjs *SonicJSONStreamer) appendFragment(fragment string) error {
	sjs.buffer.WriteString(fragment)
	sjs.lastUpdate = time.Now()
	sjs.fragmentCount++
	sjs.totalBytes += len(fragment)

	return nil
}

// tryParseWithSonic 使用Sonic尝试解析当前缓冲区
func (sjs *SonicJSONStreamer) tryParseWithSonic() string {
	content := sjs.buffer.Bytes()
	if len(content) == 0 {
		return "empty"
	}

	// 尝试使用Sonic完整JSON解析
	var result map[string]interface{}
	if err := utils.FastUnmarshal(content, &result); err == nil {
		sjs.result = result
		sjs.state.hasValidJSON = true
		logger.Debug("Sonic完整JSON解析成功",
			logger.String("toolUseId", sjs.toolUseId),
			logger.Int("resultKeys", len(result)))
		return "complete"
	}

	// 检查是否为部分有效的JSON开始
	if sjs.isSonicValidJSONStart(content) {
		sjs.state.isPartialJSON = true
		return "partial"
	}

	// 检查是否只是值片段（无键）
	if sjs.looksLikeValueFragment(string(content)) {
		sjs.state.isValueFragment = true
		return "value_fragment"
	}

	return "invalid"
}

// isSonicValidJSONStart 使用Sonic检查是否为有效的JSON开始
func (sjs *SonicJSONStreamer) isSonicValidJSONStart(content []byte) bool {
	contentStr := strings.TrimSpace(string(content))
	if !strings.HasPrefix(contentStr, "{") {
		return false
	}

	// 使用Sonic尝试解析
	var testValue interface{}
	err := utils.FastUnmarshal(content, &testValue)

	// 如果错误是由于不完整的JSON，那么说明开始是有效的
	if err != nil {
		// Sonic在遇到不完整JSON时会返回特定错误
		errStr := err.Error()
		if strings.Contains(errStr, "unexpected end") ||
			strings.Contains(errStr, "incomplete") ||
			strings.Contains(errStr, "EOF") ||
			strings.Contains(errStr, "reached end") {
			logger.Debug("Sonic检测到不完整但有效的JSON开始",
				logger.String("toolUseId", sjs.toolUseId),
				logger.String("error", errStr))
			return true
		}
		return false
	}

	// 如果成功解析，说明是有效的JSON片段
	return true
}

// looksLikeValueFragment 检查是否看起来像值片段
func (sjs *SonicJSONStreamer) looksLikeValueFragment(content string) bool {
	content = strings.TrimSpace(content)

	// 检查是否看起来像路径
	if strings.Contains(content, "/") && !strings.Contains(content, " ") {
		return true
	}

	// 检查是否看起来像命令
	if strings.Contains(content, " ") && len(content) > 3 {
		return true
	}

	// 检查是否为简单字符串值
	if len(content) > 0 && !strings.HasPrefix(content, "{") && !strings.HasPrefix(content, "[") {
		return true
	}

	return false
}

// sonicIntelligentComplete 使用Sonic智能补全JSON
func (ssja *SonicStreamingJSONAggregator) sonicIntelligentComplete(streamer *SonicJSONStreamer) map[string]interface{} {
	content := strings.TrimSpace(streamer.buffer.String())

	logger.Debug("Sonic智能补全JSON",
		logger.String("toolName", streamer.toolName),
		logger.String("content", func() string {
			if len(content) > 50 {
				return content[:50] + "..."
			}
			return content
		}()),
		logger.Bool("isValueFragment", streamer.state.isValueFragment),
		logger.Bool("isPartialJSON", streamer.state.isPartialJSON))

	// 如果内容看起来像值片段，根据工具类型构建JSON
	if streamer.state.isValueFragment || streamer.looksLikeValueFragment(content) {
		return ssja.buildJSONFromValue(streamer.toolName, content)
	}

	// 如果是不完整的JSON，尝试补全
	if strings.HasPrefix(content, "{") || streamer.state.isPartialJSON {
		return ssja.sonicCompletePartialJSON(content, streamer.toolName)
	}

	return nil
}

// buildJSONFromValue 从值构建JSON（使用Sonic优化）
func (ssja *SonicStreamingJSONAggregator) buildJSONFromValue(toolName, value string) map[string]interface{} {
	// 清理值
	value = strings.Trim(value, "\"")

	var result map[string]interface{}

	switch strings.ToLower(toolName) {
	case "ls":
		result = map[string]interface{}{"path": value}
	case "read":
		result = map[string]interface{}{"file_path": value}
	case "write":
		if strings.Contains(value, "/") {
			result = map[string]interface{}{"file_path": value, "content": ""}
		} else {
			result = map[string]interface{}{"file_path": "", "content": value}
		}
	case "bash":
		result = map[string]interface{}{"command": value}
	case "edit":
		if strings.Contains(value, "/") {
			result = map[string]interface{}{"file_path": value, "old_string": "", "new_string": ""}
		} else {
			result = map[string]interface{}{"file_path": "", "old_string": value, "new_string": ""}
		}
	case "glob":
		result = map[string]interface{}{"pattern": value}
	case "grep":
		result = map[string]interface{}{"pattern": value}
	default:
		if strings.Contains(value, "/") {
			result = map[string]interface{}{"path": value}
		} else {
			result = map[string]interface{}{"input": value}
		}
	}

	logger.Debug("Sonic构建JSON成功",
		logger.String("toolName", toolName),
		logger.String("value", value),
		logger.Int("resultKeys", len(result)))

	return result
}

// sonicCompletePartialJSON 使用Sonic补全不完整的JSON
func (ssja *SonicStreamingJSONAggregator) sonicCompletePartialJSON(content, toolName string) map[string]interface{} {
	logger.Debug("Sonic尝试补全不完整JSON",
		logger.String("content", func() string {
			if len(content) > 100 {
				return content[:100] + "..."
			}
			return content
		}()),
		logger.String("toolName", toolName))

	// 尝试修复缺失的引号和括号
	fixed := content

	// 计算括号和引号的平衡
	braceCount := strings.Count(fixed, "{") - strings.Count(fixed, "}")
	quoteCount := strings.Count(fixed, "\"")

	// 如果引号数量为奇数，补全最后一个引号
	if quoteCount%2 == 1 {
		fixed += "\""
	}

	// 补全缺失的右括号
	for i := 0; i < braceCount; i++ {
		fixed += "}"
	}

	// 使用Sonic尝试解析修复后的JSON
	var result map[string]interface{}
	if err := utils.FastUnmarshal([]byte(fixed), &result); err == nil {
		logger.Debug("Sonic JSON补全成功",
			logger.String("fixed", func() string {
				if len(fixed) > 50 {
					return fixed[:50] + "..."
				}
				return fixed
			}()),
			logger.Int("resultKeys", len(result)))
		return result
	}

	logger.Debug("Sonic JSON补全失败，使用fallback",
		logger.String("fixed", func() string {
			if len(fixed) > 50 {
				return fixed[:50] + "..."
			}
			return fixed
		}()),
		logger.String("toolName", toolName))

	// 如果仍然解析失败，返回nil让调用者使用fallback
	return nil
}

// generateFallbackJSON 生成回退JSON
func (ssja *SonicStreamingJSONAggregator) generateFallbackJSON(toolName string) string {
	switch strings.ToLower(toolName) {
	case "ls":
		return `{"path": "."}`
	case "read":
		return `{"file_path": ""}`
	case "write":
		return `{"file_path": "", "content": ""}`
	case "bash":
		return `{"command": ""}`
	case "edit":
		return `{"file_path": "", "old_string": "", "new_string": ""}`
	case "glob":
		return `{"pattern": ""}`
	case "grep":
		return `{"pattern": ""}`
	default:
		return `{}`
	}
}

// onAggregationComplete 聚合完成回调
func (ssja *SonicStreamingJSONAggregator) onAggregationComplete(toolUseId string, fullInput string) {
	if ssja.updateCallback != nil {
		logger.Debug("触发Sonic流式JSON聚合回调",
			logger.String("toolUseId", toolUseId),
			logger.String("inputPreview", func() string {
				if len(fullInput) > 50 {
					return fullInput[:50] + "..."
				}
				return fullInput
			}()))
		ssja.updateCallback(toolUseId, fullInput)
	} else {
		logger.Debug("Sonic聚合回调函数为空",
			logger.String("toolUseId", toolUseId))
	}
}

// CleanupExpiredBuffers 清理过期的缓冲区
func (ssja *SonicStreamingJSONAggregator) CleanupExpiredBuffers(timeout time.Duration) {
	ssja.mu.Lock()
	defer ssja.mu.Unlock()

	now := time.Now()
	cleanedCount := 0
	for toolUseId, streamer := range ssja.activeStreamers {
		if now.Sub(streamer.lastUpdate) > timeout {
			logger.Warn("清理过期的Sonic JSON流式解析器",
				logger.String("toolUseId", toolUseId),
				logger.Duration("age", now.Sub(streamer.lastUpdate)),
				logger.Int("fragments", streamer.fragmentCount),
				logger.Int("totalBytes", streamer.totalBytes))
			delete(ssja.activeStreamers, toolUseId)
			cleanedCount++
		}
	}

	if cleanedCount > 0 {
		logger.Debug("Sonic聚合器清理完成",
			logger.Int("cleanedCount", cleanedCount),
			logger.Int("remainingCount", len(ssja.activeStreamers)))
	}
}

// GetStats 获取聚合器统计信息
func (ssja *SonicStreamingJSONAggregator) GetStats() map[string]interface{} {
	ssja.mu.RLock()
	defer ssja.mu.RUnlock()

	totalFragments := 0
	totalBytes := 0
	for _, streamer := range ssja.activeStreamers {
		totalFragments += streamer.fragmentCount
		totalBytes += streamer.totalBytes
	}

	return map[string]interface{}{
		"active_streamers": len(ssja.activeStreamers),
		"total_fragments":  totalFragments,
		"total_bytes":      totalBytes,
		"engine":           "sonic",
	}
}
