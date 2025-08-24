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
func isValidUTF8(s string) bool {
	return len(s) > 0 && len(s) < 1024*1024 && // 限制大小，避免处理过大的数据
		!strings.Contains(s, "\x00") && // 避免空字节
		utf8.ValidString(s)
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

	logger.Info("原始数据已保存",
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

// LoadRawDataForReplay 加载保存的原始数据用于回放测试
func LoadRawDataForReplay(filepath string) (*RawDataRecord, error) {
	data, err := os.ReadFile(filepath)
	if err != nil {
		return nil, fmt.Errorf("读取文件失败: %w", err)
	}

	var record RawDataRecord
	if err := FastUnmarshal(data, &record); err != nil {
		return nil, fmt.Errorf("解析JSON失败: %w", err)
	}

	return &record, nil
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

// ListSavedRawData 列出所有保存的原始数据文件
func ListSavedRawData() ([]string, error) {
	// 尝试多个可能的路径
	possiblePaths := []string{
		"test/raw_data_replay",    // 从项目根目录运行时
		"raw_data_replay",         // 从test目录运行时
		"../test/raw_data_replay", // 从其他子目录运行时
	}

	var saveDir string
	var entries []os.DirEntry
	var err error

	// 找到第一个存在的目录
	for _, path := range possiblePaths {
		entries, err = os.ReadDir(path)
		if err == nil {
			saveDir = path
			break
		}
	}

	// 如果所有路径都不存在
	if saveDir == "" {
		return []string{}, nil // 返回空列表
	}

	var files []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".json") {
			files = append(files, filepath.Join(saveDir, entry.Name()))
		}
	}

	return files, nil
}
