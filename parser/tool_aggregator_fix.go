package parser

import (
	"encoding/json"
	"fmt"
	"kiro2api/logger"
	"strings"
	"sync"
)

// ToolCallAggregator 工具调用参数聚合器 - 改进版
type ToolCallAggregator struct {
	// 使用map存储每个工具的完整参数片段序列
	toolCallData map[string]*ToolCallBuffer
	mu           sync.RWMutex
}

// ToolCallBuffer 存储单个工具调用的所有片段
type ToolCallBuffer struct {
	ToolUseId   string
	Name        string
	Fragments   []string       // 按顺序存储所有片段
	FragmentMap map[int]string // 索引到片段的映射，处理乱序
	IsComplete  bool
	FinalJSON   string
}

// NewToolCallAggregator 创建新的工具调用聚合器
func NewToolCallAggregator() *ToolCallAggregator {
	return &ToolCallAggregator{
		toolCallData: make(map[string]*ToolCallBuffer),
	}
}

// AddFragment 添加工具调用片段
func (tca *ToolCallAggregator) AddFragment(toolUseId, name string, fragment string, index int) {
	tca.mu.Lock()
	defer tca.mu.Unlock()

	// 获取或创建工具缓存
	buffer, exists := tca.toolCallData[toolUseId]
	if !exists {
		buffer = &ToolCallBuffer{
			ToolUseId:   toolUseId,
			Name:        name,
			Fragments:   make([]string, 0),
			FragmentMap: make(map[int]string),
		}
		tca.toolCallData[toolUseId] = buffer

		logger.Debug("创建新的工具调用缓冲区",
			logger.String("toolUseId", toolUseId),
			logger.String("name", name))
	}

	// 更新名称（如果提供）
	if name != "" && buffer.Name == "" {
		buffer.Name = name
	}

	// 存储片段 - 即使是空字符串也要记录，以保持顺序
	buffer.Fragments = append(buffer.Fragments, fragment)
	if index >= 0 {
		buffer.FragmentMap[index] = fragment
	}

	logger.Debug("添加工具调用片段",
		logger.String("toolUseId", toolUseId),
		logger.String("fragment", fragment),
		logger.Int("index", index),
		logger.Int("total_fragments", len(buffer.Fragments)))
}

// GetAggregatedJSON 获取聚合后的JSON
func (tca *ToolCallAggregator) GetAggregatedJSON(toolUseId string) (string, error) {
	tca.mu.RLock()
	defer tca.mu.RUnlock()

	buffer, exists := tca.toolCallData[toolUseId]
	if !exists {
		return "", fmt.Errorf("工具调用 %s 不存在", toolUseId)
	}

	// 如果已经有最终JSON，直接返回
	if buffer.IsComplete && buffer.FinalJSON != "" {
		return buffer.FinalJSON, nil
	}

	// 聚合所有片段
	aggregated := strings.Join(buffer.Fragments, "")

	logger.Debug("原始聚合结果",
		logger.String("toolUseId", toolUseId),
		logger.String("raw", aggregated),
		logger.Int("fragments", len(buffer.Fragments)))

	// 尝试修复常见的JSON问题
	fixed := tca.repairJSON(aggregated, buffer.Name)

	// 验证JSON
	var testObj map[string]interface{}
	if err := json.Unmarshal([]byte(fixed), &testObj); err != nil {
		logger.Warn("聚合后的JSON无效，尝试进一步修复",
			logger.String("aggregated", aggregated),
			logger.String("fixed", fixed),
			logger.Err(err))

		// 尝试更激进的修复
		fixed = tca.aggressiveRepairJSON(aggregated, buffer.Name)
	}

	return fixed, nil
}

// repairJSON 修复常见的JSON格式问题
func (tca *ToolCallAggregator) repairJSON(jsonStr string, toolName string) string {
	// 移除前后空白
	jsonStr = strings.TrimSpace(jsonStr)

	// 如果不是以 { 开头，尝试添加
	if !strings.HasPrefix(jsonStr, "{") {
		// 检查是否是截断的键值对
		if strings.Contains(jsonStr, "\":") {
			// 根据工具名称和内容推断缺失的键名
			if toolName == "Write" && strings.HasPrefix(jsonStr, "th\":") {
				// Write工具的file_path参数被截断
				jsonStr = "{\"file_pa" + jsonStr
			} else if toolName == "Bash" && strings.HasPrefix(jsonStr, "and\":") {
				// Bash工具的command参数被截断
				jsonStr = "{\"comm" + jsonStr
			} else {
				// 通用修复：查找第一个引号之前的内容
				firstQuoteIdx := strings.Index(jsonStr, "\"")
				if firstQuoteIdx > 0 {
					// 尝试推断缺失的部分
					prefix := jsonStr[:firstQuoteIdx]
					switch prefix {
					case "th", "ath":
						jsonStr = "{\"file_pa" + jsonStr
					case "and", "mand":
						jsonStr = "{\"comm" + jsonStr
					case "tent", "ontent":
						jsonStr = "{\"con" + jsonStr
					case "cription":
						jsonStr = "{\"des" + jsonStr
					default:
						jsonStr = "{\"" + jsonStr
					}
				} else {
					jsonStr = "{\"" + jsonStr
				}
			}
		} else {
			jsonStr = "{" + jsonStr
		}
	}

	// 如果不是以 } 结尾，添加
	if !strings.HasSuffix(jsonStr, "}") {
		jsonStr = jsonStr + "}"
	}

	// 修复转义问题
	jsonStr = strings.ReplaceAll(jsonStr, "\\\\n", "\\n")
	jsonStr = strings.ReplaceAll(jsonStr, "\\\\\"", "\\\"")

	return jsonStr
}

// aggressiveRepairJSON 更激进的JSON修复
func (tca *ToolCallAggregator) aggressiveRepairJSON(jsonStr string, toolName string) string {
	// 基本修复
	jsonStr = tca.repairJSON(jsonStr, toolName)

	// 针对特定工具的修复策略
	switch toolName {
	case "Write":
		// 确保Write工具有必需的参数
		if !strings.Contains(jsonStr, "file_path") && strings.Contains(jsonStr, "content") {
			// 可能file_path被截断了，尝试重建
			contentIdx := strings.Index(jsonStr, "\"content\"")
			if contentIdx > 0 {
				// 提取content之前的部分，尝试修复file_path
				beforeContent := jsonStr[:contentIdx]
				afterContent := jsonStr[contentIdx:]

				// 查找可能的路径片段
				if pathMatch := strings.LastIndex(beforeContent, "\": \""); pathMatch > 0 {
					// 重建JSON
					pathStart := strings.LastIndex(beforeContent[:pathMatch], "\"")
					if pathStart >= 0 {
						pathFragment := beforeContent[pathStart+1 : pathMatch]
						// 补全可能缺失的file_path键名
						if !strings.Contains(pathFragment, "file_path") {
							beforeContent = "{\"file_path" + beforeContent[pathStart:]
						}
					}
				}
				jsonStr = beforeContent + afterContent
			}
		}
	case "Bash":
		// 确保Bash工具有command参数
		if !strings.Contains(jsonStr, "command") && !strings.Contains(jsonStr, "description") {
			// 尝试添加缺失的command键名
			if strings.Contains(jsonStr, "mkdir") || strings.Contains(jsonStr, "echo") {
				jsonStr = strings.Replace(jsonStr, "{\"", "{\"command\": \"", 1)
			}
		}
	}

	// 最后验证并尝试修复
	var testObj map[string]interface{}
	if err := json.Unmarshal([]byte(jsonStr), &testObj); err != nil {
		// 如果还是无效，尝试提取有效的键值对并重建
		validPairs := make(map[string]string)

		// 提取所有可能的键值对
		pairs := strings.Split(jsonStr, ",")
		for _, pair := range pairs {
			if kv := strings.SplitN(pair, "\":", 2); len(kv) == 2 {
				key := strings.Trim(kv[0], " \"{}")
				value := strings.Trim(kv[1], " \"}")
				if key != "" && value != "" {
					validPairs[key] = value
				}
			}
		}

		// 重建JSON
		if len(validPairs) > 0 {
			parts := make([]string, 0, len(validPairs))
			for k, v := range validPairs {
				parts = append(parts, fmt.Sprintf("\"%s\": \"%s\"", k, v))
			}
			jsonStr = "{" + strings.Join(parts, ", ") + "}"
		}
	}

	return jsonStr
}

// MarkComplete 标记工具调用为完成状态
func (tca *ToolCallAggregator) MarkComplete(toolUseId string) {
	tca.mu.Lock()
	defer tca.mu.Unlock()

	if buffer, exists := tca.toolCallData[toolUseId]; exists {
		buffer.IsComplete = true
		// 尝试生成最终JSON
		if finalJSON, err := tca.GetAggregatedJSON(toolUseId); err == nil {
			buffer.FinalJSON = finalJSON

			logger.Debug("工具调用标记为完成",
				logger.String("toolUseId", toolUseId),
				logger.String("finalJSON", finalJSON))
		}
	}
}

// Clear 清除指定工具的缓存数据
func (tca *ToolCallAggregator) Clear(toolUseId string) {
	tca.mu.Lock()
	defer tca.mu.Unlock()
	delete(tca.toolCallData, toolUseId)
}

// ClearAll 清除所有缓存数据
func (tca *ToolCallAggregator) ClearAll() {
	tca.mu.Lock()
	defer tca.mu.Unlock()
	tca.toolCallData = make(map[string]*ToolCallBuffer)
}
