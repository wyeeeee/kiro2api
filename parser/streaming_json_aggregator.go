package parser

import (
	"encoding/json"
	"io"
	"kiro2api/logger"
	"strings"
	"sync"
	"time"
)

// StreamingJSONAggregator 流式JSON聚合器
type StreamingJSONAggregator struct {
	activeStreamers map[string]*JSONStreamer
	mu              sync.RWMutex
	updateCallback  ToolParamsUpdateCallback
}

// JSONStreamer 单个工具调用的JSON流式解析器
type JSONStreamer struct {
	toolUseId  string
	toolName   string
	buffer     *strings.Builder
	decoder    *json.Decoder
	reader     *strings.Reader
	state      JSONParseState
	lastUpdate time.Time
	isComplete bool
	result     map[string]interface{}
}

// JSONParseState JSON解析状态
type JSONParseState struct {
	expectingKey bool
	hasValidJSON bool
}

// NewStreamingJSONAggregator 创建流式JSON聚合器
func NewStreamingJSONAggregator() *StreamingJSONAggregator {
	return &StreamingJSONAggregator{
		activeStreamers: make(map[string]*JSONStreamer),
	}
}

// NewStreamingJSONAggregatorWithCallback 创建带回调的流式JSON聚合器
func NewStreamingJSONAggregatorWithCallback(callback ToolParamsUpdateCallback) *StreamingJSONAggregator {
	return &StreamingJSONAggregator{
		activeStreamers: make(map[string]*JSONStreamer),
		updateCallback:  callback,
	}
}

// SetUpdateCallback 设置更新回调
func (sja *StreamingJSONAggregator) SetUpdateCallback(callback ToolParamsUpdateCallback) {
	sja.mu.Lock()
	defer sja.mu.Unlock()
	sja.updateCallback = callback
}

// ProcessToolData 处理工具调用数据片段（流式版本）
func (sja *StreamingJSONAggregator) ProcessToolData(toolUseId, name, input string, stop bool, fragmentIndex int) (complete bool, fullInput string) {
	sja.mu.Lock()
	defer sja.mu.Unlock()

	// 获取或创建流式解析器
	streamer, exists := sja.activeStreamers[toolUseId]
	if !exists {
		streamer = sja.createJSONStreamer(toolUseId, name)
		sja.activeStreamers[toolUseId] = streamer

		logger.Debug("创建JSON流式解析器",
			logger.String("toolUseId", toolUseId),
			logger.String("toolName", name))
	}

	// 处理输入片段
	if input != "" {
		if err := streamer.appendFragment(input); err != nil {
			logger.Warn("追加JSON片段失败",
				logger.String("toolUseId", toolUseId),
				logger.String("fragment", input),
				logger.Err(err))
		}
	}

	// 尝试解析当前缓冲区
	parseResult := streamer.tryParse()

	logger.Debug("流式JSON解析进度",
		logger.String("toolUseId", toolUseId),
		logger.String("fragment", input),
		logger.Bool("hasValidJSON", streamer.state.hasValidJSON),
		logger.Bool("stop", stop),
		logger.String("parseStatus", parseResult))

	// 如果收到停止信号
	if stop {
		streamer.isComplete = true

		// 最终尝试解析或生成基础JSON
		if !streamer.state.hasValidJSON {
			logger.Debug("停止时JSON未完整，尝试智能补全",
				logger.String("toolUseId", toolUseId),
				logger.String("buffer", streamer.buffer.String()))

			result := sja.intelligentComplete(streamer)
			if result != nil {
				streamer.result = result
				streamer.state.hasValidJSON = true
			}
		}

		if streamer.state.hasValidJSON && streamer.result != nil {
			// 序列化结果
			if jsonBytes, err := json.Marshal(streamer.result); err == nil {
				fullInput = string(jsonBytes)
			} else {
				fullInput = sja.generateFallbackJSON(streamer.toolName)
			}
		} else {
			fullInput = sja.generateFallbackJSON(streamer.toolName)
		}

		// 清理完成的流式解析器
		delete(sja.activeStreamers, toolUseId)

		// 触发回调
		sja.onAggregationComplete(toolUseId, fullInput)

		logger.Debug("流式JSON聚合完成",
			logger.String("toolUseId", toolUseId),
			logger.String("toolName", name),
			logger.String("result", fullInput))

		return true, fullInput
	}

	return false, ""
}

// createJSONStreamer 创建JSON流式解析器
func (sja *StreamingJSONAggregator) createJSONStreamer(toolUseId, toolName string) *JSONStreamer {
	buffer := &strings.Builder{}
	reader := strings.NewReader("")

	return &JSONStreamer{
		toolUseId:  toolUseId,
		toolName:   toolName,
		buffer:     buffer,
		reader:     reader,
		decoder:    json.NewDecoder(reader),
		lastUpdate: time.Now(),
		state: JSONParseState{
			expectingKey: true,
		},
		result: make(map[string]interface{}),
	}
}

// appendFragment 追加JSON片段
func (js *JSONStreamer) appendFragment(fragment string) error {
	js.buffer.WriteString(fragment)
	js.lastUpdate = time.Now()

	// 更新reader和decoder
	js.reader = strings.NewReader(js.buffer.String())
	js.decoder = json.NewDecoder(js.reader)

	return nil
}

// tryParse 尝试解析当前缓冲区
func (js *JSONStreamer) tryParse() string {
	content := js.buffer.String()
	if content == "" {
		return "empty"
	}

	// 尝试完整JSON解析
	var result map[string]interface{}
	if err := json.Unmarshal([]byte(content), &result); err == nil {
		js.result = result
		js.state.hasValidJSON = true
		return "complete"
	}

	// 尝试部分解析 - 检查是否为有效的JSON开始
	if js.isValidJSONStart(content) {
		return "partial"
	}

	// 检查是否只是值片段（无键）
	if js.looksLikeValueFragment(content) {
		return "value_fragment"
	}

	return "invalid"
}

// isValidJSONStart 检查是否为有效的JSON开始
func (js *JSONStreamer) isValidJSONStart(content string) bool {
	content = strings.TrimSpace(content)
	if !strings.HasPrefix(content, "{") {
		return false
	}

	// 使用token解析器检查结构
	decoder := json.NewDecoder(strings.NewReader(content))
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			if err == io.EOF {
				break
			}
			return false
		}
		// 如果能解析出至少一个token，说明结构开始是有效的
		_ = token
	}
	return true
}

// looksLikeValueFragment 检查是否看起来像值片段
func (js *JSONStreamer) looksLikeValueFragment(content string) bool {
	content = strings.TrimSpace(content)

	// 检查是否看起来像路径
	if strings.Contains(content, "/") && !strings.Contains(content, " ") {
		return true
	}

	// 检查是否看起来像命令
	if strings.Contains(content, " ") && len(content) > 3 {
		return true
	}

	return false
}

// intelligentComplete 智能补全JSON
func (sja *StreamingJSONAggregator) intelligentComplete(streamer *JSONStreamer) map[string]interface{} {
	content := strings.TrimSpace(streamer.buffer.String())

	logger.Debug("智能补全JSON",
		logger.String("toolName", streamer.toolName),
		logger.String("content", content))

	// 如果内容看起来像值片段，根据工具类型构建JSON
	if streamer.looksLikeValueFragment(content) {
		return sja.buildJSONFromValue(streamer.toolName, content)
	}

	// 如果是不完整的JSON，尝试补全
	if strings.HasPrefix(content, "{") {
		return sja.completePartialJSON(content, streamer.toolName)
	}

	return nil
}

// buildJSONFromValue 从值构建JSON
func (sja *StreamingJSONAggregator) buildJSONFromValue(toolName, value string) map[string]interface{} {
	// 清理值
	value = strings.Trim(value, "\"")

	switch strings.ToLower(toolName) {
	case "ls":
		return map[string]interface{}{"path": value}
	case "read":
		return map[string]interface{}{"file_path": value}
	case "write":
		if strings.Contains(value, "/") {
			return map[string]interface{}{"file_path": value, "content": ""}
		}
		return map[string]interface{}{"file_path": "", "content": value}
	case "bash":
		return map[string]interface{}{"command": value}
	case "edit":
		if strings.Contains(value, "/") {
			return map[string]interface{}{"file_path": value, "old_string": "", "new_string": ""}
		}
		return map[string]interface{}{"file_path": "", "old_string": value, "new_string": ""}
	case "glob":
		return map[string]interface{}{"pattern": value}
	case "grep":
		return map[string]interface{}{"pattern": value}
	default:
		if strings.Contains(value, "/") {
			return map[string]interface{}{"path": value}
		}
		return map[string]interface{}{"input": value}
	}
}

// completePartialJSON 补全不完整的JSON
func (sja *StreamingJSONAggregator) completePartialJSON(content, toolName string) map[string]interface{} {
	logger.Debug("尝试补全不完整JSON",
		logger.String("content", content),
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

	// 尝试解析修复后的JSON
	var result map[string]interface{}
	if err := json.Unmarshal([]byte(fixed), &result); err == nil {
		logger.Debug("JSON补全成功", logger.String("fixed", fixed))
		return result
	}

	logger.Debug("JSON补全失败，使用fallback",
		logger.String("fixed", fixed),
		logger.String("toolName", toolName))

	// 如果仍然解析失败，返回nil让调用者使用fallback
	return nil
}

// generateFallbackJSON 生成回退JSON
func (sja *StreamingJSONAggregator) generateFallbackJSON(toolName string) string {
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
func (sja *StreamingJSONAggregator) onAggregationComplete(toolUseId string, fullInput string) {
	if sja.updateCallback != nil {
		logger.Debug("触发流式JSON聚合回调",
			logger.String("toolUseId", toolUseId))
		sja.updateCallback(toolUseId, fullInput)
	}
}

// CleanupExpiredBuffers 清理过期的缓冲区
func (sja *StreamingJSONAggregator) CleanupExpiredBuffers(timeout time.Duration) {
	sja.mu.Lock()
	defer sja.mu.Unlock()

	now := time.Now()
	for toolUseId, streamer := range sja.activeStreamers {
		if now.Sub(streamer.lastUpdate) > timeout {
			logger.Warn("清理过期的JSON流式解析器",
				logger.String("toolUseId", toolUseId),
				logger.Duration("age", now.Sub(streamer.lastUpdate)))
			delete(sja.activeStreamers, toolUseId)
		}
	}
}
