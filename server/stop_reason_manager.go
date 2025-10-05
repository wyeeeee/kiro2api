package server

import (
	"kiro2api/logger"
	"kiro2api/types"
	"slices"
)

// StopReasonManager 管理符合Claude规范的stop_reason决策
type StopReasonManager struct {
	maxTokens          int
	requestedMaxTokens int
	stopSequences      []string
	hasActiveToolCalls bool
	hasCompletedTools  bool
	actualTokensUsed   int
}

// NewStopReasonManager 创建stop_reason管理器
func NewStopReasonManager(anthropicReq types.AnthropicRequest) *StopReasonManager {
	return &StopReasonManager{
		maxTokens:          anthropicReq.MaxTokens,
		requestedMaxTokens: anthropicReq.MaxTokens,
		stopSequences:      []string{}, // AnthropicRequest 当前不包含 StopSequences 字段
		hasActiveToolCalls: false,
		hasCompletedTools:  false,
		actualTokensUsed:   0,
	}
}

// UpdateToolCallStatus 更新工具调用状态
func (srm *StopReasonManager) UpdateToolCallStatus(hasActiveCalls, hasCompleted bool) {
	srm.hasActiveToolCalls = hasActiveCalls
	srm.hasCompletedTools = hasCompleted

	logger.Debug("更新工具调用状态",
		logger.Bool("has_active_tools", hasActiveCalls),
		logger.Bool("has_completed_tools", hasCompleted))
}

// SetActualTokensUsed 设置实际使用的token数量
func (srm *StopReasonManager) SetActualTokensUsed(tokens int) {
	srm.actualTokensUsed = tokens
}

// DetermineStopReason 根据Claude官方规范确定stop_reason
func (srm *StopReasonManager) DetermineStopReason() string {
	// logger.Debug("开始确定stop_reason",
	// 	logger.Int("max_tokens", srm.maxTokens),
	// 	logger.Int("actual_tokens", srm.actualTokensUsed),
	// 	logger.Bool("has_active_tools", srm.hasActiveToolCalls),
	// 	logger.Bool("has_completed_tools", srm.hasCompletedTools))

	// 规则1: 检查是否达到token限制 - 根据Claude规范优先级最高
	if srm.maxTokens > 1 && srm.actualTokensUsed >= srm.maxTokens {
		logger.Debug("确定stop_reason: max_tokens - 达到token限制")
		return "max_tokens"
	}

	// 规则2: 检查是否有活跃的工具调用等待执行
	// 根据Claude规范：只有当Claude主动调用工具且期待工具执行时才使用tool_use
	if srm.hasActiveToolCalls {
		logger.Debug("确定stop_reason: tool_use - 检测到活跃工具调用")
		return "tool_use"
	}

	// 规则4: 检查停止序列（如果实现了相关逻辑）
	// 注意：当前实现可能需要额外的停止序列检测逻辑
	if len(srm.stopSequences) > 0 {
		// TODO: 实现停止序列检测
		logger.Debug("检测停止序列", logger.Int("sequences_count", len(srm.stopSequences)))
	}

	// 规则5: 默认情况 - 自然完成响应
	logger.Debug("确定stop_reason: end_turn - 自然完成响应")
	return "end_turn"
}

// DetermineStopReasonFromUpstream 从上游响应中提取stop_reason
// 用于当上游已经提供了stop_reason时的情况
func (srm *StopReasonManager) DetermineStopReasonFromUpstream(upstreamStopReason string) string {
	if upstreamStopReason == "" {
		return srm.DetermineStopReason()
	}

	// 验证上游stop_reason是否符合Claude规范
	validStopReasons := map[string]bool{
		"end_turn":      true,
		"max_tokens":    true,
		"stop_sequence": true,
		"tool_use":      true,
		"pause_turn":    true,
		"refusal":       true,
	}

	if !validStopReasons[upstreamStopReason] {
		logger.Warn("上游提供了无效的stop_reason，使用本地逻辑",
			logger.String("upstream_stop_reason", upstreamStopReason))
		return srm.DetermineStopReason()
	}

	logger.Debug("使用上游stop_reason",
		logger.String("upstream_stop_reason", upstreamStopReason))
	return upstreamStopReason
}

// ValidateStopReason 验证stop_reason是否符合Claude规范
func ValidateStopReason(stopReason string) bool {
	validStopReasons := []string{
		"end_turn",
		"max_tokens",
		"stop_sequence",
		"tool_use",
		"pause_turn",
		"refusal",
	}

	if slices.Contains(validStopReasons, stopReason) {
		return true
	}

	logger.Warn("检测到无效的stop_reason", logger.String("stop_reason", stopReason))
	return false
}

// GetStopReasonDescription 获取stop_reason的描述（用于调试）
func GetStopReasonDescription(stopReason string) string {
	descriptions := map[string]string{
		"end_turn":      "Claude自然完成了响应",
		"max_tokens":    "达到了token限制",
		"stop_sequence": "遇到了自定义停止序列",
		"tool_use":      "Claude正在调用工具并期待执行",
		"pause_turn":    "服务器工具操作暂停",
		"refusal":       "Claude拒绝生成响应",
	}

	if desc, exists := descriptions[stopReason]; exists {
		return desc
	}
	return "未知的stop_reason"
}
