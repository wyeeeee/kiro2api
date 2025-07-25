package types

import (
	"sync"
	"time"
)

// TokenInfo 表示token信息的统一结构
type TokenInfo struct {
	AccessToken  string    `json:"accessToken"`
	RefreshToken string    `json:"refreshToken"`
	ExpiresAt    time.Time `json:"expiresAt,omitempty"`
}

// RefreshRequest 刷新token的请求结构
type RefreshRequest struct {
	RefreshToken string `json:"refreshToken"`
}

// TokenPool token池管理结构
type TokenPool struct {
	tokens      []string    // refresh token列表
	currentIdx  int         // 当前使用的token索引
	failedCount map[int]int // 每个token的失败次数
	mutex       sync.RWMutex
	maxRetries  int // 最大重试次数
}

// NewTokenPool 创建新的token池
func NewTokenPool(tokens []string, maxRetries int) *TokenPool {
	return &TokenPool{
		tokens:      tokens,
		currentIdx:  0,
		failedCount: make(map[int]int),
		maxRetries:  maxRetries,
	}
}

// GetNextToken 获取下一个可用的token
func (tp *TokenPool) GetNextToken() (string, int, bool) {
	tp.mutex.Lock()
	defer tp.mutex.Unlock()

	startIdx := tp.currentIdx
	for {
		// 检查当前token是否超过最大重试次数
		if tp.failedCount[tp.currentIdx] < tp.maxRetries {
			token := tp.tokens[tp.currentIdx]
			idx := tp.currentIdx
			tp.currentIdx = (tp.currentIdx + 1) % len(tp.tokens)
			return token, idx, true
		}

		// 移动到下一个token
		tp.currentIdx = (tp.currentIdx + 1) % len(tp.tokens)
		
		// 如果回到起始位置，说明所有token都不可用
		if tp.currentIdx == startIdx {
			return "", -1, false
		}
	}
}

// MarkTokenFailed 标记token失败
func (tp *TokenPool) MarkTokenFailed(idx int) {
	tp.mutex.Lock()
	defer tp.mutex.Unlock()
	tp.failedCount[idx]++
}

// MarkTokenSuccess 标记token成功，重置失败计数
func (tp *TokenPool) MarkTokenSuccess(idx int) {
	tp.mutex.Lock()
	defer tp.mutex.Unlock()
	tp.failedCount[idx] = 0
}

// ResetFailedCounts 重置所有失败计数（可用于定期重置）
func (tp *TokenPool) ResetFailedCounts() {
	tp.mutex.Lock()
	defer tp.mutex.Unlock()
	tp.failedCount = make(map[int]int)
}

// GetStats 获取token池统计信息
func (tp *TokenPool) GetStats() map[string]interface{} {
	tp.mutex.RLock()
	defer tp.mutex.RUnlock()
	
	stats := map[string]interface{}{
		"total_tokens":   len(tp.tokens),
		"current_index":  tp.currentIdx,
		"max_retries":    tp.maxRetries,
		"failed_counts":  make(map[int]int),
	}
	
	for idx, count := range tp.failedCount {
		stats["failed_counts"].(map[int]int)[idx] = count
	}
	
	return stats
}

// TokenCache token缓存结构
type TokenCache struct {
	cachedToken *TokenInfo
	mutex       sync.RWMutex
}

// NewTokenCache 创建新的token缓存
func NewTokenCache() *TokenCache {
	return &TokenCache{}
}

// Get 获取缓存的token
func (tc *TokenCache) Get() (TokenInfo, bool) {
	tc.mutex.RLock()
	defer tc.mutex.RUnlock()
	
	if tc.cachedToken == nil || time.Now().After(tc.cachedToken.ExpiresAt) {
		return TokenInfo{}, false
	}
	
	return *tc.cachedToken, true
}

// Set 设置缓存的token
func (tc *TokenCache) Set(token TokenInfo) {
	tc.mutex.Lock()
	defer tc.mutex.Unlock()
	
	tc.cachedToken = &token
}

// Clear 清除缓存的token
func (tc *TokenCache) Clear() {
	tc.mutex.Lock()
	defer tc.mutex.Unlock()
	tc.cachedToken = nil
}

// IsExpired 检查token是否过期
func (tc *TokenCache) IsExpired() bool {
	tc.mutex.RLock()
	defer tc.mutex.RUnlock()
	
	if tc.cachedToken == nil {
		return true
	}
	
	return time.Now().After(tc.cachedToken.ExpiresAt)
}

