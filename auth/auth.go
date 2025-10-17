package auth

import (
	"fmt"
	"kiro2api/logger"
	"kiro2api/types"
	"kiro2api/webconfig"
)

// AuthService 认证服务（推荐使用依赖注入方式）
type AuthService struct {
	tokenManager *TokenManager
	configs      []AuthConfig
	configManager *webconfig.Manager // 用于动态重载配置
}

// NewAuthService 创建新的认证服务（推荐使用此方法而不是全局函数）
func NewAuthService() (*AuthService, error) {
	logger.Info("创建AuthService实例")

	// 加载配置
	configs, err := loadConfigs()
	if err != nil {
		return nil, fmt.Errorf("加载配置失败: %w", err)
	}

	if len(configs) == 0 {
		return nil, fmt.Errorf("未找到有效的token配置")
	}

	// 创建token管理器
	tokenManager := NewTokenManager(configs)

	// 预热第一个可用token
	_, warmupErr := tokenManager.getBestToken()
	if warmupErr != nil {
		logger.Warn("token预热失败", logger.Err(warmupErr))
	}

	logger.Info("AuthService创建完成", logger.Int("config_count", len(configs)))

	return &AuthService{
		tokenManager: tokenManager,
		configs:      configs,
	}, nil
}

// GetToken 获取可用的token
func (as *AuthService) GetToken() (types.TokenInfo, error) {
	if as.tokenManager == nil {
		return types.TokenInfo{}, fmt.Errorf("token管理器未初始化")
	}
	return as.tokenManager.getBestToken()
}

// GetTokenManager 获取底层的TokenManager（用于高级操作）
func (as *AuthService) GetTokenManager() *TokenManager {
	return as.tokenManager
}

// GetConfigs 获取认证配置
func (as *AuthService) GetConfigs() []AuthConfig {
	return as.configs
}

// ReloadConfigs 重新加载配置
func (as *AuthService) ReloadConfigs() error {
	if as.configManager == nil {
		return fmt.Errorf("configManager未初始化，无法重载配置")
	}

	logger.Info("开始重新加载认证配置")

	// 从Web配置重新加载认证配置
	newConfigs, err := loadConfigsFromWebConfig(as.configManager)
	if err != nil {
		return fmt.Errorf("重新加载配置失败: %w", err)
	}

	// 创建新的token管理器
	newTokenManager := NewTokenManager(newConfigs)

	// 预热第一个可用token
	_, warmupErr := newTokenManager.getBestToken()
	if warmupErr != nil {
		logger.Warn("新配置token预热失败", logger.Err(warmupErr))
	}

	// 原子性地替换配置
	oldConfigCount := len(as.configs)
	as.configs = newConfigs
	as.tokenManager = newTokenManager

	logger.Info("认证配置重新加载完成",
		logger.Int("旧配置数量", oldConfigCount),
		logger.Int("新配置数量", len(newConfigs)))

	return nil
}

// NewAuthServiceWithConfig 使用Web配置管理器创建认证服务
func NewAuthServiceWithConfig(configManager *webconfig.Manager) (*AuthService, error) {
	logger.Info("创建AuthService实例（使用Web配置）")

	// 从Web配置加载认证配置
	configs, err := loadConfigsFromWebConfig(configManager)
	if err != nil {
		return nil, fmt.Errorf("从Web配置加载失败: %w", err)
	}

	if len(configs) == 0 {
		return nil, fmt.Errorf("未找到有效的token配置")
	}

	// 创建token管理器
	tokenManager := NewTokenManager(configs)

	// 预热第一个可用token
	_, warmupErr := tokenManager.getBestToken()
	if warmupErr != nil {
		logger.Warn("token预热失败", logger.Err(warmupErr))
	}

	logger.Info("AuthService创建完成", logger.Int("config_count", len(configs)))

	return &AuthService{
		tokenManager: tokenManager,
		configs:      configs,
		configManager: configManager, // 保存configManager引用以便重载
	}, nil
}
