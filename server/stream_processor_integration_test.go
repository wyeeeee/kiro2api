package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestFlushBeforeContentBlockStart 测试content_block_start前冲刷待处理文本
// 这是对debug.log中2222行错误的回归测试
func TestFlushBeforeContentBlockStart(t *testing.T) {
	// 简化测试：只验证聚合器和flush逻辑
	aggregator := NewTextAggregator()
	
	// 场景1: 模拟debug.log中的问题 - 文本"查："被缓冲
	t.Log("场景1: 添加文本'查：'到aggregator")
	aggregator.AddText(0, "查：")
	
	// 验证1: "查："包含冒号，应触发ShouldFlush()
	assert.True(t, aggregator.ShouldFlush(), 
		"'查：'包含冒号，应立即触发冲刷")
	assert.Equal(t, 6, len(aggregator.pendingText), 
		"'查：'应占用6字节")
	
	// 场景2: 在content_block_start前应该flush
	t.Log("场景2: 模拟content_block_start前的flush")
	event, ok := aggregator.Flush()
	assert.True(t, ok, "应成功冲刷")
	assert.NotNil(t, event, "应返回事件")
	
	// 验证2: flush后aggregator应被清空
	assert.Equal(t, 0, aggregator.GetPendingTextLength(), 
		"flush后aggregator应被清空")
	assert.False(t, aggregator.hasPending, 
		"flush后hasPending应为false")
	
	// 验证3: 返回的事件格式正确
	assert.Equal(t, "content_block_delta", event["type"])
	assert.Equal(t, 0, event["index"])
	delta, ok := event["delta"].(map[string]any)
	assert.True(t, ok)
	assert.Equal(t, "text_delta", delta["type"])
	assert.Equal(t, "查：", delta["text"])
	
	t.Log("✅ 验证通过：文本聚合器在包含标点时正确触发冲刷")
}

// TestVariousPunctuationsFlushScenarios 测试各种标点的冲刷场景
func TestVariousPunctuationsFlushScenarios(t *testing.T) {
	scenarios := []struct {
		name        string
		text        string
		shouldFlush bool
		reason      string
	}{
		{"中文冒号-debug.log案例", "查：", true, "包含中文冒号"},
		{"中文句号", "完成。", true, "包含中文句号"},
		{"英文冒号", "Check:", true, "包含英文冒号"},
		{"英文逗号", "Wait,", true, "包含英文逗号"},
		{"无标点短文本", "测", false, "无标点且未达最小长度(3字节<6)"},
		{"刚好达标", "测试", true, "刚好达到6字节阈值"},
		{"达标长度无标点", "这是测试文本", true, "超过最小长度阈值"},
	}
	
	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			aggregator := NewTextAggregator()
			aggregator.AddText(0, scenario.text)
			
			result := aggregator.ShouldFlush()
			assert.Equal(t, scenario.shouldFlush, result,
				"文本'%s': %s, 期望ShouldFlush()=%v, 实际=%v",
				scenario.text, scenario.reason, scenario.shouldFlush, result)
			
			if scenario.shouldFlush {
				event, ok := aggregator.Flush()
				assert.True(t, ok, "应能成功冲刷")
				assert.NotNil(t, event)
				
				delta := event["delta"].(map[string]any)
				assert.Equal(t, scenario.text, delta["text"],
					"冲刷的文本应与输入一致")
			}
		})
	}
}
