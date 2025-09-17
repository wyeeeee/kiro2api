package server

import (
	"fmt"
	"kiro2api/logger"

	"github.com/gin-gonic/gin"
)

// BlockState 内容块状态
type BlockState struct {
	Index     int    `json:"index"`
	Type      string `json:"type"`      // "text" | "tool_use"
	Started   bool   `json:"started"`
	Stopped   bool   `json:"stopped"`
	ToolUseID string `json:"tool_use_id,omitempty"` // 仅用于工具块
}

// SSEStateManager SSE事件状态管理器，确保事件序列符合Claude规范
type SSEStateManager struct {
	messageStarted  bool                `json:"message_started"`
	messageDeltaSent bool               `json:"message_delta_sent"` // 新增：跟踪message_delta是否已发送
	activeBlocks    map[int]*BlockState `json:"active_blocks"`
	messageEnded    bool                `json:"message_ended"`
	nextBlockIndex  int                 `json:"next_block_index"`
	strictMode      bool                `json:"strict_mode"`
}

// NewSSEStateManager 创建SSE状态管理器
func NewSSEStateManager(strictMode bool) *SSEStateManager {
	return &SSEStateManager{
		activeBlocks: make(map[int]*BlockState),
		strictMode:   strictMode,
	}
}

// Reset 重置状态管理器
func (ssm *SSEStateManager) Reset() {
	ssm.messageStarted = false
	ssm.messageDeltaSent = false // 重置message_delta发送状态
	ssm.messageEnded = false
	ssm.activeBlocks = make(map[int]*BlockState)
	ssm.nextBlockIndex = 0
}

// SendEvent 受控的事件发送，确保符合Claude规范
func (ssm *SSEStateManager) SendEvent(c *gin.Context, sender StreamEventSender, eventData map[string]any) error {
	eventType, ok := eventData["type"].(string)
	if !ok {
		return fmt.Errorf("无效的事件类型")
	}

	// 记录事件发送
	logger.Debug("发送SSE事件",
		logger.String("event", eventType),
		logger.Bool("message_started", ssm.messageStarted),
		logger.Bool("message_ended", ssm.messageEnded),
		logger.Int("active_blocks", len(ssm.activeBlocks)))

	// 状态验证和处理
	switch eventType {
	case "message_start":
		return ssm.handleMessageStart(c, sender, eventData)
	case "content_block_start":
		return ssm.handleContentBlockStart(c, sender, eventData)
	case "content_block_delta":
		return ssm.handleContentBlockDelta(c, sender, eventData)
	case "content_block_stop":
		return ssm.handleContentBlockStop(c, sender, eventData)
	case "message_delta":
		return ssm.handleMessageDelta(c, sender, eventData)
	case "message_stop":
		return ssm.handleMessageStop(c, sender, eventData)
	default:
		// 其他事件直接转发
		return sender.SendEvent(c, eventData)
	}
}

// handleMessageStart 处理消息开始事件
func (ssm *SSEStateManager) handleMessageStart(c *gin.Context, sender StreamEventSender, eventData map[string]any) error {
	if ssm.messageStarted {
		errMsg := "违规：message_start只能出现一次"
		logger.Error(errMsg)
		if ssm.strictMode {
			return fmt.Errorf(errMsg)
		}
		return nil // 非严格模式下跳过重复的message_start
	}

	ssm.messageStarted = true
	return sender.SendEvent(c, eventData)
}

// handleContentBlockStart 处理内容块开始事件
func (ssm *SSEStateManager) handleContentBlockStart(c *gin.Context, sender StreamEventSender, eventData map[string]any) error {
	if !ssm.messageStarted {
		errMsg := "违规：content_block_start必须在message_start之后"
		logger.Error(errMsg)
		if ssm.strictMode {
			return fmt.Errorf(errMsg)
		}
	}

	if ssm.messageEnded {
		errMsg := "违规：message已结束，不能发送content_block_start"
		logger.Error(errMsg)
		if ssm.strictMode {
			return fmt.Errorf(errMsg)
		}
		return nil
	}

	// 提取块索引
	index, ok := eventData["index"].(int)
	if !ok {
		if indexFloat, ok := eventData["index"].(float64); ok {
			index = int(indexFloat)
		} else {
			index = ssm.nextBlockIndex
		}
	}

	// 检查是否重复启动同一块
	if block, exists := ssm.activeBlocks[index]; exists && block.Started && !block.Stopped {
		errMsg := fmt.Sprintf("违规：索引%d的content_block已经started但未stopped", index)
		logger.Error(errMsg, logger.Int("block_index", index))
		if ssm.strictMode {
			return fmt.Errorf(errMsg)
		}
		return nil // 跳过重复的start
	}

	// 确定块类型
	blockType := "text"
	if contentBlock, ok := eventData["content_block"].(map[string]any); ok {
		if cbType, ok := contentBlock["type"].(string); ok {
			blockType = cbType
		}
	}

	// 创建或更新块状态
	toolUseID := ""
	if blockType == "tool_use" {
		if contentBlock, ok := eventData["content_block"].(map[string]any); ok {
			if id, ok := contentBlock["id"].(string); ok {
				toolUseID = id
			}
		}
	}

	ssm.activeBlocks[index] = &BlockState{
		Index:     index,
		Type:      blockType,
		Started:   true,
		Stopped:   false,
		ToolUseID: toolUseID,
	}

	if index >= ssm.nextBlockIndex {
		ssm.nextBlockIndex = index + 1
	}

	logger.Debug("内容块已启动",
		logger.Int("index", index),
		logger.String("type", blockType),
		logger.String("tool_use_id", toolUseID))

	return sender.SendEvent(c, eventData)
}

// handleContentBlockDelta 处理内容块增量事件
func (ssm *SSEStateManager) handleContentBlockDelta(c *gin.Context, sender StreamEventSender, eventData map[string]any) error {
	index, ok := eventData["index"].(int)
	if !ok {
		if indexFloat, ok := eventData["index"].(float64); ok {
			index = int(indexFloat)
		} else {
			errMsg := "content_block_delta缺少有效索引"
			logger.Error(errMsg)
			if ssm.strictMode {
				return fmt.Errorf(errMsg)
			}
			return nil
		}
	}

	// 检查块是否已启动，如果没有则自动启动（遵循Claude规范的动态启动）
	block, exists := ssm.activeBlocks[index]
	if !exists || !block.Started {
		logger.Debug("检测到content_block_delta但块未启动，自动生成content_block_start",
			logger.Int("block_index", index))

		// 推断块类型：检查delta内容来确定类型
		blockType := "text" // 默认为文本块
		if delta, ok := eventData["delta"].(map[string]any); ok {
			if deltaType, ok := delta["type"].(string); ok {
				if deltaType == "input_json_delta" {
					blockType = "tool_use"
				}
			}
		}

		// 自动生成并发送content_block_start事件
		startEvent := map[string]any{
			"type":  "content_block_start",
			"index": index,
			"content_block": map[string]any{
				"type": blockType,
			},
		}

		if blockType == "text" {
			startEvent["content_block"].(map[string]any)["text"] = ""
		} else if blockType == "tool_use" {
			// 为工具使用块添加必要字段
			startEvent["content_block"].(map[string]any)["id"] = fmt.Sprintf("tooluse_auto_%d", index)
			startEvent["content_block"].(map[string]any)["name"] = "auto_detected"
			startEvent["content_block"].(map[string]any)["input"] = map[string]any{}
		}

		// 先处理start事件来更新状态
		if err := ssm.handleContentBlockStart(c, sender, startEvent); err != nil {
			return err
		}

		// 重新获取更新后的block状态
		block = ssm.activeBlocks[index]
	}

	if block != nil && block.Stopped {
		errMsg := fmt.Sprintf("违规：索引%d的content_block已停止，不能发送delta", index)
		logger.Error(errMsg, logger.Int("block_index", index))
		if ssm.strictMode {
			return fmt.Errorf(errMsg)
		}
		return nil
	}

	return sender.SendEvent(c, eventData)
}

// handleContentBlockStop 处理内容块停止事件
func (ssm *SSEStateManager) handleContentBlockStop(c *gin.Context, sender StreamEventSender, eventData map[string]any) error {
	index, ok := eventData["index"].(int)
	if !ok {
		if indexFloat, ok := eventData["index"].(float64); ok {
			index = int(indexFloat)
		} else {
			errMsg := "content_block_stop缺少有效索引"
			logger.Error(errMsg)
			if ssm.strictMode {
				return fmt.Errorf(errMsg)
			}
			return nil
		}
	}

	// 验证块状态
	block, exists := ssm.activeBlocks[index]
	if !exists || !block.Started {
		errMsg := fmt.Sprintf("违规：索引%d的content_block未启动就发送stop", index)
		logger.Error(errMsg, logger.Int("block_index", index))
		if ssm.strictMode {
			return fmt.Errorf(errMsg)
		}
		return nil
	}

	if block.Stopped {
		errMsg := fmt.Sprintf("违规：索引%d的content_block重复停止", index)
		logger.Error(errMsg, logger.Int("block_index", index))
		if ssm.strictMode {
			return fmt.Errorf(errMsg)
		}
		return nil
	}

	// 标记为已停止
	block.Stopped = true

	logger.Debug("内容块已停止",
		logger.Int("index", index),
		logger.String("type", block.Type))

	return sender.SendEvent(c, eventData)
}

// handleMessageDelta 处理消息增量事件
func (ssm *SSEStateManager) handleMessageDelta(c *gin.Context, sender StreamEventSender, eventData map[string]any) error {
	if !ssm.messageStarted {
		errMsg := "违规：message_delta必须在message_start之后"
		logger.Error(errMsg)
		if ssm.strictMode {
			return fmt.Errorf(errMsg)
		}
	}

	// *** 关键修复：防止重复的message_delta事件 ***
	// 根据Claude规范，message_delta在一次消息中只能出现一次
	if ssm.messageDeltaSent {
		errMsg := "违规：message_delta只能出现一次"
		logger.Error(errMsg,
			logger.Bool("message_started", ssm.messageStarted),
			logger.Bool("message_delta_sent", ssm.messageDeltaSent),
			logger.Bool("message_ended", ssm.messageEnded))
		if ssm.strictMode {
			return fmt.Errorf(errMsg)
		}
		logger.Debug("跳过重复的message_delta事件")
		return nil // 非严格模式下跳过重复的message_delta
	}

	// *** 关键修复：在发送message_delta之前，确保所有content_block都已关闭 ***
	// 根据Claude规范，message_delta必须在所有content_block_stop之后发送
	var unclosedBlocks []int
	for index, block := range ssm.activeBlocks {
		if block.Started && !block.Stopped {
			unclosedBlocks = append(unclosedBlocks, index)
		}
	}

	if len(unclosedBlocks) > 0 {
		logger.Debug("message_delta前自动关闭未关闭的content_block",
			logger.Any("unclosed_blocks", unclosedBlocks))
		// 在非严格模式下，自动关闭未关闭的块
		if !ssm.strictMode {
			for _, index := range unclosedBlocks {
				stopEvent := map[string]any{
					"type":  "content_block_stop",
					"index": index,
				}
				sender.SendEvent(c, stopEvent)
				ssm.activeBlocks[index].Stopped = true
				logger.Debug("自动关闭未关闭的content_block（message_delta前）", logger.Int("index", index))
			}
		}
	}

	// 标记message_delta已发送，防止后续重复发送
	ssm.messageDeltaSent = true
	logger.Debug("message_delta事件已发送", logger.Bool("message_delta_sent", ssm.messageDeltaSent))

	return sender.SendEvent(c, eventData)
}

// handleMessageStop 处理消息停止事件
func (ssm *SSEStateManager) handleMessageStop(c *gin.Context, sender StreamEventSender, eventData map[string]any) error {
	if !ssm.messageStarted {
		errMsg := "违规：message_stop必须在message_start之后"
		logger.Error(errMsg)
		if ssm.strictMode {
			return fmt.Errorf(errMsg)
		}
	}

	if ssm.messageEnded {
		errMsg := "违规：message_stop只能出现一次"
		logger.Error(errMsg)
		if ssm.strictMode {
			return fmt.Errorf(errMsg)
		}
		return nil
	}

	// 注意：未关闭的content_block检查已移至handleMessageDelta中
	// 确保符合Claude规范：所有content_block_stop必须在message_delta之前发送

	ssm.messageEnded = true
	return sender.SendEvent(c, eventData)
}

// GetBlockState 获取指定索引的块状态
func (ssm *SSEStateManager) GetBlockState(index int) *BlockState {
	return ssm.activeBlocks[index]
}

// GetActiveBlocks 获取所有活跃块
func (ssm *SSEStateManager) GetActiveBlocks() map[int]*BlockState {
	return ssm.activeBlocks
}

// GetNextBlockIndex 获取下一个可用的块索引
func (ssm *SSEStateManager) GetNextBlockIndex() int {
	return ssm.nextBlockIndex
}

// IsMessageStarted 检查消息是否已开始
func (ssm *SSEStateManager) IsMessageStarted() bool {
	return ssm.messageStarted
}

// IsMessageEnded 检查消息是否已结束
func (ssm *SSEStateManager) IsMessageEnded() bool {
	return ssm.messageEnded
}

// IsMessageDeltaSent 检查message_delta是否已发送
func (ssm *SSEStateManager) IsMessageDeltaSent() bool {
	return ssm.messageDeltaSent
}