package utils

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"kiro2api/logger"
)

// RawDataRecord 原始数据记录结构
type RawDataRecord struct {
	Timestamp    time.Time `json:"timestamp"`
	RequestID    string    `json:"request_id"`
	MessageID    string    `json:"message_id"`
	Model        string    `json:"model"`
	TotalBytes   int       `json:"total_bytes"`
	MD5Hash      string    `json:"md5_hash"`
	HexData      string    `json:"hex_data"`       // 十六进制编码的原始数据
	RawData      string    `json:"raw_data"`       // UTF-8可读的数据（如果可转换）
	IsStream     bool      `json:"is_stream"`      // 是否为流式数据
	HasToolCalls bool      `json:"has_tool_calls"` // 是否包含工具调用
	Metadata     Metadata  `json:"metadata"`
}

// Metadata 附加元数据
type Metadata struct {
	ClientIP       string            `json:"client_ip,omitempty"`
	UserAgent      string            `json:"user_agent,omitempty"`
	RequestHeaders map[string]string `json:"request_headers,omitempty"`
	ErrorInfo      string            `json:"error_info,omitempty"`
	ParseSuccess   bool              `json:"parse_success"`
	EventsCount    int               `json:"events_count,omitempty"`
}

// SaveRawDataForReplay 保存原始数据以供回放和测试使用
func SaveRawDataForReplay(rawData []byte, requestID, messageID, model string, isStream bool, metadata Metadata) error {
	if len(rawData) == 0 {
		return nil // 跳过空数据
	}

	// 创建数据记录
	record := RawDataRecord{
		Timestamp:    time.Now(),
		RequestID:    requestID,
		MessageID:    messageID,
		Model:        model,
		TotalBytes:   len(rawData),
		MD5Hash:      generateMD5Hash(rawData),
		HexData:      hex.EncodeToString(rawData),
		IsStream:     isStream,
		HasToolCalls: containsToolIndicators(rawData),
		Metadata:     metadata,
	}

	// 尝试转换为UTF-8可读格式
	if utf8Data := string(rawData); isValidUTF8(utf8Data) {
		record.RawData = utf8Data
	}

	// 保存到文件
	return saveToFile(record)
}

// generateMD5Hash 生成数据的MD5哈希
func generateMD5Hash(data []byte) string {
	hash := md5.Sum(data)
	return hex.EncodeToString(hash[:])
}

// containsToolIndicators 检查数据是否包含工具调用指示器
func containsToolIndicators(data []byte) bool {
	dataStr := string(data)
	indicators := []string{
		"tool_use",
		"toolUseEvent",
		"<tool_use>",
		"assistantResponseEvent",
		"\"type\":\"tool_use\"",
	}

	for _, indicator := range indicators {
		if strings.Contains(dataStr, indicator) {
			return true
		}
	}
	return false
}

// isValidUTF8 检查字符串是否为有效的UTF-8
// 对于二进制协议数据，采用更智能的判断策略
// isBinaryProtocolData 检查数据是否为二进制协议格式（如AWS EventStream）
func isBinaryProtocolData(s string) bool {
	if len(s) < 8 {
		return false
	}

	// 检查是否以EventStream协议的典型模式开头
	// EventStream格式: [4字节长度][4字节头长度][头部数据][载荷数据]
	bytes := []byte(s)

	// 检查前8字节是否为长度标识符模式 (大端序)
	if bytes[0] == 0x00 && bytes[4] == 0x00 {
		return true
	}

	// 检查是否包含EventStream事件类型标识符
	eventStreamIndicators := []string{
		":event-type",
		"assistantResponseEvent",
		"toolUseEvent",
		"application/json",
		":message-type",
	}

	for _, indicator := range eventStreamIndicators {
		if strings.Contains(s, indicator) {
			return true
		}
	}

	// 检查空字节密度（二进制数据通常包含较多空字节）
	nullCount := strings.Count(s, "\x00")
	if float64(nullCount)/float64(len(s)) > 0.1 { // 超过10%的空字节
		return true
	}

	return false
}

// extractReadableContent 从二进制协议数据中提取可读内容
func extractReadableContent(s string) string {
	var readableParts []string

	// 查找JSON片段
	if matches := findJSONFragments(s); len(matches) > 0 {
		readableParts = append(readableParts, matches...)
	}

	// 查找事件类型信息
	eventTypes := []string{
		"assistantResponseEvent",
		"toolUseEvent",
		"application/json",
		"event",
	}

	for _, eventType := range eventTypes {
		if strings.Contains(s, eventType) {
			readableParts = append(readableParts, eventType)
		}
	}

	// 查找其他可读文本片段（连续的可打印ASCII字符）
	var currentWord strings.Builder
	for _, b := range []byte(s) {
		if b >= 32 && b <= 126 { // 可打印ASCII范围
			currentWord.WriteByte(b)
		} else {
			if word := currentWord.String(); len(word) >= 3 {
				readableParts = append(readableParts, word)
			}
			currentWord.Reset()
		}
	}

	// 添加最后一个词
	if word := currentWord.String(); len(word) >= 3 {
		readableParts = append(readableParts, word)
	}

	return strings.Join(readableParts, " ")
}

// findJSONFragments 查找字符串中的JSON片段
func findJSONFragments(s string) []string {
	var fragments []string

	// 查找 {"content":"..."} 模式
	for i := 0; i < len(s)-10; i++ {
		if s[i:i+10] == `"content":"` {
			start := i
			end := i + 10

			// 查找结束引号
			for j := end; j < len(s); j++ {
				if s[j] == '"' && (j == 0 || s[j-1] != '\\') {
					end = j + 1
					break
				}
			}

			if end > start+10 {
				fragments = append(fragments, s[start:end])
				i = end - 1 // 跳过已处理的部分
			}
		}
	}

	return fragments
}

// isValidUTF8 检查字符串是否为有效的UTF-8
// 对于二进制协议数据，采用更智能的判断策略
func isValidUTF8(s string) bool {
	if len(s) == 0 || len(s) >= 1024*1024 {
		return false // 空数据或过大数据
	}

	// 检查是否为二进制协议数据 (如EventStream)
	if isBinaryProtocolData(s) {
		// 尝试提取可读部分
		if readablePart := extractReadableContent(s); len(readablePart) > 10 {
			return utf8.ValidString(readablePart)
		}
		return false
	}

	// 对于非二进制数据，保持原有逻辑
	return !strings.Contains(s, "\x00") && utf8.ValidString(s)
}

// saveToFile 保存记录到文件
func saveToFile(record RawDataRecord) error {
	// 创建保存目录
	saveDir := "test/raw_data_replay"
	if err := os.MkdirAll(saveDir, 0755); err != nil {
		return fmt.Errorf("创建保存目录失败: %w", err)
	}

	// 生成文件名：时间戳_请求ID.json
	filename := fmt.Sprintf("%s_%s.json",
		record.Timestamp.Format("20060102_150405"),
		sanitizeFilename(record.RequestID))

	filepath := filepath.Join(saveDir, filename)

	// 序列化为JSON
	jsonData, err := MarshalIndent(record, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化记录失败: %w", err)
	}

	// 写入文件
	if err := os.WriteFile(filepath, jsonData, 0644); err != nil {
		return fmt.Errorf("写入文件失败: %w", err)
	}

	logger.Debug("原始数据已保存",
		logger.String("file", filepath),
		logger.Int("bytes", record.TotalBytes),
		logger.String("md5", record.MD5Hash),
		logger.Bool("has_tools", record.HasToolCalls))

	return nil
}

// sanitizeFilename 清理文件名中的非法字符
func sanitizeFilename(filename string) string {
	// 替换非法字符
	invalidChars := []string{"/", "\\", ":", "*", "?", "\"", "<", ">", "|"}
	result := filename
	for _, char := range invalidChars {
		result = strings.ReplaceAll(result, char, "_")
	}

	// 限制长度
	if len(result) > 50 {
		result = result[:50]
	}

	return result
}

// GetRawDataBytes 从记录中获取原始字节数据
func (r *RawDataRecord) GetRawDataBytes() ([]byte, error) {
	if r.HexData == "" {
		return nil, fmt.Errorf("十六进制数据为空")
	}

	data, err := hex.DecodeString(r.HexData)
	if err != nil {
		return nil, fmt.Errorf("解码十六进制数据失败: %w", err)
	}

	// 验证MD5哈希
	if expectedHash := generateMD5Hash(data); expectedHash != r.MD5Hash {
		return nil, fmt.Errorf("数据完整性验证失败: 期望%s, 实际%s", r.MD5Hash, expectedHash)
	}

	return data, nil
}
