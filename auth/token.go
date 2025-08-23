package auth

import (
	"bytes"
	"fmt"
	"io"
	"kiro2api/config"
	"kiro2api/logger"
	"kiro2api/types"
	"kiro2api/utils"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// 全局token池和缓存实例
var (
	tokenPool      *types.TokenPool
	atomicCache    *utils.AtomicTokenCache    // 使用原子缓存替代传统缓存
	refreshManager *utils.TokenRefreshManager // token刷新并发控制管理器
	poolOnce       sync.Once
	cacheOnce      sync.Once
	refreshOnce    sync.Once
)

// initTokenPool 初始化token池
func initTokenPool() {
	authMethod := config.GetAuthMethod()

	var refreshTokens string
	switch authMethod {
	case config.AuthMethodIdC:
		refreshTokens = os.Getenv("IDC_REFRESH_TOKEN")
		if refreshTokens == "" {
			logger.Debug("IDC_REFRESH_TOKEN环境变量未设置，token池初始化失败")
			return
		}
	case config.AuthMethodSocial:
		refreshTokens = os.Getenv("AWS_REFRESHTOKEN")
		if refreshTokens == "" {
			logger.Debug("AWS_REFRESHTOKEN环境变量未设置，token池初始化失败")
			return
		}
	}

	tokens := strings.Split(refreshTokens, ",")
	for i := range tokens {
		tokens[i] = strings.TrimSpace(tokens[i])
	}

	// 过滤空token
	var validTokens []string
	for _, token := range tokens {
		if token != "" {
			validTokens = append(validTokens, token)
		}
	}

	if len(validTokens) > 0 {
		tokenPool = types.NewTokenPool(validTokens, 3) // 最大重试3次
		logger.Info("Token池初始化完成", logger.Int("token_count", len(validTokens)), logger.String("auth_method", string(authMethod)))
	}
}

// getTokenPool 获取token池实例
func getTokenPool() *types.TokenPool {
	poolOnce.Do(initTokenPool)
	return tokenPool
}

// initTokenCache 初始化原子token缓存
func initTokenCache() {
	atomicCache = utils.NewAtomicTokenCache()
	// 启动后台清理协程
	atomicCache.StartCleanupRoutine()
	logger.Info("原子Token缓存初始化完成", logger.String("type", "atomic_cache"))
}

// initRefreshManager 初始化token刷新管理器
func initRefreshManager() {
	refreshManager = utils.NewTokenRefreshManager()
	logger.Info("Token刷新管理器初始化完成", logger.String("type", "refresh_manager"))
}

// getAtomicCache 获取原子缓存实例
func getAtomicCache() *utils.AtomicTokenCache {
	cacheOnce.Do(initTokenCache)
	return atomicCache
}

// getRefreshManager 获取刷新管理器实例
func getRefreshManager() *utils.TokenRefreshManager {
	refreshOnce.Do(initRefreshManager)
	return refreshManager
}

// refreshTokenAndReturn 刷新token并返回TokenInfo，使用token池管理
func refreshTokenAndReturn() (types.TokenInfo, error) {
	pool := getTokenPool()
	if pool == nil {
		authMethod := config.GetAuthMethod()
		switch authMethod {
		case config.AuthMethodIdC:
			logger.Error("IDC_REFRESH_TOKEN环境变量未设置")
			return types.TokenInfo{}, fmt.Errorf("IDC_REFRESH_TOKEN环境变量未设置，请设置后重新启动服务")
		case config.AuthMethodSocial:
			logger.Error("AWS_REFRESHTOKEN环境变量未设置")
			return types.TokenInfo{}, fmt.Errorf("AWS_REFRESHTOKEN环境变量未设置，请设置后重新启动服务")
		}
	}

	// 尝试从token池获取可用token
	for {
		refreshToken, tokenIdx, hasToken := pool.GetNextToken()
		if !hasToken {
			logger.Error("所有refresh token都已达到最大重试次数")
			return types.TokenInfo{}, fmt.Errorf("所有refresh token都已达到最大重试次数")
		}

		logger.Debug("尝试使用refresh token", logger.Int("token_index", tokenIdx+1))

		// 根据认证方式尝试刷新token
		tokenInfo, err := tryRefreshTokenByAuthMethod(refreshToken)
		if err != nil {
			logger.Error("Token刷新失败", logger.Err(err), logger.Int("token_index", tokenIdx+1))
			pool.MarkTokenFailed(tokenIdx)
			continue
		}

		// 刷新成功，重置失败计数
		pool.MarkTokenSuccess(tokenIdx)
		logger.Info("Token刷新成功", logger.Int("token_index", tokenIdx+1))
		return tokenInfo, nil
	}
}

// tryRefreshTokenByAuthMethod 根据认证方式刷新token
func tryRefreshTokenByAuthMethod(refreshToken string) (types.TokenInfo, error) {
	authMethod := config.GetAuthMethod()

	switch authMethod {
	case config.AuthMethodIdC:
		return tryRefreshIdcToken(refreshToken)
	case config.AuthMethodSocial:
		return tryRefreshToken(refreshToken)
	default:
		return types.TokenInfo{}, fmt.Errorf("不支持的认证方式: %v", authMethod)
	}
}

// tryRefreshIdcToken 使用IdC认证方式刷新token
func tryRefreshIdcToken(refreshToken string) (types.TokenInfo, error) {
	clientId := os.Getenv("IDC_CLIENT_ID")
	clientSecret := os.Getenv("IDC_CLIENT_SECRET")

	if clientId == "" || clientSecret == "" {
		return types.TokenInfo{}, fmt.Errorf("IDC_CLIENT_ID和IDC_CLIENT_SECRET环境变量必须设置")
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

	logger.Debug("发送IdC token刷新请求", logger.String("url", config.IdcRefreshTokenURL))

	// 发送刷新请求
	req, err := http.NewRequest("POST", config.IdcRefreshTokenURL, bytes.NewBuffer(reqBody))
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

	logger.Debug("发送token刷新请求", logger.String("url", config.RefreshTokenURL))

	// 发送刷新请求
	req, err := http.NewRequest("POST", config.RefreshTokenURL, bytes.NewBuffer(reqBody))
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

	// 返回兼容的TokenInfo（由于类型别名，这是相同的类型）
	return token, nil
}

// GetToken 获取当前token，支持多token轮换使用
func GetToken() (types.TokenInfo, error) {
	pool := getTokenPool()
	cache := getAtomicCache()

	// 如果没有token池，回退到原有逻辑
	if pool == nil {
		return getSingleToken(cache)
	}

	// 使用轮换策略获取token
	return getRotatedToken(pool, cache)
}

// getSingleToken 单token模式（向后兼容）
func getSingleToken(cache *utils.AtomicTokenCache) (types.TokenInfo, error) {
	// 尝试从热点缓存获取token（最快路径）
	if cachedToken, exists := cache.GetHot(); exists {
		logger.Debug("使用热点缓存的Access Token",
			logger.String("access_token", cachedToken.AccessToken),
			logger.String("expires_at", cachedToken.ExpiresAt.Format("2006-01-02 15:04:05")))
		return *cachedToken, nil
	}

	// 缓存中没有或已过期，刷新token
	logger.Debug("缓存中没有有效token，开始刷新")
	tokenInfo, err := refreshTokenAndReturn()
	if err != nil {
		return types.TokenInfo{}, err
	}

	// 缓存新的token为热点缓存
	cache.SetHot(0, &tokenInfo)
	logger.Debug("新token已缓存为热点", logger.String("expires_at", tokenInfo.ExpiresAt.Format("2006-01-02 15:04:05")))

	return tokenInfo, nil
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
		// 如果当前索引的token刷新失败，尝试其他可用的token
		logger.Warn("当前索引token刷新失败，尝试其他token", logger.Int("failed_index", accessIdx), logger.Err(err))

		// 标记当前token失败
		pool.MarkTokenFailed(accessIdx)

		// 尝试获取其他可用token
		return fallbackToAvailableToken(pool, cache)
	}

	// 刷新成功，缓存新的token（设为热点）
	cache.SetHot(accessIdx, &tokenInfo)
	pool.MarkTokenSuccess(accessIdx)

	logger.Info("Token刷新成功", logger.Int("token_index", accessIdx))
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

	// 这是新的刷新任务，执行实际的刷新逻辑
	logger.Debug("开始执行Token刷新", logger.Int("token_index", idx))

	// 获取对应索引的refresh token
	authMethod := config.GetAuthMethod()
	var tokens string

	switch authMethod {
	case config.AuthMethodIdC:
		tokens = os.Getenv("IDC_REFRESH_TOKEN")
		if tokens == "" {
			refreshMgr.CompleteRefresh(idx, nil, fmt.Errorf("IDC_REFRESH_TOKEN环境变量未设置"))
			return types.TokenInfo{}, fmt.Errorf("IDC_REFRESH_TOKEN环境变量未设置")
		}
	case config.AuthMethodSocial:
		tokens = os.Getenv("AWS_REFRESHTOKEN")
		if tokens == "" {
			refreshMgr.CompleteRefresh(idx, nil, fmt.Errorf("AWS_REFRESHTOKEN环境变量未设置"))
			return types.TokenInfo{}, fmt.Errorf("AWS_REFRESHTOKEN环境变量未设置")
		}
	}

	tokenList := strings.Split(tokens, ",")
	if idx >= len(tokenList) {
		err := fmt.Errorf("token索引超出范围: %d", idx)
		refreshMgr.CompleteRefresh(idx, nil, err)
		return types.TokenInfo{}, err
	}

	refreshToken := strings.TrimSpace(tokenList[idx])
	if refreshToken == "" {
		err := fmt.Errorf("索引%d的refresh token为空", idx)
		refreshMgr.CompleteRefresh(idx, nil, err)
		return types.TokenInfo{}, err
	}

	// 尝试刷新指定的token
	tokenInfo, err := tryRefreshTokenByAuthMethod(refreshToken)

	// 通知刷新管理器完成状态
	refreshMgr.CompleteRefresh(idx, &tokenInfo, err)

	return tokenInfo, err
}

// fallbackToAvailableToken 回退到其他可用token
func fallbackToAvailableToken(pool *types.TokenPool, cache *utils.AtomicTokenCache) (types.TokenInfo, error) {
	// 尝试使用refreshTokenAndReturn获取任何可用的token
	tokenInfo, err := refreshTokenAndReturn()
	if err != nil {
		return types.TokenInfo{}, fmt.Errorf("所有token都无法使用: %v", err)
	}

	// 缓存为热点token（索引为-1表示备用）
	cache.SetHot(-1, &tokenInfo)
	return tokenInfo, nil
}

// ClearTokenCache 清除token缓存（用于强制刷新）
func ClearTokenCache() {
	cache := getAtomicCache()
	cache.Clear()
	logger.Info("原子Token缓存已清除")
}
