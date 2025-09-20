package types

import (
	"sync"
	"sync/atomic"
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
	ProfileArn string `json:"profileArn,omitempty"`

	// IdC认证方式专用字段
	TokenType string `json:"tokenType,omitempty"`

	// 可能的其他响应字段
	OriginSessionId    *string `json:"originSessionId,omitempty"`
	IssuedTokenType    *string `json:"issuedTokenType,omitempty"`
	AwsSsoAppSessionId *string `json:"aws_sso_app_session_id,omitempty"`
	IdToken            *string `json:"idToken,omitempty"`
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
	tokens      []string // refresh token列表
	currentIdx  int64    // 当前使用的token索引 - 使用atomic
	accessIdx   int64    // 当前访问的access token索引 - 使用atomic
	failedCount sync.Map // 每个token的失败次数 - 保持sync.Map
	maxRetries  int      // 最大重试次数
}

// NewTokenPool 创建新的token池
func NewTokenPool(tokens []string, maxRetries int) *TokenPool {
	return &TokenPool{
		tokens:      tokens,
		currentIdx:  0,
		accessIdx:   0,
		failedCount: sync.Map{}, // 直接初始化sync.Map
		maxRetries:  maxRetries,
	}
}

// GetNextAccessIndex 获取下一个访问索引（用于轮换access token）
func (tp *TokenPool) GetNextAccessIndex() int {
	tokenCount := len(tp.tokens)
	startIdx := int(atomic.LoadInt64(&tp.accessIdx))

	for i := 0; i < tokenCount; i++ {
		currentIdx := (startIdx + i) % tokenCount

		// 检查当前索引的token失败次数
		if failedCountVal, exists := tp.failedCount.Load(currentIdx); !exists || failedCountVal.(int) < tp.maxRetries {
			// 原子更新accessIdx到下一个位置
			atomic.StoreInt64(&tp.accessIdx, int64((currentIdx+1)%tokenCount))
			return currentIdx
		}
	}

	// 所有token都不可用，返回起始索引
	return startIdx
}

// GetCurrentAccessIndex 获取当前访问索引
func (tp *TokenPool) GetCurrentAccessIndex() int {
	return int(atomic.LoadInt64(&tp.accessIdx))
}

// SetAccessIndex 设置访问索引
func (tp *TokenPool) SetAccessIndex(idx int) {
	if idx >= 0 && idx < len(tp.tokens) {
		atomic.StoreInt64(&tp.accessIdx, int64(idx))
	}
}

// GetTokenCount 获取token总数
func (tp *TokenPool) GetTokenCount() int {
	return len(tp.tokens) // tokens slice是只读的，无需锁
}

// GetNextToken 获取下一个可用的token
func (tp *TokenPool) GetNextToken() (string, int, bool) {
	tokenCount := len(tp.tokens)
	startIdx := int(atomic.LoadInt64(&tp.currentIdx))

	for i := 0; i < tokenCount; i++ {
		currentIdx := (startIdx + i) % tokenCount

		// 检查当前token失败次数
		if failedCountVal, exists := tp.failedCount.Load(currentIdx); !exists || failedCountVal.(int) < tp.maxRetries {
			token := tp.tokens[currentIdx]
			// 原子更新currentIdx到下一个位置
			atomic.StoreInt64(&tp.currentIdx, int64((currentIdx+1)%tokenCount))
			return token, currentIdx, true
		}
	}

	// 所有token都不可用
	return "", -1, false
}

// MarkTokenFailed 标记token失败
func (tp *TokenPool) MarkTokenFailed(idx int) {
	// 使用sync.Map原子操作，无需额外锁
	if val, exists := tp.failedCount.Load(idx); exists {
		tp.failedCount.Store(idx, val.(int)+1)
	} else {
		tp.failedCount.Store(idx, 1)
	}
}

// MarkTokenSuccess 标记token成功，重置失败计数
func (tp *TokenPool) MarkTokenSuccess(idx int) {
	tp.failedCount.Store(idx, 0) // 直接使用sync.Map的原子操作
}

// GetStats 获取token池统计信息
func (tp *TokenPool) GetStats() map[string]any {
	stats := map[string]any{
		"total_tokens":  len(tp.tokens),
		"current_index": atomic.LoadInt64(&tp.currentIdx),
		"access_index":  atomic.LoadInt64(&tp.accessIdx),
		"max_retries":   tp.maxRetries,
		"failed_counts": make(map[int]int),
	}

	// 使用sync.Map的Range方法饁历失败计数
	tp.failedCount.Range(func(key, value any) bool {
		stats["failed_counts"].(map[int]int)[key.(int)] = value.(int)
		return true
	})

	return stats
}

// TokenWithAuthType 带认证类型的Token信息 (用于Dashboard API)
type TokenWithAuthType struct {
	TokenInfo
	AuthType string `json:"auth_type"`
}
