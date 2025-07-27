package types

import (
	"sync"
	"time"
)

// Token 统一的token管理结构，合并了TokenInfo、RefreshResponse、RefreshRequest的功能
type Token struct {
	// 核心token信息
	AccessToken  string    `json:"accessToken,omitempty"`
	RefreshToken string    `json:"refreshToken"`
	ExpiresAt    time.Time `json:"expiresAt,omitempty"`
	
	// API响应字段
	ExpiresIn  int    `json:"expiresIn,omitempty"`  // 多少秒后失效，来自RefreshResponse
	ProfileArn string `json:"profileArn,omitempty"` // 来自RefreshResponse
}

// ToTokenInfo 转换为TokenInfo格式（向后兼容）
func (t *Token) ToTokenInfo() TokenInfo {
	return TokenInfo{
		AccessToken:  t.AccessToken,
		RefreshToken: t.RefreshToken,
		ExpiresAt:    t.ExpiresAt,
	}
}

// ToRefreshResponse 转换为RefreshResponse格式（向后兼容）
func (t *Token) ToRefreshResponse() RefreshResponse {
	return RefreshResponse{
		AccessToken:  t.AccessToken,
		RefreshToken: t.RefreshToken,
		ExpiresIn:    t.ExpiresIn,
		ProfileArn:   t.ProfileArn,
	}
}

// ToRefreshRequest 转换为RefreshRequest格式（向后兼容）
func (t *Token) ToRefreshRequest() RefreshRequest {
	return RefreshRequest{
		RefreshToken: t.RefreshToken,
	}
}

// FromRefreshResponse 从RefreshResponse创建Token
func (t *Token) FromRefreshResponse(resp RefreshResponse, originalRefreshToken string) {
	t.AccessToken = resp.AccessToken
	t.RefreshToken = originalRefreshToken // 保持原始refresh token
	t.ExpiresIn = resp.ExpiresIn
	t.ProfileArn = resp.ProfileArn
	t.ExpiresAt = time.Now().Add(time.Duration(resp.ExpiresIn) * time.Second)
}

// IsExpired 检查token是否已过期
func (t *Token) IsExpired() bool {
	return time.Now().After(t.ExpiresAt)
}

// 兼容性别名 - 逐步迁移时使用
type TokenInfo = Token     // TokenInfo现在是Token的别名
type RefreshResponse struct {
	AccessToken  string `json:"accessToken"`
	ExpiresIn    int    `json:"expiresIn"` // 多少秒后失效
	ProfileArn   string `json:"profileArn"`
	RefreshToken string `json:"refreshToken"`
}

type RefreshRequest struct {
	RefreshToken string `json:"refreshToken"`
}

// TokenPool token池管理结构
type TokenPool struct {
	tokens       []string    // refresh token列表
	currentIdx   int         // 当前使用的token索引
	accessIdx    int         // 当前访问的access token索引（用于轮换）
	failedCount  map[int]int // 每个token的失败次数
	mutex        sync.RWMutex
	maxRetries   int // 最大重试次数
}

// NewTokenPool 创建新的token池
func NewTokenPool(tokens []string, maxRetries int) *TokenPool {
	return &TokenPool{
		tokens:      tokens,
		currentIdx:  0,
		accessIdx:   0,
		failedCount: make(map[int]int),
		maxRetries:  maxRetries,
	}
}

// GetNextAccessIndex 获取下一个访问索引（用于轮换access token）
func (tp *TokenPool) GetNextAccessIndex() int {
	tp.mutex.Lock()
	defer tp.mutex.Unlock()
	
	// 找到下一个有效的access token索引
	startIdx := tp.accessIdx
	for {
		// 检查当前索引的token是否可用
		if tp.failedCount[tp.accessIdx] < tp.maxRetries {
			idx := tp.accessIdx
			tp.accessIdx = (tp.accessIdx + 1) % len(tp.tokens)
			return idx
		}
		
		// 移动到下一个索引
		tp.accessIdx = (tp.accessIdx + 1) % len(tp.tokens)
		
		// 如果回到起始位置，返回当前索引（即使可能不可用）
		if tp.accessIdx == startIdx {
			return tp.accessIdx
		}
	}
}

// GetCurrentAccessIndex 获取当前访问索引
func (tp *TokenPool) GetCurrentAccessIndex() int {
	tp.mutex.RLock()
	defer tp.mutex.RUnlock()
	return tp.accessIdx
}

// SetAccessIndex 设置访问索引
func (tp *TokenPool) SetAccessIndex(idx int) {
	tp.mutex.Lock()
	defer tp.mutex.Unlock()
	if idx >= 0 && idx < len(tp.tokens) {
		tp.accessIdx = idx
	}
}

// GetTokenCount 获取token总数
func (tp *TokenPool) GetTokenCount() int {
	tp.mutex.RLock()
	defer tp.mutex.RUnlock()
	return len(tp.tokens)
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



// GetStats 获取token池统计信息
func (tp *TokenPool) GetStats() map[string]any {
	tp.mutex.RLock()
	defer tp.mutex.RUnlock()

	stats := map[string]any{
		"total_tokens":  len(tp.tokens),
		"current_index": tp.currentIdx,
		"max_retries":   tp.maxRetries,
		"failed_counts": make(map[int]int),
	}

	for idx, count := range tp.failedCount {
		stats["failed_counts"].(map[int]int)[idx] = count
	}

	return stats
}

// TokenCache 多token缓存结构，支持按索引缓存多个access token
type TokenCache struct {
	cachedTokens map[int]*TokenInfo // 按refresh token索引缓存access token
	currentIdx   int                // 当前使用的token索引
	mutex        sync.RWMutex
}

// NewTokenCache 创建新的token缓存
func NewTokenCache() *TokenCache {
	return &TokenCache{
		cachedTokens: make(map[int]*TokenInfo),
		currentIdx:   0,
	}
}

// Get 获取当前轮换到的缓存token
func (tc *TokenCache) Get() (TokenInfo, bool) {
	tc.mutex.RLock()
	defer tc.mutex.RUnlock()

	// 获取当前索引的token
	if token, exists := tc.cachedTokens[tc.currentIdx]; exists {
		// 使用新的IsExpired方法检查token是否过期
		if !token.IsExpired() {
			return *token, true
		}
		// token已过期，删除缓存
		delete(tc.cachedTokens, tc.currentIdx)
	}

	return TokenInfo{}, false
}

// GetByIndex 根据索引获取特定的缓存token
func (tc *TokenCache) GetByIndex(idx int) (TokenInfo, bool) {
	tc.mutex.RLock()
	defer tc.mutex.RUnlock()

	if token, exists := tc.cachedTokens[idx]; exists {
		// 使用新的IsExpired方法检查token是否过期
		if !token.IsExpired() {
			return *token, true
		}
		// token已过期，删除缓存
		delete(tc.cachedTokens, idx)
	}

	return TokenInfo{}, false
}

// Set 设置指定索引的缓存token
func (tc *TokenCache) SetByIndex(idx int, token TokenInfo) {
	tc.mutex.Lock()
	defer tc.mutex.Unlock()
	tc.cachedTokens[idx] = &token
}

// SetCurrentIndex 设置当前使用的token索引
func (tc *TokenCache) SetCurrentIndex(idx int) {
	tc.mutex.Lock()
	defer tc.mutex.Unlock()
	tc.currentIdx = idx
}

// GetCurrentIndex 获取当前使用的token索引
func (tc *TokenCache) GetCurrentIndex() int {
	tc.mutex.RLock()
	defer tc.mutex.RUnlock()
	return tc.currentIdx
}

// Set 设置当前索引的缓存token（保持向后兼容）
func (tc *TokenCache) Set(token TokenInfo) {
	tc.mutex.Lock()
	defer tc.mutex.Unlock()
	tc.cachedTokens[tc.currentIdx] = &token
}

// Clear 清除所有缓存的token
func (tc *TokenCache) Clear() {
	tc.mutex.Lock()
	defer tc.mutex.Unlock()
	tc.cachedTokens = make(map[int]*TokenInfo)
}

// ClearByIndex 清除指定索引的缓存token
func (tc *TokenCache) ClearByIndex(idx int) {
	tc.mutex.Lock()
	defer tc.mutex.Unlock()
	delete(tc.cachedTokens, idx)
}

// IsExpired 检查当前token是否过期
func (tc *TokenCache) IsExpired() bool {
	tc.mutex.RLock()
	defer tc.mutex.RUnlock()

	if token, exists := tc.cachedTokens[tc.currentIdx]; exists {
		return token.IsExpired()
	}
	return true
}

// GetCachedCount 获取缓存的token数量
func (tc *TokenCache) GetCachedCount() int {
	tc.mutex.RLock()
	defer tc.mutex.RUnlock()
	return len(tc.cachedTokens)
}

// GetAllCachedIndexes 获取所有已缓存的token索引
func (tc *TokenCache) GetAllCachedIndexes() []int {
	tc.mutex.RLock()
	defer tc.mutex.RUnlock()

	var indexes []int
	for idx := range tc.cachedTokens {
		indexes = append(indexes, idx)
	}
	return indexes
}
