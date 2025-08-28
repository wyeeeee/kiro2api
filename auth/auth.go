package auth

import (
	"fmt"
	"kiro2api/logger"
	"kiro2api/types"
	"sync"
)

// 全局token管理器实例
var (
	globalTokenManager *TokenManager
	initOnce           sync.Once
)

// Initialize 初始化token系统
func Initialize() error {
	var err error
	initOnce.Do(func() {
		logger.Info("初始化简化的token系统")

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
func GetToken() (types.TokenInfo, error) {
	if globalTokenManager == nil {
		return types.TokenInfo{}, fmt.Errorf("token系统未初始化")
	}

	return globalTokenManager.getBestToken()
}
