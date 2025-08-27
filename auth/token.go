package auth

import (
	"bytes"
	"fmt"
	"io"
	"kiro2api/auth/config"
	globalConfig "kiro2api/config"
	"kiro2api/logger"
	"kiro2api/types"
	"kiro2api/utils"
	"net/http"
	"sync"
	"time"
)

// 全局token池和缓存实例
var (
	tokenPool      *types.TokenPool
	atomicCache    *utils.AtomicTokenCache    // 使用原子缓存替代传统缓存
	refreshManager *utils.TokenRefreshManager // token刷新并发控制管理器
	configProvider config.ConfigProvider      // 配置提供者
)

// InitializeTokenSystem 程序启动时主动初始化整个token系统
func InitializeTokenSystem() error {
	// 1. 初始化配置提供者
	configProvider = config.NewDefaultConfigProvider()

	// 2. 初始化原子缓存
	atomicCache = utils.NewAtomicTokenCache()
	atomicCache.StartCleanupRoutine()

	// 3. 初始化刷新管理器
	refreshManager = utils.NewTokenRefreshManager()

	// 4. 初始化token池
	initTokenPool()
	// 5. 验证token可用性
	return InitializeTokenPoolAndValidate()
}

// initTokenPool 初始化token池 - 使用ConfigProvider
func initTokenPool() {
	provider := getConfigProvider()

	// 使用ConfigProvider加载所有配置
	configs, err := provider.LoadConfigs()
	if err != nil {
		logger.Error("加载认证配置失败", logger.Err(err))
		return
	}

	if len(configs) == 0 {
		logger.Debug("未找到任何有效的token配置")
		return
	}

	// 提取所有refresh token
	var allValidTokens []string
	for _, cfg := range configs {
		if !cfg.Disabled {
			allValidTokens = append(allValidTokens, cfg.RefreshToken)
		}
	}

	// 初始化token池
	if len(allValidTokens) > 0 {
		tokenPool = types.NewTokenPool(allValidTokens, 3) // 最大重试3次

		logger.Info("Token池初始化完成",
			logger.Int("total_token_count", len(allValidTokens)),
			logger.Int("total_configs", len(configs)))
	} else {
		logger.Debug("未找到任何可用的token配置")
	}
}

// 注意：Token解析和去重逻辑已移至auth/config包的ConfigProvider中

// InitializeTokenPoolAndValidate 启动时主动初始化token池并验证可用性
func InitializeTokenPoolAndValidate() error {
	// 强制初始化token池
	pool := getTokenPool()
	if pool == nil {
		return fmt.Errorf("token池初始化失败：未找到任何有效的token配置")
	}

	// 记录token池状态
	tokenCount := pool.GetTokenCount()
	if tokenCount == 0 {
		return fmt.Errorf("token池为空：未找到任何可用的token")
	}

	logger.Info("Token池初始化成功",
		logger.Int("token_count", tokenCount))

	// 🚀 新功能：检查并缓存所有token
	logger.Info("开始检查并缓存所有token...")

	// 获取配置提供者和原子缓存
	provider := getConfigProvider()
	atomicCache := getAtomicCache()

	configs, err := provider.LoadConfigs()
	if err != nil {
		return fmt.Errorf("加载配置失败: %v", err)
	}

	var usableTokens int
	var totalErrors []string

	// 遍历所有token索引进行预热
	for i := 0; i < tokenCount; i++ {
		logger.Debug("检查token", logger.Int("token_index", i))

		// 检查配置是否存在
		if i >= len(configs) {
			errorMsg := fmt.Sprintf("token索引%d超出配置范围", i)
			totalErrors = append(totalErrors, errorMsg)
			logger.Warn(errorMsg, logger.Int("configs_count", len(configs)))
			continue
		}

		config := configs[i]

		// 跳过禁用的token
		if config.Disabled {
			logger.Info("跳过已禁用的token", logger.Int("token_index", i), logger.String("auth_type", config.AuthType))
			continue
		}

		// 尝试刷新token并检查使用情况
		tokenInfo, refreshErr := refreshTokenByIndex(pool, i)
		if refreshErr != nil {
			errorMsg := fmt.Sprintf("token索引%d刷新失败: %v", i, refreshErr)
			totalErrors = append(totalErrors, errorMsg)
			logger.Warn("Token刷新失败",
				logger.Int("token_index", i),
				logger.String("auth_type", config.AuthType),
				logger.Err(refreshErr))
			continue
		}

		// 将token放入原子缓存
		atomicCache.Set(i, &tokenInfo)
		logger.Debug("Token已加入原子缓存",
			logger.Int("token_index", i),
			logger.String("expires_at", tokenInfo.ExpiresAt.Format("2006-01-02 15:04:05")))

		// 检查并增强token，同时放入增强token缓存
		enhancedToken := CheckAndEnhanceToken(tokenInfo)

		// 加入增强token缓存
		enhancedTokenCacheMutex.Lock()
		enhancedTokenCache[tokenInfo.AccessToken] = &enhancedToken
		enhancedTokenCacheMutex.Unlock()

		if enhancedToken.IsUsable() {
			usableTokens++
			logger.Info("Token预热完成",
				logger.Int("token_index", i),
				logger.String("auth_type", config.AuthType),
				logger.String("user_email", enhancedToken.GetUserEmailDisplay()),
				logger.String("token_preview", enhancedToken.TokenPreview),
				logger.Int("available_count", enhancedToken.AvailableCount))
		} else {
			logger.Warn("Token可用额度不足",
				logger.Int("token_index", i),
				logger.String("auth_type", config.AuthType),
				logger.String("user_email", enhancedToken.GetUserEmailDisplay()),
				logger.Int("available_count", enhancedToken.AvailableCount))
		}
	}

	// 记录预热结果
	logger.Info("Token池预热完成",
		logger.Int("total_tokens", tokenCount),
		logger.Int("usable_tokens", usableTokens),
		logger.Int("errors", len(totalErrors)))

	// 如果没有可用的token，记录详细错误信息
	if usableTokens == 0 {
		logger.Error("没有找到任何可用的token")
		for _, errMsg := range totalErrors {
			logger.Error("Token错误", logger.String("error", errMsg))
		}
		return fmt.Errorf("所有token都不可用，共%d个错误", len(totalErrors))
	}

	// 获取缓存统计信息
	cacheStats := atomicCache.GetStats()
	logger.Info("缓存统计",
		logger.Any("atomic_cache_stats", cacheStats),
		logger.Int("enhanced_cache_count", len(enhancedTokenCache)))

	return nil
}

// 注意：JSON配置解析逻辑已移至auth/config包的ConfigProvider中

// getTokenPool 获取token池实例
func getTokenPool() *types.TokenPool {
	// 系统已在启动时初始化，直接返回实例
	return tokenPool
}

// getConfigProvider 获取配置提供者实例
func getConfigProvider() config.ConfigProvider {
	// 系统已在启动时初始化，直接返回实例
	return configProvider
}

// getAtomicCache 获取原子缓存实例
func getAtomicCache() *utils.AtomicTokenCache {
	// 系统已在启动时初始化，直接返回实例
	return atomicCache
}

// getRefreshManager 获取刷新管理器实例
func getRefreshManager() *utils.TokenRefreshManager {
	// 系统已在启动时初始化，直接返回实例
	return refreshManager
}

// tryRefreshTokenByAuthMethod 根据认证方式刷新token
func tryRefreshTokenByAuthMethod(refreshToken string) (types.TokenInfo, error) {
	// 从配置中找到对应的refresh token配置
	provider := getConfigProvider()
	configs, err := provider.LoadConfigs()
	if err != nil {
		return types.TokenInfo{}, fmt.Errorf("加载配置失败: %v", err)
	}

	// 找到匹配的配置
	var targetConfig *config.AuthConfig
	for _, cfg := range configs {
		if cfg.RefreshToken == refreshToken {
			targetConfig = &cfg
			break
		}
	}

	if targetConfig == nil {
		return types.TokenInfo{}, fmt.Errorf("未找到refresh token对应的配置")
	}

	// 根据配置中的认证类型刷新token
	switch targetConfig.AuthType {
	case config.AuthMethodIdC:
		return tryRefreshIdcTokenWithConfig(targetConfig)
	case config.AuthMethodSocial:
		return tryRefreshToken(refreshToken)
	default:
		return types.TokenInfo{}, fmt.Errorf("不支持的认证方式: %v", targetConfig.AuthType)
	}
}

// tryRefreshIdcTokenWithConfig 使用IdC认证方式和配置刷新token
func tryRefreshIdcTokenWithConfig(authConfig *config.AuthConfig) (types.TokenInfo, error) {
	clientId := authConfig.ClientID
	clientSecret := authConfig.ClientSecret
	refreshToken := authConfig.RefreshToken

	if clientId == "" || clientSecret == "" {
		return types.TokenInfo{}, fmt.Errorf("IdC认证需要ClientID和ClientSecret")
	}

	// 准备刷新请求
	refreshReq := types.IdcRefreshRequest{
		ClientId:     clientId,
		ClientSecret: clientSecret,
		GrantType:    "refresh_token",
		RefreshToken: refreshToken,
	}

	reqBody, err := utils.FastMarshal(refreshReq)
	if err != nil {
		return types.TokenInfo{}, fmt.Errorf("序列化IdC请求失败: %v", err)
	}

	logger.Debug("发送IdC token刷新请求", logger.String("url", globalConfig.IdcRefreshTokenURL))

	// 发送刷新请求
	req, err := http.NewRequest("POST", globalConfig.IdcRefreshTokenURL, bytes.NewBuffer(reqBody))
	if err != nil {
		return types.TokenInfo{}, fmt.Errorf("创建IdC请求失败: %v", err)
	}

	// 设置IdC认证所需的特殊headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Host", "oidc.us-east-1.amazonaws.com")
	req.Header.Set("Connection", "keep-alive")
	req.Header.Set("x-amz-user-agent", "aws-sdk-js/3.738.0 ua/2.1 os/other lang/js md/browser#unknown_unknown api/sso-oidc#3.738.0 m/E KiroIDE")
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Language", "*")
	req.Header.Set("sec-fetch-mode", "cors")
	req.Header.Set("User-Agent", "node")
	req.Header.Set("Accept-Encoding", "br, gzip, deflate")

	resp, err := utils.SharedHTTPClient.Do(req)
	if err != nil {
		return types.TokenInfo{}, fmt.Errorf("IdC刷新token请求失败: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return types.TokenInfo{}, fmt.Errorf("IdC刷新token失败: 状态码 %d, 响应: %s", resp.StatusCode, string(body))
	}

	// 解析响应
	var refreshResp types.RefreshResponse
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return types.TokenInfo{}, fmt.Errorf("读取IdC响应失败: %v", err)
	}

	logger.Debug("IdC API响应内容", logger.String("response_body", string(body)))

	if err := utils.SafeUnmarshal(body, &refreshResp); err != nil {
		return types.TokenInfo{}, fmt.Errorf("解析IdC刷新响应失败: %v", err)
	}

	logger.Debug("新的IdC Access Token", logger.String("access_token", refreshResp.AccessToken))
	logger.Debug("IdC Token过期信息", logger.Int("expires_in_seconds", refreshResp.ExpiresIn))

	// 转换为统一的Token结构
	var token types.Token
	token.AccessToken = refreshResp.AccessToken
	token.RefreshToken = refreshToken // 保持原始refresh token
	token.ExpiresIn = refreshResp.ExpiresIn
	token.ExpiresAt = time.Now().Add(time.Duration(refreshResp.ExpiresIn) * time.Second)

	logger.Info("IdC Token过期时间已计算",
		logger.String("expires_at", token.ExpiresAt.Format("2006-01-02 15:04:05")),
		logger.Int("expires_in_seconds", refreshResp.ExpiresIn))

	// 🚀 关键改进：token刷新后立即检查使用限制
	logger.Debug("开始检查IdC token使用限制")
	enhancedToken := CheckAndEnhanceToken(token)

	// 记录增强后的token状态
	logger.Info("IdC Token使用状态检查完成",
		logger.String("user_email", enhancedToken.GetUserEmailDisplay()),
		logger.String("token_preview", enhancedToken.TokenPreview),
		logger.Int("available_vibe_count", enhancedToken.GetAvailableVIBECount()),
		logger.Bool("is_usable", enhancedToken.IsUsable()))

	// 如果token不可用，记录警告但仍然返回（让上层决定如何处理）
	if !enhancedToken.IsUsable() {
		logger.Warn("IdC Token已无可用额度",
			logger.String("user_email", enhancedToken.GetUserEmailDisplay()),
			logger.String("token_preview", enhancedToken.TokenPreview),
			logger.Int("available_count", enhancedToken.AvailableCount),
			logger.String("recommendation", "考虑切换到其他token"))
	}

	return token, nil
}

// tryRefreshToken 尝试刷新单个token (social方式)
func tryRefreshToken(refreshToken string) (types.TokenInfo, error) {
	// 准备刷新请求
	refreshReq := types.RefreshRequest{
		RefreshToken: refreshToken,
	}

	reqBody, err := utils.FastMarshal(refreshReq)
	if err != nil {
		return types.TokenInfo{}, fmt.Errorf("序列化请求失败: %v", err)
	}

	logger.Debug("发送token刷新请求", logger.String("url", globalConfig.RefreshTokenURL))

	// 发送刷新请求
	req, err := http.NewRequest("POST", globalConfig.RefreshTokenURL, bytes.NewBuffer(reqBody))
	if err != nil {
		return types.TokenInfo{}, fmt.Errorf("创建请求失败: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := utils.SharedHTTPClient.Do(req)
	if err != nil {
		return types.TokenInfo{}, fmt.Errorf("刷新token请求失败: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return types.TokenInfo{}, fmt.Errorf("刷新token失败: 状态码 %d, 响应: %s", resp.StatusCode, string(body))
	}

	// 解析响应
	var refreshResp types.RefreshResponse
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return types.TokenInfo{}, fmt.Errorf("读取响应失败: %v", err)
	}

	logger.Debug("API响应内容", logger.String("response_body", string(body)))

	if err := utils.SafeUnmarshal(body, &refreshResp); err != nil {
		return types.TokenInfo{}, fmt.Errorf("解析刷新响应失败: %v", err)
	}

	logger.Debug("新的Access Token", logger.String("access_token", refreshResp.AccessToken))
	logger.Debug("Token过期信息", logger.Int("expires_in_seconds", refreshResp.ExpiresIn))
	logger.Debug("获取到的ProfileArn", logger.String("profile_arn", refreshResp.ProfileArn))

	// 使用新的Token结构进行转换
	var token types.Token
	token.FromRefreshResponse(refreshResp, refreshToken)

	logger.Info("Token过期时间已计算",
		logger.String("expires_at", token.ExpiresAt.Format("2006-01-02 15:04:05")),
		logger.Int("expires_in_seconds", refreshResp.ExpiresIn))

	// 🚀 关键改进：token刷新后立即检查使用限制
	logger.Debug("开始检查Social token使用限制")
	enhancedToken := CheckAndEnhanceToken(token)

	// 记录增强后的token状态
	logger.Info("Social Token使用状态检查完成",
		logger.String("user_email", enhancedToken.GetUserEmailDisplay()),
		logger.String("token_preview", enhancedToken.TokenPreview),
		logger.Int("available_vibe_count", enhancedToken.GetAvailableVIBECount()),
		logger.Bool("is_usable", enhancedToken.IsUsable()))

	// 如果token不可用，记录警告但仍然返回（让上层决定如何处理）
	if !enhancedToken.IsUsable() {
		logger.Warn("Social Token已无可用额度",
			logger.String("user_email", enhancedToken.GetUserEmailDisplay()),
			logger.String("token_preview", enhancedToken.TokenPreview),
			logger.Int("available_count", enhancedToken.AvailableCount),
			logger.String("recommendation", "考虑切换到其他token"))
	}

	// 返回兼容的TokenInfo（由于类型别名，这是相同的类型）
	return token, nil
}

// GetToken 获取当前token，使用token池进行轮换
func GetToken() (types.TokenInfo, error) {
	pool := getTokenPool()
	cache := getAtomicCache()

	if pool == nil {
		return types.TokenInfo{}, fmt.Errorf("token池未初始化，请检查token配置")
	}

	// 使用轮换策略获取token
	return getRotatedToken(pool, cache)
}

// getRotatedToken 多token轮换模式
func getRotatedToken(pool *types.TokenPool, cache *utils.AtomicTokenCache) (types.TokenInfo, error) {
	// 获取下一个访问索引
	accessIdx := pool.GetNextAccessIndex()

	logger.Debug("使用轮换索引", logger.Int("access_index", accessIdx))

	// 尝试从原子缓存获取对应索引的token
	if cachedToken, exists := cache.Get(accessIdx); exists {
		logger.Debug("使用缓存的Access Token",
			logger.Int("token_index", accessIdx),
			logger.String("access_token", cachedToken.AccessToken),
			logger.String("expires_at", cachedToken.ExpiresAt.Format("2006-01-02 15:04:05")))
		return *cachedToken, nil
	}

	// 缓存中没有或已过期，需要刷新对应的token
	logger.Debug("索引token缓存失效，开始刷新", logger.Int("token_index", accessIdx))

	// 刷新指定索引的token
	tokenInfo, err := refreshTokenByIndex(pool, accessIdx)
	if err != nil {
		// 如果当前索引的token刷新失败，标记为失败并返回错误
		logger.Error("当前索引token刷新失败", logger.Int("failed_index", accessIdx), logger.Err(err))
		pool.MarkTokenFailed(accessIdx)
		return types.TokenInfo{}, fmt.Errorf("token刷新失败: %v", err)
	}

	// 刷新成功，缓存新的token（设为热点）
	cache.SetHot(accessIdx, &tokenInfo)
	pool.MarkTokenSuccess(accessIdx)

	return tokenInfo, nil
}

// refreshTokenByIndex 刷新指定索引的token，支持并发控制
func refreshTokenByIndex(pool *types.TokenPool, idx int) (types.TokenInfo, error) {
	if idx < 0 || idx >= pool.GetTokenCount() {
		return types.TokenInfo{}, fmt.Errorf("无效的token索引: %d", idx)
	}

	refreshMgr := getRefreshManager()

	// 检查是否已经在刷新中
	_, isNew := refreshMgr.StartRefresh(idx)
	if !isNew {
		// 其他goroutine正在刷新，等待结果
		logger.Debug("Token正在被其他请求刷新，等待完成", logger.Int("token_index", idx))

		tokenInfo, err := refreshMgr.WaitForRefresh(idx, 30*time.Second) // 30秒超时
		if err != nil {
			return types.TokenInfo{}, fmt.Errorf("等待token %d刷新失败: %v", idx, err)
		}
		return *tokenInfo, nil
	}

	// 获取对应索引的refresh token配置
	provider := getConfigProvider()
	configs, err := provider.LoadConfigs()
	if err != nil {
		refreshMgr.CompleteRefresh(idx, nil, fmt.Errorf("加载配置失败: %v", err))
		return types.TokenInfo{}, fmt.Errorf("加载配置失败: %v", err)
	}

	if idx >= len(configs) {
		err := fmt.Errorf("token索引超出配置范围: %d", idx)
		refreshMgr.CompleteRefresh(idx, nil, err)
		return types.TokenInfo{}, err
	}

	targetConfig := configs[idx]
	if targetConfig.Disabled {
		err := fmt.Errorf("索引%d的token配置已禁用", idx)
		refreshMgr.CompleteRefresh(idx, nil, err)
		return types.TokenInfo{}, err
	}

	// 尝试刷新指定的token
	tokenInfo, err := tryRefreshTokenByAuthMethod(targetConfig.RefreshToken)

	// 通知刷新管理器完成状态
	refreshMgr.CompleteRefresh(idx, &tokenInfo, err)

	return tokenInfo, err
}

// GetTokenPool 获取token池实例（公开方法，用于Dashboard）
func GetTokenPool() *types.TokenPool {
	return getTokenPool()
}

// RefreshTokenByIndex 根据索引刷新并获取token（公开方法，用于Dashboard）
func RefreshTokenByIndex(index int) (types.TokenInfo, error) {
	pool := getTokenPool()
	if pool == nil {
		return types.TokenInfo{}, fmt.Errorf("token池未初始化")
	}

	return refreshTokenByIndex(pool, index)
}

// RefreshTokenByIndexWithAuthType 根据索引刷新并获取带认证类型的token（用于Dashboard）
func RefreshTokenByIndexWithAuthType(index int) (types.TokenWithAuthType, error) {
	pool := getTokenPool()
	if pool == nil {
		return types.TokenWithAuthType{}, fmt.Errorf("token池未初始化")
	}

	// 获取配置来确定认证类型
	provider := getConfigProvider()
	configs, err := provider.LoadConfigs()
	if err != nil {
		return types.TokenWithAuthType{}, fmt.Errorf("加载配置失败: %v", err)
	}

	if index >= len(configs) {
		return types.TokenWithAuthType{}, fmt.Errorf("token索引超出配置范围: %d", index)
	}

	// 刷新token
	tokenInfo, err := refreshTokenByIndex(pool, index)
	if err != nil {
		return types.TokenWithAuthType{}, err
	}

	// 返回带认证类型的token
	return types.TokenWithAuthType{
		TokenInfo: tokenInfo,
		AuthType:  configs[index].AuthType,
	}, nil
}

// ClearTokenCache 清除token缓存（用于强制刷新）
func ClearTokenCache() {
	cache := getAtomicCache()
	cache.Clear()
	logger.Info("原子Token缓存已清除")
}

var (
	enhancedTokenCache      = make(map[string]*types.TokenWithUsage)
	enhancedTokenCacheMutex = &sync.RWMutex{}
)

// GetEnhancedToken gets a token and enhances it with usage information.
func GetEnhancedToken() (*types.TokenWithUsage, error) {
	tokenInfo, err := GetToken()
	if err != nil {
		return nil, err
	}

	enhancedTokenCacheMutex.RLock()
	cachedToken, ok := enhancedTokenCache[tokenInfo.AccessToken]
	enhancedTokenCacheMutex.RUnlock()

	if ok && !cachedToken.NeedsUsageRefresh() {
		logger.Debug("Using cached enhanced token", logger.String("token_preview", cachedToken.TokenPreview))
		return cachedToken, nil
	}

	logger.Debug("Enhanced token not in cache or needs refresh, checking usage", logger.String("token_preview", tokenInfo.AccessToken[:20]+"..."))
	enhancedToken := CheckAndEnhanceToken(tokenInfo)

	enhancedTokenCacheMutex.Lock()
	enhancedTokenCache[enhancedToken.AccessToken] = &enhancedToken
	enhancedTokenCacheMutex.Unlock()

	return &enhancedToken, nil
}

// DecrementVIBECount decrements the VIBE count for a given token.
func DecrementVIBECount(accessToken string) {
	enhancedTokenCacheMutex.Lock()
	defer enhancedTokenCacheMutex.Unlock()

	if enhancedToken, ok := enhancedTokenCache[accessToken]; ok {
		if enhancedToken.UsageLimits != nil {
			for i, breakdown := range enhancedToken.UsageLimits.UsageBreakdownList {
				if breakdown.ResourceType == "VIBE" {
					// Decrement the available count by incrementing the current usage
					enhancedToken.UsageLimits.UsageBreakdownList[i].CurrentUsage++
					logger.Info("VIBE usage incremented",
						logger.String("token_preview", enhancedToken.TokenPreview),
						logger.Int("new_usage", enhancedToken.UsageLimits.UsageBreakdownList[i].CurrentUsage))
					return
				}
			}
		}
	}
}

// GetAtomicCache 获取原子缓存实例（公开方法，用于Dashboard）
func GetAtomicCache() *utils.AtomicTokenCache {
	return getAtomicCache()
}
