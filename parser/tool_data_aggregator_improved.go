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
			result := strings.Join(orderedFragments, "")
			logger.Debug("使用有序片段聚合",
				logger.Int("orderedCount", len(orderedFragments)),
				logger.String("result", func() string {
					if len(result) > 50 {
						return result[:50] + "..."
					}
					return result
				}()))
			return result
		}
	}

	// 否则使用原始顺序
	result := strings.Join(buffer.fragments, "")
	logger.Debug("使用原始顺序聚合",
		logger.Int("fragmentCount", len(buffer.fragments)),
		logger.String("result", func() string {
			if len(result) > 50 {
				return result[:50] + "..."
			}
			return result
		}()))
	return result
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
				return input
			}
		}
		return input
	}

	logger.Debug("JSON无效，开始修复",
		logger.String("original", func() string {
			if len(input) > 100 {
				return input[:100] + "..."
			}
			return input
		}()),
		logger.String("toolName", toolName))

	// 智能修复
	fixed := tda.smartJSONRepair(input, toolName)

	// 再次验证
	if err := json.Unmarshal([]byte(fixed), &temp); err != nil {
		logger.Warn("首次修复失败，尝试深度修复",
			logger.String("fixed", fixed),
			logger.Err(err))

		// 深度修复
		fixed = tda.deepJSONRepair(fixed, toolName)

		// 最终验证
		if err := json.Unmarshal([]byte(fixed), &temp); err != nil {
			logger.Error("所有修复方法失败，返回基础JSON",
				logger.String("toolName", toolName),
				logger.Err(err))
			// 返回工具特定的基础JSON结构
			return tda.generateBasicJSON(toolName)
		}
	}

	return fixed
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
	default:
		// 对于未知工具，只要是有效JSON就认为有效
		return len(params) > 0
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
