package auth

import (
	"fmt"
	"kiro2api/logger"
	"kiro2api/types"
	"os"
	"strings"
	"sync"
	"time"
)

// TokenManager 简化的token管理器
type TokenManager struct {
	cache            *SimpleTokenCache
	configs          []AuthConfig
	mutex            sync.RWMutex
	lastRefresh      time.Time
	selectionStrategy TokenSelectionStrategy // 新增：token选择策略
	configOrder      []string                // 新增：配置顺序
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
	// 确定选择策略类型
	strategyType := getTokenSelectionStrategy()

	// 生成配置顺序
	configOrder := generateConfigOrder(configs)

	// 创建选择策略
	strategy, err := CreateTokenSelectionStrategy(strategyType, configOrder)
	if err != nil {
		logger.Warn("创建token选择策略失败，使用默认策略",
			logger.Err(err),
			logger.String("requested_strategy", string(strategyType)))
		strategy = NewOptimalSelectionStrategy()
	}

	logger.Info("TokenManager初始化",
		logger.String("selection_strategy", strategy.Name()),
		logger.Int("config_count", len(configs)),
		logger.Int("config_order_count", len(configOrder)))

	return &TokenManager{
		cache:            NewSimpleTokenCache(5 * time.Minute),
		configs:          configs,
		selectionStrategy: strategy,
		configOrder:      configOrder,
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

// selectBestToken 使用选择策略选择最优token
func (tm *TokenManager) selectBestToken() *CachedToken {
	tm.cache.mutex.RLock()
	tokens := tm.cache.tokens // 获取token映射的引用
	tm.cache.mutex.RUnlock()

	// 过滤出可用的token
	availableTokens := make(map[string]*CachedToken)
	for key, cached := range tokens {
		// 检查token是否过期
		if time.Since(cached.CachedAt) > tm.cache.ttl {
			continue
		}

		// 检查token是否可用
		if cached.IsUsable() {
			availableTokens[key] = cached
		}
	}

	if len(availableTokens) == 0 {
		logger.Debug("没有可用的token")
		return nil
	}

	// 使用策略选择token
	selectedKey := tm.selectionStrategy.SelectToken(availableTokens)
	if selectedKey == "" {
		logger.Warn("选择策略未能选择任何token",
			logger.String("strategy", tm.selectionStrategy.Name()),
			logger.Int("available_count", len(availableTokens)))
		return nil
	}

	selectedToken := availableTokens[selectedKey]

	logger.Debug("token选择完成",
		logger.String("strategy", tm.selectionStrategy.Name()),
		logger.String("selected_key", selectedKey),
		logger.Int("available_count", selectedToken.Available))

	return selectedToken
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

// getTokenSelectionStrategy 从环境变量获取token选择策略
func getTokenSelectionStrategy() TokenSelectionStrategyType {
	strategyEnv := strings.ToLower(strings.TrimSpace(os.Getenv("TOKEN_SELECTION_STRATEGY")))

	switch strategyEnv {
	case "sequential", "sequence", "顺序":
		return StrategySequential
	case "optimal", "best", "最优":
		return StrategyOptimal
	case "balanced", "均衡":
		return StrategyBalanced
	default:
		// 默认使用顺序策略（满足用户需求）
		return StrategySequential
	}
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

// SetSelectionStrategy 动态设置选择策略（用于测试和动态配置）
func (tm *TokenManager) SetSelectionStrategy(strategyType TokenSelectionStrategyType) error {
	tm.mutex.Lock()
	defer tm.mutex.Unlock()

	strategy, err := CreateTokenSelectionStrategy(strategyType, tm.configOrder)
	if err != nil {
		return fmt.Errorf("创建选择策略失败: %w", err)
	}

	oldStrategy := tm.selectionStrategy.Name()
	tm.selectionStrategy = strategy

	logger.Info("token选择策略已更新",
		logger.String("old_strategy", oldStrategy),
		logger.String("new_strategy", strategy.Name()))

	return nil
}

// GetSelectionStrategyStatus 获取选择策略状态（用于监控和调试）
func (tm *TokenManager) GetSelectionStrategyStatus() map[string]interface{} {
	tm.mutex.RLock()
	defer tm.mutex.RUnlock()

	status := map[string]interface{}{
		"strategy_name": tm.selectionStrategy.Name(),
		"config_order":  tm.configOrder,
	}

	// 如果是顺序策略，获取详细状态
	if seqStrategy, ok := tm.selectionStrategy.(*SequentialSelectionStrategy); ok {
		status["sequential_status"] = seqStrategy.GetCurrentStatus()
	}

	return status
}
