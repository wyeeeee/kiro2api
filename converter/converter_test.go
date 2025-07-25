package converter

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCleanToolParameters(t *testing.T) {
	// 示例OpenAI工具参数，包含$schema等不支持的字段
	params := map[string]any{
		"$schema":    "http://json-schema.org/draft-07/schema#",
		"type":       "object",
		"properties": map[string]any{
			"command": map[string]any{
				"type":        "string",
				"description": "The command to execute.",
			},
		},
		"required": []string{"command"},
	}

	// 调用被测试函数
	cleanedParams := cleanToolParameters(params)

	// 断言：检查不支持的字段是否被移除
	assert.NotContains(t, cleanedParams, "$schema", "不支持的$schema字段应该被移除")

	// 断言：检查保留的字段是否存在
	assert.Contains(t, cleanedParams, "type", "type字段应该被保留")
	assert.Contains(t, cleanedParams, "properties", "properties字段应该被保留")
	assert.Contains(t, cleanedParams, "required", "required字段应该被保留")

	// 断言：检查嵌套的字段是否正确
	properties, ok := cleanedParams["properties"].(map[string]any)
	assert.True(t, ok, "properties字段应该是map[string]any类型")
	assert.Contains(t, properties, "command", "command字段应该被保留")
}
