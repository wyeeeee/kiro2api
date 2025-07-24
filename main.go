package main

import (
	"fmt"
	"os"

	"kiro2api/auth"
	"kiro2api/config"
	"kiro2api/logger"
	"kiro2api/server"
)

func main() {
	// 初始化日志器
	if err := logger.InitLogger(logger.ParseConfig()); err != nil {
		fmt.Printf("初始化日志器失败: %v\n", err)
	}
	defer logger.Close()

	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	command := os.Args[1]

	switch command {
	case "read":
		auth.ReadToken()
	case "refresh":
		auth.RefreshToken()
	case "export":
		auth.ExportEnvVars()
	case "authToken":
		auth.GenerateAuthToken()
	case "server":
		port := "8080"                       // 默认端口
		authToken := config.DefaultAuthToken // 默认使用 DefaultAuthToken
		if len(os.Args) > 2 {
			port = os.Args[2]
		}
		if len(os.Args) > 3 {
			authToken = os.Args[3]
		}
		server.StartServer(port, authToken)
	default:
		logger.Errorf("未知命令: %s", command)
		os.Exit(1)
	}
}

func printUsage() {
	logger.Println("用法:")
	logger.Println("  kiro2api read    - 读取并显示token")
	logger.Println("  kiro2api refresh - 刷新token")
	logger.Println("  kiro2api export  - 导出环境变量")
	logger.Println("  kiro2api authToken - 显示认证令牌")
	logger.Println("  kiro2api server [port] [authToken] - 启动Anthropic API代理服务器")
}
