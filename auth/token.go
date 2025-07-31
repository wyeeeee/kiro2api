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
)

// 全局token池和缓存实例
var (
	tokenPool   *types.TokenPool
	atomicCache *utils.AtomicTokenCache // 使用原子缓存替代传统缓存
	poolOnce    sync.Once
	cacheOnce   sync.Once
)

// initTokenPool 初始化token池
func initTokenPool() {
	refreshTokens := os.Getenv("AWS_REFRESHTOKEN")
	if refreshTokens == "" {
		return
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
		logger.Info("Token池初始化完成", logger.Int("token_count", len(validTokens)))
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

// getAtomicCache 获取原子缓存实例
func getAtomicCache() *utils.AtomicTokenCache {
	cacheOnce.Do(initTokenCache)
	return atomicCache
}

// RefreshTokenForServer 刷新token，用于服务器模式，返回错误而不是退出程序
func RefreshTokenForServer() error {
	_, err := refreshTokenAndReturn()
	return err
}

// refreshTokenAndReturn 刷新token并返回TokenInfo，使用token池管理
func refreshTokenAndReturn() (types.TokenInfo, error) {
	pool := getTokenPool()
	if pool == nil {
		logger.Error("AWS_REFRESHTOKEN环境变量未设置")
		return types.TokenInfo{}, fmt.Errorf("AWS_REFRESHTOKEN环境变量未设置，请设置后重新启动服务")
	}

	// 尝试从token池获取可用token
	for {
		refreshToken, tokenIdx, hasToken := pool.GetNextToken()
		if !hasToken {
			logger.Error("所有refresh token都已达到最大重试次数")
			return types.TokenInfo{}, fmt.Errorf("所有refresh token都已达到最大重试次数")
		}

		logger.Debug("尝试使用refresh token", logger.Int("token_index", tokenIdx+1))

		// 尝试刷新token
		tokenInfo, err := tryRefreshToken(refreshToken)
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

// tryRefreshToken 尝试刷新单个token
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

	if err := utils.FastUnmarshal(body, &refreshResp); err != nil {
		return types.TokenInfo{}, fmt.Errorf("解析刷新响应失败: %v", err)
	}

	logger.Debug("新的Access Token", logger.String("access_token", refreshResp.AccessToken))
	logger.Debug("Token过期信息", logger.Int("expires_in_seconds", refreshResp.ExpiresIn))

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

// refreshTokenByIndex 刷新指定索引的token
func refreshTokenByIndex(pool *types.TokenPool, idx int) (types.TokenInfo, error) {
	if idx < 0 || idx >= pool.GetTokenCount() {
		return types.TokenInfo{}, fmt.Errorf("无效的token索引: %d", idx)
	}

	// 获取对应索引的refresh token
	tokens := os.Getenv("AWS_REFRESHTOKEN")
	if tokens == "" {
		return types.TokenInfo{}, fmt.Errorf("AWS_REFRESHTOKEN环境变量未设置")
	}

	tokenList := strings.Split(tokens, ",")
	if idx >= len(tokenList) {
		return types.TokenInfo{}, fmt.Errorf("token索引超出范围: %d", idx)
	}

	refreshToken := strings.TrimSpace(tokenList[idx])
	if refreshToken == "" {
		return types.TokenInfo{}, fmt.Errorf("索引%d的refresh token为空", idx)
	}

	// 尝试刷新指定的token
	return tryRefreshToken(refreshToken)
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

// ClearTokenCacheByIndex 清除指定索引的token缓存
func ClearTokenCacheByIndex(idx int) {
	cache := getAtomicCache()
	cache.Delete(idx)
	logger.Info("指定索引Token缓存已清除", logger.Int("index", idx))
}

// GetTokenPoolStats 获取token池统计信息
func GetTokenPoolStats() map[string]any {
	pool := getTokenPool()
	if pool == nil {
		return map[string]any{
			"pool_enabled": false,
			"message":      "Token池未初始化",
		}
	}

	stats := pool.GetStats()
	stats["pool_enabled"] = true

	// 添加原子缓存统计信息
	cache := getAtomicCache()
	cacheStats := cache.GetStats()
	for k, v := range cacheStats {
		stats["cache_"+k] = v
	}

	return stats
}

// ForceRefreshToken 强制刷新指定索引的token
func ForceRefreshToken(idx int) error {
	pool := getTokenPool()
	if pool == nil {
		return fmt.Errorf("Token池未初始化")
	}

	cache := getAtomicCache()

	// 清除指定索引的缓存
	cache.Delete(idx)

	// 刷新指定索引的token
	tokenInfo, err := refreshTokenByIndex(pool, idx)
	if err != nil {
		return fmt.Errorf("强制刷新索引%d的token失败: %v", idx, err)
	}

	// 缓存新的token
	cache.Set(idx, &tokenInfo)
	pool.MarkTokenSuccess(idx)

	logger.Info("强制刷新Token成功", logger.Int("token_index", idx))
	return nil
}
