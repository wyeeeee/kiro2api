package auth

import (
	"fmt"
	"io"
	"kiro2api/logger"
	"kiro2api/types"
	"kiro2api/utils"
	"net/http"
	"net/url"
	"time"
)

// UsageLimitsChecker 使用限制检查器 (遵循SRP原则)
type UsageLimitsChecker struct {
	httpClient *http.Client
}

// NewUsageLimitsChecker 创建使用限制检查器
func NewUsageLimitsChecker() *UsageLimitsChecker {
	return &UsageLimitsChecker{
		httpClient: utils.SharedHTTPClient,
	}
}

// CheckUsageLimits 检���token的使用限制 (基于token.md API规范)
func (c *UsageLimitsChecker) CheckUsageLimits(token types.TokenInfo) (*types.UsageLimits, error) {
	// 构建请求URL (完全遵循token.md中的示例)
	baseURL := "https://codewhisperer.us-east-1.amazonaws.com/getUsageLimits"
	params := url.Values{}
	params.Add("isEmailRequired", "true")
	params.Add("origin", "AI_EDITOR")
	params.Add("resourceType", "AGENTIC_REQUEST")
	
	requestURL := fmt.Sprintf("%s?%s", baseURL, params.Encode())
	
	// 创建HTTP请求
	req, err := http.NewRequest("GET", requestURL, nil)
	if err != nil {
		return nil, fmt.Errorf("创建使用限制检查请求失败: %v", err)
	}
	
	// 设置请求头 (严格按照token.md中的示例)
	req.Header.Set("x-amz-user-agent", "aws-sdk-js/1.0.0 KiroIDE-0.2.13-66c23a8c5d15afabec89ef9954ef52a119f10d369df04d548fc6c1eac694b0d1")
	req.Header.Set("user-agent", "aws-sdk-js/1.0.0 ua/2.1 os/darwin#24.6.0 lang/js md/nodejs#20.16.0 api/codewhispererruntime#1.0.0 m/E KiroIDE-0.2.13-66c23a8c5d15afabec89ef9954ef52a119f10d369df04d548fc6c1eac694b0d1")
	req.Header.Set("host", "codewhisperer.us-east-1.amazonaws.com")
	req.Header.Set("amz-sdk-invocation-id", generateInvocationID())
	req.Header.Set("amz-sdk-request", "attempt=1; max=1")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token.AccessToken))
	req.Header.Set("Connection", "close")
	
	// 发送请求
	logger.Debug("发送使用限制检查请求", 
		logger.String("url", requestURL),
		logger.String("token_preview", token.AccessToken[:20]+"..."))
	
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("使用限制检查请求失败: %v", err)
	}
	defer resp.Body.Close()
	
	// 读取响应
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取使用限制响应失败: %v", err)
	}
	
	logger.Debug("使用限制API响应", 
		logger.Int("status_code", resp.StatusCode),
		logger.String("response_body", string(body)))
	
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("使用限制检查失败: 状态码 %d, 响应: %s", resp.StatusCode, string(body))
	}
	
	// 解析响应
	var usageLimits types.UsageLimits
	if err := utils.SafeUnmarshal(body, &usageLimits); err != nil {
		return nil, fmt.Errorf("解析使用限制响应失败: %v", err)
	}
	
	// 记录关键信息
	c.logUsageLimits(&usageLimits)
	
	return &usageLimits, nil
}

// logUsageLimits 记录使用限制的关键信息
func (c *UsageLimitsChecker) logUsageLimits(limits *types.UsageLimits) {
	for _, breakdown := range limits.UsageBreakdownList {
		if breakdown.ResourceType == "VIBE" {
			// 计算总可用次数 (遵循token.md公式)
			totalLimit := breakdown.UsageLimit
			totalUsed := breakdown.CurrentUsage
			
			if breakdown.FreeTrialInfo != nil && breakdown.FreeTrialInfo.FreeTrialStatus == "ACTIVE" {
				totalLimit += breakdown.FreeTrialInfo.UsageLimit
				totalUsed += breakdown.FreeTrialInfo.CurrentUsage
			}
			
			available := totalLimit - totalUsed
			
			logger.Info("VIBE使用状态",
				logger.String("resource_type", breakdown.ResourceType),
				logger.Int("total_limit", totalLimit),
				logger.Int("total_used", totalUsed),
				logger.Int("available", available),
				logger.String("free_trial_status", func() string {
					if breakdown.FreeTrialInfo != nil {
						return breakdown.FreeTrialInfo.FreeTrialStatus
					}
					return "NONE"
				}()))
			
			if available <= 5 {
				logger.Warn("VIBE使用量即将耗尽", 
					logger.Int("remaining", available),
					logger.String("recommendation", "考虑切换到其他token"))
			}
			
			break
		}
	}
	
	// 记录订阅信息
	logger.Debug("订阅信息",
		logger.String("subscription_type", limits.SubscriptionInfo.Type),
		logger.String("subscription_title", limits.SubscriptionInfo.SubscriptionTitle),
		logger.String("user_email", limits.UserInfo.Email))
}

// generateInvocationID 生成请求ID (简化版本)
func generateInvocationID() string {
	return fmt.Sprintf("%d-%s", time.Now().UnixNano(), "kiro2api")
}

// CheckAndEnhanceToken 检查并增强token信息 (核心集成函数)
func CheckAndEnhanceToken(token types.TokenInfo) types.TokenWithUsage {
	checker := NewUsageLimitsChecker()
	
	enhancedToken := types.TokenWithUsage{
		TokenInfo:       token,
		LastUsageCheck:  time.Now(),
		IsUsageExceeded: false,
	}
	
	// 立即生成token预览
	enhancedToken.TokenPreview = enhancedToken.GenerateTokenPreview()
	
	// 尝试获取使用限制
	usageLimits, err := checker.CheckUsageLimits(token)
	if err != nil {
		logger.Warn("获取使用限制失败", 
			logger.Err(err),
			logger.String("token_preview", enhancedToken.TokenPreview),
			logger.String("fallback", "将使用默认行为"))
		
		enhancedToken.UsageCheckError = err.Error()
		// 设置保守的默认值
		enhancedToken.AvailableCount = 1 // 保守估计还能用1次
		enhancedToken.UserEmail = "unknown"
		return enhancedToken
	}
	
	// 成功获取使用限制
	enhancedToken.UsageLimits = usageLimits
	enhancedToken.AvailableCount = enhancedToken.GetAvailableVIBECount()
	enhancedToken.IsUsageExceeded = enhancedToken.AvailableCount <= 0
	enhancedToken.UsageCheckError = "" // 清除错误
	
	// 🚀 关键改进：提取并保存用户email信息
	enhancedToken.UpdateUserInfo()
	
	logger.Info("Token使用状态检查完成",
		logger.String("user_email", enhancedToken.GetUserEmailDisplay()),
		logger.String("token_preview", enhancedToken.TokenPreview),
		logger.Int("available_count", enhancedToken.AvailableCount),
		logger.Bool("is_usable", enhancedToken.IsUsable()))
	
	return enhancedToken
}