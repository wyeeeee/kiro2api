package main

import (
	"fmt"
	"os"

	"kiro2api/logger"
	"kiro2api/server"

	"github.com/joho/godotenv"
)

func main() {
	// 自动加载.env文件
	if err := godotenv.Load(); err != nil {
		fmt.Printf("警告: 无法加载.env文件: %v\n", err)
	}

	// 初始化日志器
	if err := logger.InitLogger(logger.ParseConfig()); err != nil {
		fmt.Printf("初始化日志器失败: %v\n", err)
	}
	defer logger.Close()

	// 检查必需的环境变量
	if os.Getenv("AWS_REFRESHTOKEN") == "" {
		fmt.Printf("错误: AWS_REFRESHTOKEN环境变量未设置\n")
		fmt.Printf("请设置AWS_REFRESHTOKEN环境变量后重新启动程序\n")
		fmt.Printf("示例: export AWS_REFRESHTOKEN=\"your_refresh_token_here\"\n")
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
