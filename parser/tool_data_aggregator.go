package parser

import (
	"kiro2api/logger"
	"kiro2api/utils"
	"strings"
	"sync"
	"time"
)

// ToolDataAggregatorInterface 定义工具数据聚合器接口
type ToolDataAggregatorInterface interface {
	ProcessToolData(toolUseId, name, input string, stop bool, fragmentIndex int) (complete bool, fullInput string)
	CleanupExpiredBuffers(timeout time.Duration)
}

// toolDataBuffer 工具数据缓冲区
type toolDataBuffer struct {
	toolUseId  string
	name       string
	inputParts []string
	lastUpdate time.Time
	isComplete bool
}

// ToolDataAggregator 工具数据聚合器实现
type ToolDataAggregator struct {
	activeTools map[string]*toolDataBuffer
	mu          sync.RWMutex
}

// NewToolDataAggregator 创建新的工具数据聚合器
func NewToolDataAggregator() *ToolDataAggregator {
	return &ToolDataAggregator{
		activeTools: make(map[string]*toolDataBuffer),
	}
}

// ProcessToolData 处理工具数据片段
func (tda *ToolDataAggregator) ProcessToolData(toolUseId, name, input string, stop bool, fragmentIndex int) (complete bool, fullInput string) {
	tda.mu.Lock()
	defer tda.mu.Unlock()

	if tda.activeTools == nil {
		tda.activeTools = make(map[string]*toolDataBuffer)
	}

	// 尝试找到匹配的缓冲区（包括模糊匹配）
	buffer := tda.findMatchingBuffer(toolUseId, name)
	if buffer == nil {
		// 创建新缓冲区时记录调试信息
		logger.Debug("创建新的工具调用缓冲区",
			logger.String("toolUseId", toolUseId),
			logger.String("name", name),
			logger.String("initialInput", input),
			logger.Bool("stop", stop),
			logger.Int("fragmentIndex", fragmentIndex))

		buffer = &toolDataBuffer{
			toolUseId:  toolUseId,
			name:       name,
			inputParts: make([]string, 0),
			lastUpdate: time.Now(),
		}
		tda.activeTools[toolUseId] = buffer
	} else {
		// 检查是否已经完成的工具调用
		if buffer.isComplete {
			logger.Warn("尝试处理已完成的工具调用，跳过",
				logger.String("toolUseId", toolUseId),
				logger.String("name", name),
				logger.Bool("originalComplete", buffer.isComplete))
			return false, ""
		}

		if buffer.toolUseId != toolUseId {
			// 找到了相似的buffer，记录日志
			logger.Debug("使用模糊匹配找到相似的工具调用缓冲区",
				logger.String("originalId", buffer.toolUseId),
				logger.String("currentId", toolUseId),
				logger.String("name", name))
		}
	}

	// 添加新的输入片段（即使是空字符串也记录，用于调试）
	if input != "" {
		buffer.inputParts = append(buffer.inputParts, input)
		logger.Debug("添加工具调用数据片段",
			logger.String("toolUseId", toolUseId),
			logger.String("fragment", input),
			logger.Int("fragmentIndex", fragmentIndex),
			logger.Int("totalParts", len(buffer.inputParts)))
	} else if !stop {
		// 如果input为空且不是stop事件，记录警告
		logger.Warn("收到空的工具调用输入片段",
			logger.String("toolUseId", toolUseId),
			logger.String("name", name),
			logger.Int("fragmentIndex", fragmentIndex))
	}
	buffer.lastUpdate = time.Now()

	// 如果标记为停止，则认为数据完整
	if stop {
		buffer.isComplete = true

		// 使用智能JSON重组逻辑
		fullInput = tda.reconstructJSON(buffer.inputParts)

		// 清理已完成的工具数据（需要清理所有相关的缓冲区）
		tda.cleanupRelatedBuffers(toolUseId, name)

		fullInputPreview := fullInput
		if len(fullInputPreview) > 100 {
			fullInputPreview = fullInputPreview[:100] + "..."
		}

		logger.Debug("工具调用数据聚合完成",
			logger.String("toolUseId", toolUseId),
			logger.String("name", name),
			logger.String("fullInput", fullInputPreview),
			logger.Int("inputParts", len(buffer.inputParts)),
			logger.Int("finalFragmentIndex", fragmentIndex))

		// 额外校验：打印关键字段与长度，辅助定位缺参/路径截断
		if name == "Write" || name == "Bash" {
			var obj map[string]any
			if err := utils.FastUnmarshal([]byte(fullInput), &obj); err == nil {
				switch name {
				case "Write":
					fp, _ := obj["file_path"].(string)
					content, _ := obj["content"].(string)

					logger.Debug("Write聚合校验",
						logger.Int("file_path_len", len(fp)),
						logger.String("file_path", fp),
						logger.Int("content_len", len(content)))
				case "Bash":
					cmd, _ := obj["command"].(string)
					logger.Debug("Bash聚合校验",
						logger.Int("command_len", len(cmd)),
						logger.String("command_preview", func() string {
							if len(cmd) > 64 {
								return cmd[:64] + "..."
							}
							return cmd
						}()))
				}
			} else {
				logger.Warn("聚合后JSON解析失败", logger.Err(err), logger.String("raw", fullInputPreview))
			}
		}

		return true, fullInput
	}

	return false, ""
}

// CleanupExpiredBuffers 清理过期的缓冲区
func (tda *ToolDataAggregator) CleanupExpiredBuffers(timeout time.Duration) {
	tda.mu.Lock()
	defer tda.mu.Unlock()

	now := time.Now()
	for toolUseId, buffer := range tda.activeTools {
		if now.Sub(buffer.lastUpdate) > timeout {
			logger.Warn("清理过期的工具调用缓冲区",
				logger.String("toolUseId", toolUseId),
				logger.Duration("expired", now.Sub(buffer.lastUpdate)),
				logger.Int("lostParts", len(buffer.inputParts)))
			delete(tda.activeTools, toolUseId)
		}
	}
}

// reconstructJSON 重建JSON字符串
func (tda *ToolDataAggregator) reconstructJSON(parts []string) string {
	if len(parts) == 0 {
		return "{}"
	}

	// 连接所有片段
	rawInput := strings.Join(parts, "")

	// 基本清理
	cleaned := strings.TrimSpace(rawInput)

	// 移除可能的控制字符
	cleaned = strings.ReplaceAll(cleaned, "\x00", "")
	cleaned = strings.ReplaceAll(cleaned, "\ufffd", "")

	// 基本括号修复
	if !strings.HasPrefix(cleaned, "{") {
		cleaned = "{" + cleaned
	}
	if !strings.HasSuffix(cleaned, "}") {
		cleaned = cleaned + "}"
	}

	// 验证JSON格式
	var temp any
	if err := utils.FastUnmarshal([]byte(cleaned), &temp); err == nil {
		logger.Debug("JSON重组成功",
			logger.Int("原始长度", len(rawInput)),
			logger.Int("清理后长度", len(cleaned)))
		return cleaned
	}

	// 如果简单修复失败，记录错误并返回空对象
	logger.Warn("工具参数JSON格式无效，使用空参数",
		logger.String("原始数据", func() string {
			if len(rawInput) > 200 {
				return rawInput[:200] + "..."
			}
			return rawInput
		}()))

	return "{}"
}

// cleanupRelatedBuffers 清理相关的缓冲区
func (tda *ToolDataAggregator) cleanupRelatedBuffers(toolUseId, _ string) {
	// 清理精确匹配的缓冲区
	delete(tda.activeTools, toolUseId)

	// 不再清理相似的缓冲区，仅清理精确匹配的
	// 已废弃模糊匹配逻辑，避免数据损坏
}

// findMatchingBuffer 查找匹配的缓冲区
func (tda *ToolDataAggregator) findMatchingBuffer(toolUseId, _ string) *toolDataBuffer {
	// 只使用精确匹配，避免模糊匹配导致的数据损坏
	if buffer, exists := tda.activeTools[toolUseId]; exists {
		return buffer
	}

	// 不再使用模糊匹配，返回nil让调用方创建新缓冲区
	return nil
}
