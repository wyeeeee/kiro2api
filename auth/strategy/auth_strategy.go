package strategy

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"kiro2api/auth/config"
	globalconfig "kiro2api/config"
	"kiro2api/types"
	"kiro2api/utils"
	"net/http"
	"time"
)

// AuthStrategy 认证策略接口 (避免循环导入)
type AuthStrategy interface {
	RefreshToken(ctx context.Context, config config.AuthConfig) (types.TokenInfo, error)
	StrategyName() string
	ValidateConfig(config config.AuthConfig) error
}

// SocialAuthStrategy Social认证策略实现 (遵循SRP)
type SocialAuthStrategy struct {
	httpClient *http.Client
}

// NewSocialAuthStrategy 创建Social认证策略
func NewSocialAuthStrategy(httpClient *http.Client) AuthStrategy {
	if httpClient == nil {
		httpClient = utils.SharedHTTPClient
	}
	return &SocialAuthStrategy{
		httpClient: httpClient,
	}
}

// StrategyName 返回策略名称
func (s *SocialAuthStrategy) StrategyName() string {
	return "Social"
}

// ValidateConfig 验证Social认证配置
func (s *SocialAuthStrategy) ValidateConfig(config config.AuthConfig) error {
	if config.RefreshToken == "" {
		return fmt.Errorf("Social认证需要RefreshToken")
	}
	return nil
}

// RefreshToken 刷新Social认证token (遵循原有逻辑)
func (s *SocialAuthStrategy) RefreshToken(ctx context.Context, config config.AuthConfig) (types.TokenInfo, error) {
	// 准备刷新请求
	refreshReq := types.RefreshRequest{
		RefreshToken: config.RefreshToken,
	}

	reqBody, err := utils.FastMarshal(refreshReq)
	if err != nil {
		return types.TokenInfo{}, fmt.Errorf("序列化Social请求失败: %v", err)
	}

	// 创建HTTP请求
	req, err := http.NewRequestWithContext(ctx, "POST", globalconfig.RefreshTokenURL, bytes.NewBuffer(reqBody))
	if err != nil {
		return types.TokenInfo{}, fmt.Errorf("创建Social请求失败: %v", err)
	}

	// 设置请求头
	req.Header.Set("Content-Type", "application/json")

	// 发送请求
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return types.TokenInfo{}, fmt.Errorf("Social刷新token请求失败: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return types.TokenInfo{}, fmt.Errorf("Social刷新token失败: 状态码 %d, 响应: %s", resp.StatusCode, string(body))
	}

	// 解析响应
	var refreshResp types.RefreshResponse
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return types.TokenInfo{}, fmt.Errorf("读取Social响应失败: %v", err)
	}

	if err := utils.SafeUnmarshal(body, &refreshResp); err != nil {
		return types.TokenInfo{}, fmt.Errorf("解析Social刷新响应失败: %v", err)
	}

	// 转换为TokenInfo
	var token types.Token
	token.FromRefreshResponse(refreshResp, config.RefreshToken)

	return token, nil
}

// IdCAuthStrategy IdC认证策略实现 (遵循SRP)
type IdCAuthStrategy struct {
	httpClient *http.Client
}

// NewIdCAuthStrategy 创建IdC认证策略
func NewIdCAuthStrategy(httpClient *http.Client) AuthStrategy {
	if httpClient == nil {
		httpClient = utils.SharedHTTPClient
	}
	return &IdCAuthStrategy{
		httpClient: httpClient,
	}
}

// StrategyName 返回策略名称
func (i *IdCAuthStrategy) StrategyName() string {
	return "IdC"
}

// ValidateConfig 验证IdC认证配置
func (i *IdCAuthStrategy) ValidateConfig(config config.AuthConfig) error {
	if config.RefreshToken == "" {
		return fmt.Errorf("IdC认证需要RefreshToken")
	}
	if config.ClientID == "" {
		return fmt.Errorf("IdC认证需要ClientID")
	}
	if config.ClientSecret == "" {
		return fmt.Errorf("IdC认证需要ClientSecret")
	}
	return nil
}

// RefreshToken 刷新IdC认证token (遵循原有逻辑)
func (i *IdCAuthStrategy) RefreshToken(ctx context.Context, config config.AuthConfig) (types.TokenInfo, error) {
	// 准备IdC刷新请求
	refreshReq := types.IdcRefreshRequest{
		ClientId:     config.ClientID,
		ClientSecret: config.ClientSecret,
		GrantType:    "refresh_token",
		RefreshToken: config.RefreshToken,
	}

	reqBody, err := utils.FastMarshal(refreshReq)
	if err != nil {
		return types.TokenInfo{}, fmt.Errorf("序列化IdC请求失败: %v", err)
	}

	// 创建HTTP请求
	req, err := http.NewRequestWithContext(ctx, "POST", globalconfig.IdcRefreshTokenURL, bytes.NewBuffer(reqBody))
	if err != nil {
		return types.TokenInfo{}, fmt.Errorf("创建IdC请求失败: %v", err)
	}

	// 设置IdC认证所需的特殊headers (保持与原实现一致)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Host", "oidc.us-east-1.amazonaws.com")
	req.Header.Set("Connection", "keep-alive")
	req.Header.Set("x-amz-user-agent", "aws-sdk-js/3.738.0 ua/2.1 os/other lang/js md/browser#unknown_unknown api/sso-oidc#3.738.0 m/E KiroIDE")
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Language", "*")
	req.Header.Set("sec-fetch-mode", "cors")
	req.Header.Set("User-Agent", "node")
	req.Header.Set("Accept-Encoding", "br, gzip, deflate")

	// 发送请求
	resp, err := i.httpClient.Do(req)
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

	if err := utils.SafeUnmarshal(body, &refreshResp); err != nil {
		return types.TokenInfo{}, fmt.Errorf("解析IdC刷新响应失败: %v", err)
	}

	// 转换为统一的Token结构
	var token types.Token
	token.AccessToken = refreshResp.AccessToken
	token.RefreshToken = config.RefreshToken // 保持原始refresh token
	token.ExpiresIn = refreshResp.ExpiresIn
	token.ExpiresAt = time.Now().Add(time.Duration(refreshResp.ExpiresIn) * time.Second)

	return token, nil
}

// StrategyRegistry 策略注册表 (工厂模式)
type StrategyRegistry struct {
	strategies map[string]func(*http.Client) AuthStrategy
}

// NewStrategyRegistry 创建策略注册表
func NewStrategyRegistry() *StrategyRegistry {
	registry := &StrategyRegistry{
		strategies: make(map[string]func(*http.Client) AuthStrategy),
	}

	// 注册内置策略
	registry.Register("Social", NewSocialAuthStrategy)
	registry.Register("IdC", NewIdCAuthStrategy)

	return registry
}

// Register 注册新策略 (遵循OCP - 开闭原则)
func (r *StrategyRegistry) Register(name string, factory func(*http.Client) AuthStrategy) {
	r.strategies[name] = factory
}

// Create 创建策略实例
func (r *StrategyRegistry) Create(name string, httpClient *http.Client) (AuthStrategy, error) {
	factory, exists := r.strategies[name]
	if !exists {
		return nil, fmt.Errorf("未注册的认证策略: %s", name)
	}
	return factory(httpClient), nil
}

// GetSupportedStrategies 获取支持的策略列表
func (r *StrategyRegistry) GetSupportedStrategies() []string {
	var names []string
	for name := range r.strategies {
		names = append(names, name)
	}
	return names
}
