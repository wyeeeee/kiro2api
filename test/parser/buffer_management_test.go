package main

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"testing"
	"time"

	"kiro2api/parser"
)

// TestBufferManagement 测试缓冲区管理的改进
func TestBufferManagement(t *testing.T) {
	p := parser.NewRobustEventStreamParser(false)

	t.Run("处理部分消息", func(t *testing.T) {
		// 创建一个有效的消息
		message := createValidMessage("test", "event", []byte(`{"data":"test"}`))

		// 分两次发送
		part1 := message[:len(message)/2]
		part2 := message[len(message)/2:]

		// 第一次解析，应该没有消息
		messages1, err := p.ParseStream(part1)
		if err != nil {
			t.Fatalf("第一次解析失败: %v", err)
		}
		if len(messages1) != 0 {
			t.Errorf("期望0条消息，得到 %d 条", len(messages1))
		}

		// 第二次解析，应该有一条完整消息
		messages2, err := p.ParseStream(part2)
		if err != nil {
			t.Fatalf("第二次解析失败: %v", err)
		}
		if len(messages2) != 1 {
			t.Errorf("期望1条消息，得到 %d 条", len(messages2))
		}
	})

	t.Run("处理多个连续消息", func(t *testing.T) {
		p.Reset()

		// 创建三个消息
		msg1 := createValidMessage("msg1", "event", []byte(`{"id":1}`))
		msg2 := createValidMessage("msg2", "event", []byte(`{"id":2}`))
		msg3 := createValidMessage("msg3", "event", []byte(`{"id":3}`))

		// 连接所有消息
		allData := append(append(msg1, msg2...), msg3...)

		// 一次性解析
		messages, err := p.ParseStream(allData)
		if err != nil {
			t.Fatalf("解析失败: %v", err)
		}
		if len(messages) != 3 {
			t.Errorf("期望3条消息，得到 %d 条", len(messages))
		}
	})

	t.Run("处理损坏的消息并恢复", func(t *testing.T) {
		p.Reset()

		// 创建一个损坏的消息和一个正常的消息
		corruptedMsg := []byte{0xFF, 0xFF, 0xFF, 0xFF, 0x00, 0x00, 0x00, 0x10} // 无效的消息头
		validMsg := createValidMessage("valid", "event", []byte(`{"valid":true}`))

		// 连接消息
		allData := append(corruptedMsg, validMsg...)

		// 解析（宽松模式应该能恢复）
		messages, err := p.ParseStream(allData)
		if err != nil {
			t.Logf("解析有错误（预期）: %v", err)
		}

		// 应该至少能解析出有效消息
		if len(messages) < 1 {
			t.Errorf("应该至少解析出1条有效消息，得到 %d 条", len(messages))
		}
	})

	t.Run("缓冲区内存优化", func(t *testing.T) {
		p.Reset()

		// 发送大量小消息，测试缓冲区是否会正确清理
		for i := 0; i < 100; i++ {
			msg := createValidMessage("test", "event", []byte(`{"index":1}`))
			_, err := p.ParseStream(msg)
			if err != nil {
				t.Fatalf("第 %d 次解析失败: %v", i, err)
			}
		}

		// 如果没有内存泄漏或panic，测试通过
		t.Log("缓冲区内存管理测试通过")
	})
}

// TestErrorRecovery 测试错误恢复机制
func TestErrorRecovery(t *testing.T) {
	p := parser.NewRobustEventStreamParser(false)

	t.Run("避免死循环", func(t *testing.T) {
		// 创建会导致解析循环的数据
		badData := []byte{
			0x00, 0x00, 0x00, 0x20, // 总长度：32
			0x00, 0x00, 0x00, 0x08, // 头部长度：8
			0x00, 0x00, 0x00, 0x00, // Prelude CRC (错误的)
			// 数据不足...
		}

		// 多次发送相同的坏数据
		for i := 0; i < 5; i++ {
			_, err := p.ParseStream(badData)
			if err != nil {
				t.Logf("第 %d 次解析错误（预期）: %v", i, err)
			}
		}

		// 如果没有死循环，测试通过
		t.Log("成功避免死循环")
	})

	t.Run("快速消息边界查找", func(t *testing.T) {
		p.Reset()

		// 创建包含垃圾数据和有效消息的混合数据
		garbage := make([]byte, 1024)
		for i := range garbage {
			garbage[i] = byte(i % 256)
		}
		validMsg := createValidMessage("valid", "event", []byte(`{"found":true}`))

		mixedData := append(garbage, validMsg...)

		// 测试解析性能
		start := time.Now()
		messages, _ := p.ParseStream(mixedData)
		elapsed := time.Since(start)

		if len(messages) > 0 {
			t.Logf("成功从垃圾数据中恢复，找到 %d 条消息，耗时: %v", len(messages), elapsed)
		}

		// 确保查找不会太慢
		if elapsed > 100*time.Millisecond {
			t.Errorf("消息边界查找太慢: %v", elapsed)
		}
	})
}

// createValidMessage 创建一个有效的AWS EventStream消息
func createValidMessage(msgType, eventType string, payload []byte) []byte {
	// 计算各部分长度
	headers := createHeaders(msgType, eventType)
	headerLen := uint32(len(headers))
	payloadLen := uint32(len(payload))
	totalLen := uint32(12 + headerLen + payloadLen + 4) // 12=prelude, 4=message CRC

	// 创建消息缓冲区
	buf := bytes.NewBuffer(nil)

	// 写入总长度和头部长度
	binary.Write(buf, binary.BigEndian, totalLen)
	binary.Write(buf, binary.BigEndian, headerLen)

	// 计算并写入Prelude CRC
	preludeData := buf.Bytes()
	preludeCRC := crc32.ChecksumIEEE(preludeData)
	binary.Write(buf, binary.BigEndian, preludeCRC)

	// 写入头部和载荷
	buf.Write(headers)
	buf.Write(payload)

	// 计算并写入消息CRC
	messageData := buf.Bytes()
	messageCRC := crc32.ChecksumIEEE(messageData)
	binary.Write(buf, binary.BigEndian, messageCRC)

	return buf.Bytes()
}

// createHeaders 创建消息头部
func createHeaders(msgType, eventType string) []byte {
	buf := bytes.NewBuffer(nil)

	// :message-type header
	buf.WriteByte(14) // 长度
	buf.WriteString(":message-type")
	buf.WriteByte(7) // STRING type
	binary.Write(buf, binary.BigEndian, uint16(len(msgType)))
	buf.WriteString(msgType)

	// :event-type header
	buf.WriteByte(11) // 长度
	buf.WriteString(":event-type")
	buf.WriteByte(7) // STRING type
	binary.Write(buf, binary.BigEndian, uint16(len(eventType)))
	buf.WriteString(eventType)

	return buf.Bytes()
}
