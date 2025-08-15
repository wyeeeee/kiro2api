package main

import (
	"bytes"
	"kiro2api/parser"
	"testing"
)

// HeaderValueTypes 头部值类型常量
var HeaderValueTypes = struct {
	BOOL_TRUE  parser.ValueType
	BOOL_FALSE parser.ValueType
	BYTE       parser.ValueType
	SHORT      parser.ValueType
	INTEGER    parser.ValueType
	LONG       parser.ValueType
	BYTE_ARRAY parser.ValueType
	STRING     parser.ValueType
	TIMESTAMP  parser.ValueType
	UUID       parser.ValueType
}{
	BOOL_TRUE:  parser.ValueType_BOOL_TRUE,
	BOOL_FALSE: parser.ValueType_BOOL_FALSE,
	BYTE:       parser.ValueType_BYTE,
	SHORT:      parser.ValueType_SHORT,
	INTEGER:    parser.ValueType_INTEGER,
	LONG:       parser.ValueType_LONG,
	BYTE_ARRAY: parser.ValueType_BYTE_ARRAY,
	STRING:     parser.ValueType_STRING,
	TIMESTAMP:  parser.ValueType_TIMESTAMP,
	UUID:       parser.ValueType_UUID,
}

// buildCompliantEventFrame 构建符合AWS规范的事件帧 - 使用正确的CRC32计算
func buildCompliantEventFrame(messageType, eventType, contentType, payload string) []byte {
	return createCompliantEventFrame(messageType, eventType, contentType, payload)
}

func TestCompliantEventStreamParser_ParseBasicEvents(t *testing.T) {
	eventParser := parser.NewCompliantEventStreamParser(false)

	// 构建测试数据流
	stream := &bytes.Buffer{}

	// 1. Session start事件
	stream.Write(buildCompliantEventFrame(
		parser.MessageTypes.EVENT,
		parser.EventTypes.SESSION_START,
		"application/json",
		`{"sessionId":"test-session","timestamp":"2024-01-01T00:00:00Z"}`,
	))

	// 2. Completion事件
	stream.Write(buildCompliantEventFrame(
		parser.MessageTypes.EVENT,
		parser.EventTypes.COMPLETION,
		"application/json",
		`{"content":"Hello world","finishReason":"end_turn"}`,
	))

	// 3. Tool execution start事件
	stream.Write(buildCompliantEventFrame(
		parser.MessageTypes.EVENT,
		parser.EventTypes.TOOL_EXECUTION_START,
		"application/json",
		`{"toolCallId":"tool-123","toolName":"Bash","executionId":"exec-456"}`,
	))

	// 4. Tool call request事件
	stream.Write(buildCompliantEventFrame(
		parser.MessageTypes.EVENT,
		parser.EventTypes.TOOL_CALL_REQUEST,
		"application/json",
		`{"toolCallId":"tool-123","toolName":"Bash","input":{"command":"ls -la"}}`,
	))

	// 5. Tool call result事件
	stream.Write(buildCompliantEventFrame(
		parser.MessageTypes.EVENT,
		parser.EventTypes.TOOL_CALL_RESULT,
		"application/json",
		`{"toolCallId":"tool-123","result":"file1.txt\nfile2.txt","success":true}`,
	))

	// 6. Tool execution end事件
	stream.Write(buildCompliantEventFrame(
		parser.MessageTypes.EVENT,
		parser.EventTypes.TOOL_EXECUTION_END,
		"application/json",
		`{"toolCallId":"tool-123","executionId":"exec-456","status":"completed"}`,
	))

	// 7. Session end事件
	stream.Write(buildCompliantEventFrame(
		parser.MessageTypes.EVENT,
		parser.EventTypes.SESSION_END,
		"application/json",
		`{"sessionId":"test-session","duration":5000}`,
	))

	// 解析完整响应
	result, err := eventParser.ParseResponse(stream.Bytes())
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}

	// 验证结果
	if len(result.Messages) != 7 {
		t.Errorf("期望7条消息，实际得到%d条", len(result.Messages))
	}

	if len(result.Events) == 0 {
		t.Error("期望有SSE事件，但没有生成")
	}

	// 验证会话信息
	if result.SessionInfo.SessionID == "" {
		t.Error("期望会话ID不为空")
	}

	if result.SessionInfo.StartTime.IsZero() {
		t.Error("期望会话开始时间不为零值")
	}

	// 验证工具执行
	if len(result.ToolExecutions) == 0 {
		t.Error("期望有工具执行记录，但没有找到")
	}

	// 验证摘要
	if result.Summary == nil {
		t.Fatal("期望有解析摘要")
	}

	if result.Summary.TotalMessages != 7 {
		t.Errorf("摘要中消息总数错误，期望7，实际%d", result.Summary.TotalMessages)
	}

	if !result.Summary.HasToolCalls {
		t.Error("摘要应该显示有工具调用")
	}

	if !result.Summary.HasSessionEvents {
		t.Error("摘要应该显示有会话事件")
	}

	t.Logf("解析结果: %d条消息, %d个事件, %d个工具执行",
		len(result.Messages), len(result.Events), len(result.ToolExecutions))
}

func TestCompliantEventStreamParser_ErrorHandling(t *testing.T) {
	// 测试严格模式
	strictParser := parser.NewCompliantEventStreamParser(true)

	// 构建损坏的数据
	corruptedData := []byte{0x00, 0x00, 0x00, 0x10, 0x00, 0x00, 0x00, 0x04, 0xFF, 0xFF}

	_, err := strictParser.ParseResponse(corruptedData)
	if err == nil {
		t.Error("严格模式下期望解析损坏数据时报错")
	}

	// 测试非严格模式
	lenientParser := parser.NewCompliantEventStreamParser(false)

	result, err := lenientParser.ParseResponse(corruptedData)
	if err != nil {
		t.Errorf("非严格模式下不应该因损坏数据而失败: %v", err)
	}

	if result == nil {
		t.Error("非严格模式下应该返回结果，即使有错误")
	}
}

func TestCompliantEventStreamParser_StreamProcessing(t *testing.T) {
	eventParser := parser.NewCompliantEventStreamParser(false)

	// 构建分批数据
	frame1 := buildCompliantEventFrame(
		parser.MessageTypes.EVENT,
		parser.EventTypes.COMPLETION_CHUNK,
		"application/json",
		`{"delta":"Hello "}`,
	)

	frame2 := buildCompliantEventFrame(
		parser.MessageTypes.EVENT,
		parser.EventTypes.COMPLETION_CHUNK,
		"application/json",
		`{"delta":"world!"}`,
	)

	// 分批解析
	events1, err1 := eventParser.ParseStream(frame1)
	if err1 != nil {
		t.Fatalf("第一批解析失败: %v", err1)
	}

	events2, err2 := eventParser.ParseStream(frame2)
	if err2 != nil {
		t.Fatalf("第二批解析失败: %v", err2)
	}

	totalEvents := len(events1) + len(events2)
	if totalEvents == 0 {
		t.Error("期望生成事件，但没有")
	}

	t.Logf("流式处理生成 %d + %d = %d 个事件", len(events1), len(events2), totalEvents)
}

func TestCompliantEventStreamParser_ToolLifecycle(t *testing.T) {
	eventParser := parser.NewCompliantEventStreamParser(false)

	// 构建完整的工具生命周期
	stream := &bytes.Buffer{}

	// 工具执行开始
	stream.Write(buildCompliantEventFrame(
		parser.MessageTypes.EVENT,
		parser.EventTypes.TOOL_EXECUTION_START,
		"application/json",
		`{"toolCallId":"tool-test","toolName":"Write","executionId":"exec-test"}`,
	))

	// 工具调用请求
	stream.Write(buildCompliantEventFrame(
		parser.MessageTypes.EVENT,
		parser.EventTypes.TOOL_CALL_REQUEST,
		"application/json",
		`{"toolCallId":"tool-test","toolName":"Write","input":{"file_path":"/tmp/test.txt","content":"Hello"}}`,
	))

	// 工具调用结果
	stream.Write(buildCompliantEventFrame(
		parser.MessageTypes.EVENT,
		parser.EventTypes.TOOL_CALL_RESULT,
		"application/json",
		`{"toolCallId":"tool-test","result":"File written successfully","success":true}`,
	))

	// 工具执行结束
	stream.Write(buildCompliantEventFrame(
		parser.MessageTypes.EVENT,
		parser.EventTypes.TOOL_EXECUTION_END,
		"application/json",
		`{"toolCallId":"tool-test","executionId":"exec-test","status":"completed"}`,
	))

	result, err := eventParser.ParseResponse(stream.Bytes())
	if err != nil {
		t.Fatalf("解析工具生命周期失败: %v", err)
	}

	// 验证工具执行记录
	if len(result.ToolExecutions) == 0 {
		t.Fatal("期望有工具执行记录")
	}

	execution := result.ToolExecutions["tool-test"]
	if execution == nil {
		t.Fatal("未找到工具执行记录")
	}

	if execution.Name != "Write" {
		t.Errorf("工具名称错误，期望Write，实际%s", execution.Name)
	}

	if execution.Status != parser.ToolStatusCompleted {
		t.Errorf("工具状态错误，期望%s，实际%s", parser.ToolStatusCompleted, execution.Status)
	}

	// 检查是否有错误（没有错误表示成功）
	if execution.Error != "" {
		t.Errorf("工具执行不应该有错误: %s", execution.Error)
	}

	t.Logf("工具执行验证通过: %s -> %s", execution.Name, execution.Status)
}

func TestCompliantEventStreamParser_SessionManagement(t *testing.T) {
	eventParser := parser.NewCompliantEventStreamParser(false)

	stream := &bytes.Buffer{}

	// 会话开始
	stream.Write(buildCompliantEventFrame(
		parser.MessageTypes.EVENT,
		parser.EventTypes.SESSION_START,
		"application/json",
		`{"sessionId":"session-123","timestamp":"2024-01-01T10:00:00Z"}`,
	))

	// 一些内容
	stream.Write(buildCompliantEventFrame(
		parser.MessageTypes.EVENT,
		parser.EventTypes.COMPLETION,
		"application/json",
		`{"content":"Processing..."}`,
	))

	// 会话结束
	stream.Write(buildCompliantEventFrame(
		parser.MessageTypes.EVENT,
		parser.EventTypes.SESSION_END,
		"application/json",
		`{"sessionId":"session-123","duration":30000}`,
	))

	result, err := eventParser.ParseResponse(stream.Bytes())
	if err != nil {
		t.Fatalf("解析会话管理失败: %v", err)
	}

	// 验证会话信息
	sessionInfo := result.SessionInfo
	if sessionInfo.SessionID != "session-123" {
		t.Errorf("会话ID错误，期望session-123，实际%s", sessionInfo.SessionID)
	}

	// 计算持续时间
	var duration int64
	if sessionInfo.EndTime != nil && !sessionInfo.StartTime.IsZero() {
		duration = sessionInfo.EndTime.Sub(sessionInfo.StartTime).Milliseconds()
	}

	if duration == 0 {
		t.Error("会话持续时间应该大于0")
	}

	if sessionInfo.StartTime.IsZero() {
		t.Error("会话开始时间不应该为零值")
	}

	if sessionInfo.EndTime == nil || sessionInfo.EndTime.IsZero() {
		t.Error("会话结束时间不应该为零值")
	}

	t.Logf("会话管理验证通过: ID=%s, 持续时间=%dms",
		sessionInfo.SessionID, duration)
}

func BenchmarkCompliantEventStreamParser(b *testing.B) {
	benchParser := parser.NewCompliantEventStreamParser(false)

	// 准备测试数据
	testData := buildCompliantEventFrame(
		parser.MessageTypes.EVENT,
		parser.EventTypes.COMPLETION,
		"application/json",
		`{"content":"This is a benchmark test message with some content"}`,
	)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := benchParser.ParseStream(testData)
		if err != nil {
			b.Fatalf("解析失败: %v", err)
		}
		benchParser.Reset() // 重置以供下次测试
	}
}
