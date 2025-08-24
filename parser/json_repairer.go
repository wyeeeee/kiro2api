package parser

import (
	"strings"
)

// JSONRepairer JSON修复器，负责修复和重建损坏的JSON内容
type JSONRepairer struct{}

// NewJSONRepairer 创建新的JSON修复器实例
func NewJSONRepairer() *JSONRepairer {
	return &JSONRepairer{}
}

// IntelligentJSONFix 智能修复不完整的JSON
func (jr *JSONRepairer) IntelligentJSONFix(content, toolName string) string {
	fixed := content

	// 1. 修复截断的中文字符和键名
	fixed = jr.FixTruncatedChineseCharacters(fixed)

	// 2. 修复损坏的JSON结构
	fixed = jr.FixBrokenJSONStructure(fixed, toolName)

	// 3. 处理截断的键名
	if strings.Contains(fixed, "\"file_pa") && !strings.Contains(fixed, "\"file_path\"") {
		fixed = strings.Replace(fixed, "\"file_pa", "\"file_path", 1)
	}
	if strings.Contains(fixed, "\"relative_pa") && !strings.Contains(fixed, "\"relative_path\"") {
		fixed = strings.Replace(fixed, "\"relative_pa", "\"relative_path", 1)
	}
	if strings.Contains(fixed, "\"comm") && !strings.Contains(fixed, "\"command\"") && strings.ToLower(toolName) == "bash" {
		fixed = strings.Replace(fixed, "\"comm", "\"command", 1)
	}

	// 4. 计算括号和引号的平衡
	braceCount := strings.Count(fixed, "{") - strings.Count(fixed, "}")
	quoteCount := strings.Count(fixed, "\"")

	// 5. 处理未闭合的字符串值
	lastColonIdx := strings.LastIndex(fixed, ":")
	if lastColonIdx > 0 {
		afterColon := strings.TrimSpace(fixed[lastColonIdx+1:])
		// 如果值以引号开始但没有结束引号
		if strings.HasPrefix(afterColon, "\"") && !strings.HasSuffix(afterColon, "\"") && !strings.HasSuffix(afterColon, "}") {
			fixed += "\""
			quoteCount++
		}
	}

	// 6. 如果引号数量为奇数，补全最后一个引号
	if quoteCount%2 == 1 {
		fixed += "\""
	}

	// 7. 补全缺失的右括号
	for i := 0; i < braceCount; i++ {
		fixed += "}"
	}

	return fixed
}

// FixTruncatedChineseCharacters 修复截断的中文字符
func (jr *JSONRepairer) FixTruncatedChineseCharacters(content string) string {
	// 移除硬编码的中文字符截断模式修复
	// 现在依赖通用的UTF-8完整性检查机制
	return content
}

// FixBrokenJSONStructure 修复损坏的JSON结构
func (jr *JSONRepairer) FixBrokenJSONStructure(content, toolName string) string {
	fixed := content

	// 通用的JSON结构修复，移除硬编码的特定内容模式
	// 修复缺失引号的常见模式
	fixed = strings.ReplaceAll(fixed, "}content\":", "\"content\":")

	// 针对TodoWrite工具的通用结构修复
	if strings.ToLower(toolName) == "todowrite" {
		// 修复todos数组结构
		if strings.Contains(fixed, "\"todos\"") && !strings.Contains(fixed, "[{") {
			// 确保todos后面有正确的数组开始
			fixed = strings.Replace(fixed, "\"todos\":", "\"todos\":[", 1)
		}

		// 修复对象分隔符
		fixed = strings.ReplaceAll(fixed, "}content\":", "},{\"content\":")
		fixed = strings.ReplaceAll(fixed, "}\"content\":", "},{\"content\":")

		// 修复数组结束
		if strings.Contains(fixed, "\"todos\":[") && !strings.HasSuffix(strings.TrimSpace(fixed), "]}") && !strings.HasSuffix(strings.TrimSpace(fixed), "}]") {
			// 确保数组正确结束
			if strings.HasSuffix(strings.TrimSpace(fixed), "}") {
				fixed = strings.TrimSpace(fixed) + "]"
			}
		}
	}

	return fixed
}

// ExtractAndRebuildJSON 从损坏的内容中提取键值对并重建JSON
func (jr *JSONRepairer) ExtractAndRebuildJSON(content, toolName string) map[string]interface{} {
	result := make(map[string]interface{})

	// 尝试提取路径
	if path := jr.ExtractPath(content); path != "" {
		// 根据工具类型设置正确的键名
		if strings.HasPrefix(strings.ToLower(toolName), "mcp__") {
			result["relative_path"] = path
			// MCP工具的额外参数
			parts := strings.Split(toolName, "__")
			if len(parts) >= 3 {
				actualToolName := parts[2]
				switch strings.ToLower(actualToolName) {
				case "list_dir":
					result["recursive"] = false
				case "find_file":
					result["file_mask"] = "*"
				}
			}
		} else {
			switch strings.ToLower(toolName) {
			case "read":
				result["file_path"] = path
			case "write":
				result["file_path"] = path
				result["content"] = ""
			case "ls":
				result["path"] = path
			default:
				result["path"] = path
			}
		}
		return result
	}

	// 尝试提取命令（针对bash工具）
	if strings.ToLower(toolName) == "bash" {
		if cmd := jr.ExtractCommand(content); cmd != "" {
			result["command"] = cmd
			return result
		}
	}

	return nil
}

// ExtractPath 从内容中提取路径
func (jr *JSONRepairer) ExtractPath(content string) string {
	// 查找绝对路径模式
	if idx := strings.Index(content, "/Users/"); idx >= 0 {
		path := content[idx:]
		// 找到路径的结束位置
		endIdx := strings.IndexAny(path, "\",}")
		if endIdx > 0 {
			path = path[:endIdx]
		}
		return path
	}

	// 查找相对路径模式
	if idx := strings.Index(content, "./"); idx >= 0 {
		path := content[idx:]
		endIdx := strings.IndexAny(path, "\",}")
		if endIdx > 0 {
			path = path[:endIdx]
		}
		return path
	}

	return ""
}

// ExtractCommand 从内容中提取命令
func (jr *JSONRepairer) ExtractCommand(content string) string {
	// 查找命令模式
	patterns := []string{
		"go build",
		"go test",
		"go run",
		"./kiro2api",
		"curl",
		"ls",
		"mkdir",
		"rm",
		"mv",
		"cp",
	}

	for _, pattern := range patterns {
		if strings.Contains(content, pattern) {
			// 提取包含该命令的完整行
			lines := strings.Split(content, "\n")
			for _, line := range lines {
				if strings.Contains(line, pattern) {
					// 清理引号和其他符号
					cmd := strings.Trim(line, "\"}{,")
					cmd = strings.TrimSpace(cmd)
					return cmd
				}
			}
		}
	}

	return ""
}
