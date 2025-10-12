package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestTextAggregator_ChinesePunctuationFlush 测试中文标点触发冲刷
func TestTextAggregator_ChinesePunctuationFlush(t *testing.T) {
	testCases := []struct {
		name string
		text string
	}{
		{"中文句号", "好。"},
		{"中文感叹号", "是！"},
		{"中文问号", "吗？"},
		{"中文分号", "啊；"},
		{"中文冒号", "查："},
		{"中文逗号", "嗯，"},
		{"中文顿号", "对、"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			aggregator := NewTextAggregator()
			aggregator.AddText(0, tc.text)
			
			assert.True(t, aggregator.ShouldFlush(), 
				"文本'%s'应触发冲刷", tc.text)
		})
	}
}

// TestTextAggregator_EnglishPunctuationFlush 测试英文标点触发冲刷
func TestTextAggregator_EnglishPunctuationFlush(t *testing.T) {
	testCases := []struct {
		name string
		text string
	}{
		{"英文句号", "Ok."},
		{"英文感叹号", "Yes!"},
		{"英文问号", "Really?"},
		{"英文分号", "Wait;"},
		{"英文冒号", "Check:"},
		{"英文逗号", "Well,"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			aggregator := NewTextAggregator()
			aggregator.AddText(0, tc.text)
			
			assert.True(t, aggregator.ShouldFlush(), 
				"文本'%s'应触发冲刷", tc.text)
		})
	}
}

// TestTextAggregator_MinLengthFlush 测试最小长度触发冲刷
func TestTextAggregator_MinLengthFlush(t *testing.T) {
	testCases := []struct {
		name        string
		text        string
		shouldFlush bool
	}{
		{"6字节刚好达标", "测试ab", true},   // 4+2=6字节
		{"低于阈值", "测ab", false},        // 3+2=5字节
		{"7字节超过阈值", "测试abc", true}, // 4+3=7字节
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			aggregator := NewTextAggregator()
			aggregator.AddText(0, tc.text)
			
			assert.Equal(t, tc.shouldFlush, aggregator.ShouldFlush(), 
				"文本'%s'(%d字节)冲刷判断不符合预期", tc.text, len(tc.text))
		})
	}
}

// TestTextAggregator_MaxLengthFlush 测试最大长度强制冲刷
func TestTextAggregator_MaxLengthFlush(t *testing.T) {
	aggregator := NewTextAggregator()
	
	// 构造64字节的文本（无标点）
	longText := "这是一段很长的文本用于测试最大长度限制这段文本故意不包含任何标点符号来确保只有长度阈值被触发"
	aggregator.AddText(0, longText)
	
	assert.True(t, aggregator.ShouldFlush(), 
		"超过最大长度的文本应强制冲刷")
}

// TestTextAggregator_IndexTracking 测试索引跟踪
func TestTextAggregator_IndexTracking(t *testing.T) {
	aggregator := NewTextAggregator()
	
	// 添加到索引0
	aggregator.AddText(0, "文本0")
	event0, ok := aggregator.Flush()
	assert.True(t, ok)
	assert.Equal(t, 0, event0["index"])
	
	// 添加到索引2
	aggregator.AddText(2, "文本2：")
	event2, ok := aggregator.Flush()
	assert.True(t, ok)
	assert.Equal(t, 2, event2["index"])
}

// TestTextAggregator_FlushClearsState 测试冲刷清空状态
func TestTextAggregator_FlushClearsState(t *testing.T) {
	aggregator := NewTextAggregator()
	
	aggregator.AddText(0, "测试文本。")
	assert.True(t, aggregator.hasPending)
	
	_, ok := aggregator.Flush()
	assert.True(t, ok)
	
	// 冲刷后状态应该被清空
	assert.False(t, aggregator.hasPending)
	assert.Equal(t, "", aggregator.pendingText)
	assert.Equal(t, 0, aggregator.GetPendingTextLength())
}

// TestTextAggregator_DuplicateDetection 测试重复检测
func TestTextAggregator_DuplicateDetection(t *testing.T) {
	aggregator := NewTextAggregator()
	
	// 第一次发送
	aggregator.AddText(0, "重复文本。")
	event1, ok := aggregator.Flush()
	assert.True(t, ok)
	assert.NotNil(t, event1)
	
	// 第二次发送相同文本
	aggregator.AddText(0, "重复文本。")
	event2, ok := aggregator.Flush()
	assert.False(t, ok, "重复文本应被过滤")
	assert.Nil(t, event2)
}

// TestTextAggregator_NewlineFlush 测试换行触发冲刷
func TestTextAggregator_NewlineFlush(t *testing.T) {
	aggregator := NewTextAggregator()
	
	aggregator.AddText(0, "第一行\n")
	assert.True(t, aggregator.ShouldFlush(), 
		"包含换行符的文本应触发冲刷")
}

// TestTextAggregator_EmptyTextSkip 测试空文本跳过
func TestTextAggregator_EmptyTextSkip(t *testing.T) {
	aggregator := NewTextAggregator()
	
	aggregator.AddText(0, "")
	assert.False(t, aggregator.ShouldFlush(), 
		"空文本不应触发冲刷")
	
	event, ok := aggregator.Flush()
	assert.False(t, ok, "空文本不应生成事件")
	assert.Nil(t, event)
}

// TestTextAggregator_RealWorldScenario 测试真实场景："查："文本
func TestTextAggregator_RealWorldScenario(t *testing.T) {
	aggregator := NewTextAggregator()
	
	// 模拟debug.log中的实际场景
	aggregator.AddText(0, "查：")
	
	// 应该因为包含冒号而触发冲刷
	assert.True(t, aggregator.ShouldFlush(), 
		"'查：'应因冒号触发冲刷")
	
	event, ok := aggregator.Flush()
	assert.True(t, ok)
	assert.Equal(t, 0, event["index"])
	
	delta, ok := event["delta"].(map[string]any)
	assert.True(t, ok)
	assert.Equal(t, "查：", delta["text"])
}
