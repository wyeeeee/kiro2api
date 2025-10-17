package main

import (
	"fmt"
	"os"
	"sync"

	"kiro2api/auth"
	"kiro2api/logger"
	"kiro2api/server"
	"kiro2api/webconfig"
)

// 全局AuthService实例，用于动态重载
var (
	globalAuthService *auth.AuthService
	authServiceMutex  sync.RWMutex
)

// GetGlobalAuthService 获取全局AuthService实例（线程安全）
func GetGlobalAuthService() *auth.AuthService {
	authServiceMutex.RLock()
	defer authServiceMutex.RUnlock()
	return globalAuthService
}

// ReloadGlobalAuthService 重载全局AuthService配置
func ReloadGlobalAuthService() error {
	authServiceMutex.Lock()
	defer authServiceMutex.Unlock()

	if globalAuthService == nil {
		return fmt.Errorf("AuthService未初始化")
	}

	return globalAuthService.ReloadConfigs()
}

func main() {
	// 初始化配置管理器
	configManager := webconfig.GetGlobalManager()

	// 检查是否首次运行
	if configManager.IsFirstRun() {
		fmt.Println("🚀 欢迎使用 Kiro2API!")
		fmt.Println("首次运行需要初始化配置...")
		fmt.Println("请在浏览器中访问 http://0.0.0.0:8083 进行初始化设置")
		fmt.Println("或者在命令行中运行以下命令进行初始化:")
		fmt.Println("curl -X POST http://0.0.0.0:8083/api/init \\")
		fmt.Println("  -H \"Content-Type: application/json\" \\")
		fmt.Println("  -d '{\"loginPassword\":\"your-admin-password\",\"clientToken\":\"your-api-token\"}'")
	}

	// 获取配置
	config := configManager.GetConfig()

	// 使用新配置系统初始化日志
	initializeLogger(config)

	logger.Info("🚀 Kiro2API 启动中...")

	// 创建AuthService实例（使用依赖注入）
	var authService *auth.AuthService
	var err error

	// 只有在有Token配置时才创建AuthService
	tokens := configManager.GetEnabledTokens()
	if len(tokens) > 0 {
		logger.Info("正在创建AuthService...")
		authService, err = auth.NewAuthServiceWithConfig(configManager)
		if err != nil {
			logger.Error("AuthService创建失败", logger.Err(err))
			logger.Error("请检查token配置后重新启动服务器")
			os.Exit(1)
		}

		// 保存到全局变量
		authServiceMutex.Lock()
		globalAuthService = authService
		authServiceMutex.Unlock()

		// 注册配置更新回调
		configManager.AddConfigChangeCallback(func() {
			logger.Info("检测到配置更新，正在重新加载AuthService...")
			if err := ReloadGlobalAuthService(); err != nil {
				logger.Error("重载AuthService失败", logger.Err(err))
			} else {
				logger.Info("AuthService重载成功")
			}
		})
	} else {
		logger.Info("未配置Token，仅启动Web配置管理界面")
	}

	// 启动服务器（包含Web配置管理）
	port := fmt.Sprintf("%d", config.ServiceConfig.Port)
	clientToken := config.ServiceConfig.ClientToken
	server.StartServerWithConfig(port, clientToken, authService, configManager)
}

// initializeLogger 使用新配置初始化日志系统
func initializeLogger(config *webconfig.WebConfig) {
	// 设置日志级别
	switch config.LogConfig.Level {
	case "debug":
		logger.SetLogLevel(logger.DEBUG)
	case "info":
		logger.SetLogLevel(logger.INFO)
	case "warn":
		logger.SetLogLevel(logger.WARN)
	case "error":
		logger.SetLogLevel(logger.ERROR)
	case "fatal":
		logger.SetLogLevel(logger.FATAL)
	default:
		logger.SetLogLevel(logger.INFO)
	}

	// 设置日志格式
	if config.LogConfig.Format == "json" {
		logger.SetJSONFormat()
	} else {
		logger.SetTextFormat()
	}

	// 设置日志文件
	if config.LogConfig.File != "" {
		logger.SetLogFile(config.LogConfig.File)
	}

	// 设置控制台输出
	logger.SetConsoleOutput(config.LogConfig.Console)

	// 设置调用栈信息
	logger.SetCallerEnabled(config.LogConfig.EnableCaller)
	logger.SetCallerSkip(config.LogConfig.CallerSkip)

	logger.Info("日志系统初始化完成",
		logger.String("level", config.LogConfig.Level),
		logger.String("format", config.LogConfig.Format),
		logger.Bool("console", config.LogConfig.Console),
		logger.String("file", config.LogConfig.File))
}
