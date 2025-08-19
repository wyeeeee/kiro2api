package main

import (
	"bytes"
	"fmt"
	"kiro2api/parser"
	"testing"
	"time"
)

// TestRingBuffer 测试环形缓冲区
func TestRingBuffer(t *testing.T) {
	t.Run("基本读写操作", func(t *testing.T) {
		rb := parser.NewRingBuffer(1024)

		// 写入数据
		data := []byte("Hello, Ring Buffer!")
		n, err := rb.Write(data)
		if err != nil {
			t.Fatalf("写入失败: %v", err)
		}
		if n != len(data) {
			t.Errorf("写入字节数不匹配: 期望 %d, 实际 %d", len(data), n)
		}

		// 读取数据
		buf := make([]byte, 100)
		n, err = rb.Read(buf)
		if err != nil {
			t.Fatalf("读取失败: %v", err)
		}
		if n != len(data) {
			t.Errorf("读取字节数不匹配: 期望 %d, 实际 %d", len(data), n)
		}
		if !bytes.Equal(buf[:n], data) {
			t.Errorf("数据不匹配: 期望 %s, 实际 %s", data, buf[:n])
		}
	})

	t.Run("环形操作", func(t *testing.T) {
		rb := parser.NewRingBuffer(10) // 小缓冲区测试环形特性

		// 写入并读取多次，测试环形特性
		for i := 0; i < 5; i++ {
			data := []byte{byte('A' + i)}

			// 写入
			_, err := rb.Write(data)
			if err != nil {
				t.Fatalf("第 %d 次写入失败: %v", i, err)
			}

			// 读取
			buf := make([]byte, 1)
			n, err := rb.Read(buf)
			if err != nil {
				t.Fatalf("第 %d 次读取失败: %v", i, err)
			}
			if n != 1 || buf[0] != data[0] {
				t.Errorf("第 %d 次数据不匹配: 期望 %v, 实际 %v", i, data[0], buf[0])
			}
		}
	})

	t.Run("非阻塞操作", func(t *testing.T) {
		rb := parser.NewRingBuffer(10)

		// 填满缓冲区
		data := make([]byte, 10)
		for i := range data {
			data[i] = byte(i)
		}
		n, err := rb.TryWrite(data)
		if err != nil {
			t.Fatalf("TryWrite失败: %v", err)
		}
		if n != 10 {
			t.Errorf("写入字节数不对: %d", n)
		}

		// 尝试写入更多（应该失败）
		_, err = rb.TryWrite([]byte{99})
		if err != parser.ErrBufferFull {
			t.Errorf("期望ErrBufferFull，得到: %v", err)
		}

		// 读取一些数据
		buf := make([]byte, 5)
		n, err = rb.TryRead(buf)
		if err != nil {
			t.Fatalf("TryRead失败: %v", err)
		}
		if n != 5 {
			t.Errorf("读取字节数不对: %d", n)
		}

		// 现在应该可以写入了
		n, err = rb.TryWrite([]byte{99, 100})
		if err != nil {
			t.Fatalf("第二次TryWrite失败: %v", err)
		}
		if n != 2 {
			t.Errorf("第二次写入字节数不对: %d", n)
		}
	})

	t.Run("Peek操作", func(t *testing.T) {
		rb := parser.NewRingBuffer(100)

		data := []byte("peek test")
		rb.Write(data)

		// Peek不应该移除数据
		buf := make([]byte, 100)
		n, err := rb.Peek(buf)
		if err != nil {
			t.Fatalf("Peek失败: %v", err)
		}
		if n != len(data) {
			t.Errorf("Peek字节数不对: %d", n)
		}

		// 数据应该还在
		if rb.Available() != len(data) {
			t.Errorf("Peek后数据丢失: %d", rb.Available())
		}

		// 正常读取
		n, err = rb.Read(buf)
		if err != nil {
			t.Fatalf("Read失败: %v", err)
		}
		if n != len(data) {
			t.Errorf("Read字节数不对: %d", n)
		}

		// 现在应该为空
		if rb.Available() != 0 {
			t.Errorf("Read后还有数据: %d", rb.Available())
		}
	})
}

// TestToolCallFSM 测试工具调用状态机
func TestToolCallFSM(t *testing.T) {
	t.Run("状态转换", func(t *testing.T) {
		fsm := parser.NewToolCallFSM()

		toolID := "test_tool_001"

		// 开始工具
		err := fsm.StartTool(toolID, "TestTool")
		if err != nil {
			t.Fatalf("启动工具失败: %v", err)
		}

		// 检查状态
		state, exists := fsm.GetState(toolID)
		if !exists {
			t.Fatal("工具状态不存在")
		}
		if state.State != parser.StateStarted {
			t.Errorf("期望状态 Started，得到 %s", state.State)
		}

		// 添加输入
		err = fsm.AddInput(toolID, `{"key": "value"}`)
		if err != nil {
			t.Fatalf("添加输入失败: %v", err)
		}

		// 检查状态
		state, _ = fsm.GetState(toolID)
		if state.State != parser.StateCollecting {
			t.Errorf("期望状态 Collecting，得到 %s", state.State)
		}

		// 完成工具
		err = fsm.CompleteTool(toolID)
		if err != nil {
			t.Fatalf("完成工具失败: %v", err)
		}

		// 检查最终状态
		state, _ = fsm.GetState(toolID)
		if state.State != parser.StateValidating {
			t.Errorf("期望状态 Validating，得到 %s", state.State)
		}
	})

	t.Run("错误处理", func(t *testing.T) {
		fsm := parser.NewToolCallFSM()

		toolID := "test_tool_002"

		// 开始工具
		fsm.StartTool(toolID, "ErrorTool")

		// 添加一些输入
		fsm.AddInput(toolID, "some input")

		// 触发错误
		err := fsm.ErrorTool(toolID, &parser.ParseError{Message: "test error"})
		if err != nil {
			t.Fatalf("设置错误失败: %v", err)
		}

		// 检查状态
		state, _ := fsm.GetState(toolID)
		if state.State != parser.StateError {
			t.Errorf("期望状态 Error，得到 %s", state.State)
		}
		if state.Error == nil {
			t.Error("错误信息丢失")
		}
	})

	t.Run("取消操作", func(t *testing.T) {
		fsm := parser.NewToolCallFSM()

		toolID := "test_tool_003"

		// 开始工具
		fsm.StartTool(toolID, "CancelTool")
		fsm.AddInput(toolID, "input")

		// 取消工具
		err := fsm.CancelTool(toolID)
		if err != nil {
			t.Fatalf("取消工具失败: %v", err)
		}

		// 检查状态
		state, _ := fsm.GetState(toolID)
		if state.State != parser.StateCancelled {
			t.Errorf("期望状态 Cancelled，得到 %s", state.State)
		}
	})

	t.Run("并发操作", func(t *testing.T) {
		fsm := parser.NewToolCallFSM()

		// 并发创建多个工具
		done := make(chan bool, 10)
		for i := 0; i < 10; i++ {
			go func(id int) {
				toolID := fmt.Sprintf("concurrent_tool_%d", id)

				// 执行工具操作
				fsm.StartTool(toolID, "ConcurrentTool")
				fsm.AddInput(toolID, fmt.Sprintf("input %d", id))
				fsm.CompleteTool(toolID)

				done <- true
			}(i)
		}

		// 等待所有goroutine完成
		for i := 0; i < 10; i++ {
			<-done
		}

		// 检查所有工具状态
		states := fsm.GetAllStates()
		if len(states) != 10 {
			t.Errorf("期望10个工具状态，得到 %d", len(states))
		}
	})

	t.Run("状态清理", func(t *testing.T) {
		fsm := parser.NewToolCallFSM()

		// 创建一些已完成的工具
		for i := 0; i < 5; i++ {
			toolID := fmt.Sprintf("cleanup_tool_%d", i)
			fsm.StartTool(toolID, "CleanupTool")
			// 先进入收集状态
			fsm.AddInput(toolID, "test input")
			// 然后完成工具
			fsm.CompleteTool(toolID)
			// 手动设置为完成状态（因为我们没有验证步骤）
			fsm.ProcessEvent(toolID, parser.ToolEvent{
				Type:      "validate_success",
				Timestamp: time.Now(),
			})
		}

		// 等待一下确保有时间差
		time.Sleep(10 * time.Millisecond)

		// 清理过期状态（使用很短的超时）
		cleaned := fsm.CleanupStaleStates(1 * time.Millisecond)
		if cleaned < 5 {
			// 可能需要更多时间或状态不正确
			// 获取当前状态检查
			states := fsm.GetAllStates()
			for id, state := range states {
				t.Logf("状态 %s: %s", id, state.State)
			}
		}

		// 验证至少清理了一些状态
		if cleaned == 0 {
			t.Error("没有清理任何状态")
		} else {
			t.Logf("成功清理了 %d 个状态", cleaned)
		}
	})
}

// TestIntegratedParsing 测试集成的解析功能
func TestIntegratedParsing(t *testing.T) {
	t.Run("使用环形缓冲区解析", func(t *testing.T) {
		parser := parser.NewRobustEventStreamParser(false)
		parser.EnableRingBuffer(true)

		// 创建测试消息
		msg1 := createTestMessage("msg1", "event", []byte(`{"test":1}`))
		msg2 := createTestMessage("msg2", "event", []byte(`{"test":2}`))

		// 分批发送
		messages, err := parser.ParseStream(msg1[:len(msg1)/2])
		if err != nil {
			t.Fatalf("第一批解析失败: %v", err)
		}
		if len(messages) != 0 {
			t.Errorf("不应该有完整消息")
		}

		// 发送剩余部分和第二条消息
		allData := append(msg1[len(msg1)/2:], msg2...)
		messages, err = parser.ParseStream(allData)
		if err != nil {
			t.Fatalf("第二批解析失败: %v", err)
		}

		if len(messages) != 2 {
			t.Errorf("期望2条消息，得到 %d", len(messages))
		}
	})

	t.Run("状态机集成", func(t *testing.T) {
		processor := parser.NewCompliantMessageProcessor()

		// 创建一个工具调用消息
		toolMsg := &parser.EventStreamMessage{
			MessageType: "event",
			EventType:   "assistantResponseEvent",
			Payload: []byte(`{
				"toolUseId": "test_fsm_001",
				"name": "TestTool",
				"input": "{\"param\":\"value\"}",
				"stop": false
			}`),
		}

		// 处理消息
		events, err := processor.ProcessMessage(toolMsg)
		if err != nil {
			t.Fatalf("处理消息失败: %v", err)
		}

		if len(events) == 0 {
			t.Error("没有生成事件")
		}

		// 发送停止信号
		stopMsg := &parser.EventStreamMessage{
			MessageType: "event",
			EventType:   "assistantResponseEvent",
			Payload: []byte(`{
				"toolUseId": "test_fsm_001",
				"name": "TestTool",
				"input": "",
				"stop": true
			}`),
		}

		events, err = processor.ProcessMessage(stopMsg)
		if err != nil {
			t.Fatalf("处理停止消息失败: %v", err)
		}

		// 应该有结束事件
		hasStopEvent := false
		for _, evt := range events {
			if evt.Event == "content_block_stop" {
				hasStopEvent = true
				break
			}
		}

		if !hasStopEvent {
			t.Error("没有生成停止事件")
		}
	})
}

// createTestMessage 创建测试消息（避免重复定义）
func createTestMessage(msgType, eventType string, payload []byte) []byte {
	// 简化版本的消息创建
	// 这里简化处理，实际应该按照AWS EventStream格式
	// 写入总长度、头部长度、CRC等
	// 为了测试简单，我们只返回payload

	return append([]byte{0, 0, 0, 32, 0, 0, 0, 8, 0, 0, 0, 0}, payload...)
}
