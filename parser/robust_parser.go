package parser

import (
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
	"kiro2api/logger"
	"kiro2api/utils"
	"strings"
	"sync"
	"time"
)

// RobustEventStreamParser 带CRC校验和错误恢复的解析器
type RobustEventStreamParser struct {
	headerParser *HeaderParser
	strictMode   bool
	errorCount   int
	maxErrors    int
	crcTable     *crc32.Table
	ringBuffer   *RingBuffer // 新的环形缓冲区
	// *** 新增：并发访问控制和状态保护 ***
	mu            sync.RWMutex // 保护并发访问
	lastProcessed int64        // 最后处理的字节数，用于监控
	parsingActive bool         // 是否正在解析中，防止重入
}

// NewRobustEventStreamParser 创建健壮的事件流解析器
func NewRobustEventStreamParser(strictMode bool) *RobustEventStreamParser {
	return &RobustEventStreamParser{
		headerParser: NewHeaderParser(),
		strictMode:   strictMode,
		maxErrors:    10,
		crcTable:     crc32.MakeTable(crc32.IEEE),
		ringBuffer:   NewRingBuffer(512 * 1024), // *** 环形缓冲区也提升到512KB ***
	}
}

// SetMaxErrors 设置最大错误次数
func (rp *RobustEventStreamParser) SetMaxErrors(maxErrors int) {
	rp.maxErrors = maxErrors
}

// Reset 重置解析器状态
func (rp *RobustEventStreamParser) Reset() {
	rp.errorCount = 0
	if rp.ringBuffer != nil {
		rp.ringBuffer.Reset()
	}
}

// ParseStream 解析流数据并返回消息
func (rp *RobustEventStreamParser) ParseStream(data []byte) ([]*EventStreamMessage, error) {
	// *** 关键修复：并发访问保护 ***
	rp.mu.Lock()
	defer rp.mu.Unlock()

	// 防止重入调用
	if rp.parsingActive {
		logger.Warn("检测到解析重入调用，等待当前解析完成")
		return []*EventStreamMessage{}, nil
	}
	rp.parsingActive = true
	defer func() {
		rp.parsingActive = false
	}()

	// 记录处理开始时间，用于性能监控
	startTime := time.Now()
	defer func() {
		duration := time.Since(startTime)
		rp.lastProcessed = time.Now().Unix()
		if duration > 100*time.Millisecond {
			logger.Warn("解析耗时过长，可能存在性能问题",
				logger.Duration("duration", duration),
				logger.Int("input_bytes", len(data)))
		}
	}()

	return rp.parseStreamWithRingBuffer(data)

}

// parseSingleMessageWithValidation 解析单个消息并进行CRC校验
func (rp *RobustEventStreamParser) parseSingleMessageWithValidation(data []byte) (*EventStreamMessage, int, error) {
	if len(data) < 16 { // AWS EventStream 最小消息长度：4+4+4+4=16字节
		return nil, 0, NewParseError("数据长度不足", nil)
	}

	// 每条消息开始前重置头部解析器，避免跨消息状态泄漏
	if rp.headerParser != nil {
		rp.headerParser.Reset()
	}

	// 读取消息长度
	totalLength := binary.BigEndian.Uint32(data[:4])
	headerLength := binary.BigEndian.Uint32(data[4:8])

	// *** 关键修复：严格验证数据完整性 ***
	if int(totalLength) != len(data) {
		return nil, 0, NewParseError(fmt.Sprintf("数据长度不匹配: 期望 %d 字节，实际 %d 字节", totalLength, len(data)), nil)
	}

	// AWS EventStream 格式验证：检查 Prelude CRC
	if len(data) < 12 {
		return nil, 0, NewParseError("数据长度不足以包含 Prelude CRC", nil)
	}
	preludeCRC := binary.BigEndian.Uint32(data[8:12])

	// 验证 Prelude CRC（前8字节：totalLength + headerLength）
	calculatedPreludeCRC := crc32.Checksum(data[:8], rp.crcTable)
	if preludeCRC != calculatedPreludeCRC {
		logger.Warn("Prelude CRC 校验失败",
			logger.String("expected_crc", fmt.Sprintf("%08x", preludeCRC)),
			logger.String("calculated_crc", fmt.Sprintf("%08x", calculatedPreludeCRC)))
		// 在非严格模式下继续处理
		if rp.strictMode {
			return nil, int(totalLength), NewParseError(fmt.Sprintf("Prelude CRC 校验失败: 期望 %08x, 实际 %08x", preludeCRC, calculatedPreludeCRC), nil)
		}
	}

	// 验证长度合理性（考虑 Prelude CRC）
	if totalLength < 16 { // 最小: 4(totalLen) + 4(headerLen) + 4(preludeCRC) + 4(msgCRC) = 16
		return nil, 0, NewParseError(fmt.Sprintf("消息总长度异常: %d", totalLength), nil)
	}
	if totalLength > 16*1024*1024 { // 16MB 限制
		return nil, 4, NewParseError(fmt.Sprintf("消息长度过大: %d", totalLength), nil) // 🔧 修复: 返回4字节而非0，避免死循环
	}

	// 头部长度验证（考虑 Prelude CRC）
	if headerLength > totalLength-16 { // 总长度减去固定开销: 4+4+4+4=16
		return nil, int(totalLength), NewParseError(fmt.Sprintf("头部长度异常: %d", headerLength), nil)
	}

	// 提取消息各部分（考虑 Prelude CRC）
	headerData := data[12 : 12+headerLength] // 从第12字节开始（跳过 Prelude CRC）
	payloadStart := int(12 + headerLength)
	payloadEnd := int(totalLength) - 4

	// *** 关键修复：严格边界检查 ***
	if payloadStart > payloadEnd || payloadEnd > len(data) {
		return nil, int(totalLength), NewParseError(fmt.Sprintf("payload边界异常: start=%d, end=%d, data_len=%d", payloadStart, payloadEnd, len(data)), nil)
	}

	payloadData := data[payloadStart:payloadEnd]

	// 添加详细的payload调试信息
	logger.Debug("Payload调试信息（修复后）",
		logger.Int("total_length", int(totalLength)),
		logger.Int("header_length", int(headerLength)),
		logger.String("prelude_crc", fmt.Sprintf("%08x", preludeCRC)),
		logger.Int("payload_start", int(payloadStart)),
		logger.Int("payload_end", payloadEnd),
		logger.Int("payload_len", len(payloadData)),
		logger.String("payload_hex", func() string {
			if len(payloadData) > 20 {
				return fmt.Sprintf("%x", payloadData[:20]) + "..."
			}
			return fmt.Sprintf("%x", payloadData)
		}()),
		logger.String("payload_raw", func() string {
			if len(payloadData) > 100 {
				return string(payloadData[:100]) + "..."
			}
			return string(payloadData)
		}()))

	// CRC 校验（消息 CRC 覆盖整个消息除了最后4字节）
	expectedCRC := binary.BigEndian.Uint32(data[payloadEnd:totalLength])
	calculatedCRC := crc32.Checksum(data[:payloadEnd], rp.crcTable)

	if expectedCRC != calculatedCRC {
		err := NewParseError(fmt.Sprintf("CRC 校验失败: 期望 %08x, 实际 %08x", expectedCRC, calculatedCRC), nil)
		if rp.strictMode {
			return nil, int(totalLength), err
		} else {
			logger.Warn("CRC校验失败但继续处理",
				logger.String("expected_crc", fmt.Sprintf("%08x", expectedCRC)),
				logger.String("calculated_crc", fmt.Sprintf("%08x", calculatedCRC)))
		}
	}

	// 解析头部 - 支持空头部的容错处理和断点续传
	var headers map[string]HeaderValue
	var err error

	if len(headerData) == 0 {
		logger.Debug("检测到空头部，创建默认头部")
		headers = map[string]HeaderValue{
			":message-type": {Type: ValueType_STRING, Value: MessageTypes.EVENT},
			":event-type":   {Type: ValueType_STRING, Value: EventTypes.ASSISTANT_RESPONSE_EVENT},
			":content-type": {Type: ValueType_STRING, Value: "application/json"},
		}
	} else {
		headers, err = rp.headerParser.ParseHeaders(headerData)
		if err != nil {
			// 检查是否可以进行智能恢复
			if rp.headerParser.IsHeaderParseRecoverable(rp.headerParser.GetState()) {
				logger.Warn("头部解析部分失败，使用已解析的头部", logger.Err(err))
				headers = rp.headerParser.ForceCompleteHeaderParsing(rp.headerParser.GetState())
				rp.headerParser.Reset()
			} else {
				// 无法恢复，使用默认头部
				logger.Warn("头部解析失败，使用默认头部", logger.Err(err))
				rp.headerParser.Reset()
				headers = map[string]HeaderValue{
					":message-type": {Type: ValueType_STRING, Value: MessageTypes.EVENT},
					":event-type":   {Type: ValueType_STRING, Value: EventTypes.ASSISTANT_RESPONSE_EVENT},
					":content-type": {Type: ValueType_STRING, Value: "application/json"},
				}
			}
		}
	}

	// 验证头部 - 宽松验证
	if err := rp.headerParser.ValidateHeaders(headers); err != nil {
		logger.Warn("头部验证失败，但继续处理", logger.Err(err))
	}

	// *** 关键修复：增强payload完整性验证 ***
	if len(payloadData) > 0 {
		// 验证payload是否为有效的JSON（如果是JSON类型）
		if contentType := GetContentTypeFromHeaders(headers); contentType == "application/json" {
			if !utils.Valid(payloadData) {
				// JSON无效但不是致命错误，记录警告
				logger.Warn("检测到无效JSON payload",
					logger.String("payload_preview", func() string {
						if len(payloadData) > 50 {
							return string(payloadData[:50]) + "..."
						}
						return string(payloadData)
					}()),
					logger.Int("payload_len", len(payloadData)))

				// 尝试简单的JSON修复
				payloadStr := string(payloadData)
				if strings.Contains(payloadStr, "\"input\":") && strings.Contains(payloadStr, "\"name\":") {
					logger.Debug("检测到可能的工具调用payload损坏，记录但继续处理")
				}
			}
		}
	}

	message := &EventStreamMessage{
		Headers:     headers,
		Payload:     payloadData,
		MessageType: GetMessageTypeFromHeaders(headers),
		EventType:   GetEventTypeFromHeaders(headers),
		ContentType: GetContentTypeFromHeaders(headers),
	}

	// 添加工具调用完整性验证
	rp.validateToolUseIdIntegrity(message)

	logger.Debug("消息解析成功",
		logger.String("message_type", message.MessageType),
		logger.String("event_type", message.EventType),
		logger.Int("header_count", len(headers)),
		logger.Int("payload_len", len(payloadData)))

	return message, int(totalLength), nil
}

// ParseEventsFromReader 从Reader读取并解析事件流
func (rp *RobustEventStreamParser) ParseEventsFromReader(reader io.Reader) ([]*EventStreamMessage, error) {
	var allMessages []*EventStreamMessage
	buf := make([]byte, 4096)

	for {
		n, err := reader.Read(buf)
		if n > 0 {
			messages, parseErr := rp.ParseStream(buf[:n])
			if parseErr != nil && rp.strictMode {
				return allMessages, parseErr
			}
			allMessages = append(allMessages, messages...)
		}

		if err == io.EOF {
			break
		}
		if err != nil {
			return allMessages, fmt.Errorf("读取数据失败: %w", err)
		}
	}

	return allMessages, nil
}

// validateToolUseIdIntegrity 验证工具调用中的tool_use_id完整性
func (rp *RobustEventStreamParser) validateToolUseIdIntegrity(message *EventStreamMessage) {
	if message == nil || len(message.Payload) == 0 {
		return
	}

	payloadStr := string(message.Payload)

	// 检查是否包含工具调用相关内容
	if strings.Contains(payloadStr, "tool_use_id") || strings.Contains(payloadStr, "toolUseId") {
		logger.Debug("检测到工具调用消息，验证完整性",
			logger.String("message_type", message.MessageType),
			logger.String("event_type", message.EventType),
			logger.String("payload_preview", func() string {
				if len(payloadStr) > 200 {
					return payloadStr[:200] + "..."
				}
				return payloadStr
			}()))

		// 提取所有可能的tool_use_id
		toolUseIds := rp.extractToolUseIds(payloadStr)
		for _, toolUseId := range toolUseIds {
			if !rp.isValidToolUseIdFormat(toolUseId) {
				logger.Warn("检测到可能损坏的tool_use_id",
					logger.String("tool_use_id", toolUseId),
					logger.String("message_type", message.MessageType),
					logger.String("event_type", message.EventType))
			} else {
				logger.Debug("tool_use_id格式验证通过",
					logger.String("tool_use_id", toolUseId))
			}
		}
	}
}

// extractToolUseIds 从payload中提取所有tool_use_id
func (rp *RobustEventStreamParser) extractToolUseIds(payload string) []string {
	var toolUseIds []string

	// 使用更严格的字符串查找，避免匹配到损坏的ID
	searchStr := "tooluse_"
	startPos := 0

	for {
		idx := strings.Index(payload[startPos:], searchStr)
		if idx == -1 {
			break
		}

		actualStart := startPos + idx

		// 确保前面是引号或其他分隔符，避免匹配到 "tooluluse_" 这样的损坏ID
		if actualStart > 0 {
			prevChar := payload[actualStart-1]
			if prevChar != '"' && prevChar != ':' && prevChar != ' ' && prevChar != '{' {
				// 跳过这个匹配，可能是损坏的ID
				startPos = actualStart + 1
				continue
			}
		}

		// 查找ID的结束位置
		end := actualStart + len(searchStr)
		for end < len(payload) {
			char := payload[end]
			// 有效的tool_use_id字符: 字母、数字、下划线、连字符
			if !((char >= 'a' && char <= 'z') ||
				(char >= 'A' && char <= 'Z') ||
				(char >= '0' && char <= '9') ||
				char == '_' || char == '-') {
				break
			}
			end++
		}

		if end > actualStart+len(searchStr) {
			toolUseId := payload[actualStart:end]

			// 验证格式有效性
			if rp.isValidToolUseIdFormat(toolUseId) {
				toolUseIds = append(toolUseIds, toolUseId)
				logger.Debug("提取到tool_use_id",
					logger.String("tool_use_id", toolUseId),
					logger.Int("start_pos", actualStart),
					logger.Int("end_pos", end))
			} else {
				logger.Warn("跳过格式无效的tool_use_id",
					logger.String("invalid_id", toolUseId))
			}
		}

		startPos = actualStart + 1
	}

	return toolUseIds
}

// isValidToolUseIdFormat 验证tool_use_id格式是否有效
func (rp *RobustEventStreamParser) isValidToolUseIdFormat(toolUseId string) bool {
	// 基本格式检查
	if !strings.HasPrefix(toolUseId, "tooluse_") {
		return false
	}

	// 长度检查 - 标准格式应该是 "tooluse_" + 22字符的Base64编码ID
	if len(toolUseId) < 20 || len(toolUseId) > 50 {
		logger.Debug("tool_use_id长度异常",
			logger.String("id", toolUseId),
			logger.Int("length", len(toolUseId)))
		return false
	}

	// 字符有效性检查（base64字符 + 下划线和连字符）
	suffix := toolUseId[8:]
	for i, char := range suffix {
		if !((char >= 'a' && char <= 'z') ||
			(char >= 'A' && char <= 'Z') ||
			(char >= '0' && char <= '9') ||
			char == '_' || char == '-') {
			logger.Debug("tool_use_id包含无效字符",
				logger.String("id", toolUseId),
				logger.Int("invalid_pos", i+8),
				logger.String("invalid_char", string(char)))
			return false
		}
	}

	// 检查是否包含明显的损坏模式（如多余的"ul"）
	if strings.Contains(toolUseId, "tooluluse_") || strings.Contains(toolUseId, "tooluse_tooluse_") {
		logger.Warn("检测到明显损坏的tool_use_id模式",
			logger.String("id", toolUseId))
		return false
	}

	return true
}

// parseStreamWithRingBuffer 使用环形缓冲区解析流数据
func (rp *RobustEventStreamParser) parseStreamWithRingBuffer(data []byte) ([]*EventStreamMessage, error) {
	// 写入新数据到环形缓冲区
	written, err := rp.ringBuffer.TryWrite(data)
	if err == ErrBufferFull {
		// 缓冲区满，尝试处理现有数据
		logger.Warn("环形缓冲区已满，处理现有数据")
	} else if written < len(data) {
		logger.Warn("环形缓冲区空间不足",
			logger.Int("written", written),
			logger.Int("total", len(data)))
	}

	messages := make([]*EventStreamMessage, 0, 8)
	tempBuffer := make([]byte, 16*1024) // 16KB临时缓冲区

	for {
		// 查看可用数据
		available := rp.ringBuffer.Available()
		if available < 16 { // 最小消息大小
			break
		}

		// 查看消息头
		n, _ := rp.ringBuffer.Peek(tempBuffer[:16])
		if n < 16 {
			break
		}

		// 解析消息长度
		totalLength := binary.BigEndian.Uint32(tempBuffer[:4])

		// 验证长度合理性
		if totalLength < 16 || totalLength > 16*1024*1024 {
			// 跳过无效数据
			rp.ringBuffer.Skip(1)
			rp.errorCount++
			logger.Warn("跳过无效消息头",
				logger.Int("total_length", int(totalLength)))
			continue
		}

		// 检查是否有足够的数据
		if available < int(totalLength) {
			// 等待更多数据
			break
		}

		// 读取完整消息
		messageData := make([]byte, totalLength)
		n, _ = rp.ringBuffer.Read(messageData)
		if n != int(totalLength) {
			logger.Error("读取消息失败",
				logger.Int("expected", int(totalLength)),
				logger.Int("actual", n))
			break
		}

		// 解析消息
		message, _, err := rp.parseSingleMessageWithValidation(messageData)
		if err != nil {
			if rp.strictMode {
				return messages, err
			}
			logger.Warn("消息解析失败", logger.Err(err))
			rp.errorCount++
			continue
		}

		if message != nil {
			messages = append(messages, message)
		}
	}

	// 检查错误计数
	if rp.errorCount >= rp.maxErrors {
		return messages, fmt.Errorf("错误次数过多 (%d)，停止解析", rp.errorCount)
	}

	return messages, nil
}
