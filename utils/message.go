package utils

import (
	"fmt"
	"strings"

	"kiro2api/types"

	"github.com/bytedance/sonic"
)

// GetMessageContent 从消息中提取文本内容的辅助函数
func GetMessageContent(content any) (string, error) {
	switch v := content.(type) {
	case types.AnthropicSystemMessage:
		return v.Text, nil
	case string:
		if len(v) == 0 {
			return "answer for user question", nil
		}
		return v, nil
	case []any:
		var texts []string
		for _, block := range v {
			if m, ok := block.(map[string]any); ok {
				var cb types.ContentBlock
				if data, err := sonic.Marshal(m); err == nil {
					if err := sonic.Unmarshal(data, &cb); err == nil {
						switch cb.Type {
						case "tool_result":
							if cb.Content != nil {
								texts = append(texts, *cb.Content)
							}
						case "text":
							if cb.Text != nil {
								texts = append(texts, *cb.Text)
							}
						}
					}
				}
			}
		}
		if len(texts) == 0 {
			return "answer for user question", nil
		}
		return strings.Join(texts, "\n"), nil
	default:
		return "", fmt.Errorf("unsupported content type: %T", v)
	}
}
