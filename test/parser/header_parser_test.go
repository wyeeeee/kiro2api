package main

import (
	"kiro2api/parser"
	"testing"
)

// TestHeaderParserDataAccumulation 测试头部解析器的数据累积功能
func TestHeaderParserDataAccumulation(t *testing.T) {
	headerParser := parser.NewHeaderParser()

	// 构建测试数据：一个完整的头部数据
	// 头部名称: ":message-type" (13 bytes)
	// 值类型: STRING (7)
	// 值长度: 5 bytes
	// 值: "event"
	completeData := []byte{
		13,                                                              // 名称长度 (位置0)
		':', 'm', 'e', 's', 's', 'a', 'g', 'e', '-', 't', 'y', 'p', 'e', // 名称 (位置1-13，共13字节)
		7,    // 值类型 (位置14)
		0, 5, // 值长度 (位置15-16，共2字节)
		'e', 'v', 'e', 'n', 't', // 值 (位置17-21，共5字节)
	}

	// 打印完整数据信息用于调试
	t.Logf("完整数据长度: %d, 数据内容: %v", len(completeData), completeData)

	t.Run("完整数据解析", func(t *testing.T) {
		headerParser.Reset()
		headers, err := headerParser.ParseHeaders(completeData)

		if err != nil {
			t.Fatalf("完整数据解析失败: %v", err)
		}

		if len(headers) != 1 {
			t.Fatalf("期望解析1个头部，实际解析%d个", len(headers))
		}

		if header, exists := headers[":message-type"]; !exists {
			t.Fatal("缺少 :message-type 头部")
		} else if header.Value != "event" {
			t.Fatalf("头部值错误: 期望 'event'，实际 '%v'", header.Value)
		}
	})

	t.Run("数据不足_无法读取名称长度", func(t *testing.T) {
		headerParser.Reset()

		// 空数据，无法读取名称长度
		emptyData := []byte{}
		headers, err := headerParser.ParseHeaders(emptyData)

		if err != nil {
			t.Fatalf("空数据应该返回空头部而不是错误: %v", err)
		}

		if len(headers) != 0 {
			t.Fatalf("空数据应该返回空头部，实际返回%d个头部", len(headers))
		}
	})

	t.Run("数据不足_部分名称长度", func(t *testing.T) {
		headerParser.Reset()

		// 第一次：只有名称长度
		chunk1 := completeData[:1] // 只有名称长度字节
		headers, err := headerParser.ParseHeaders(chunk1)

		if err == nil {
			t.Fatal("部分数据应该返回数据不足错误")
		}

		if len(headers) != 0 {
			t.Fatalf("部分数据应该返回空头部，实际返回%d个头部", len(headers))
		}

		// 第二次：提供剩余数据 - 需要创建包含完整名称的数据块
		// 因为第一次只处理了名称长度，第二次需要从名称开始的完整数据
		chunk2 := completeData[1:] // 从名称开始的数据
		headers, err = headerParser.ParseHeaders(chunk2)

		if err != nil {
			t.Fatalf("提供剩余数据后解析失败: %v", err)
		}

		if len(headers) != 1 {
			t.Fatalf("期望解析1个头部，实际解析%d个", len(headers))
		}
	})

	t.Run("数据不足_部分名称数据", func(t *testing.T) {
		headerParser.Reset()

		// 第一次：名称长度 + 部分名称
		chunk1 := completeData[:5] // 名称长度 + 4字节名称（需要13字节）
		_, err := headerParser.ParseHeaders(chunk1)

		if err == nil {
			t.Fatal("部分名称数据应该返回数据不足错误")
		}

		// 验证状态保存
		state := headerParser.GetState()
		if state.Phase != parser.PhaseReadName {
			t.Fatalf("期望处于名称读取阶段，实际阶段: %d", state.Phase)
		}

		if len(state.PartialName) != 4 {
			t.Fatalf("期望部分名称长度为4，实际为%d", len(state.PartialName))
		}

		// 第二次：提供剩余数据
		chunk2 := completeData[5:]
		headers, err := headerParser.ParseHeaders(chunk2)

		if err != nil {
			t.Fatalf("提供剩余数据后解析失败: %v", err)
		}

		if len(headers) != 1 {
			t.Fatalf("期望解析1个头部，实际解析%d个", len(headers))
		}
	})

	t.Run("数据不足_无法读取值类型", func(t *testing.T) {
		headerParser.Reset()

		// 第一次：名称长度 + 完整名称，但没有值类型
		chunk1 := completeData[:14] // 到名称结束，缺少值类型
		_, err := headerParser.ParseHeaders(chunk1)

		// 调试信息
		state := headerParser.GetState()
		t.Logf("第一次解析后状态: Phase=%d, ParsedCount=%d, PartialName=%s",
			state.Phase, len(state.ParsedHeaders), string(state.PartialName))

		if err == nil {
			t.Fatal("缺少值类型应该返回数据不足错误")
		}

		// 验证状态
		if state.Phase != parser.PhaseReadValueType {
			t.Fatalf("期望处于值类型读取阶段，实际阶段: %d", state.Phase)
		}

		// 第二次：提供剩余数据
		chunk2 := completeData[14:]
		headers, err := headerParser.ParseHeaders(chunk2)

		if err != nil {
			t.Fatalf("提供剩余数据后解析失败: %v", err)
		}

		if len(headers) != 1 {
			t.Fatalf("期望解析1个头部，实际解析%d个", len(headers))
		}
	})

	t.Run("数据不足_无法读取值长度", func(t *testing.T) {
		headerParser.Reset()

		// 第一次：到值类型，但值长度只有1字节（需要2字节）
		chunk1 := completeData[:16] // 名称(1+13) + 值类型(1) + 1字节值长度
		t.Logf("chunk1 长度: %d, 内容: %v", len(chunk1), chunk1)
		_, err := headerParser.ParseHeaders(chunk1)

		// 调试信息
		state := headerParser.GetState()
		t.Logf("第一次解析后状态: Phase=%d, ParsedCount=%d",
			state.Phase, len(state.ParsedHeaders))

		if err == nil {
			t.Fatal("部分值长度应该返回数据不足错误")
		}

		// 验证状态
		if state.Phase != parser.PhaseReadValueLength {
			t.Fatalf("期望处于值长度读取阶段，实际阶段: %d", state.Phase)
		}

		// 第二次：提供剩余数据
		chunk2 := completeData[16:]
		t.Logf("chunk2 长度: %d, 内容: %v", len(chunk2), chunk2)
		headers, err := headerParser.ParseHeaders(chunk2)

		if err != nil {
			t.Fatalf("提供剩余数据后解析失败: %v", err)
		}

		if len(headers) != 1 {
			t.Fatalf("期望解析1个头部，实际解析%d个", len(headers))
		}
	})

	t.Run("数据不足_部分值数据", func(t *testing.T) {
		headerParser.Reset()

		// 第一次：到值长度，但值数据只有3字节（需要5字节）
		chunk1 := completeData[:20] // 完整头部-2字节值数据
		_, err := headerParser.ParseHeaders(chunk1)

		if err == nil {
			t.Fatal("部分值数据应该返回数据不足错误")
		}

		// 验证状态
		state := headerParser.GetState()
		if state.Phase != parser.PhaseReadValue {
			t.Fatalf("期望处于值读取阶段，实际阶段: %d", state.Phase)
		}

		if len(state.PartialValue) != 3 {
			t.Fatalf("期望部分值长度为3，实际为%d", len(state.PartialValue))
		}

		// 第二次：提供剩余数据
		chunk2 := completeData[20:]
		headers, err := headerParser.ParseHeaders(chunk2)

		if err != nil {
			t.Fatalf("提供剩余数据后解析失败: %v", err)
		}

		if len(headers) != 1 {
			t.Fatalf("期望解析1个头部，实际解析%d个", len(headers))
		}

		if header, exists := headers[":message-type"]; !exists {
			t.Fatal("缺少 :message-type 头部")
		} else if header.Value != "event" {
			t.Fatalf("头部值错误: 期望 'event'，实际 '%v'", header.Value)
		}
	})
}

// TestHeaderParserMultipleHeaders 测试多个头部的累积解析
func TestHeaderParserMultipleHeaders(t *testing.T) {
	headerParser := parser.NewHeaderParser()

	// 构建两个头部的测试数据
	// 头部1: ":message-type" = "event"
	// 头部2: ":event-type" = "assistantResponseEvent"
	completeData := []byte{
		// 头部1
		13,                                                              // 名称长度
		':', 'm', 'e', 's', 's', 'a', 'g', 'e', '-', 't', 'y', 'p', 'e', // 名称
		7,    // 值类型 (STRING)
		0, 5, // 值长度
		'e', 'v', 'e', 'n', 't', // 值
		// 头部2
		11,                                                    // 名称长度
		':', 'e', 'v', 'e', 'n', 't', '-', 't', 'y', 'p', 'e', // 名称
		7,     // 值类型 (STRING)
		0, 22, // 值长度
		'a', 's', 's', 'i', 's', 't', 'a', 'n', 't', 'R', 'e', 's', 'p', 'o', 'n', 's', 'e', 'E', 'v', 'e', 'n', 't', // 值
	}

	t.Run("多头部数据累积解析", func(t *testing.T) {
		headerParser.Reset()

		// 第一次：只提供第一个头部的部分数据
		chunk1 := completeData[:10] // 第一个头部的部分名称
		headers, err := headerParser.ParseHeaders(chunk1)

		if err == nil {
			t.Fatal("部分数据应该返回数据不足错误")
		}

		if len(headers) != 0 {
			t.Fatalf("部分数据应该返回空头部，实际返回%d个头部", len(headers))
		}

		// 第二次：提供到第一个头部结束 + 第二个头部开始
		chunk2 := completeData[10:30]
		headers, err = headerParser.ParseHeaders(chunk2)

		if err == nil {
			t.Fatal("部分数据应该返回数据不足错误")
		}

		// 应该已经解析完第一个头部
		if len(headers) != 1 {
			t.Fatalf("期望解析1个头部，实际解析%d个", len(headers))
		}

		// 第三次：提供剩余所有数据
		chunk3 := completeData[30:]
		headers, err = headerParser.ParseHeaders(chunk3)

		if err != nil {
			t.Fatalf("提供剩余数据后解析失败: %v", err)
		}

		// 现在应该有两个头部
		if len(headers) != 2 {
			t.Fatalf("期望解析2个头部，实际解析%d个", len(headers))
		}

		if header, exists := headers[":message-type"]; !exists {
			t.Fatal("缺少 :message-type 头部")
		} else if header.Value != "event" {
			t.Fatalf("message-type值错误: 期望 'event'，实际 '%v'", header.Value)
		}

		if header, exists := headers[":event-type"]; !exists {
			t.Fatal("缺少 :event-type 头部")
		} else if header.Value != "assistantResponseEvent" {
			t.Fatalf("event-type值错误: 期望 'assistantResponseEvent'，实际 '%v'", header.Value)
		}
	})
}

// TestHeaderParserStateReset 测试状态重置功能
func TestHeaderParserStateReset(t *testing.T) {
	headerParser := parser.NewHeaderParser()

	// 部分数据
	partialData := []byte{13, ':', 'm', 'e'}

	t.Run("状态重置", func(t *testing.T) {
		// 第一次解析部分数据
		_, err := headerParser.ParseHeaders(partialData)
		if err == nil {
			t.Fatal("部分数据应该返回错误")
		}

		// 验证状态已保存
		state := headerParser.GetState()
		if state.Phase == parser.PhaseReadNameLength {
			t.Fatal("状态应该已经改变")
		}

		// 重置状态
		headerParser.Reset()
		state = headerParser.GetState()

		// 验证状态已重置
		if state.Phase != parser.PhaseReadNameLength {
			t.Fatal("状态应该重置到初始阶段")
		}

		if len(state.ParsedHeaders) != 0 {
			t.Fatal("解析的头部应该被清空")
		}

		if len(state.PartialName) != 0 {
			t.Fatal("部分名称数据应该被清空")
		}
	})
}
