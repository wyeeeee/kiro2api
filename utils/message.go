package utils

import (
	"strings"

	"kiro2api/types"

	"github.com/bytedance/sonic"
)

// GetMessageContent 从消息中提取文本内容的辅助函数
func GetMessageContent(content any) string {
	switch v := content.(type) {
	case string:
		if len(v) == 0 {
			return "answer for user question"
		}
		return v
	case []any:
		var texts []string
		for _, block := range v {
			if m, ok := block.(map[string]any); ok {
				var cb types.ContentBlock
				if data, err := sonic.Marshal(m); err == nil {
					if err := sonic.Unmarshal(data, &cb); err == nil {
						switch cb.Type {
						case "tool_result":
							texts = append(texts, *cb.Content)
						case "text":
							texts = append(texts, *cb.Text)
						}
					}
				}
			}
		}
		if len(texts) == 0 {
			return "answer for user question"
		}
		return strings.Join(texts, "\n")
	default:
		return "answer for user question"
	}
}
