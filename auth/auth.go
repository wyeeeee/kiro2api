package auth

import (
	"fmt"
	"kiro2api/logger"
	"kiro2api/types"
	"sync"
)

// AuthService 认证服务（推荐使用依赖注入方式）
type AuthService struct {
	tokenManager *TokenManager
	configs      []AuthConfig
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

// ========== 向后兼容的全局函数（不推荐使用，将在v2.0中移除） ==========

// 全局token管理器实例（已弃用：请使用AuthService代替）
var (
	globalTokenManager *TokenManager
	initOnce           sync.Once
)

// Initialize 初始化token系统
// Deprecated: 请使用 NewAuthService() 代替。此函数将在v2.0中移除。
func Initialize() error {
	var err error
	initOnce.Do(func() {
		logger.Warn("使用已弃用的全局Initialize函数，请迁移到AuthService")

		// 加载配置
		configs, loadErr := loadConfigs()
		if loadErr != nil {
			err = fmt.Errorf("加载配置失败: %v", loadErr)
			return
		}

		if len(configs) == 0 {
			err = fmt.Errorf("未找到有效的token配置")
			return
		}

		// 创建token管理器
		globalTokenManager = NewTokenManager(configs)

		// 预热第一个可用token
		_, warmupErr := globalTokenManager.getBestToken()
		if warmupErr != nil {
			logger.Warn("token预热失败", logger.Err(warmupErr))
		}

		logger.Info("token系统初始化完成",
			logger.Int("config_count", len(configs)))
	})

	return err
}

// GetToken 获取可用的token（统一入口）
// Deprecated: 请使用 AuthService.GetToken() 代替。此函数将在v2.0中移除。
func GetToken() (types.TokenInfo, error) {
	if globalTokenManager == nil {
		return types.TokenInfo{}, fmt.Errorf("token系统未初始化")
	}

	return globalTokenManager.getBestToken()
}
