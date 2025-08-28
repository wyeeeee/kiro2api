package auth

import (
	"fmt"
	"kiro2api/logger"
	"kiro2api/types"
	"sync"
	"time"
)

// TokenManager 简化的token管理器
type TokenManager struct {
	cache       *SimpleTokenCache
	configs     []AuthConfig
	mutex       sync.RWMutex
	lastRefresh time.Time
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
	Available int
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
	return &TokenManager{
		cache:   NewSimpleTokenCache(5 * time.Minute),
		configs: configs,
	}
}

// getBestToken 获取最优可用token
func (tm *TokenManager) getBestToken() (types.TokenInfo, error) {
	tm.mutex.RLock()

	// 检查是否需要刷新缓存
	needRefresh := time.Since(tm.lastRefresh) > 5*time.Minute
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

	logger.Debug("选择token成功",
		logger.String("token_preview", bestToken.Token.AccessToken[:20]+"..."),
		logger.Int("available_count", bestToken.Available))

	return bestToken.Token, nil
}

// selectBestToken 选择最优token（简化策略）
func (tm *TokenManager) selectBestToken() *CachedToken {
	tm.cache.mutex.RLock()
	defer tm.cache.mutex.RUnlock()

	var bestToken *CachedToken

	for _, cached := range tm.cache.tokens {
		// 检查token是否过期
		if time.Since(cached.CachedAt) > tm.cache.ttl {
			continue
		}

		// 检查token是否可用
		if !cached.IsUsable() {
			continue
		}

		// 选择可用次数最多的token
		if bestToken == nil || cached.Available > bestToken.Available {
			bestToken = cached
		}
	}

	return bestToken
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
		var available int

		checker := NewUsageLimitsChecker()
		if usage, checkErr := checker.CheckUsageLimits(token); checkErr == nil {
			usageInfo = usage
			available = calculateAvailableCount(usage)
		} else {
			logger.Warn("检查使用限制失败", logger.Err(checkErr))
			available = 100 // 默认值
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
			logger.Int("available", available))
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

// calculateAvailableCount 计算可用次数
func calculateAvailableCount(usage *types.UsageLimits) int {
	for _, breakdown := range usage.UsageBreakdownList {
		if breakdown.ResourceType == "VIBE" {
			totalLimit := breakdown.UsageLimit
			totalUsed := breakdown.CurrentUsage

			if breakdown.FreeTrialInfo != nil && breakdown.FreeTrialInfo.FreeTrialStatus == "ACTIVE" {
				totalLimit += breakdown.FreeTrialInfo.UsageLimit
				totalUsed += breakdown.FreeTrialInfo.CurrentUsage
			}

			available := totalLimit - totalUsed
			if available < 0 {
				return 0
			}
			return available
		}
	}
	return 0
}

// CalculateAvailableCount 公开的可用次数计算函数
func CalculateAvailableCount(usage *types.UsageLimits) int {
	return calculateAvailableCount(usage)
}
