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
// ToolParamsUpdateCallback 工具参数更新回调函数类型
type ToolParamsUpdateCallback func(toolUseId string, fullParams string)

// AWS EventStream流式传输配置
// 由于EventStream按字节边界分片传输，导致UTF-8字符截断，
// 因此只在收到停止信号时进行JSON解析，避免解析损坏的片段

type SonicStreamingJSONAggregator struct {
	activeStreamers map[string]*SonicJSONStreamer
	mu              sync.RWMutex
	updateCallback  ToolParamsUpdateCallback
}

// SonicJSONStreamer 单个工具调用的Sonic流式解析器
type SonicJSONStreamer struct {
	toolUseId      string
	toolName       string
	buffer         *bytes.Buffer
	state          SonicParseState
	lastUpdate     time.Time
	isComplete     bool
	result         map[string]any
	fragmentCount  int
	totalBytes     int
	incompleteUTF8 string // 用于存储跨片段的不完整UTF-8字符
}

// SonicParseState Sonic JSON解析状态
type SonicParseState struct {
	hasValidJSON    bool
	isPartialJSON   bool
	expectingValue  bool
	isValueFragment bool
}

// 辅助函数：获取两个整数的最大值
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
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

	// AWS EventStream按字节边界分片传输，导致UTF-8中文字符截断问题
	// 只有在收到停止信号时才进行最终解析，避免中途解析损坏的JSON片段
	shouldParse := stop

	var parseResult string
	if shouldParse {
		// 使用Sonic尝试解析当前缓冲区
		parseResult = streamer.tryParseWithSonic()

		logger.Debug("Sonic流式JSON解析进度",
			logger.String("toolUseId", toolUseId),
			logger.String("fragment", input),
			logger.Bool("hasValidJSON", streamer.state.hasValidJSON),
			logger.Bool("isPartialJSON", streamer.state.isPartialJSON),
			logger.Bool("stop", stop),
			logger.String("parseStatus", parseResult),
			logger.Int("fragmentCount", streamer.fragmentCount),
			logger.Int("totalBytes", streamer.totalBytes))
	} else {
		// AWS EventStream分片传输：仅累积数据，避免解析截断的UTF-8字符
		logger.Debug("EventStream分片累积数据",
			logger.String("toolUseId", toolUseId),
			logger.String("fragment", input),
			logger.Int("bufferSize", streamer.buffer.Len()),
			logger.Int("fragmentCount", streamer.fragmentCount),
			logger.Int("totalBytes", streamer.totalBytes),
			logger.String("reason", "awaiting_stop_signal_for_complete_json"))

		parseResult = "streaming_accumulation"
	}

	// 如果收到停止信号
	if stop {
		streamer.isComplete = true

		// *** 关键修复：区分无参数工具和真正的JSON不完整 ***
		// 检查是否真的有数据但JSON不完整
		hasActualData := streamer.fragmentCount > 0 || streamer.totalBytes > 0

		// 最终尝试解析或生成基础JSON
		if !streamer.state.hasValidJSON {
			if hasActualData {
				// 只有真正收到数据但解析失败时才记录"未完整"日志
				logger.Debug("停止时JSON未完整",
					logger.String("toolUseId", toolUseId),
					logger.Int("bufferSize", streamer.buffer.Len()),
					logger.Int("fragmentCount", streamer.fragmentCount),
					logger.Int("totalBytes", streamer.totalBytes))

			} else {
				// 无参数工具的正常情况，直接生成默认JSON，不记录误导性日志
				logger.Debug("工具无参数，使用默认参数格式",
					logger.String("toolUseId", toolUseId),
					logger.String("toolName", name))
			}
		}

		if streamer.state.hasValidJSON && streamer.result != nil {
			// 使用Sonic序列化结果
			if jsonBytes, err := utils.FastMarshal(streamer.result); err == nil {
				fullInput = string(jsonBytes)
			} else {
				logger.Error("Sonic序列化失败，无法生成工具输入",
					logger.Err(err),
					logger.String("toolName", streamer.toolName))
				// 使用空JSON对象，让工具调用失败
				fullInput = "{}"
			}
		} else {
			logger.Error("流式解析失败，无有效JSON结果",
				logger.String("toolName", streamer.toolName),
				logger.Bool("hasValidJSON", streamer.state.hasValidJSON))
			// 使用空JSON对象，让工具调用失败
			fullInput = "{}"
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
		result: make(map[string]any),
	}
}

// appendFragment 追加JSON片段
func (sjs *SonicJSONStreamer) appendFragment(fragment string) error {
	// 确保UTF-8字符完整性
	safeFragment := sjs.ensureUTF8Integrity(fragment)

	sjs.buffer.WriteString(safeFragment)
	sjs.lastUpdate = time.Now()
	sjs.fragmentCount++
	sjs.totalBytes += len(fragment) // 使用原始长度统计

	return nil
}

// ensureUTF8Integrity 确保UTF-8字符完整性
func (sjs *SonicJSONStreamer) ensureUTF8Integrity(fragment string) string {
	if fragment == "" {
		return fragment
	}

	// 检查片段是否以不完整的UTF-8字符结尾
	bytes := []byte(fragment)
	n := len(bytes)
	if n == 0 {
		return fragment
	}

	// 从末尾开始检查UTF-8字符边界
	for i := n - 1; i >= 0 && i >= n-4; i-- {
		b := bytes[i]

		// 检查是否为UTF-8多字节序列的开始
		if b&0x80 == 0 {
			// ASCII字符，边界正确
			break
		} else if b&0xE0 == 0xC0 {
			// 2字节UTF-8序列开始
			if n-i < 2 {
				logger.Debug("检测到截断的UTF-8字符(2字节)",
					logger.String("toolUseId", sjs.toolUseId),
					logger.Int("position", i),
					logger.String("fragment_end", fragment[max(0, len(fragment)-10):]))
				// 保存截断的字符到下一个片段处理
				sjs.incompleteUTF8 = string(bytes[i:])
				return string(bytes[:i])
			}
			break
		} else if b&0xF0 == 0xE0 {
			// 3字节UTF-8序列开始
			if n-i < 3 {
				logger.Debug("检测到截断的UTF-8字符(3字节)",
					logger.String("toolUseId", sjs.toolUseId),
					logger.Int("position", i),
					logger.String("fragment_end", fragment[max(0, len(fragment)-10):]))
				sjs.incompleteUTF8 = string(bytes[i:])
				return string(bytes[:i])
			}
			break
		} else if b&0xF8 == 0xF0 {
			// 4字节UTF-8序列开始
			if n-i < 4 {
				logger.Debug("检测到截断的UTF-8字符(4字节)",
					logger.String("toolUseId", sjs.toolUseId),
					logger.Int("position", i),
					logger.String("fragment_end", fragment[max(0, len(fragment)-10):]))
				sjs.incompleteUTF8 = string(bytes[i:])
				return string(bytes[:i])
			}
			break
		}
		// 继续字符(10xxxxxx)，继续向前检查
	}

	// 检查是否有之前的不完整UTF-8字符需要拼接
	if sjs.incompleteUTF8 != "" {
		combined := sjs.incompleteUTF8 + fragment
		logger.Debug("恢复截断的UTF-8字符",
			logger.String("toolUseId", sjs.toolUseId),
			logger.String("incomplete", sjs.incompleteUTF8),
			logger.String("current_fragment", fragment[:min(10, len(fragment))]),
			logger.String("combined_start", combined[:min(20, len(combined))]))
		sjs.incompleteUTF8 = ""                  // 清空
		return sjs.ensureUTF8Integrity(combined) // 递归处理合并结果
	}

	return fragment
}

// tryParseWithSonic 使用Sonic尝试解析当前缓冲区
func (sjs *SonicJSONStreamer) tryParseWithSonic() string {
	content := sjs.buffer.Bytes()
	if len(content) == 0 {
		return "empty"
	}

	// 尝试使用Sonic完整JSON解析
	var result map[string]any
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
	var testValue any
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
func (ssja *SonicStreamingJSONAggregator) GetStats() map[string]any {
	ssja.mu.RLock()
	defer ssja.mu.RUnlock()

	totalFragments := 0
	totalBytes := 0
	for _, streamer := range ssja.activeStreamers {
		totalFragments += streamer.fragmentCount
		totalBytes += streamer.totalBytes
	}

	// 统计pending状态的工具数量
	pendingCount := 0
	for _, streamer := range ssja.activeStreamers {
		if !streamer.state.hasValidJSON && !streamer.isComplete {
			pendingCount++
		}
	}

	return map[string]any{
		"active_streamers":    len(ssja.activeStreamers),
		"streaming_streamers": pendingCount,
		"total_fragments":     totalFragments,
		"total_bytes":         totalBytes,
		"engine":              "sonic",
		"strategy":            "stop_signal_only",
		"utf8_safe":           true,
	}
}
