package parser

import (
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
	"kiro2api/logger"
	"strings"
)

// RobustEventStreamParser 带CRC校验和错误恢复的解析器
type RobustEventStreamParser struct {
	headerParser  *HeaderParser
	strictMode    bool
	errorCount    int
	maxErrors     int
	crcTable      *crc32.Table
	buffer        []byte      // 保留原有buffer用于兼容
	ringBuffer    *RingBuffer // 新的环形缓冲区
	useRingBuffer bool        // 是否使用环形缓冲区
}

// NewRobustEventStreamParser 创建健壮的事件流解析器
func NewRobustEventStreamParser(strictMode bool) *RobustEventStreamParser {
	return &RobustEventStreamParser{
		headerParser:  NewHeaderParser(),
		strictMode:    strictMode,
		maxErrors:     10,
		crcTable:      crc32.MakeTable(crc32.IEEE),
		buffer:        make([]byte, 0, 4096),
		ringBuffer:    NewRingBuffer(64 * 1024), // 64KB环形缓冲区
		useRingBuffer: false,                    // 默认不启用，保持兼容性
	}
}

// EnableRingBuffer 启用环形缓冲区
func (rp *RobustEventStreamParser) EnableRingBuffer(enable bool) {
	rp.useRingBuffer = enable
	if enable && rp.ringBuffer == nil {
		rp.ringBuffer = NewRingBuffer(64 * 1024)
	}
}

// SetMaxErrors 设置最大错误次数
func (rp *RobustEventStreamParser) SetMaxErrors(maxErrors int) {
	rp.maxErrors = maxErrors
}

// Reset 重置解析器状态
func (rp *RobustEventStreamParser) Reset() {
	rp.buffer = rp.buffer[:0]
	rp.errorCount = 0
	if rp.ringBuffer != nil {
		rp.ringBuffer.Reset()
	}
}

// ParseStream 解析流数据并返回消息
func (rp *RobustEventStreamParser) ParseStream(data []byte) ([]*EventStreamMessage, error) {
	if rp.useRingBuffer {
		return rp.parseStreamWithRingBuffer(data)
	}

	// 原有的buffer处理逻辑
	rp.buffer = append(rp.buffer, data...)

	messages := make([]*EventStreamMessage, 0, 8)
	offset := 0

	lastErrorOffset := -1
	consecutiveErrors := 0

	for offset < len(rp.buffer) && rp.errorCount < rp.maxErrors {
		message, consumed, err := rp.parseSingleMessageWithValidation(rp.buffer[offset:])

		if err != nil {
			if rp.strictMode {
				return messages, fmt.Errorf("严格模式下解析失败: %w", err)
			}

			// 检测死循环：如果在同一位置连续出错
			if offset == lastErrorOffset {
				consecutiveErrors++
				if consecutiveErrors > 3 {
					// 强制跳过一定字节数以打破循环
					skipBytes := minInt(32, len(rp.buffer)-offset)
					if skipBytes <= 1 {
						logger.Warn("无法跳过更多数据，停止解析",
							logger.Int("offset", offset),
							logger.Int("buffer_len", len(rp.buffer)))
						break
					}
					offset += skipBytes
					logger.Warn("检测到解析死循环，强制跳过字节",
						logger.Int("skip_bytes", skipBytes),
						logger.Int("new_offset", offset))
					consecutiveErrors = 0
					continue
				}
			} else {
				lastErrorOffset = offset
				consecutiveErrors = 1
			}

			// 对于流数据场景：数据可能尚未完全到达
			if isMoreDataNeededError(err) {
				logger.Debug("数据不完整，等待更多数据",
					logger.Int("offset", offset),
					logger.Int("buffer_len", len(rp.buffer)))
				break
			}

			// 记录错误
			logger.Warn("消息解析失败，尝试恢复",
				logger.Err(err),
				logger.Int("offset", offset),
				logger.Int("error_count", rp.errorCount))
			rp.errorCount++

			// 智能恢复：根据错误类型选择策略
			var recoveryOffset int
			if strings.Contains(err.Error(), "CRC") {
				// CRC错误：尝试找到下一个有效消息
				recoveryOffset = rp.findNextMessageBoundary(rp.buffer[offset+1:])
				if recoveryOffset > 0 {
					offset += recoveryOffset + 1
				} else {
					offset += 4 // 跳过一个可能的长度字段
				}
			} else if strings.Contains(err.Error(), "长度") || strings.Contains(err.Error(), "消息总长度异常") {
				// 长度异常：快速跳过
				offset += 16 // 跳过最小消息大小
			} else {
				// 其他错误：逐字节查找
				recoveryOffset = rp.findNextMessageBoundary(rp.buffer[offset+1:])
				if recoveryOffset > 0 {
					offset += recoveryOffset + 1
				} else {
					offset++
				}
			}

			logger.Debug("错误恢复策略执行",
				logger.String("error_type", err.Error()[:minInt(50, len(err.Error()))]),
				logger.Int("new_offset", offset))
			continue
		}

		if message != nil {
			messages = append(messages, message)
			logger.Debug("成功解析消息",
				logger.String("message_type", message.GetMessageType()),
				logger.String("event_type", message.GetEventType()),
				logger.Int("payload_len", len(message.Payload)))
		}

		offset += consumed
	}

	// 简化的缓冲区管理：安全移除已处理的数据
	if offset > 0 {
		remaining := len(rp.buffer) - offset

		if remaining > 0 {
			// 保留未处理的数据
			if remaining < len(rp.buffer)/2 && cap(rp.buffer) > 8192 {
				// 如果剩余数据很少且缓冲区很大，重新分配以节省内存
				newBuffer := make([]byte, remaining)
				copy(newBuffer, rp.buffer[offset:])
				rp.buffer = newBuffer
				logger.Debug("重新分配缓冲区以节省内存",
					logger.Int("old_cap", cap(rp.buffer)),
					logger.Int("new_size", remaining))
			} else {
				// 移动数据到缓冲区开始位置
				copy(rp.buffer[:remaining], rp.buffer[offset:])
				rp.buffer = rp.buffer[:remaining]
			}

			// 快速验证剩余数据是否可能是有效消息的开始
			if remaining >= 12 {
				totalLen := binary.BigEndian.Uint32(rp.buffer[:4])
				if totalLen >= 16 && totalLen <= 16*1024*1024 {
					logger.Debug("缓冲区包含潜在的下一条消息",
						logger.Int("remaining_bytes", remaining),
						logger.Int("expected_msg_len", int(totalLen)))
				}
			}
		} else {
			// 没有剩余数据，清空缓冲区
			rp.buffer = rp.buffer[:0]
			// 如果缓冲区过大，重置为默认大小
			if cap(rp.buffer) > 64*1024 {
				rp.buffer = make([]byte, 0, 4096)
				logger.Debug("重置缓冲区到默认大小")
			}
		}

		logger.Debug("缓冲区清理完成",
			logger.Int("processed", offset),
			logger.Int("remaining", remaining))
	}

	if rp.errorCount >= rp.maxErrors {
		return messages, fmt.Errorf("错误次数过多 (%d)，停止解析", rp.errorCount)
	}

	return messages, nil
}

// isMoreDataNeededError 判断是否属于"等待更多数据"的可恢复情况
func isMoreDataNeededError(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()

	// 仅数据帧本身不完整才等待更多数据
	if strings.Contains(s, "数据不完整: 需要") && !strings.Contains(s, "头部") {
		return true
	}

	// 头部解析相关错误应该尝试恢复而不是等待
	if strings.Contains(s, "数据不足：需要更多数据继续解析") {
		return false // 改为立即尝试恢复
	}

	return false
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
		return nil, 0, NewParseError(fmt.Sprintf("消息长度过大: %d", totalLength), nil)
	}

	if int(totalLength) > len(data) {
		return nil, 0, NewParseError(fmt.Sprintf("数据不完整: 需要 %d 字节，实际 %d 字节", totalLength, len(data)), nil)
	}

	// 头部长度验证（考虑 Prelude CRC）
	if headerLength > totalLength-16 { // 总长度减去固定开销: 4+4+4+4=16
		return nil, int(totalLength), NewParseError(fmt.Sprintf("头部长度异常: %d", headerLength), nil)
	}

	// 提取消息各部分（考虑 Prelude CRC）
	headerData := data[12 : 12+headerLength] // 从第12字节开始（跳过 Prelude CRC）
	payloadStart := 12 + headerLength
	payloadEnd := int(totalLength) - 4
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
	} else {
		// logger.Debug("CRC校验通过",
		// logger.String("crc", fmt.Sprintf("%08x", expectedCRC)))
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

// findNextMessageBoundary 查找下一个消息边界，用于错误恢复
func (rp *RobustEventStreamParser) findNextMessageBoundary(data []byte) int {
	// 优化：使用步进搜索减少CPU开销
	step := 1
	maxSearch := minInt(1024, len(data)) // 限制搜索范围

	for i := 0; i < maxSearch-16; i += step {
		// 快速检查：是否可能是消息开始
		if i+12 > len(data) {
			break
		}

		totalLen := binary.BigEndian.Uint32(data[i : i+4])

		// 快速筛选：长度必须合理
		if totalLen < 16 || totalLen > 16*1024*1024 {
			continue
		}

		headerLen := binary.BigEndian.Uint32(data[i+4 : i+8])
		if headerLen > totalLen-16 {
			continue
		}

		// 验证 Prelude CRC（快速验证）
		if i+12 <= len(data) {
			preludeCRC := binary.BigEndian.Uint32(data[i+8 : i+12])
			calculatedCRC := crc32.ChecksumIEEE(data[i : i+8])
			if preludeCRC == calculatedCRC {
				logger.Debug("找到有效消息边界（CRC验证通过）",
					logger.Int("offset", i),
					logger.Int("msg_len", int(totalLen)))
				return i
			}
		}

		// 如果前几次没找到，增加步长加快搜索
		if i > 64 && i%64 == 0 {
			step = 4
		}
	}

	return 0
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

	// 直接查找包含 tooluse_ 的字符串
	if strings.Contains(payload, "tooluse_") {
		start := strings.Index(payload, "tooluse_")
		if start >= 0 {
			// 查找ID的结束位置
			end := start + 8 // "tooluse_" 长度
			for end < len(payload) && (payload[end] != '"' && payload[end] != ',' && payload[end] != '}' && payload[end] != ' ') {
				end++
			}
			if end > start+8 {
				toolUseId := payload[start:end]
				toolUseIds = append(toolUseIds, toolUseId)
				logger.Debug("提取到tool_use_id",
					logger.String("tool_use_id", toolUseId),
					logger.Int("start_pos", start),
					logger.Int("end_pos", end))
			}
		}
	}

	return toolUseIds
}

// isValidToolUseIdFormat 验证tool_use_id格式是否有效
func (rp *RobustEventStreamParser) isValidToolUseIdFormat(toolUseId string) bool {
	// 基本格式检查
	if !strings.HasPrefix(toolUseId, "tooluse_") {
		return false
	}

	// 长度检查
	if len(toolUseId) < 20 {
		return false
	}

	// 字符有效性检查（base64字符 + 下划线）
	suffix := toolUseId[8:]
	for _, char := range suffix {
		if !((char >= 'a' && char <= 'z') ||
			(char >= 'A' && char <= 'Z') ||
			(char >= '0' && char <= '9') ||
			char == '_' || char == '-') {
			return false
		}
	}

	return true
}

// minInt 返回两个整数中的最小值
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
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
