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

	"github.com/bytedance/sonic"
)

// 全局token池和缓存实例
var (
	tokenPool  *types.TokenPool
	tokenCache *types.TokenCache
	poolOnce   sync.Once
	cacheOnce  sync.Once
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

// initTokenCache 初始化token缓存
func initTokenCache() {
	// token缓存时间现在基于token自身的ExpiresAt字段
	tokenCache = types.NewTokenCache()
	logger.Info("Token缓存初始化完成", logger.String("expiry_mode", "基于token自身过期时间"))
}

// getTokenCache 获取token缓存实例
func getTokenCache() *types.TokenCache {
	cacheOnce.Do(initTokenCache)
	return tokenCache
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

	reqBody, err := sonic.Marshal(refreshReq)
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

	if err := sonic.Unmarshal(body, &refreshResp); err != nil {
		return types.TokenInfo{}, fmt.Errorf("解析刷新响应失败: %v", err)
	}

	logger.Debug("新的Access Token", logger.String("access_token", refreshResp.AccessToken))
	logger.Debug("Token过期信息", logger.Int("expires_in_seconds", refreshResp.ExpiresIn))

	// 根据expiresIn计算过期时间
	expiresAt := time.Now().Add(time.Duration(refreshResp.ExpiresIn) * time.Second)
	logger.Info("Token过期时间已计算",
		logger.String("expires_at", expiresAt.Format("2006-01-02 15:04:05")),
		logger.Int("expires_in_seconds", refreshResp.ExpiresIn))

	// 返回包含有效AccessToken的TokenInfo
	return types.TokenInfo{
		RefreshToken: refreshToken,
		AccessToken:  refreshResp.AccessToken,
		ExpiresAt:    expiresAt, // 使用计算出的过期时间
	}, nil
}

// GetToken 获取当前token，优先使用缓存，token失效时才刷新
func GetToken() (types.TokenInfo, error) {
	cache := getTokenCache()

	// 尝试从缓存获取token
	if cachedToken, exists := cache.Get(); exists {
		logger.Debug("使用缓存的Access Token", logger.String("expires_at", cachedToken.ExpiresAt.Format("2006-01-02 15:04:05")))
		return cachedToken, nil
	}

	// 缓存中没有或已过期，刷新token
	logger.Debug("缓存中没有有效token，开始刷新")
	tokenInfo, err := refreshTokenAndReturn()
	if err != nil {
		return types.TokenInfo{}, err
	}

	// 缓存新的token
	cache.Set(tokenInfo)
	logger.Debug("新token已缓存", logger.String("expires_at", tokenInfo.ExpiresAt.Format("2006-01-02 15:04:05")))

	return tokenInfo, nil
}

// ClearTokenCache 清除token缓存（用于强制刷新）
func ClearTokenCache() {
	cache := getTokenCache()
	cache.Clear()
	logger.Info("Token缓存已清除")
}
