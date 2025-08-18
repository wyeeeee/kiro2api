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
		ProfileArn:   t.ProfileArn, // 确保ProfileArn被包含在转换中
		ExpiresIn:    t.ExpiresIn,
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
type TokenInfo = Token // TokenInfo现在是Token的别名
// RefreshResponse 统一的token刷新响应结构，支持Social和IdC两种认证方式
type RefreshResponse struct {
	AccessToken  string `json:"accessToken"`
	ExpiresIn    int    `json:"expiresIn"` // 多少秒后失效
	RefreshToken string `json:"refreshToken,omitempty"`
	
	// Social认证方式专用字段
	ProfileArn   string `json:"profileArn,omitempty"`
	
	// IdC认证方式专用字段  
	TokenType    string `json:"tokenType,omitempty"`
	
	// 可能的其他响应字段
	OriginSessionId      *string `json:"originSessionId,omitempty"`
	IssuedTokenType      *string `json:"issuedTokenType,omitempty"`
	AwsSsoAppSessionId   *string `json:"aws_sso_app_session_id,omitempty"`
	IdToken             *string `json:"idToken,omitempty"`
}

// RefreshRequest Social认证方式的刷新请求结构
type RefreshRequest struct {
	RefreshToken string `json:"refreshToken"`
}

// IdcRefreshRequest IdC认证方式的刷新请求结构
type IdcRefreshRequest struct {
	ClientId     string `json:"clientId"`
	ClientSecret string `json:"clientSecret"`
	GrantType    string `json:"grantType"`
	RefreshToken string `json:"refreshToken"`
}

// TokenPool token池管理结构
type TokenPool struct {
	tokens      []string    // refresh token列表
	currentIdx  int         // 当前使用的token索引
	accessIdx   int         // 当前访问的access token索引（用于轮换）
	failedCount map[int]int // 每个token的失败次数
	mutex       sync.RWMutex
	maxRetries  int // 最大重试次数
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
