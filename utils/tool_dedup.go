package utils

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/bytedance/sonic"
)

// ToolDedupManager 管理工具调用去重的结构
type ToolDedupManager struct {
	processedHashes map[string]bool
}

// NewToolDedupManager 创建新的工具去重管理器
func NewToolDedupManager() *ToolDedupManager {
	return &ToolDedupManager{
		processedHashes: make(map[string]bool),
	}
}

// normalizeInput 规范化输入参数以确保序列化的一致性
func normalizeInput(input interface{}) (string, error) {
	if input == nil {
		return "{}", nil
	}

	// 直接序列化，不进行反序列化/重新序列化以避免类型转换问题
	inputJSON, err := sonic.Marshal(input)
	if err != nil {
		return "", err
	}

	return string(inputJSON), nil
}

// GenerateToolHash 基于工具名称和输入参数生成SHA256哈希
func (m *ToolDedupManager) GenerateToolHash(toolName string, toolInput interface{}) (string, error) {
	// 规范化输入参数
	inputJSON, err := normalizeInput(toolInput)
	if err != nil {
		return "", fmt.Errorf("规范化工具输入失败: %w", err)
	}

	// 组合工具名称和输入参数
	combined := fmt.Sprintf("%s:%s", toolName, inputJSON)
	
	// 生成SHA256哈希
	hash := sha256.Sum256([]byte(combined))
	return hex.EncodeToString(hash[:]), nil
}

// IsToolProcessed 检查工具是否已被处理
func (m *ToolDedupManager) IsToolProcessed(toolName string, toolInput interface{}) (bool, error) {
	hash, err := m.GenerateToolHash(toolName, toolInput)
	if err != nil {
		return false, err
	}
	
	return m.processedHashes[hash], nil
}

// MarkToolProcessed 标记工具为已处理
func (m *ToolDedupManager) MarkToolProcessed(toolName string, toolInput interface{}) error {
	hash, err := m.GenerateToolHash(toolName, toolInput)
	if err != nil {
		return err
	}
	
	m.processedHashes[hash] = true
	return nil
}

// Reset 重置去重管理器状态
func (m *ToolDedupManager) Reset() {
	m.processedHashes = make(map[string]bool)
}

