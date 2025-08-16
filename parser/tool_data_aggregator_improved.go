package parser

import (
	"encoding/json"
	"fmt"
	"kiro2api/logger"
	"strings"
	"sync"
	"time"
)

// ToolParamsUpdateCallback 工具参数更新回调函数类型
type ToolParamsUpdateCallback func(toolUseId string, fullParams string)

// ImprovedToolDataAggregator 改进版的工具数据聚合器
type ImprovedToolDataAggregator struct {
	activeTools    map[string]*ImprovedToolBuffer
	mu             sync.RWMutex
	updateCallback ToolParamsUpdateCallback // 参数更新回调函数
}

// ImprovedToolBuffer 改进的工具数据缓冲区
type ImprovedToolBuffer struct {
	toolUseId     string
	name          string
	fragments     []string       // 保存所有片段，包括空字符串
	fragmentOrder map[int]string // 片段顺序映射
	lastUpdate    time.Time
	isComplete    bool
	startIndex    int // 记录起始片段索引
}

// NewImprovedToolDataAggregator 创建改进的工具数据聚合器
func NewImprovedToolDataAggregator() *ImprovedToolDataAggregator {
	return &ImprovedToolDataAggregator{
		activeTools: make(map[string]*ImprovedToolBuffer),
	}
}

// NewImprovedToolDataAggregatorWithCallback 创建带回调的改进工具数据聚合器
func NewImprovedToolDataAggregatorWithCallback(callback ToolParamsUpdateCallback) *ImprovedToolDataAggregator {
	logger.Debug("创建带回调的聚合器",
		logger.Bool("callback_is_nil", callback == nil))

	return &ImprovedToolDataAggregator{
		activeTools:    make(map[string]*ImprovedToolBuffer),
		updateCallback: callback,
	}
}

// SetUpdateCallback 设置参数更新回调函数
func (tda *ImprovedToolDataAggregator) SetUpdateCallback(callback ToolParamsUpdateCallback) {
	tda.mu.Lock()
	defer tda.mu.Unlock()
	tda.updateCallback = callback
}

// ProcessToolData 处理工具调用数据片段（改进版）
func (tda *ImprovedToolDataAggregator) ProcessToolData(toolUseId, name, input string, stop bool, fragmentIndex int) (complete bool, fullInput string) {
	tda.mu.Lock()
	defer tda.mu.Unlock()

	// 验证tool_use_id的完整性
	originalToolUseId := toolUseId
	if !tda.isValidToolUseId(toolUseId) {
		logger.Warn("检测到无效的tool_use_id，尝试修复",
			logger.String("original_toolUseId", toolUseId))

		// 尝试从活动工具中找到匹配的前缀
		fixedToolUseId := tda.findMatchingToolUseId(toolUseId)
		if fixedToolUseId != "" {
			toolUseId = fixedToolUseId
			logger.Info("成功修复tool_use_id",
				logger.String("original", originalToolUseId),
				logger.String("fixed", toolUseId))
		} else {
			logger.Error("无法修复tool_use_id，跳过此片段",
				logger.String("invalid_toolUseId", originalToolUseId))
			return false, ""
		}
	}

	// 获取或创建缓冲区
	buffer, exists := tda.activeTools[toolUseId]
	if !exists {
		logger.Debug("创建新的工具调用缓冲区",
			logger.String("toolUseId", toolUseId),
			logger.String("name", name),
			logger.String("initialInput", input),
			logger.Int("fragmentIndex", fragmentIndex),
			logger.Bool("stop", stop))

		buffer = &ImprovedToolBuffer{
			toolUseId:     toolUseId,
			name:          name,
			fragments:     make([]string, 0),
			fragmentOrder: make(map[int]string),
			lastUpdate:    time.Now(),
			startIndex:    fragmentIndex,
		}
		tda.activeTools[toolUseId] = buffer
	}

	// 更新名称
	if name != "" && buffer.name == "" {
		buffer.name = name
	}

	// 保存片段
	buffer.fragments = append(buffer.fragments, input)
	if fragmentIndex >= 0 {
		buffer.fragmentOrder[fragmentIndex] = input
	}
	buffer.lastUpdate = time.Now()

	logger.Debug("添加工具调用数据片段",
		logger.String("toolUseId", toolUseId),
		logger.String("fragment", input),
		logger.Int("fragmentIndex", fragmentIndex),
		logger.Int("totalFragments", len(buffer.fragments)),
		logger.Int("orderMapSize", len(buffer.fragmentOrder)))

	// 如果收到停止信号，开始聚合
	if stop {
		buffer.isComplete = true

		// 使用改进的聚合逻辑
		fullInput = tda.aggregateFragments(buffer)

		// 验证并修复JSON
		fullInput = tda.validateAndRepairJSON(fullInput, buffer.name)

		// 清理缓冲区
		delete(tda.activeTools, toolUseId)

		logger.Debug("工具调用数据聚合完成",
			logger.String("toolUseId", toolUseId),
			logger.String("name", name),
			logger.String("fullInput", func() string {
				if len(fullInput) > 100 {
					return fullInput[:100] + "..."
				}
				return fullInput
			}()),
			logger.Int("totalFragments", len(buffer.fragments)))

		// 通知聚合完成，用于后续参数更新
		tda.onAggregationComplete(toolUseId, fullInput)

		return true, fullInput
	}

	return false, ""
}

// aggregateFragments 聚合所有片段
func (tda *ImprovedToolDataAggregator) aggregateFragments(buffer *ImprovedToolBuffer) string {
	// 获取连接后的原始值
	var rawValue string

	// 如果有顺序映射，优先使用
	if len(buffer.fragmentOrder) > 0 {
		// 找到最小和最大索引
		minIdx, maxIdx := -1, -1
		for idx := range buffer.fragmentOrder {
			if minIdx == -1 || idx < minIdx {
				minIdx = idx
			}
			if maxIdx == -1 || idx > maxIdx {
				maxIdx = idx
			}
		}

		// 按顺序拼接
		var orderedFragments []string
		for i := minIdx; i <= maxIdx; i++ {
			if fragment, exists := buffer.fragmentOrder[i]; exists {
				orderedFragments = append(orderedFragments, fragment)
			}
		}

		if len(orderedFragments) > 0 {
			rawValue = strings.Join(orderedFragments, "")
			logger.Debug("使用有序片段聚合",
				logger.Int("orderedCount", len(orderedFragments)),
				logger.String("rawValue", func() string {
					if len(rawValue) > 50 {
						return rawValue[:50] + "..."
					}
					return rawValue
				}()))
		}
	} else {
		// 否则使用原始顺序
		rawValue = strings.Join(buffer.fragments, "")
		logger.Debug("使用原始顺序聚合",
			logger.Int("fragmentCount", len(buffer.fragments)),
			logger.String("rawValue", func() string {
				if len(rawValue) > 50 {
					return rawValue[:50] + "..."
				}
				return rawValue
			}()))
	}

	// 重构完整的JSON对象
	return tda.reconstructToolJSON(rawValue, buffer.name)
}

// reconstructToolJSON 将原始值重构为完整的工具参数JSON
func (tda *ImprovedToolDataAggregator) reconstructToolJSON(rawValue, toolName string) string {
	// 清理原始值
	rawValue = strings.TrimSpace(rawValue)

	// 移除可能的尾部JSON字符（如 "} 或 "}）
	rawValue = strings.TrimSuffix(rawValue, "\"}")
	rawValue = strings.TrimSuffix(rawValue, "}")
	rawValue = strings.TrimSuffix(rawValue, "\"")

	// 移除可能的开头引号
	rawValue = strings.TrimPrefix(rawValue, "\"")

	logger.Debug("重构工具JSON",
		logger.String("toolName", toolName),
		logger.String("cleanedValue", rawValue))

	// 根据工具类型构建适当的JSON
	switch strings.ToLower(toolName) {
	case "ls":
		return fmt.Sprintf(`{"path": "%s"}`, rawValue)
	case "read":
		return fmt.Sprintf(`{"file_path": "%s"}`, rawValue)
	case "write":
		// 对于Write工具，需要解析更复杂的结构
		return tda.reconstructWriteJSON(rawValue)
	case "bash":
		return fmt.Sprintf(`{"command": "%s"}`, rawValue)
	case "edit":
		// 对于Edit工具，需要解析更复杂的结构
		return tda.reconstructEditJSON(rawValue)
	case "glob":
		return fmt.Sprintf(`{"pattern": "%s"}`, rawValue)
	case "grep":
		return fmt.Sprintf(`{"pattern": "%s"}`, rawValue)
	default:
		// 对于未知工具，尝试智能猜测参数名
		if strings.Contains(rawValue, "/") && !strings.Contains(rawValue, " ") {
			// 看起来像路径
			return fmt.Sprintf(`{"path": "%s"}`, rawValue)
		} else {
			// 通用参数
			return fmt.Sprintf(`{"input": "%s"}`, rawValue)
		}
	}
}

// reconstructWriteJSON 重构Write工具的JSON
func (tda *ImprovedToolDataAggregator) reconstructWriteJSON(rawValue string) string {
	// Write工具通常有file_path和content两个参数
	// 如果rawValue看起来像路径，就作为file_path
	if strings.Contains(rawValue, "/") && !strings.Contains(rawValue, "\n") {
		return fmt.Sprintf(`{"file_path": "%s", "content": ""}`, rawValue)
	}
	// 否则可能是content
	return fmt.Sprintf(`{"file_path": "", "content": "%s"}`, rawValue)
}

// reconstructEditJSON 重构Edit工具的JSON
func (tda *ImprovedToolDataAggregator) reconstructEditJSON(rawValue string) string {
	// Edit工具通常有file_path, old_string, new_string参数
	// 如果rawValue看起来像路径，就作为file_path
	if strings.Contains(rawValue, "/") && !strings.Contains(rawValue, "\n") {
		return fmt.Sprintf(`{"file_path": "%s", "old_string": "", "new_string": ""}`, rawValue)
	}
	// 否则可能是old_string
	return fmt.Sprintf(`{"file_path": "", "old_string": "%s", "new_string": ""}`, rawValue)
}

// validateAndRepairJSON 验证并修复JSON
func (tda *ImprovedToolDataAggregator) validateAndRepairJSON(input string, toolName string) string {
	input = strings.TrimSpace(input)

	// 首先尝试解析
	var temp interface{}
	if err := json.Unmarshal([]byte(input), &temp); err == nil {
		// JSON已经有效，进一步验证工具特定参数
		if parsedMap, ok := temp.(map[string]interface{}); ok {
			if isValidToolParams(toolName, parsedMap) {
				logger.Debug("JSON验证通过，无需修复",
					logger.String("toolName", toolName),
					logger.String("input", func() string {
						if len(input) > 100 {
							return input[:100] + "..."
						}
						return input
					}()))
				return input
			}
		}
		return input
	}

	logger.Debug("JSON验证失败，开始修复",
		logger.String("original", func() string {
			if len(input) > 100 {
				return input[:100] + "..."
			}
			return input
		}()),
		logger.String("toolName", toolName))

	// 如果JSON无效，但我们的重构逻辑已经处理过，说明可能是更深层的问题
	// 尝试基本的JSON修复
	fixed := tda.basicJSONRepair(input, toolName)

	// 再次验证
	if err := json.Unmarshal([]byte(fixed), &temp); err != nil {
		logger.Warn("JSON修复失败，返回基础JSON",
			logger.String("toolName", toolName),
			logger.String("fixed", fixed),
			logger.Err(err))
		// 返回工具特定的基础JSON结构
		return tda.generateBasicJSON(toolName)
	}

	logger.Debug("JSON修复成功",
		logger.String("toolName", toolName),
		logger.String("original", func() string {
			if len(input) > 50 {
				return input[:50] + "..."
			}
			return input
		}()),
		logger.String("fixed", func() string {
			if len(fixed) > 50 {
				return fixed[:50] + "..."
			}
			return fixed
		}()))

	return fixed
}

// basicJSONRepair 基础JSON修复
func (tda *ImprovedToolDataAggregator) basicJSONRepair(input string, toolName string) string {
	// 基础清理
	input = strings.TrimSpace(input)

	// 确保有开始和结束大括号
	if !strings.HasPrefix(input, "{") {
		input = "{" + input
	}
	if !strings.HasSuffix(input, "}") {
		input = input + "}"
	}

	// 修复常见的转义问题
	input = strings.ReplaceAll(input, "\\\\n", "\\n")
	input = strings.ReplaceAll(input, "\\\\\"", "\\\"")
	input = strings.ReplaceAll(input, "\\\\t", "\\t")

	return input
}

// onAggregationComplete 聚合完成时调用回调函数
func (tda *ImprovedToolDataAggregator) onAggregationComplete(toolUseId string, fullInput string) {
	logger.Debug("onAggregationComplete 开始执行",
		logger.String("toolUseId", toolUseId))

	// 获取回调函数
	if tda.updateCallback != nil {
		logger.Debug("找到非空回调函数，开始执行")

		// 直接调用回调函数
		tda.updateCallback(toolUseId, fullInput)

		logger.Debug("回调函数执行完成",
			logger.String("toolUseId", toolUseId))
	} else {
		logger.Warn("回调函数为空，跳过参数更新",
			logger.String("toolUseId", toolUseId))
	}

	logger.Debug("onAggregationComplete 执行完成",
		logger.String("toolUseId", toolUseId))
}

// isValidToolParams 验证工具特定参数的有效性
func isValidToolParams(toolName string, params map[string]interface{}) bool {
	toolNameLower := strings.ToLower(toolName)
	switch toolNameLower {
	case "write":
		fp, hasFilePath := params["file_path"].(string)
		content, hasContent := params["content"].(string)
		return hasFilePath && hasContent && strings.TrimSpace(fp) != "" && strings.TrimSpace(content) != ""
	case "bash":
		cmd, hasCmd := params["command"].(string)
		return hasCmd && strings.TrimSpace(cmd) != ""
	case "read":
		fp, hasFilePath := params["file_path"].(string)
		return hasFilePath && strings.TrimSpace(fp) != ""
	case "edit":
		fp, hasFilePath := params["file_path"].(string)
		os, hasOldString := params["old_string"].(string)
		_, hasNewString := params["new_string"].(string)
		return hasFilePath && hasOldString && hasNewString &&
			strings.TrimSpace(fp) != "" && strings.TrimSpace(os) != ""
	case "ls":
		// LS工具接受path参数
		if path, hasPath := params["path"].(string); hasPath {
			return strings.TrimSpace(path) != ""
		}
		// 也可能没有参数（列出当前目录）
		return len(params) == 0
	case "glob":
		pattern, hasPattern := params["pattern"].(string)
		return hasPattern && strings.TrimSpace(pattern) != ""
	case "grep":
		pattern, hasPattern := params["pattern"].(string)
		return hasPattern && strings.TrimSpace(pattern) != ""
	default:
		// 对于未知工具，只要是有效JSON就认为有效
		return len(params) >= 0
	}
}

// generateBasicJSON 生成工具特定的基础JSON结构
func (tda *ImprovedToolDataAggregator) generateBasicJSON(toolName string) string {
	toolNameLower := strings.ToLower(toolName)
	switch toolNameLower {
	case "write":
		return `{"file_path": "", "content": ""}`
	case "bash":
		return `{"command": ""}`
	case "read":
		return `{"file_path": ""}`
	case "edit":
		return `{"file_path": "", "old_string": "", "new_string": ""}`
	case "ls":
		return `{"path": "."}`
	case "glob":
		return `{"pattern": ""}`
	case "grep":
		return `{"pattern": ""}`
	default:
		return "{}"
	}
}

// smartJSONRepair 智能JSON修复
func (tda *ImprovedToolDataAggregator) smartJSONRepair(input string, toolName string) string {
	// 检查是否缺少开头
	if !strings.HasPrefix(input, "{") {
		// 分析内容特征来推断缺失的部分
		if strings.HasPrefix(input, "th\":") {
			// 明确是 file_path 被截断
			input = "{\"file_pa" + input
		} else if strings.HasPrefix(input, "ath\":") {
			// file_path 的 path 部分
			input = "{\"file_p" + input
		} else if strings.HasPrefix(input, "and\":") {
			// command 被截断
			input = "{\"comm" + input
		} else if strings.HasPrefix(input, "mand\":") {
			// command 的 mand 部分
			input = "{\"com" + input
		} else if strings.HasPrefix(input, "tent\":") {
			// content 被截断
			input = "{\"con" + input
		} else if strings.HasPrefix(input, "ontent\":") {
			// content 的 ontent 部分
			input = "{\"c" + input
		} else if strings.Contains(input, "\":") {
			// 通用情况：找到第一个引号
			firstQuote := strings.Index(input, "\"")
			if firstQuote >= 0 {
				// 基于工具名称推断键名
				switch toolName {
				case "Write":
					if strings.Contains(input, "/Users/") || strings.Contains(input, "/home/") {
						input = "{\"file_path" + input[firstQuote:]
					} else {
						input = "{\"content" + input[firstQuote:]
					}
				case "Bash":
					input = "{\"command" + input[firstQuote:]
				default:
					input = "{" + input
				}
			} else {
				input = "{\"" + input
			}
		} else {
			input = "{" + input
		}
	}

	// 确保结尾
	if !strings.HasSuffix(input, "}") {
		input = input + "}"
	}

	// 修复转义
	input = strings.ReplaceAll(input, "\\\\n", "\\n")
	input = strings.ReplaceAll(input, "\\\\\"", "\\\"")
	input = strings.ReplaceAll(input, "\\\\t", "\\t")

	return input
}

// deepJSONRepair 深度JSON修复
func (tda *ImprovedToolDataAggregator) deepJSONRepair(input string, toolName string) string {
	logger.Debug("执行深度JSON修复",
		logger.String("toolName", toolName),
		logger.String("input", func() string {
			if len(input) > 100 {
				return input[:100] + "..."
			}
			return input
		}()))

	// 根据工具类型构建预期的JSON结构
	switch toolName {
	case "Write":
		// 尝试提取file_path和content
		var filePath, content string

		// 查找路径模式
		if idx := strings.Index(input, "/"); idx >= 0 {
			// 找到路径开始
			endIdx := strings.Index(input[idx:], "\"")
			if endIdx > 0 {
				filePath = input[idx : idx+endIdx]
			}
		}

		// 查找content
		if contentIdx := strings.Index(input, "content"); contentIdx >= 0 {
			// 找到content后的值
			startIdx := strings.Index(input[contentIdx:], "\"")
			if startIdx > 0 {
				startIdx += contentIdx + 1
				endIdx := strings.Index(input[startIdx:], "\"}")
				if endIdx > 0 {
					content = input[startIdx : startIdx+endIdx]
				}
			}
		}

		// 重建JSON
		if filePath != "" || content != "" {
			result := "{"
			parts := []string{}
			if filePath != "" {
				parts = append(parts, fmt.Sprintf(`"file_path": "%s"`, filePath))
			}
			if content != "" {
				parts = append(parts, fmt.Sprintf(`"content": "%s"`, content))
			}
			result += strings.Join(parts, ", ") + "}"
			return result
		}

	case "Bash":
		// 尝试提取command
		var command string

		// 查找常见的命令模式
		patterns := []string{"mkdir", "echo", "cat", "ls", "cd", "python", "npm", "go"}
		for _, pattern := range patterns {
			if idx := strings.Index(input, pattern); idx >= 0 {
				// 找到命令开始
				endIdx := strings.LastIndex(input, "\"")
				if endIdx > idx {
					command = input[idx:endIdx]
					break
				}
			}
		}

		if command != "" {
			return fmt.Sprintf(`{"command": "%s"}`, command)
		}
	}

	// 如果特定修复失败，尝试通用修复
	// 提取所有可能的键值对
	pairs := make(map[string]string)

	// 简单的键值对提取
	parts := strings.Split(input, "\",")
	for _, part := range parts {
		if kv := strings.SplitN(part, "\":", 2); len(kv) == 2 {
			key := strings.Trim(kv[0], " \"{}")
			value := strings.Trim(kv[1], " \"}")
			if key != "" && value != "" {
				pairs[key] = value
			}
		}
	}

	// 重建JSON
	if len(pairs) > 0 {
		jsonParts := make([]string, 0, len(pairs))
		for k, v := range pairs {
			jsonParts = append(jsonParts, fmt.Sprintf(`"%s": "%s"`, k, v))
		}
		return "{" + strings.Join(jsonParts, ", ") + "}"
	}

	// 最后的尝试：返回空JSON
	logger.Warn("深度修复失败，返回空JSON",
		logger.String("toolName", toolName))
	return "{}"
}

// CleanupExpiredBuffers 清理过期的缓冲区
func (tda *ImprovedToolDataAggregator) CleanupExpiredBuffers(timeout time.Duration) {
	tda.mu.Lock()
	defer tda.mu.Unlock()

	now := time.Now()
	for toolUseId, buffer := range tda.activeTools {
		if now.Sub(buffer.lastUpdate) > timeout {
			logger.Warn("清理过期的工具调用缓冲区",
				logger.String("toolUseId", toolUseId),
				logger.Duration("age", now.Sub(buffer.lastUpdate)),
				logger.Int("fragments", len(buffer.fragments)))
			delete(tda.activeTools, toolUseId)
		}
	}
}

// isValidToolUseId 验证tool_use_id的完整性
func (tda *ImprovedToolDataAggregator) isValidToolUseId(toolUseId string) bool {
	// tool_use_id应该符合特定格式：tooluse_[base64字符]
	if !strings.HasPrefix(toolUseId, "tooluse_") {
		logger.Debug("tool_use_id缺少正确前缀",
			logger.String("toolUseId", toolUseId))
		return false
	}

	// 检查长度（正常的tool_use_id长度应该大于15字符，放宽限制用于测试）
	if len(toolUseId) < 15 {
		logger.Debug("tool_use_id长度过短",
			logger.String("toolUseId", toolUseId),
			logger.Int("length", len(toolUseId)))
		return false
	}

	// 检查后缀部分是否包含有效字符
	suffix := toolUseId[8:] // 去掉 "tooluse_" 前缀
	if len(suffix) < 10 {
		logger.Debug("tool_use_id后缀太短",
			logger.String("suffix", suffix))
		return false
	}

	logger.Debug("tool_use_id验证通过",
		logger.String("toolUseId", toolUseId))
	return true
}

// findMatchingToolUseId 在活动工具中查找匹配的完整tool_use_id
func (tda *ImprovedToolDataAggregator) findMatchingToolUseId(partialId string) string {
	// 在活动工具中查找匹配的完整tool_use_id
	bestMatch := ""
	maxMatchLength := 0

	for fullId := range tda.activeTools {
		// 检查前缀匹配
		if strings.HasPrefix(fullId, partialId) && len(partialId) > maxMatchLength {
			bestMatch = fullId
			maxMatchLength = len(partialId)
		}
		// 检查后缀匹配（部分ID可能是完整ID的后半部分）
		if strings.HasPrefix(partialId, fullId) && len(fullId) > maxMatchLength {
			bestMatch = fullId
			maxMatchLength = len(fullId)
		}
		// 检查中间匹配（寻找最大公共子序列）
		if strings.Contains(fullId, partialId) && len(partialId) > maxMatchLength {
			bestMatch = fullId
			maxMatchLength = len(partialId)
		}
	}

	if bestMatch != "" {
		logger.Info("找到匹配的tool_use_id",
			logger.String("partial", partialId),
			logger.String("full", bestMatch),
			logger.Int("match_length", maxMatchLength))
		return bestMatch
	}

	// 如果没有找到直接匹配，尝试模糊匹配
	// 基于相似度的匹配策略
	for fullId := range tda.activeTools {
		similarity := tda.calculateSimilarity(partialId, fullId)
		if similarity > 0.7 { // 70%相似度阈值
			logger.Info("基于相似度找到tool_use_id匹配",
				logger.String("partial", partialId),
				logger.String("full", fullId),
				logger.String("similarity", fmt.Sprintf("%.2f", similarity)))
			return fullId
		}
	}

	logger.Warn("未找到匹配的tool_use_id",
		logger.String("partial", partialId),
		logger.Int("active_tools_count", len(tda.activeTools)))
	return ""
}

// calculateSimilarity 计算两个字符串的相似度
func (tda *ImprovedToolDataAggregator) calculateSimilarity(s1, s2 string) float64 {
	if len(s1) == 0 || len(s2) == 0 {
		return 0.0
	}

	// 使用最长公共子序列算法计算相似度
	longer, shorter := s1, s2
	if len(s1) < len(s2) {
		longer, shorter = s2, s1
	}

	// 计算公共字符数
	commonChars := 0
	for i := 0; i < len(shorter); i++ {
		if i < len(longer) && shorter[i] == longer[i] {
			commonChars++
		}
	}

	// 计算相似度百分比
	similarity := float64(commonChars) / float64(len(longer))

	logger.Debug("计算字符串相似度",
		logger.String("s1", s1),
		logger.String("s2", s2),
		logger.Int("common_chars", commonChars),
		logger.String("similarity", fmt.Sprintf("%.2f", similarity)))

	return similarity
}
