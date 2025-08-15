package parser

import (
	"fmt"
	"kiro2api/logger"
	"regexp"
	"strings"
	"time"
)

// XMLToolCall 表示从XML解析出的工具调用
type XMLToolCall struct {
	Name       string
	Parameters map[string]string
}

// ParseXMLToolCalls 从文本中解析XML格式的工具调用
func ParseXMLToolCalls(text string) []XMLToolCall {
	var tools []XMLToolCall
	
	// 匹配 <tool_use>...</tool_use> 块
	toolUsePattern := regexp.MustCompile(`(?s)<tool_use>(.*?)</tool_use>`)
	matches := toolUsePattern.FindAllStringSubmatch(text, -1)
	
	for _, match := range matches {
		if len(match) > 1 {
			toolContent := match[1]
			tool := parseToolContent(toolContent)
			if tool.Name != "" {
				tools = append(tools, tool)
			}
		}
	}
	
	return tools
}

// parseToolContent 解析单个工具的内容
func parseToolContent(content string) XMLToolCall {
	tool := XMLToolCall{
		Parameters: make(map[string]string),
	}
	
	// 提取工具名称
	namePattern := regexp.MustCompile(`<tool_name>(.*?)</tool_name>`)
	if match := namePattern.FindStringSubmatch(content); len(match) > 1 {
		tool.Name = strings.TrimSpace(match[1])
	}
	
	// 提取参数
	paramsPattern := regexp.MustCompile(`(?s)<parameters>(.*?)</parameters>`)
	if match := paramsPattern.FindStringSubmatch(content); len(match) > 1 {
		parseParameters(match[1], tool.Parameters)
	}
	
	return tool
}

// parseParameters 解析参数内容
func parseParameters(content string, params map[string]string) {
	// 通用参数模式，匹配 <key>value</key> 格式
	paramPattern := regexp.MustCompile(`<(\w+)>(.*?)</\w+>`)
	matches := paramPattern.FindAllStringSubmatch(content, -1)
	
	for _, match := range matches {
		if len(match) > 2 {
			key := match[1]
			value := strings.TrimSpace(match[2])
			params[key] = value
		}
	}
}

// ConvertXMLToolsToJSON 将XML工具调用转换为JSON格式
func ConvertXMLToolsToJSON(tools []XMLToolCall) []map[string]interface{} {
	var result []map[string]interface{}
	
	for _, tool := range tools {
		// 根据工具名称映射到标准函数名
		funcName := mapToolName(tool.Name)
		
		// 构建参数对象
		args := make(map[string]interface{})
		
		// 特殊处理Bash命令
		if funcName == "Bash" && len(tool.Parameters) > 0 {
			// 如果有path参数，转换为mkdir命令
			if path, hasPath := tool.Parameters["path"]; hasPath {
				args["command"] = fmt.Sprintf("mkdir -p %s", path)
			} else {
				// 其他Bash参数直接映射
				for k, v := range tool.Parameters {
					mappedKey := mapParameterName(funcName, k)
					args[mappedKey] = v
				}
			}
		} else {
			// 其他工具的参数映射
			for k, v := range tool.Parameters {
				mappedKey := mapParameterName(funcName, k)
				args[mappedKey] = v
			}
		}
		
		// 生成唯一的tool_use_id
		toolID := fmt.Sprintf("toolu_%s", generateRandomID())
		
		toolCall := map[string]interface{}{
			"id":   toolID,
			"type": "tool_use",
			"name": funcName,
			"input": args,
		}
		
		result = append(result, toolCall)
		
		logger.Debug("转换XML工具调用为JSON",
			logger.String("xml_name", tool.Name),
			logger.String("func_name", funcName),
			logger.String("tool_id", toolID),
			logger.Any("parameters", args))
	}
	
	return result
}

// mapToolName 映射XML工具名到标准函数名
func mapToolName(xmlName string) string {
	// XML中已经是标准名称，直接返回
	switch xmlName {
	case "Bash", "Write", "Read", "Edit", "MultiEdit":
		return xmlName
	case "create_folder":
		return "Bash"
	case "create_file":
		return "Write"
	default:
		return xmlName
	}
}

// mapParameterName 映射参数名称
func mapParameterName(funcName, paramName string) string {
	switch funcName {
	case "Bash":
		if paramName == "path" {
			// 对于创建文件夹，需要转换为mkdir命令
			return "command"
		}
		return paramName
	case "Write":
		if paramName == "path" {
			return "file_path"
		}
		if paramName == "file_text" {
			return "content"
		}
		return paramName
	}
	return paramName
}

// generateRandomID 生成随机ID
func generateRandomID() string {
	// 简单的ID生成，实际应该使用更复杂的方法
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

// ExtractAndConvertXMLTools 从文本中提取并转换XML工具调用
func ExtractAndConvertXMLTools(text string) (cleanText string, tools []map[string]interface{}) {
	// 提取XML工具调用
	xmlTools := ParseXMLToolCalls(text)
	
	if len(xmlTools) > 0 {
		// 转换为JSON格式
		tools = ConvertXMLToolsToJSON(xmlTools)
		
		// 清理文本，移除XML标记
		cleanText = RemoveXMLToolMarkers(text)
		
		logger.Debug("提取并转换XML工具调用",
			logger.Int("tool_count", len(tools)),
			logger.String("clean_text", cleanText))
	} else {
		cleanText = text
	}
	
	return cleanText, tools
}

// RemoveXMLToolMarkers 移除文本中的XML工具标记
func RemoveXMLToolMarkers(text string) string {
	// 移除所有 <tool_use>...</tool_use> 块
	toolUsePattern := regexp.MustCompile(`(?s)<tool_use>.*?</tool_use>`)
	text = toolUsePattern.ReplaceAllString(text, "")
	
	// 清理多余的空行
	text = regexp.MustCompile(`\n{3,}`).ReplaceAllString(text, "\n\n")
	
	return strings.TrimSpace(text)
}