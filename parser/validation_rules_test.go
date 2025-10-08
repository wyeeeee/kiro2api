package parser

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestMessageStartEndRule_Name(t *testing.T) {
	rule := &MessageStartEndRule{}
	assert.Equal(t, "message_start_end", rule.Name())
}

func TestMessageStartEndRule_MessageStart(t *testing.T) {
	rule := &MessageStartEndRule{}
	session := &ValidationSession{}
	event := SSEEvent{Event: "message_start"}

	err := rule.Validate(session, event)

	assert.Nil(t, err)
	assert.True(t, rule.hasMessageStart)
}

func TestMessageStartEndRule_DuplicateMessageStart(t *testing.T) {
	rule := &MessageStartEndRule{hasMessageStart: true}
	session := &ValidationSession{}
	event := SSEEvent{Event: "message_start"}

	err := rule.Validate(session, event)

	assert.NotNil(t, err)
	assert.Equal(t, "duplicate_message_start", err.ErrorType)
}

func TestMessageStartEndRule_MessageStop(t *testing.T) {
	rule := &MessageStartEndRule{hasMessageStart: true}
	session := &ValidationSession{}
	event := SSEEvent{Event: "message_stop"}

	err := rule.Validate(session, event)

	assert.Nil(t, err)
	assert.True(t, rule.hasMessageStop)
}

func TestMessageStartEndRule_MessageStopWithoutStart(t *testing.T) {
	rule := &MessageStartEndRule{}
	session := &ValidationSession{}
	event := SSEEvent{Event: "message_stop"}

	err := rule.Validate(session, event)

	assert.NotNil(t, err)
	assert.Equal(t, "message_stop_without_start", err.ErrorType)
}

func TestMessageStartEndRule_DuplicateMessageStop(t *testing.T) {
	rule := &MessageStartEndRule{hasMessageStart: true, hasMessageStop: true}
	session := &ValidationSession{}
	event := SSEEvent{Event: "message_stop"}

	err := rule.Validate(session, event)

	assert.NotNil(t, err)
	assert.Equal(t, "duplicate_message_stop", err.ErrorType)
}

func TestContentBlockIntegrityRule_Name(t *testing.T) {
	rule := &ContentBlockIntegrityRule{}
	assert.Equal(t, "content_block_integrity", rule.Name())
}

func TestContentBlockIntegrityRule_ContentBlockStart(t *testing.T) {
	rule := &ContentBlockIntegrityRule{}
	session := &ValidationSession{}
	event := SSEEvent{
		Event: "content_block_start",
		Data: map[string]any{
			"index": 0,
			"content_block": map[string]any{
				"type": "text",
			},
		},
	}

	err := rule.Validate(session, event)

	assert.Nil(t, err)
	assert.Equal(t, "text", rule.activeBlocks[0])
}

func TestContentBlockIntegrityRule_ToolUseIncrementsCount(t *testing.T) {
	rule := &ContentBlockIntegrityRule{}
	session := &ValidationSession{}
	event := SSEEvent{
		Event: "content_block_start",
		Data: map[string]any{
			"index": 0,
			"content_block": map[string]any{
				"type": "tool_use",
				"id":   "toolu_123",
				"name": "calculator",
			},
		},
	}

	err := rule.Validate(session, event)

	assert.Nil(t, err)
	assert.Equal(t, 1, session.toolCallCount)
}

func TestContentBlockIntegrityRule_ContentBlockStop(t *testing.T) {
	rule := &ContentBlockIntegrityRule{
		activeBlocks:    map[int]string{0: "text"},
		completedBlocks: make(map[int]bool),
	}
	session := &ValidationSession{}
	event := SSEEvent{
		Event: "content_block_stop",
		Data: map[string]any{
			"index": 0,
		},
	}

	err := rule.Validate(session, event)

	assert.Nil(t, err)
	assert.True(t, rule.completedBlocks[0])
}

func TestContentBlockIntegrityRule_ContentBlockStopWithoutStart(t *testing.T) {
	rule := &ContentBlockIntegrityRule{}
	session := &ValidationSession{}
	event := SSEEvent{
		Event: "content_block_stop",
		Data: map[string]any{
			"index": 0,
		},
	}

	err := rule.Validate(session, event)

	assert.NotNil(t, err)
	assert.Equal(t, "content_block_stop_without_start", err.ErrorType)
}

func TestContentBlockIntegrityRule_ContentBlockDelta(t *testing.T) {
	rule := &ContentBlockIntegrityRule{
		activeBlocks:    map[int]string{0: "text"},
		completedBlocks: make(map[int]bool),
	}
	session := &ValidationSession{}
	event := SSEEvent{
		Event: "content_block_delta",
		Data: map[string]any{
			"index": 0,
			"delta": map[string]any{
				"type": "text_delta",
				"text": "Hello",
			},
		},
	}

	err := rule.Validate(session, event)

	assert.Nil(t, err)
	assert.Equal(t, 5, session.textContentLength)
}

func TestToolExecutionFlowRule_Name(t *testing.T) {
	rule := &ToolExecutionFlowRule{}
	assert.Equal(t, "tool_execution_flow", rule.Name())
}

func TestToolExecutionFlowRule_ToolUseStart(t *testing.T) {
	rule := &ToolExecutionFlowRule{}
	session := &ValidationSession{}
	event := SSEEvent{
		Event: "content_block_start",
		Data: map[string]any{
			"index": 0,
			"content_block": map[string]any{
				"type": "tool_use",
				"id":   "toolu_123",
				"name": "calculator",
			},
		},
	}

	err := rule.Validate(session, event)

	assert.Nil(t, err)
	assert.NotNil(t, rule.toolExecutions["toolu_123"])
	assert.True(t, rule.toolExecutions["toolu_123"].hasStart)
	assert.Equal(t, "calculator", rule.toolExecutions["toolu_123"].toolName)
}

func TestStreamingTimeoutRule_Name(t *testing.T) {
	rule := &StreamingTimeoutRule{}
	assert.Equal(t, "streaming_timeout", rule.Name())
}

func TestStreamingTimeoutRule_FirstEvent(t *testing.T) {
	rule := &StreamingTimeoutRule{timeout: 10 * time.Second}
	session := &ValidationSession{}
	event := SSEEvent{Event: "message_start"}

	err := rule.Validate(session, event)

	assert.Nil(t, err)
	assert.False(t, rule.lastEvent.IsZero())
}

func TestStreamingTimeoutRule_WithinTimeout(t *testing.T) {
	rule := &StreamingTimeoutRule{
		timeout:   10 * time.Second,
		lastEvent: time.Now().Add(-5 * time.Second),
	}
	session := &ValidationSession{}
	event := SSEEvent{Event: "content_block_delta"}

	err := rule.Validate(session, event)

	assert.Nil(t, err)
}

func TestStreamingTimeoutRule_ExceedTimeout(t *testing.T) {
	rule := &StreamingTimeoutRule{
		timeout:   1 * time.Second,
		lastEvent: time.Now().Add(-2 * time.Second),
	}
	session := &ValidationSession{}
	event := SSEEvent{Event: "content_block_delta"}

	err := rule.Validate(session, event)

	assert.NotNil(t, err)
	assert.Equal(t, "streaming_timeout", err.ErrorType)
}

func TestDuplicateEventRule_Name(t *testing.T) {
	rule := &DuplicateEventRule{}
	assert.Equal(t, "duplicate_event", rule.Name())
}

func TestDuplicateEventRule_GenerateDataHash(t *testing.T) {
	rule := &DuplicateEventRule{}
	data := map[string]any{
		"index": 0,
		"type":  "text",
		"other": "ignored",
	}

	hash := rule.generateDataHash(data)

	assert.NotEmpty(t, hash)
	assert.Contains(t, hash, "index:0")
	assert.Contains(t, hash, "type:text")
}

func TestDuplicateEventRule_FirstEvent(t *testing.T) {
	rule := &DuplicateEventRule{}
	session := &ValidationSession{}
	event := SSEEvent{
		Event: "message_start",
		Data:  map[string]any{},
	}

	err := rule.Validate(session, event)

	assert.Nil(t, err)
	assert.Len(t, rule.eventHistory, 1)
}

func TestDuplicateEventRule_HistoryLimit(t *testing.T) {
	rule := &DuplicateEventRule{maxHistory: 5}
	session := &ValidationSession{}

	// 添加7个事件
	for i := 0; i < 7; i++ {
		event := SSEEvent{
			Event: "content_block_delta",
			Data: map[string]any{
				"index": i,
			},
		}
		rule.Validate(session, event)
		time.Sleep(1 * time.Millisecond) // 避免重复检测
	}

	assert.Len(t, rule.eventHistory, 5)
}
