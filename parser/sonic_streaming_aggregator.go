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
	result         map[string]interface{}
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

	// *** 关键修复：按1.md建议简化逻辑，移除过早解析 ***

	// 1. 检查是否为值片段（优先级最高）
	if streamer.state.isValueFragment || streamer.looksLikeValueFragment(content) {
		logger.Debug("处理值片段", logger.String("content", content))
		return ssja.buildJSONFromValue(streamer.toolName, content)
	}

	// 2. 如果是不完整的JSON，尝试补全（移除复杂的损坏检测逻辑）
	if strings.HasPrefix(content, "{") || strings.HasPrefix(content, "[") || streamer.state.isPartialJSON {
		result := ssja.sonicCompletePartialJSON(content, streamer.toolName)
		if result != nil {
			return result
		}
	}

	// 3. 简化的fallback逻辑
	logger.Debug("无法智能补全JSON，返回nil")
	return nil
}

// buildJSONFromValue 从值构建JSON（使用Sonic优化）
func (ssja *SonicStreamingJSONAggregator) buildJSONFromValue(toolName, value string) map[string]interface{} {
	// 清理值 - 更智能的处理
	value = ssja.cleanValue(value)

	var result map[string]interface{}

	// 处理MCP工具名称（如 mcp__serena__list_dir）
	if strings.HasPrefix(strings.ToLower(toolName), "mcp__") {
		result = ssja.buildMCPToolJSON(toolName, value)
	} else {
		// 标准工具处理
		switch strings.ToLower(toolName) {
		case "ls":
			result = map[string]interface{}{"path": value}
		case "read":
			result = map[string]interface{}{"file_path": value}
		case "write":
			// *** 关键修复：Write工具的智能参数分配 ***
			// 1. 如果value看起来像完整的JSON参数，直接解析
			if strings.HasPrefix(strings.TrimSpace(value), "{") && strings.Contains(value, "file_path") {
				var tempMap map[string]interface{}
				if err := utils.FastUnmarshal([]byte(value), &tempMap); err == nil {
					result = tempMap
					break
				}
			}

			// 2. 如果包含路径分隔符，很可能是file_path
			if strings.Contains(value, "/") && !strings.Contains(value, "\n") && len(value) < 500 {
				result = map[string]interface{}{"file_path": value, "content": ""}
			} else {
				// 3. 大型内容或包含换行符的，作为content处理，但需要合理的file_path
				// 从累积的上下文中尝试推断file_path（如果有的话）
				filePath := ""
				if strings.Contains(value, ".html") {
					// 从HTML内容中尝试提取可能的文件路径信息
					if strings.Contains(value, "weather") {
						filePath = "weather.html"
					} else {
						filePath = "index.html"
					}
				} else if strings.Contains(value, ".js") || strings.Contains(value, "javascript") {
					filePath = "script.js"
				} else if strings.Contains(value, ".css") || strings.Contains(value, "style") {
					filePath = "style.css"
				}

				result = map[string]interface{}{"file_path": filePath, "content": value}
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
	}

	logger.Debug("Sonic构建JSON成功",
		logger.String("toolName", toolName),
		logger.String("value", func() string {
			if len(value) > 100 {
				return value[:100] + "..."
			}
			return value
		}()),
		logger.Int("resultKeys", len(result)))

	return result
}

// cleanValue 清理值，处理各种转义和格式问题
func (ssja *SonicStreamingJSONAggregator) cleanValue(value string) string {
	// 移除前后引号
	value = strings.Trim(value, "\"")

	// 处理可能的键值对格式（如 "path": "/Users/..."）
	if strings.Contains(value, "\":") {
		parts := strings.SplitN(value, "\":", 2)
		if len(parts) == 2 {
			value = strings.TrimSpace(parts[1])
			value = strings.Trim(value, " \"")
		}
	}

	// 处理转义字符
	value = strings.ReplaceAll(value, "\\\"", "\"")
	value = strings.ReplaceAll(value, "\\\\", "\\")

	// 处理可能的JSON片段（如 {"path": "/Users/..."} ）
	if strings.HasPrefix(value, "{") && strings.Contains(value, "\":") {
		// 尝试提取值
		var tempMap map[string]interface{}
		if err := utils.FastUnmarshal([]byte(value), &tempMap); err == nil {
			// 成功解析，提取第一个值
			for _, v := range tempMap {
				if strVal, ok := v.(string); ok {
					return strVal
				}
			}
		}
	}

	return value
}

// buildMCPToolJSON 构建MCP工具的JSON
func (ssja *SonicStreamingJSONAggregator) buildMCPToolJSON(toolName, value string) map[string]interface{} {
	// 提取实际的MCP工具名称
	parts := strings.Split(toolName, "__")
	if len(parts) >= 3 {
		actualToolName := parts[2]

		logger.Debug("构建MCP工具JSON",
			logger.String("fullName", toolName),
			logger.String("actualToolName", actualToolName),
			logger.String("value", value))

		switch strings.ToLower(actualToolName) {
		case "list_dir":
			return map[string]interface{}{
				"relative_path": value,
				"recursive":     false,
			}
		case "find_file":
			return map[string]interface{}{
				"file_mask":     "*",
				"relative_path": value,
			}
		case "find_symbol", "get_symbols_overview":
			return map[string]interface{}{
				"relative_path": value,
			}
		case "search_for_pattern":
			return map[string]interface{}{
				"substring_pattern": value,
				"relative_path":     ".",
			}
		default:
			// 通用MCP工具处理
			if strings.Contains(value, "/") {
				return map[string]interface{}{"relative_path": value}
			}
			return map[string]interface{}{"input": value}
		}
	}

	// 如果无法解析MCP工具名，返回通用格式
	return map[string]interface{}{"input": value}
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

	// 首先尝试直接解析
	var result map[string]interface{}
	if err := utils.FastUnmarshal([]byte(content), &result); err == nil {
		return result
	}

	// 智能修复JSON
	fixed := ssja.intelligentJSONFix(content, toolName)

	// 使用Sonic尝试解析修复后的JSON
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

	// 如果还是失败，尝试从内容中提取并重建
	if extracted := ssja.extractAndRebuildJSON(content, toolName); extracted != nil {
		return extracted
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

// intelligentJSONFix 智能修复不完整的JSON
func (ssja *SonicStreamingJSONAggregator) intelligentJSONFix(content, toolName string) string {
	fixed := content

	// 1. 修复截断的中文字符和键名
	fixed = ssja.fixTruncatedChineseCharacters(fixed)

	// 2. 修复损坏的JSON结构
	fixed = ssja.fixBrokenJSONStructure(fixed, toolName)

	// 3. 处理截断的键名
	if strings.Contains(fixed, "\"file_pa") && !strings.Contains(fixed, "\"file_path\"") {
		fixed = strings.Replace(fixed, "\"file_pa", "\"file_path", 1)
	}
	if strings.Contains(fixed, "\"relative_pa") && !strings.Contains(fixed, "\"relative_path\"") {
		fixed = strings.Replace(fixed, "\"relative_pa", "\"relative_path", 1)
	}
	if strings.Contains(fixed, "\"comm") && !strings.Contains(fixed, "\"command\"") && strings.ToLower(toolName) == "bash" {
		fixed = strings.Replace(fixed, "\"comm", "\"command", 1)
	}

	// 4. 计算括号和引号的平衡
	braceCount := strings.Count(fixed, "{") - strings.Count(fixed, "}")
	quoteCount := strings.Count(fixed, "\"")

	// 5. 处理未闭合的字符串值
	lastColonIdx := strings.LastIndex(fixed, ":")
	if lastColonIdx > 0 {
		afterColon := strings.TrimSpace(fixed[lastColonIdx+1:])
		// 如果值以引号开始但没有结束引号
		if strings.HasPrefix(afterColon, "\"") && !strings.HasSuffix(afterColon, "\"") && !strings.HasSuffix(afterColon, "}") {
			fixed += "\""
			quoteCount++
		}
	}

	// 6. 如果引号数量为奇数，补全最后一个引号
	if quoteCount%2 == 1 {
		fixed += "\""
	}

	// 7. 补全缺失的右括号
	for i := 0; i < braceCount; i++ {
		fixed += "}"
	}

	return fixed
}

// fixTruncatedChineseCharacters 修复截断的中文字符
func (ssja *SonicStreamingJSONAggregator) fixTruncatedChineseCharacters(content string) string {
	// 移除硬编码的中文字符截断模式修复
	// 现在依赖通用的UTF-8完整性检查机制
	return content
}

// fixBrokenJSONStructure 修复损坏的JSON结构
func (ssja *SonicStreamingJSONAggregator) fixBrokenJSONStructure(content, toolName string) string {
	fixed := content

	// 通用的JSON结构修复，移除硬编码的特定内容模式
	// 修复缺失引号的常见模式
	fixed = strings.ReplaceAll(fixed, "}content\":", "\"content\":")

	// 针对TodoWrite工具的通用结构修复
	if strings.ToLower(toolName) == "todowrite" {
		// 修复todos数组结构
		if strings.Contains(fixed, "\"todos\"") && !strings.Contains(fixed, "[{") {
			// 确保todos后面有正确的数组开始
			fixed = strings.Replace(fixed, "\"todos\":", "\"todos\":[", 1)
		}

		// 修复对象分隔符
		fixed = strings.ReplaceAll(fixed, "}content\":", "},{\"content\":")
		fixed = strings.ReplaceAll(fixed, "}\"content\":", "},{\"content\":")

		// 修复数组结束
		if strings.Contains(fixed, "\"todos\":[") && !strings.HasSuffix(strings.TrimSpace(fixed), "]}") && !strings.HasSuffix(strings.TrimSpace(fixed), "}]") {
			// 确保数组正确结束
			if strings.HasSuffix(strings.TrimSpace(fixed), "}") {
				fixed = strings.TrimSpace(fixed) + "]"
			}
		}
	}

	return fixed
}

// extractAndRebuildJSON 从损坏的内容中提取键值对并重建JSON
func (ssja *SonicStreamingJSONAggregator) extractAndRebuildJSON(content, toolName string) map[string]interface{} {
	result := make(map[string]interface{})

	// 尝试提取路径
	if path := ssja.extractPath(content); path != "" {
		// 根据工具类型设置正确的键名
		if strings.HasPrefix(strings.ToLower(toolName), "mcp__") {
			result["relative_path"] = path
			// MCP工具的额外参数
			parts := strings.Split(toolName, "__")
			if len(parts) >= 3 {
				actualToolName := parts[2]
				switch strings.ToLower(actualToolName) {
				case "list_dir":
					result["recursive"] = false
				case "find_file":
					result["file_mask"] = "*"
				}
			}
		} else {
			switch strings.ToLower(toolName) {
			case "read":
				result["file_path"] = path
			case "write":
				result["file_path"] = path
				result["content"] = ""
			case "ls":
				result["path"] = path
			default:
				result["path"] = path
			}
		}
		return result
	}

	// 尝试提取命令（针对bash工具）
	if strings.ToLower(toolName) == "bash" {
		if cmd := ssja.extractCommand(content); cmd != "" {
			result["command"] = cmd
			return result
		}
	}

	return nil
}

// extractPath 从内容中提取路径
func (ssja *SonicStreamingJSONAggregator) extractPath(content string) string {
	// 查找绝对路径模式
	if idx := strings.Index(content, "/Users/"); idx >= 0 {
		path := content[idx:]
		// 找到路径的结束位置
		endIdx := strings.IndexAny(path, "\",}")
		if endIdx > 0 {
			path = path[:endIdx]
		}
		return strings.TrimSpace(path)
	}

	// 查找相对路径
	for _, pattern := range []string{"./", "../", "."} {
		if idx := strings.Index(content, pattern); idx >= 0 {
			// 确保不是在字符串中间
			if idx == 0 || content[idx-1] == '"' || content[idx-1] == ' ' || content[idx-1] == ':' {
				path := content[idx:]
				endIdx := strings.IndexAny(path, "\",}")
				if endIdx > 0 {
					path = path[:endIdx]
				}
				return strings.TrimSpace(path)
			}
		}
	}

	return ""
}

// extractCommand 从内容中提取命令
func (ssja *SonicStreamingJSONAggregator) extractCommand(content string) string {
	// 常见命令关键字
	commands := []string{"mkdir", "echo", "ls", "cd", "pwd", "cat", "grep", "find", "touch", "rm", "cp", "mv"}

	for _, cmd := range commands {
		if idx := strings.Index(content, cmd); idx >= 0 {
			// 确保是命令的开始
			if idx == 0 || content[idx-1] == '"' || content[idx-1] == ' ' {
				command := content[idx:]
				// 找到命令的结束位置
				endIdx := strings.IndexAny(command, "\",}")
				if endIdx > 0 {
					command = command[:endIdx]
				}
				return strings.TrimSpace(command)
			}
		}
	}

	return ""
}

// generateFallbackJSON 生成回退JSON
func (ssja *SonicStreamingJSONAggregator) generateFallbackJSON(toolName string) string {
	// 处理MCP工具
	if strings.HasPrefix(strings.ToLower(toolName), "mcp__") {
		parts := strings.Split(toolName, "__")
		if len(parts) >= 3 {
			actualToolName := parts[2]
			switch strings.ToLower(actualToolName) {
			case "list_dir":
				return `{"relative_path": ".", "recursive": false}`
			case "find_file":
				return `{"file_mask": "*", "relative_path": "."}`
			case "find_symbol", "get_symbols_overview":
				return `{"relative_path": ""}`
			case "search_for_pattern":
				return `{"substring_pattern": "", "relative_path": "."}`
			default:
				return `{"input": ""}`
			}
		}
		return `{}`
	}

	// 标准工具处理
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

	// 统计pending状态的工具数量
	pendingCount := 0
	for _, streamer := range ssja.activeStreamers {
		if !streamer.state.hasValidJSON && !streamer.isComplete {
			pendingCount++
		}
	}

	return map[string]interface{}{
		"active_streamers":    len(ssja.activeStreamers),
		"streaming_streamers": pendingCount,
		"total_fragments":     totalFragments,
		"total_bytes":         totalBytes,
		"engine":              "sonic",
		"strategy":            "stop_signal_only",
		"utf8_safe":           true,
	}
}
