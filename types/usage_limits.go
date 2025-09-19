package types

import (
	"strings"
	"time"
)

// UsageLimits 使用限制响应结构 (基于token.md中的API规范)
type UsageLimits struct {
	Limits               []any            `json:"limits"`
	UsageBreakdownList   []UsageBreakdown `json:"usageBreakdownList"`
	UserInfo             UserInfo         `json:"userInfo"`
	DaysUntilReset       int              `json:"daysUntilReset"`
	OverageConfiguration OverageConfig    `json:"overageConfiguration"`
	NextDateReset        float64          `json:"nextDateReset"`
	SubscriptionInfo     SubscriptionInfo `json:"subscriptionInfo"`
	UsageBreakdown       any              `json:"usageBreakdown"`
}

// UsageBreakdown 使用详细信息
type UsageBreakdown struct {
	NextDateReset   float64        `json:"nextDateReset"`
	OverageCharges  float64        `json:"overageCharges"`
	ResourceType    string         `json:"resourceType"`
	Unit            string         `json:"unit"`
	UsageLimit      int            `json:"usageLimit"`
	OverageRate     float64        `json:"overageRate"`
	CurrentUsage    int            `json:"currentUsage"`
	OverageCap      int            `json:"overageCap"`
	Currency        string         `json:"currency"`
	CurrentOverages int            `json:"currentOverages"`
	FreeTrialInfo   *FreeTrialInfo `json:"freeTrialInfo,omitempty"`
}

// FreeTrialInfo 免费试用信息
type FreeTrialInfo struct {
	FreeTrialExpiry float64 `json:"freeTrialExpiry"`
	FreeTrialStatus string  `json:"freeTrialStatus"`
	UsageLimit      int     `json:"usageLimit"`
	CurrentUsage    int     `json:"currentUsage"`
}

// UserInfo 用户信息
type UserInfo struct {
	Email  string `json:"email"`
	UserID string `json:"userId"`
}

// OverageConfig 超额配置
type OverageConfig struct {
	OverageStatus string `json:"overageStatus"`
}

// SubscriptionInfo 订阅信息
type SubscriptionInfo struct {
	SubscriptionManagementTarget string `json:"subscriptionManagementTarget"`
	OverageCapability            string `json:"overageCapability"`
	SubscriptionTitle            string `json:"subscriptionTitle"`
	Type                         string `json:"type"`
	UpgradeCapability            string `json:"upgradeCapability"`
}

// TokenWithUsage 包含使用状态的token信息 (扩展TokenInfo)
type TokenWithUsage struct {
	TokenInfo
	UsageLimits     *UsageLimits `json:"usage_limits,omitempty"`
	AvailableCount  int          `json:"available_count"`
	LastUsageCheck  time.Time    `json:"last_usage_check"`
	IsUsageExceeded bool         `json:"is_usage_exceeded"`
	UsageCheckError string       `json:"usage_check_error,omitempty"`
	UserEmail       string       `json:"user_email,omitempty"`    // 用户邮箱信息
	TokenPreview    string       `json:"token_preview,omitempty"` // Token前缀预览
}

// GetAvailableVIBECount 计算可用的VIBE调用次数 (遵循token.md公式)
func (t *TokenWithUsage) GetAvailableVIBECount() int {
	if t.UsageLimits == nil {
		return 0
	}

	for _, breakdown := range t.UsageLimits.UsageBreakdownList {
		if breakdown.ResourceType == "VIBE" {
			available := breakdown.UsageLimit - breakdown.CurrentUsage

			// 加上免费试用额度
			if breakdown.FreeTrialInfo != nil && breakdown.FreeTrialInfo.FreeTrialStatus == "ACTIVE" {
				trialAvailable := breakdown.FreeTrialInfo.UsageLimit - breakdown.FreeTrialInfo.CurrentUsage
				available += trialAvailable
			}

			return available
		}
	}

	return 0
}

// IsUsable 判断token是否可用 (综合考虑过期和使用限制)
func (t *TokenWithUsage) IsUsable() bool {
	// 检查token是否过期
	if t.IsExpired() {
		return false
	}

	// 检查使用限制
	if t.IsUsageExceeded {
		return false
	}

	// 检查可用次数
	return t.GetAvailableVIBECount() > 0
}

// NeedsUsageRefresh 判断是否需要刷新使用状态
func (t *TokenWithUsage) NeedsUsageRefresh() bool {
	// 如果从未检查过使用状态
	if t.UsageLimits == nil {
		return true
	}

	// 如果上次检查超过5分钟
	if time.Since(t.LastUsageCheck) > 5*time.Minute {
		return true
	}

	// 如果上次检查出错
	if t.UsageCheckError != "" {
		return true
	}

	return false
}

// GenerateTokenPreview 生成token预览字符串 (***+后10位)
func (t *TokenWithUsage) GenerateTokenPreview() string {
	if len(t.AccessToken) <= 10 {
		// 如果token太短，全部用*代替
		return strings.Repeat("*", len(t.AccessToken))
	}

	// 3个*号 + 后10位
	suffix := t.AccessToken[len(t.AccessToken)-10:]
	return "***" + suffix
}

// GetUserEmailDisplay 获取用户邮箱显示名称
func (t *TokenWithUsage) GetUserEmailDisplay() string {
	if t.UserEmail == "" {
		return "unknown"
	}
	return t.UserEmail
}

// UpdateUserInfo 更新用户信息 (从UsageLimits中提取)
func (t *TokenWithUsage) UpdateUserInfo() {
	t.TokenPreview = t.GenerateTokenPreview()

	if t.UsageLimits != nil && t.UsageLimits.UserInfo.Email != "" {
		t.UserEmail = t.UsageLimits.UserInfo.Email
	}
}
