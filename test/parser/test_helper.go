package main

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"kiro2api/parser"
)

// createCompliantEventFrame 创建符合AWS规范且CRC32正确的事件帧
func createCompliantEventFrame(messageType, eventType, contentType, payload string) []byte {
	headers := &bytes.Buffer{}

	// 构建头部 - 使用正确的值类型常量
	writeHeader(headers, ":message-type", messageType)

	if messageType == parser.MessageTypes.EVENT && eventType != "" {
		writeHeader(headers, ":event-type", eventType)
	}

	if contentType != "" {
		writeHeader(headers, ":content-type", contentType)
	}

	payloadBytes := []byte(payload)
	headersData := headers.Bytes()

	// 计算长度
	headersLen := uint32(len(headersData))
	totalLen := uint32(4 + 4 + len(headersData) + len(payloadBytes) + 4)

	// 构建消息（不包含CRC）
	messageWithoutCRC := &bytes.Buffer{}

	// 写入总长度
	binary.Write(messageWithoutCRC, binary.BigEndian, totalLen)
	// 写入头部长度
	binary.Write(messageWithoutCRC, binary.BigEndian, headersLen)
	// 写入头部数据
	messageWithoutCRC.Write(headersData)
	// 写入载荷数据
	messageWithoutCRC.Write(payloadBytes)

	// 计算CRC32
	dataForCRC := messageWithoutCRC.Bytes()
	crcTable := crc32.MakeTable(crc32.IEEE)
	crc := crc32.Checksum(dataForCRC, crcTable)

	// 写入CRC32
	binary.Write(messageWithoutCRC, binary.BigEndian, crc)

	return messageWithoutCRC.Bytes()
}

// writeHeader 写入头部键值对
func writeHeader(buffer *bytes.Buffer, name, value string) {
	nameBytes := []byte(name)
	valueBytes := []byte(value)

	// 名称长度 (1字节)
	buffer.WriteByte(byte(len(nameBytes)))

	// 名称
	buffer.Write(nameBytes)

	// 值类型 (1字节) - STRING类型
	buffer.WriteByte(byte(parser.ValueType_STRING))

	// 值长度 (2字节，big-endian)
	binary.Write(buffer, binary.BigEndian, uint16(len(valueBytes)))

	// 值
	buffer.Write(valueBytes)
}
