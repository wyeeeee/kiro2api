package auth

import (
	"fmt"
	"kiro2api/config"
	"kiro2api/logger"
	"kiro2api/types"
	"sync"
	"time"
)

// TokenManager 简化的token管理器
type TokenManager struct {
	cache        *SimpleTokenCache
	configs      []AuthConfig
	mutex        sync.RWMutex
	lastRefresh  time.Time
	configOrder  []string        // 配置顺序
	currentIndex int             // 当前使用的token索引
	exhausted    map[string]bool // 已耗尽的token记录
}

// SimpleTokenCache 简化的token缓存
type SimpleTokenCache struct {
	tokens map[string]*CachedToken
	mutex  sync.RWMutex
	ttl    time.Duration
}

// CachedToken 缓存的token信息
type CachedToken struct {
	Token     types.TokenInfo
	UsageInfo *types.UsageLimits
	CachedAt  time.Time
	LastUsed  time.Time
	Available float64
}

// NewSimpleTokenCache 创建简单的token缓存
func NewSimpleTokenCache(ttl time.Duration) *SimpleTokenCache {
	return &SimpleTokenCache{
		tokens: make(map[string]*CachedToken),
		ttl:    ttl,
	}
}

// NewTokenManager 创建新的token管理器
func NewTokenManager(configs []AuthConfig) *TokenManager {
	// 生成配置顺序
	configOrder := generateConfigOrder(configs)

	logger.Info("TokenManager初始化（顺序选择策略）",
		logger.Int("config_count", len(configs)),
		logger.Int("config_order_count", len(configOrder)))

	return &TokenManager{
		cache:        NewSimpleTokenCache(config.TokenCacheTTL),
		configs:      configs,
		configOrder:  configOrder,
		currentIndex: 0,
		exhausted:    make(map[string]bool),
	}
}

// getBestToken 获取最优可用token
func (tm *TokenManager) getBestToken() (types.TokenInfo, error) {
	tm.mutex.RLock()

	// 检查是否需要刷新缓存
	needRefresh := time.Since(tm.lastRefresh) > config.TokenCacheTTL
	tm.mutex.RUnlock()

	if needRefresh {
		if err := tm.refreshCache(); err != nil {
			logger.Warn("刷新token缓存失败", logger.Err(err))
		}
	}

	// 选择最优token
	bestToken := tm.selectBestToken()
	if bestToken == nil {
		return types.TokenInfo{}, fmt.Errorf("没有可用的token")
	}

	// 更新最后使用时间
	tm.cache.updateLastUsed(bestToken)

	// logger.Debug("选择token成功",
	// 	logger.String("token_preview", bestToken.Token.AccessToken[:20]+"..."),
	// 	logger.Float64("available_count", bestToken.Available))

	return bestToken.Token, nil
}

// selectBestToken 按配置顺序选择下一个可用token（并发安全）
func (tm *TokenManager) selectBestToken() *CachedToken {
	// 持有cache读锁保护tokens map的并发访问
	tm.cache.mutex.RLock()
	defer tm.cache.mutex.RUnlock()

	// 持有manager写锁保护exhausted和currentIndex的并发修改
	tm.mutex.Lock()
	defer tm.mutex.Unlock()

	// 如果没有配置顺序，降级到按map遍历顺序
	if len(tm.configOrder) == 0 {
		for key, cached := range tm.cache.tokens {
			if time.Since(cached.CachedAt) <= tm.cache.ttl && cached.IsUsable() {
				logger.Debug("顺序策略选择token（无顺序配置）",
					logger.String("selected_key", key),
					logger.Float64("available_count", cached.Available))
				return cached
			}
		}
		return nil
	}

	// 从当前索引开始，找到第一个可用的token
	for attempts := 0; attempts < len(tm.configOrder); attempts++ {
		currentKey := tm.configOrder[tm.currentIndex]

		// 检查这个token是否存在且可用
		if cached, exists := tm.cache.tokens[currentKey]; exists {
			// 检查token是否过期
			if time.Since(cached.CachedAt) > tm.cache.ttl {
				tm.exhausted[currentKey] = true
				tm.currentIndex = (tm.currentIndex + 1) % len(tm.configOrder)
				continue
			}

			// 检查token是否可用
			if cached.IsUsable() {
				logger.Debug("顺序策略选择token",
					logger.String("selected_key", currentKey),
					logger.Int("index", tm.currentIndex),
					logger.Float64("available_count", cached.Available))
				return cached
			}
		}

		// 标记当前token为已耗尽，移动到下一个
		tm.exhausted[currentKey] = true
		tm.currentIndex = (tm.currentIndex + 1) % len(tm.configOrder)

		logger.Debug("token不可用，切换到下一个",
			logger.String("exhausted_key", currentKey),
			logger.Int("next_index", tm.currentIndex))
	}

	// 所有token都不可用
	logger.Warn("所有token都不可用",
		logger.Int("total_count", len(tm.configOrder)),
		logger.Int("exhausted_count", len(tm.exhausted)))

	return nil
}

// refreshCache 刷新token缓存
func (tm *TokenManager) refreshCache() error {
	tm.mutex.Lock()
	defer tm.mutex.Unlock()

	logger.Debug("开始刷新token缓存")

	for i, config := range tm.configs {
		if config.Disabled {
			continue
		}

		// 刷新token
		token, err := tm.refreshSingleToken(config)
		if err != nil {
			logger.Warn("刷新单个token失败",
				logger.Int("config_index", i),
				logger.String("auth_type", config.AuthType),
				logger.Err(err))
			continue
		}

		// 检查使用限制
		var usageInfo *types.UsageLimits
		var available float64

		checker := NewUsageLimitsChecker()
		if usage, checkErr := checker.CheckUsageLimits(token); checkErr == nil {
			usageInfo = usage
			available = CalculateAvailableCount(usage)
		} else {
			logger.Warn("检查使用限制失败", logger.Err(checkErr))
			available = 100.0 // 默认值 - 保留硬编码避免与变量名冲突
		}

		// 更新缓存
		cacheKey := fmt.Sprintf("token_%d", i)
		tm.cache.set(cacheKey, &CachedToken{
			Token:     token,
			UsageInfo: usageInfo,
			CachedAt:  time.Now(),
			Available: available,
		})

		logger.Debug("token缓存更新",
			logger.String("cache_key", cacheKey),
			logger.Float64("available", available))
	}

	tm.lastRefresh = time.Now()
	return nil
}

// IsUsable 检查缓存的token是否可用
func (ct *CachedToken) IsUsable() bool {
	// 检查token是否过期
	if time.Now().After(ct.Token.ExpiresAt) {
		return false
	}

	// 检查可用次数
	return ct.Available > 0
}

// set 设置缓存项
func (stc *SimpleTokenCache) set(key string, token *CachedToken) {
	stc.mutex.Lock()
	defer stc.mutex.Unlock()
	stc.tokens[key] = token
}

// updateLastUsed 更新最后使用时间
func (stc *SimpleTokenCache) updateLastUsed(token *CachedToken) {
	stc.mutex.Lock()
	defer stc.mutex.Unlock()
	token.LastUsed = time.Now()
	if token.Available > 0 {
		token.Available--
	}
}

// CalculateAvailableCount 计算可用次数 (基于CREDIT资源类型，返回浮点精度)
func CalculateAvailableCount(usage *types.UsageLimits) float64 {
	for _, breakdown := range usage.UsageBreakdownList {
		if breakdown.ResourceType == "CREDIT" {
			var totalAvailable float64

			// 优先使用免费试用额度 (如果存在且处于ACTIVE状态)
			if breakdown.FreeTrialInfo != nil && breakdown.FreeTrialInfo.FreeTrialStatus == "ACTIVE" {
				freeTrialAvailable := breakdown.FreeTrialInfo.UsageLimitWithPrecision - breakdown.FreeTrialInfo.CurrentUsageWithPrecision
				totalAvailable += freeTrialAvailable
			}

			// 加上基础额度
			baseAvailable := breakdown.UsageLimitWithPrecision - breakdown.CurrentUsageWithPrecision
			totalAvailable += baseAvailable

			if totalAvailable < 0 {
				return 0.0
			}
			return totalAvailable
		}
	}
	return 0.0
}

// generateConfigOrder 生成token配置的顺序
func generateConfigOrder(configs []AuthConfig) []string {
	var order []string

	for i := range configs {
		// 使用索引生成cache key，与refreshCache中的逻辑保持一致
		cacheKey := fmt.Sprintf("token_%d", i)
		order = append(order, cacheKey)
	}

	logger.Debug("生成配置顺序",
		logger.Int("config_count", len(configs)),
		logger.Any("order", order))

	return order
}

// GetSelectionStrategyStatus 获取选择策略状态（用于监控和调试）
func (tm *TokenManager) GetSelectionStrategyStatus() map[string]interface{} {
	tm.mutex.RLock()
	defer tm.mutex.RUnlock()

	return map[string]interface{}{
		"strategy_name":  "sequential",
		"config_order":   tm.configOrder,
		"current_index":  tm.currentIndex,
		"exhausted_keys": tm.exhausted,
	}
}
