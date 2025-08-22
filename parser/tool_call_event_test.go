package parser

import (
	"encoding/json"
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

// BenchmarkIsToolCallEvent 性能基准测试
func BenchmarkIsToolCallEvent(b *testing.B) {
	payload := []byte(`{"name":"TodoWrite","toolUseId":"tooluse_123","input":"{}","stop":false}`)
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		isToolCallEvent(payload)
	}
}