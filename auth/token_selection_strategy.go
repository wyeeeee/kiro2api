package auth

import (
	"fmt"
	"kiro2api/logger"
	"sync"
)

// TokenSelectionStrategy token选择策略接口
// 遵循Strategy Pattern，支持不同的token选择算法
type TokenSelectionStrategy interface {
	// SelectToken 从可用token中选择一个
	// tokens: 所有可用的token映射 (key -> CachedToken)
	// 返回选中的token的key，如果没有可用token返回空字符串
	SelectToken(tokens map[string]*CachedToken) string

	// Name 策略名称，用于日志和调试
	Name() string

	// Reset 重置策略状态（如果策略有状态的话）
	Reset()
}

// TokenSelectionStrategyType 策略类型枚举
type TokenSelectionStrategyType string

const (
	// StrategyOptimal 最优策略：选择可用次数最多的token（当前默认）
	StrategyOptimal TokenSelectionStrategyType = "optimal"

	// StrategySequential 顺序策略：按配置顺序依次使用token
	StrategySequential TokenSelectionStrategyType = "sequential"

	// StrategyBalanced 均衡策略：轮询使用所有可用token
	StrategyBalanced TokenSelectionStrategyType = "balanced"
)

// OptimalSelectionStrategy 最优选择策略（兼容现有逻辑）
type OptimalSelectionStrategy struct{}

// NewOptimalSelectionStrategy 创建最优选择策略
func NewOptimalSelectionStrategy() *OptimalSelectionStrategy {
	return &OptimalSelectionStrategy{}
}

// SelectToken 选择可用次数最多的token
func (oss *OptimalSelectionStrategy) SelectToken(tokens map[string]*CachedToken) string {
	var bestKey string
	var bestToken *CachedToken

	// 遍历所有token，选择可用次数最多的
	for key, cached := range tokens {
		// 检查token是否可用
		if !cached.IsUsable() {
			continue
		}

		// 选择可用次数最多的token
		if bestToken == nil || cached.Available > bestToken.Available {
			bestKey = key
			bestToken = cached
		}
	}

	if bestKey != "" {
		logger.Debug("最优策略选择token",
			logger.String("selected_key", bestKey),
			logger.Int("available_count", bestToken.Available))
	}

	return bestKey
}

// Name 策略名称
func (oss *OptimalSelectionStrategy) Name() string {
	return "optimal"
}

// Reset 重置策略状态
func (oss *OptimalSelectionStrategy) Reset() {
	// 最优策略无状态，无需重置
}

// SequentialSelectionStrategy 顺序选择策略
type SequentialSelectionStrategy struct {
	mu           sync.RWMutex
	configOrder  []string        // 配置顺序的token key列表
	currentIndex int             // 当前使用的token索引
	exhausted    map[string]bool // 已耗尽的token记录
}

// NewSequentialSelectionStrategy 创建顺序选择策略
func NewSequentialSelectionStrategy(configOrder []string) *SequentialSelectionStrategy {
	return &SequentialSelectionStrategy{
		configOrder: configOrder,
		currentIndex: 0,
		exhausted:   make(map[string]bool),
	}
}

// SelectToken 按配置顺序选择下一个可用token
func (sss *SequentialSelectionStrategy) SelectToken(tokens map[string]*CachedToken) string {
	sss.mu.Lock()
	defer sss.mu.Unlock()

	// 如果没有配置顺序，降级到按map遍历顺序
	if len(sss.configOrder) == 0 {
		for key, cached := range tokens {
			if cached.IsUsable() {
				logger.Debug("顺序策略选择token（无顺序配置）",
					logger.String("selected_key", key),
					logger.Int("available_count", cached.Available))
				return key
			}
		}
		return ""
	}

	// 从当前索引开始，找到第一个可用的token
	for attempts := 0; attempts < len(sss.configOrder); attempts++ {
		currentKey := sss.configOrder[sss.currentIndex]

		// 检查这个token是否存在且可用
		if cached, exists := tokens[currentKey]; exists && cached.IsUsable() {
			logger.Debug("顺序策略选择token",
				logger.String("selected_key", currentKey),
				logger.Int("index", sss.currentIndex),
				logger.Int("available_count", cached.Available))
			return currentKey
		}

		// 标记当前token为已耗尽，移动到下一个
		sss.exhausted[currentKey] = true
		sss.currentIndex = (sss.currentIndex + 1) % len(sss.configOrder)

		logger.Debug("token不可用，切换到下一个",
			logger.String("exhausted_key", currentKey),
			logger.Int("next_index", sss.currentIndex))
	}

	// 所有token都不可用
	logger.Warn("所有token都不可用",
		logger.Int("total_count", len(sss.configOrder)),
		logger.Int("exhausted_count", len(sss.exhausted)))

	return ""
}

// Name 策略名称
func (sss *SequentialSelectionStrategy) Name() string {
	return "sequential"
}

// Reset 重置策略状态
func (sss *SequentialSelectionStrategy) Reset() {
	sss.mu.Lock()
	defer sss.mu.Unlock()

	sss.currentIndex = 0
	sss.exhausted = make(map[string]bool)

	logger.Debug("顺序策略状态重置")
}

// UpdateConfigOrder 更新配置顺序（动态配置支持）
func (sss *SequentialSelectionStrategy) UpdateConfigOrder(configOrder []string) {
	sss.mu.Lock()
	defer sss.mu.Unlock()

	sss.configOrder = configOrder
	sss.currentIndex = 0
	sss.exhausted = make(map[string]bool)

	logger.Debug("顺序策略配置更新",
		logger.Int("config_count", len(configOrder)))
}

// GetCurrentStatus 获取当前状态（用于调试）
func (sss *SequentialSelectionStrategy) GetCurrentStatus() map[string]interface{} {
	sss.mu.RLock()
	defer sss.mu.RUnlock()

	return map[string]interface{}{
		"current_index":  sss.currentIndex,
		"config_order":   sss.configOrder,
		"exhausted_keys": sss.exhausted,
		"total_configs":  len(sss.configOrder),
	}
}

// CreateTokenSelectionStrategy 策略工厂函数
func CreateTokenSelectionStrategy(strategyType TokenSelectionStrategyType, configOrder []string) (TokenSelectionStrategy, error) {
	switch strategyType {
	case StrategyOptimal:
		return NewOptimalSelectionStrategy(), nil
	case StrategySequential:
		return NewSequentialSelectionStrategy(configOrder), nil
	case StrategyBalanced:
		// TODO: 实现均衡策略（如果需要）
		return NewOptimalSelectionStrategy(), nil // 暂时降级到最优策略
	default:
		return nil, fmt.Errorf("不支持的token选择策略: %s", strategyType)
	}
}