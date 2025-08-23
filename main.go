package main

import (
	"os"

	"kiro2api/logger"
	"kiro2api/server"

	"github.com/joho/godotenv"
)

func main() {
	// 自动加载.env文件
	if err := godotenv.Load(); err != nil {
		logger.Info("未找到.env文件，使用环境变量")
	}

	// 重新初始化logger以使用.env文件中的配置
	logger.Reinitialize()

	// 显示当前日志级别设置（仅在DEBUG级别时显示详细信息）
	logger.Debug("日志系统初始化完成",
		logger.String("log_level", os.Getenv("LOG_LEVEL")),
		logger.String("log_file", os.Getenv("LOG_FILE")))

	// 检查必需的环境变量
	if os.Getenv("AWS_REFRESHTOKEN") == "" {
		logger.Error("AWS_REFRESHTOKEN环境变量未设置")
		logger.Error("请设置AWS_REFRESHTOKEN环境变量后重新启动程序")
		logger.Error("示例: export AWS_REFRESHTOKEN=\"your_refresh_token_here\"")
		os.Exit(1)
	}

	port := "8080" // 默认端口
	if len(os.Args) > 1 {
		port = os.Args[1]
	}
	// 从环境变量获取端口，覆盖命令行参数
	if envPort := os.Getenv("PORT"); envPort != "" {
		port = envPort
	}

	// 从环境变量获取客户端认证token，默认值为123456
	clientToken := os.Getenv("KIRO_CLIENT_TOKEN")
	if clientToken == "" {
		clientToken = "123456"
	}

	server.StartServer(port, clientToken)
}
