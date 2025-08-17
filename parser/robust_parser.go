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
	headerParser *HeaderParser
	strictMode   bool
	errorCount   int
	maxErrors    int
	crcTable     *crc32.Table
	buffer       []byte
}

// NewRobustEventStreamParser 创建健壮的事件流解析器
func NewRobustEventStreamParser(strictMode bool) *RobustEventStreamParser {
	return &RobustEventStreamParser{
		headerParser: NewHeaderParser(),
		strictMode:   strictMode,
		maxErrors:    10,
		crcTable:     crc32.MakeTable(crc32.IEEE),
		buffer:       make([]byte, 0, 4096),
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
}

// ParseStream 解析流数据并返回消息
func (rp *RobustEventStreamParser) ParseStream(data []byte) ([]*EventStreamMessage, error) {
	// 将新数据添加到缓冲区
	rp.buffer = append(rp.buffer, data...)

	messages := make([]*EventStreamMessage, 0, 8)
	offset := 0

	for offset < len(rp.buffer) && rp.errorCount < rp.maxErrors {
		message, consumed, err := rp.parseSingleMessageWithValidation(rp.buffer[offset:])

		if err != nil {
			if rp.strictMode {
				return messages, fmt.Errorf("严格模式下解析失败: %w", err)
			}

			// 对于流数据场景：数据可能尚未完全到达，遇到"数据长度不足/数据不完整"时，不计入错误次数，等待更多数据
			if isMoreDataNeededError(err) {
				logger.Debug("检测到数据尚未完整，等待更多数据", logger.Int("offset", offset))
				break
			}

			// 宽松模式：其他错误尝试恢复
			logger.Warn("消息解析失败，尝试恢复",
				logger.Err(err),
				logger.Int("offset", offset),
				logger.Int("error_count", rp.errorCount))
			rp.errorCount++

			// 如果是头部解析相关错误，强制跳过当前消息以防止死循环
			if strings.Contains(err.Error(), "数据不足：需要更多数据继续解析") {
				logger.Debug("检测到头部解析循环，强制跳过当前位置")
				// 尝试找到下一个消息边界
				syncOffset := rp.findNextMessageBoundary(rp.buffer[offset:])
				if syncOffset > 0 {
					offset += syncOffset
					logger.Debug("找到消息边界，继续解析", logger.Int("sync_offset", syncOffset))
					continue
				} else {
					// 无法找到边界，跳过更多字节以打破循环
					skipBytes := minInt(16, len(rp.buffer)-offset-1) // 安全跳过
					if skipBytes > 0 {
						offset += skipBytes
						logger.Debug("跳过字节以打破解析循环", logger.Int("skip_bytes", skipBytes))
					} else {
						// 数据不足，等待更多数据
						logger.Debug("缓冲区数据不足，等待更多数据")
						break
					}
					continue
				}
			}

			// 尝试重新同步到下一个有效消息
			syncOffset := rp.findNextMessageBoundary(rp.buffer[offset:])
			if syncOffset > 0 {
				offset += syncOffset
				logger.Debug("找到消息边界，继续解析", logger.Int("sync_offset", syncOffset))
				continue
			} else {
				// 无法同步，跳过一个字节继续尝试
				offset++
				continue
			}
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

	// 安全移除已处理的数据，确保消息边界完整性
	if offset > 0 {
		remaining := len(rp.buffer) - offset
		
		// 验证剩余数据是否足够进行边界检查（考虑 Prelude CRC）
		if remaining >= 12 {
			// 检查下一个消息的完整性
			nextTotalLen := binary.BigEndian.Uint32(rp.buffer[offset:offset+4])
			nextHeaderLen := binary.BigEndian.Uint32(rp.buffer[offset+4:offset+8])
			
			// 验证消息头的合理性（考虑 Prelude CRC）
			if nextTotalLen >= 16 && nextTotalLen <= 16*1024*1024 && 
			   nextHeaderLen <= nextTotalLen-16 {
				
				if remaining >= int(nextTotalLen) {
					// 下一个消息完整，安全移除已处理数据
					copy(rp.buffer, rp.buffer[offset:])
					rp.buffer = rp.buffer[:remaining]
					logger.Debug("安全移除缓冲区数据", 
						logger.Int("removed_bytes", offset),
						logger.Int("remaining_bytes", remaining),
						logger.Int("next_msg_len", int(nextTotalLen)))
				} else {
					// 下一个消息不完整，保留所有数据等待
					copy(rp.buffer, rp.buffer[offset:])
					rp.buffer = rp.buffer[:remaining]
					logger.Debug("等待完整消息，保留缓冲区数据",
						logger.Int("need_bytes", int(nextTotalLen)),
						logger.Int("have_bytes", remaining))
				}
			} else {
				// 消息头异常，可能是边界错误，保守处理
				logger.Warn("检测到异常消息头，保守处理缓冲区",
					logger.Int("next_total_len", int(nextTotalLen)),
					logger.Int("next_header_len", int(nextHeaderLen)))
				copy(rp.buffer, rp.buffer[offset:])
				rp.buffer = rp.buffer[:remaining]
			}
		} else if remaining > 0 {
			// 数据不足以判断，保留所有未处理数据
			copy(rp.buffer, rp.buffer[offset:])
			rp.buffer = rp.buffer[:remaining]
			logger.Debug("数据不足以验证边界，保留剩余数据",
				logger.Int("remaining_bytes", remaining))
		} else {
			// 没有剩余数据，清空缓冲区
			rp.buffer = rp.buffer[:0]
			logger.Debug("缓冲区已完全处理，清空")
		}
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
	// 在数据中搜索可能的消息头模式（考虑 Prelude CRC）
	for i := 1; i < len(data)-16; i++ {
		// 检查是否像一个有效的消息头
		totalLen := binary.BigEndian.Uint32(data[i : i+4])
		headerLen := binary.BigEndian.Uint32(data[i+4 : i+8])

		// 基本合理性检查（考虑 Prelude CRC）
		if totalLen >= 16 && totalLen <= 16*1024*1024 &&
			headerLen <= totalLen-16 &&
			int(totalLen) <= len(data)-i {

			// 进一步验证：尝试解析头部（使用全新的解析器以避免跨消息状态干扰）
			if int(headerLen) > 0 && i+12+int(headerLen) <= len(data) {
				headerData := data[i+12 : i+12+int(headerLen)] // 跳过 Prelude CRC
				tempParser := NewHeaderParser()
				if _, err := tempParser.ParseHeaders(headerData); err == nil {
					logger.Debug("找到潜在的消息边界", logger.Int("offset", i))
					return i
				}
			}
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
