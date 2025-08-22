package test

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"kiro2api/parser"
	"kiro2api/utils"
)

// HexDataAnalyzer 十六进制数据解析器
type HexDataAnalyzer struct {
	rawDataRecord *utils.RawDataRecord
	logger        utils.Logger
}

// ParsedHexData 解析后的十六进制数据
type ParsedHexData struct {
	BinaryData    []byte            `json:"binary_data"`
	OriginalSize  int               `json:"original_size"`
	MD5Hash       string            `json:"md5_hash"`
	IsValid       bool              `json:"is_valid"`
	Metadata      *utils.Metadata   `json:"metadata"`
	ParsedAt      time.Time         `json:"parsed_at"`
	ErrorMessage  string            `json:"error_message,omitempty"`
}

// NewHexDataAnalyzer 创建新的十六进制数据解析器
func NewHexDataAnalyzer(record *utils.RawDataRecord) *HexDataAnalyzer {
	return &HexDataAnalyzer{
		rawDataRecord: record,
		logger:        utils.GetLogger(),
	}
}

// LoadFromFile 从JSON文件加载原始数据记录
func LoadHexDataFromFile(filePath string) (*HexDataAnalyzer, error) {
	record, err := utils.LoadRawDataForReplay(filePath)
	if err != nil {
		return nil, fmt.Errorf("加载原始数据文件失败: %w", err)
	}
	
	return NewHexDataAnalyzer(record), nil
}

// ParseHexData 解析十六进制数据为二进制流
func (h *HexDataAnalyzer) ParseHexData() (*ParsedHexData, error) {
	h.logger.Debug("开始解析十六进制数据",
		utils.Int("hex_length", len(h.rawDataRecord.HexData)),
		utils.String("original_md5", h.rawDataRecord.MD5Hash))

	// 验证输入数据
	if h.rawDataRecord.HexData == "" {
		return &ParsedHexData{
			IsValid:      false,
			ErrorMessage: "十六进制数据为空",
			ParsedAt:     time.Now(),
		}, fmt.Errorf("十六进制数据为空")
	}

	// 解码十六进制字符串
	binaryData, err := hex.DecodeString(h.rawDataRecord.HexData)
	if err != nil {
		return &ParsedHexData{
			IsValid:      false,
			ErrorMessage: fmt.Sprintf("十六进制解码失败: %v", err),
			ParsedAt:     time.Now(),
		}, fmt.Errorf("十六进制解码失败: %w", err)
	}

	// 计算MD5校验和
	actualMD5 := fmt.Sprintf("%x", md5.Sum(binaryData))
	
	// 验证数据完整性
	isValid := actualMD5 == h.rawDataRecord.MD5Hash
	
	result := &ParsedHexData{
		BinaryData:   binaryData,
		OriginalSize: len(binaryData),
		MD5Hash:      actualMD5,
		IsValid:      isValid,
		Metadata:     &h.rawDataRecord.Metadata,
		ParsedAt:     time.Now(),
	}

	if !isValid {
		result.ErrorMessage = fmt.Sprintf("MD5校验失败: 期望=%s, 实际=%s", 
			h.rawDataRecord.MD5Hash, actualMD5)
		h.logger.Warn("MD5校验失败",
			utils.String("expected", h.rawDataRecord.MD5Hash),
			utils.String("actual", actualMD5))
	} else {
		h.logger.Debug("十六进制数据解析成功",
			utils.Int("binary_size", len(binaryData)),
			utils.String("md5_verified", actualMD5))
	}

	return result, nil
}

// ValidateIntegrity 验证数据完整性
func (h *HexDataAnalyzer) ValidateIntegrity() error {
	parsed, err := h.ParseHexData()
	if err != nil {
		return err
	}
	
	if !parsed.IsValid {
		return fmt.Errorf("数据完整性验证失败: %s", parsed.ErrorMessage)
	}
	
	return nil
}

// GetMetadata 获取元数据信息
func (h *HexDataAnalyzer) GetMetadata() *utils.Metadata {
	return &h.rawDataRecord.Metadata
}

// EventStreamParser AWS Event Stream协议解析器
type EventStreamParser struct {
	compliantParser *parser.CompliantEventStreamParser
	logger          utils.Logger
}

// ParsedEvent 解析后的事件
type ParsedEvent struct {
	EventType    string                 `json:"event_type"`
	Headers      map[string]interface{} `json:"headers"`
	Payload      interface{}            `json:"payload"`
	RawData      []byte                 `json:"raw_data"`
	ParsedAt     time.Time              `json:"parsed_at"`
	EventIndex   int                    `json:"event_index"`
	ByteOffset   int                    `json:"byte_offset"`
	ErrorMessage string                 `json:"error_message,omitempty"`
}

// ParseResult 解析结果
type ParseResult struct {
	Events         []*ParsedEvent `json:"events"`
	TotalEvents    int            `json:"total_events"`
	TotalBytes     int            `json:"total_bytes"`
	ParsedAt       time.Time      `json:"parsed_at"`
	ParseDuration  time.Duration  `json:"parse_duration"`
	Success        bool           `json:"success"`
	ErrorMessage   string         `json:"error_message,omitempty"`
}

// NewEventStreamParser 创建新的事件流解析器
func NewEventStreamParser() *EventStreamParser {
	compliantParser := parser.GlobalCompliantParserPool.Get()
	
	return &EventStreamParser{
		compliantParser: compliantParser,
		logger:          utils.GetLogger(),
	}
}

// Close 释放资源
func (p *EventStreamParser) Close() {
	if p.compliantParser != nil {
		parser.GlobalCompliantParserPool.Put(p.compliantParser)
		p.compliantParser = nil
	}
}

// ParseEventStream 解析事件流数据
func (p *EventStreamParser) ParseEventStream(data []byte) (*ParseResult, error) {
	startTime := time.Now()
	
	p.logger.Debug("开始解析AWS Event Stream",
		utils.Int("data_size", len(data)))

	result := &ParseResult{
		Events:    make([]*ParsedEvent, 0),
		ParsedAt:  startTime,
		Success:   false,
	}

	// 🔧 修复: 不使用分块，直接解析完整的二进制流
	// AWS EventStream需要按消息边界处理，不能任意分块
	
	// 使用符合规范的解析器解析整个数据流
	events, parseErr := p.compliantParser.ParseStream(data)
	if parseErr != nil {
		p.logger.Warn("解析EventStream时出现错误",
			utils.Err(parseErr),
			utils.Int("data_size", len(data)))
		// 在非严格模式下继续处理
	}

	// 处理解析到的事件
	eventIndex := 0
	for _, event := range events {
		parsedEvent, err := p.convertToStandardEvent(event, eventIndex, 0)
		if err != nil {
			p.logger.Warn("转换事件格式失败",
				utils.Err(err),
				utils.Int("event_index", eventIndex))
			
			// 创建错误事件记录
			parsedEvent = &ParsedEvent{
				EventType:    "parse_error",
				Headers:      make(map[string]interface{}),
				Payload:      nil,
				RawData:      data, // 使用原始数据而不是错误的chunk
				ParsedAt:     time.Now(),
				EventIndex:   eventIndex,
				ByteOffset:   0,
				ErrorMessage: err.Error(),
			}
		}
		
		result.Events = append(result.Events, parsedEvent)
		eventIndex++
	}

	result.TotalEvents = len(result.Events)
	result.TotalBytes = len(data)
	result.ParseDuration = time.Since(startTime)
	result.Success = len(result.Events) > 0

	p.logger.Debug("Event Stream解析完成",
		utils.Int("total_events", result.TotalEvents),
		utils.Int("total_bytes", result.TotalBytes),
		utils.Duration("parse_duration", result.ParseDuration))

	return result, nil
}

// convertToStandardEvent 转换为标准事件格式
func (p *EventStreamParser) convertToStandardEvent(event parser.SSEEvent, index, offset int) (*ParsedEvent, error) {
	parsedEvent := &ParsedEvent{
		EventIndex: index,
		ByteOffset: offset,
		ParsedAt:   time.Now(),
		Headers:    make(map[string]interface{}),
	}

	// 确定事件类型
	if event.Event != "" {
		parsedEvent.EventType = event.Event
	} else {
		parsedEvent.EventType = "unknown"
	}

	// 处理载荷数据
	if event.Data != nil {
		parsedEvent.Payload = event.Data
		
		// 尝试提取更多信息
		if dataMap, ok := event.Data.(map[string]interface{}); ok {
			// 检查是否是assistantResponseEvent
			if eventType, exists := dataMap["type"]; exists {
				if typeStr, ok := eventType.(string); ok {
					switch typeStr {
					case "content_block_start", "content_block_delta", "content_block_stop":
						parsedEvent.EventType = "assistantResponseEvent"
					case "message_start", "message_delta", "message_stop":
						parsedEvent.EventType = "assistantResponseEvent"
					}
				}
			}
			
			// 检查是否包含工具调用信息
			if contentBlock, exists := dataMap["content_block"]; exists {
				if cb, ok := contentBlock.(map[string]interface{}); ok {
					if cbType, exists := cb["type"]; exists && cbType == "tool_use" {
						parsedEvent.EventType = "toolUseEvent"
					}
				}
			}
		}
	}

	// 序列化原始数据（用于调试）
	if rawBytes, err := json.Marshal(event); err == nil {
		parsedEvent.RawData = rawBytes
	}

	return parsedEvent, nil
}

// ValidateEventFormat 验证事件格式
func (p *EventStreamParser) ValidateEventFormat(event *ParsedEvent) error {
	if event == nil {
		return fmt.Errorf("事件为空")
	}
	
	if event.EventType == "" {
		return fmt.Errorf("事件类型为空")
	}
	
	// 验证已知事件类型
	validEventTypes := map[string]bool{
		"assistantResponseEvent": true,
		"toolUseEvent":          true,
		"parse_error":           true,
		"unknown":               true,
	}
	
	if !validEventTypes[event.EventType] {
		return fmt.Errorf("未知的事件类型: %s", event.EventType)
	}
	
	return nil
}

// GetEventSummary 获取事件摘要
func (p *EventStreamParser) GetEventSummary(result *ParseResult) map[string]int {
	summary := make(map[string]int)
	
	for _, event := range result.Events {
		summary[event.EventType]++
	}
	
	return summary
}

// BatchParseFiles 批量解析多个文件
func BatchParseHexDataFiles(filePaths []string) ([]*ParseResult, error) {
	results := make([]*ParseResult, 0, len(filePaths))
	
	for _, filePath := range filePaths {
		// 加载hex数据
		analyzer, err := LoadHexDataFromFile(filePath)
		if err != nil {
			return nil, fmt.Errorf("加载文件 %s 失败: %w", filePath, err)
		}
		
		// 解析hex数据
		hexData, err := analyzer.ParseHexData()
		if err != nil {
			return nil, fmt.Errorf("解析hex数据失败 %s: %w", filePath, err)
		}
		
		if !hexData.IsValid {
			return nil, fmt.Errorf("文件 %s 的hex数据校验失败: %s", filePath, hexData.ErrorMessage)
		}
		
		// 解析事件流
		parser := NewEventStreamParser()
		defer parser.Close()
		
		parseResult, err := parser.ParseEventStream(hexData.BinaryData)
		if err != nil {
			return nil, fmt.Errorf("解析事件流失败 %s: %w", filePath, err)
		}
		
		results = append(results, parseResult)
	}
	
	return results, nil
}