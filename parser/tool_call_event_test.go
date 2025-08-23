package parser

import (
	"encoding/json"
	"os"
	"testing"
)

// TestIsToolCallEvent 测试工具调用事件检测功能
func TestIsToolCallEvent(t *testing.T) {
	tests := []struct {
		name     string
		payload  string
		expected bool
		reason   string
	}{
		{
			name:     "完整工具调用事件",
			payload:  `{"name":"TodoWrite","toolUseId":"tooluse_123","input":"{}","stop":false}`,
			expected: true,
			reason:   "包含所有必要字段",
		},
		{
			name:     "只有toolUseId的事件",
			payload:  `{"toolUseId":"tooluse_123"}`,
			expected: true,
			reason:   "有基本工具字段",
		},
		{
			name:     "只有name的事件",
			payload:  `{"name":"Bash"}`,
			expected: true,
			reason:   "有基本工具字段",
		},
		{
			name:     "只有stop标志",
			payload:  `{"content":"hello","stop":true}`,
			expected: false,
			reason:   "仅有stop字段不足以判断为工具调用",
		},
		{
			name:     "XML工具调用",
			payload:  `{"content":"<tool_use><tool_name>Bash</tool_name></tool_use>"}`,
			expected: true,
			reason:   "包含XML工具标记",
		},
		{
			name:     "普通消息",
			payload:  `{"content":"这是普通文本消息"}`,
			expected: false,
			reason:   "不包含任何工具标记",
		},
		{
			name:     "空载荷",
			payload:  ``,
			expected: false,
			reason:   "空载荷",
		},
		{
			name:     "无效JSON",
			payload:  `{invalid json`,
			expected: false,
			reason:   "无效JSON但不包含工具标记",
		},
		{
			name:     "包含tool_use标记",
			payload:  `{"type":"tool_use","content":"some content"}`,
			expected: true,
			reason:   "包含tool_use标记",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isToolCallEvent([]byte(tt.payload))
			if result != tt.expected {
				t.Errorf("isToolCallEvent() = %v, expected %v, reason: %s, payload: %s", 
					result, tt.expected, tt.reason, tt.payload)
			}
		})
	}
}

// TestLegacyToolUseEventHandler 测试Legacy工具事件处理器
func TestLegacyToolUseEventHandler(t *testing.T) {
	// 创建处理器
	toolManager := NewToolLifecycleManager()
	aggregator := NewSonicStreamingJSONAggregatorWithCallback(nil)
	
	handler := &LegacyToolUseEventHandler{
		toolManager: toolManager,
		aggregator:  aggregator,
	}

	// 测试用例
	tests := []struct {
		name        string
		payload     string
		expectError bool
		expectEvents bool
	}{
		{
			name:        "完整工具事件",
			payload:     `{"name":"TodoWrite","toolUseId":"tooluse_123","input":"{}","stop":true}`,
			expectError: false,
			expectEvents: true,
		},
		{
			name:        "部分工具事件",
			payload:     `{"name":"Bash","toolUseId":"tooluse_456"}`,
			expectError: false,
			expectEvents: true,
		},
		{
			name:        "损坏的JSON",
			payload:     `{"name":"Write"`,
			expectError: false,
			expectEvents: false,
		},
		{
			name:        "无字段事件",
			payload:     `{"content":"hello"}`,
			expectError: false,
			expectEvents: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			message := &EventStreamMessage{
				Payload: []byte(tt.payload),
				Headers: map[string]HeaderValue{
					":event-type": {Value: "toolUseEvent"},
					":message-type": {Value: "event"},
				},
			}

			events, err := handler.handleToolCallEvent(message)

			if tt.expectError && err == nil {
				t.Error("expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if tt.expectEvents && len(events) == 0 {
				t.Error("expected events but got none")
			}
			if !tt.expectEvents && len(events) > 0 {
				t.Errorf("unexpected events: %d", len(events))
			}
		})
	}
}

// TestJSONFix 测试JSON修复功能
func TestJSONFix(t *testing.T) {
	handler := &LegacyToolUseEventHandler{}

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "缺少开始大括号",
			input:    `"file_path": "/test"}`,
			expected: `{"file_path": "/test"}`,
		},
		{
			name:     "缺少结束大括号",
			input:    `{"command": "ls"`,
			expected: `{"command": "ls"}`,
		},
		{
			name:     "包含控制字符",
			input:    "{\x00\"path\": \"test\"\ufffd}",
			expected: `{"path": "test"}`,
		},
		{
			name:     "正常JSON",
			input:    `{"name": "value"}`,
			expected: `{"name": "value"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := handler.attemptJSONFix(tt.input)
			if result != tt.expected {
				t.Errorf("attemptJSONFix() = %q, expected %q", result, tt.expected)
			}

			// 验证修复后的JSON是否有效
			var testObj map[string]interface{}
			if err := json.Unmarshal([]byte(result), &testObj); err != nil {
				t.Errorf("fixed JSON is invalid: %v, result: %s", err, result)
			}
		})
	}
}

// TestExtractField 测试字段提取功能
func TestExtractField(t *testing.T) {
	handler := &LegacyToolUseEventHandler{}

	tests := []struct {
		name      string
		jsonStr   string
		fieldName string
		expected  string
	}{
		{
			name:      "提取toolUseId",
			jsonStr:   `{"name":"Write","toolUseId":"tooluse_123","input":"{}"}`,
			fieldName: "toolUseId",
			expected:  "tooluse_123",
		},
		{
			name:      "提取name",
			jsonStr:   `{"name":"Bash","toolUseId":"tooluse_456"}`,
			fieldName: "name",
			expected:  "Bash",
		},
		{
			name:      "字段不存在",
			jsonStr:   `{"content":"hello"}`,
			fieldName: "toolUseId",
			expected:  "",
		},
		{
			name:      "损坏JSON中提取",
			jsonStr:   `{"name":"Write","toolUseId":"tooluse_789"`,
			fieldName: "name",
			expected:  "Write",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := handler.extractField(tt.jsonStr, tt.fieldName)
			if result != tt.expected {
				t.Errorf("extractField() = %q, expected %q", result, tt.expected)
			}
		})
	}
}

// TestIncrementalToolEventFix 测试工具调用增量事件修复
func TestIncrementalToolEventFix(t *testing.T) {
	// 设置环境变量启用增量事件
	os.Setenv("ENABLE_INCREMENTAL_TOOL_EVENTS", "true")
	defer os.Unsetenv("ENABLE_INCREMENTAL_TOOL_EVENTS")

	// 创建处理器
	toolManager := NewToolLifecycleManager()
	aggregator := NewSonicStreamingJSONAggregatorWithCallback(nil)
	
	handler := &LegacyToolUseEventHandler{
		toolManager: toolManager,
		aggregator:  aggregator,
	}

	t.Run("流式工具调用修复验证", func(t *testing.T) {
		// 第一个片段：注册工具
		firstFragment := &EventStreamMessage{
			Payload: []byte(`{"name":"Write","toolUseId":"tooluse_fix_test","input":"","stop":false}`),
			Headers: map[string]HeaderValue{
				":event-type":   {Value: "toolUseEvent"},
				":message-type": {Value: "event"},
			},
		}

		// 处理第一个片段 - 应该注册工具并返回开始事件
		events1, err := handler.handleToolCallEvent(firstFragment)
		if err != nil {
			t.Fatalf("第一个片段处理失败: %v", err)
		}
		
		if len(events1) == 0 {
			t.Fatal("第一个片段应该返回工具注册事件")
		}
		
		t.Logf("第一个片段事件数量: %d", len(events1))

		// 第二个片段：输入片段 - 修复前会返回空事件，修复后应该返回增量事件
		secondFragment := &EventStreamMessage{
			Payload: []byte(`{"name":"Write","toolUseId":"tooluse_fix_test","input":"{\"file_path\":\"/test","stop":false}`),
			Headers: map[string]HeaderValue{
				":event-type":   {Value: "toolUseEvent"},
				":message-type": {Value: "event"},
			},
		}

		events2, err := handler.handleToolCallEvent(secondFragment)
		if err != nil {
			t.Fatalf("第二个片段处理失败: %v", err)
		}
		
		// 核心验证：修复后应该返回增量事件，而不是空事件
		if len(events2) == 0 {
			t.Error("❌ 修复失败：第二个片段仍然返回空事件，claude-cli将无法接收到增量更新")
		} else {
			t.Logf("✅ 修复成功：第二个片段返回了 %d 个事件", len(events2))
			
			// 验证事件类型
			for i, event := range events2 {
				if event.Event == "content_block_delta" {
					t.Logf("✅ 事件 %d: 正确的增量事件类型 'content_block_delta'", i)
					
					// 验证事件数据结构
					if data, ok := event.Data.(map[string]interface{}); ok {
						if delta, exists := data["delta"]; exists {
							t.Logf("✅ 包含 delta 字段: %+v", delta)
						} else {
							t.Error("❌ 缺少 delta 字段")
						}
					}
				} else {
					t.Logf("事件 %d: 类型 '%s'", i, event.Event)
				}
			}
		}

		// 第三个片段：更多输入
		thirdFragment := &EventStreamMessage{
			Payload: []byte(`{"name":"Write","toolUseId":"tooluse_fix_test","input":".txt\"}","stop":false}`),
			Headers: map[string]HeaderValue{
				":event-type":   {Value: "toolUseEvent"},
				":message-type": {Value: "event"},
			},
		}

		events3, err := handler.handleToolCallEvent(thirdFragment)
		if err != nil {
			t.Fatalf("第三个片段处理失败: %v", err)
		}
		
		if len(events3) > 0 {
			t.Logf("✅ 第三个片段也返回了增量事件: %d", len(events3))
		}

		// 第四个片段：完成
		finalFragment := &EventStreamMessage{
			Payload: []byte(`{"name":"Write","toolUseId":"tooluse_fix_test","input":"","stop":true}`),
			Headers: map[string]HeaderValue{
				":event-type":   {Value: "toolUseEvent"},
				":message-type": {Value: "event"},
			},
		}

		events4, err := handler.handleToolCallEvent(finalFragment)
		if err != nil {
			t.Fatalf("最终片段处理失败: %v", err)
		}
		
		if len(events4) > 0 {
			t.Logf("✅ 最终片段返回了完成事件: %d", len(events4))
		}
	})

	t.Run("配置禁用时的行为", func(t *testing.T) {
		// 禁用增量事件
		os.Setenv("ENABLE_INCREMENTAL_TOOL_EVENTS", "false")
		defer os.Setenv("ENABLE_INCREMENTAL_TOOL_EVENTS", "true")

		// 先注册工具
		firstFragment := &EventStreamMessage{
			Payload: []byte(`{"name":"Write","toolUseId":"tooluse_disabled_test","input":"","stop":false}`),
			Headers: map[string]HeaderValue{
				":event-type":   {Value: "toolUseEvent"},
				":message-type": {Value: "event"},
			},
		}
		handler.handleToolCallEvent(firstFragment)

		// 输入片段 - 配置禁用时应该返回空事件
		inputFragment := &EventStreamMessage{
			Payload: []byte(`{"name":"Write","toolUseId":"tooluse_disabled_test","input":"{\"test\":\"value\"}","stop":false}`),
			Headers: map[string]HeaderValue{
				":event-type":   {Value: "toolUseEvent"},
				":message-type": {Value: "event"},
			},
		}

		events, err := handler.handleToolCallEvent(inputFragment)
		if err != nil {
			t.Fatalf("处理失败: %v", err)
		}
		
		if len(events) != 0 {
			t.Errorf("配置禁用时应该返回空事件，但得到了 %d 个事件", len(events))
		} else {
			t.Log("✅ 配置禁用时正确返回空事件")
		}
	})
}

// BenchmarkIsToolCallEvent 性能基准测试
func BenchmarkIsToolCallEvent(b *testing.B) {
	payload := []byte(`{"name":"TodoWrite","toolUseId":"tooluse_123","input":"{}","stop":false}`)
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		isToolCallEvent(payload)
	}
}