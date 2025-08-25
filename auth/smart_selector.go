package auth

import (
	"fmt"
	"kiro2api/logger"
	"kiro2api/types"
	"sort"
	"time"
)

// TokenSelector 智能token选择器 (遵循策略模式)
type TokenSelector struct {
	selectionStrategy SelectionStrategy
}

// SelectionStrategy token选择策略接口
type SelectionStrategy interface {
	SelectBestToken(tokens []types.TokenWithUsage) (*types.TokenWithUsage, error)
	StrategyName() string
}

// NewTokenSelector 创建智能token选择器
func NewTokenSelector(strategy SelectionStrategy) *TokenSelector {
	return &TokenSelector{
		selectionStrategy: strategy,
	}
}

// OptimalUsageStrategy 最优使用策略 (根据剩余次数和状态选择)
type OptimalUsageStrategy struct{}

func (s *OptimalUsageStrategy) StrategyName() string {
	return "OptimalUsage"
}

func (s *OptimalUsageStrategy) SelectBestToken(tokens []types.TokenWithUsage) (*types.TokenWithUsage, error) {
	if len(tokens) == 0 {
		return nil, fmt.Errorf("无可选token")
	}

	// 1. 过滤出可用的token
	var usableTokens []types.TokenWithUsage
	for _, token := range tokens {
		if token.IsUsable() {
			usableTokens = append(usableTokens, token)
		}
	}

	if len(usableTokens) == 0 {
		return nil, fmt.Errorf("所有token都不可用")
	}

	// 2. 按照智能策略排序
	sort.Slice(usableTokens, func(i, j int) bool {
		tokenA, tokenB := &usableTokens[i], &usableTokens[j]
		
		// 优先级1: 有使用限制信息的token优于没有的
		hasUsageA := tokenA.UsageLimits != nil
		hasUsageB := tokenB.UsageLimits != nil
		if hasUsageA != hasUsageB {
			return hasUsageA
		}

		// 优先级2: 剩余次数更多的优先
		if tokenA.AvailableCount != tokenB.AvailableCount {
			return tokenA.AvailableCount > tokenB.AvailableCount
		}

		// 优先级3: 使用限制检查时间更新的优先
		return tokenA.LastUsageCheck.After(tokenB.LastUsageCheck)
	})

	selectedToken := &usableTokens[0]
	
	logger.Info("智能token选择完成",
		logger.String("strategy", s.StrategyName()),
		logger.Int("available_tokens", len(usableTokens)),
		logger.String("selected_user_email", selectedToken.GetUserEmailDisplay()),
		logger.String("selected_token_preview", selectedToken.TokenPreview),
		logger.Int("selected_available_count", selectedToken.AvailableCount))

	return selectedToken, nil
}

// BalancedStrategy 均衡使用策略 (轮换使用，避免单一token耗尽)
type BalancedStrategy struct{}

func (s *BalancedStrategy) StrategyName() string {
	return "Balanced"
}

func (s *BalancedStrategy) SelectBestToken(tokens []types.TokenWithUsage) (*types.TokenWithUsage, error) {
	if len(tokens) == 0 {
		return nil, fmt.Errorf("无可选token")
	}

	// 1. 过滤出可用的token
	var usableTokens []types.TokenWithUsage
	for _, token := range tokens {
		if token.IsUsable() {
			usableTokens = append(usableTokens, token)
		}
	}

	if len(usableTokens) == 0 {
		return nil, fmt.Errorf("所有token都不可用")
	}

	// 2. 选择使用次数相对较少的token (均衡策略)
	sort.Slice(usableTokens, func(i, j int) bool {
		tokenA, tokenB := &usableTokens[i], &usableTokens[j]
		
		// 计算使用率 (已用次数 / 总次数)
		usageRateA := s.calculateUsageRate(tokenA)
		usageRateB := s.calculateUsageRate(tokenB)
		
		// 使用率低的优先
		if usageRateA != usageRateB {
			return usageRateA < usageRateB
		}
		
		// 使用率相同时，剩余次数多的优先
		return tokenA.AvailableCount > tokenB.AvailableCount
	})

	selectedToken := &usableTokens[0]
	
	logger.Info("均衡token选择完成",
		logger.String("strategy", s.StrategyName()),
		logger.Int("available_tokens", len(usableTokens)),
		logger.String("selected_user_email", selectedToken.GetUserEmailDisplay()),
		logger.String("selected_token_preview", selectedToken.TokenPreview),
		logger.String("selected_usage_rate", fmt.Sprintf("%.2f", s.calculateUsageRate(selectedToken))),
		logger.Int("selected_available_count", selectedToken.AvailableCount))

	return selectedToken, nil
}

func (s *BalancedStrategy) calculateUsageRate(token *types.TokenWithUsage) float64 {
	if token.UsageLimits == nil {
		return 0.5 // 默认中等使用率
	}

	for _, breakdown := range token.UsageLimits.UsageBreakdownList {
		if breakdown.ResourceType == "VIBE" {
			totalLimit := breakdown.UsageLimit
			if breakdown.FreeTrialInfo != nil {
				totalLimit += breakdown.FreeTrialInfo.UsageLimit
			}
			
			if totalLimit == 0 {
				return 1.0 // 无配额视为已用完
			}
			
			totalUsed := breakdown.CurrentUsage
			if breakdown.FreeTrialInfo != nil {
				totalUsed += breakdown.FreeTrialInfo.CurrentUsage
			}
			
			return float64(totalUsed) / float64(totalLimit)
		}
	}
	
	return 0.5 // 默认值
}

// EnhancedTokenManager 增强的token管理器 (集成智能选择)
type EnhancedTokenManager struct {
	selector    *TokenSelector
	cache       map[string]*types.TokenWithUsage // token缓存
	lastRefresh time.Time
}

// NewEnhancedTokenManager 创建增强token管理器
func NewEnhancedTokenManager(strategy SelectionStrategy) *EnhancedTokenManager {
	if strategy == nil {
		strategy = &OptimalUsageStrategy{} // 默认使用最优策略
	}
	
	return &EnhancedTokenManager{
		selector: NewTokenSelector(strategy),
		cache:    make(map[string]*types.TokenWithUsage),
	}
}

// GetBestToken 获取最优token (替代原有的简单获取逻辑)
func (m *EnhancedTokenManager) GetBestToken() (types.TokenInfo, error) {
	// 1. 检查是否需要刷新token列表
	if time.Since(m.lastRefresh) > 5*time.Minute || len(m.cache) == 0 {
		if err := m.refreshTokenList(); err != nil {
			logger.Warn("刷新token列表失败，使用缓存", logger.Err(err))
			
			// 如果刷新失败，回退到原有的简单获取逻辑
			return GetToken()
		}
	}

	// 2. 将缓存转换为slice用于选择
	var tokens []types.TokenWithUsage
	for _, token := range m.cache {
		tokens = append(tokens, *token)
	}

	// 3. 使用智能策略选择最优token
	selectedToken, err := m.selector.selectionStrategy.SelectBestToken(tokens)
	if err != nil {
		logger.Warn("智能选择失败，回退到原有逻辑", logger.Err(err))
		return GetToken()
	}

	return selectedToken.TokenInfo, nil
}

// refreshTokenList 刷新token列表 (获取所有可用token的使用状态)
func (m *EnhancedTokenManager) refreshTokenList() error {
	logger.Debug("开始刷新智能token列表")
	
	// 这里简化实现，在实际应用中应该获取所有配置的token
	// 目前先从现有系统获取当前token作为示例
	currentToken, err := GetToken()
	if err != nil {
		return fmt.Errorf("获取当前token失败: %v", err)
	}

	// 检查并增强token
	enhancedToken := CheckAndEnhanceToken(currentToken)
	
	// 更新缓存
	tokenKey := currentToken.AccessToken[:20] // 使用token前20个字符作为key
	m.cache[tokenKey] = &enhancedToken
	m.lastRefresh = time.Now()
	
	logger.Info("智能token列表刷新完成", 
		logger.Int("cached_tokens", len(m.cache)),
		logger.String("token_user_email", enhancedToken.GetUserEmailDisplay()),
		logger.String("token_preview", enhancedToken.TokenPreview),
		logger.Int("available_count", enhancedToken.AvailableCount))
	
	return nil
}

// 全局智能token管理器实例
var enhancedTokenManager *EnhancedTokenManager

// GetBestTokenGlobally 全局获取最优token (新的推荐入口点)
func GetBestTokenGlobally() (types.TokenInfo, error) {
	if enhancedTokenManager == nil {
		enhancedTokenManager = NewEnhancedTokenManager(&OptimalUsageStrategy{})
	}
	
	return enhancedTokenManager.GetBestToken()
}