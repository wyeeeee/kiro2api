package main

import (
	"fmt"
	"os"

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

	port := "8080"                       // 默认端口
	authToken := config.DefaultAuthToken // 默认使用 DefaultAuthToken
	if len(os.Args) > 2 {
		port = os.Args[2]
	}
	if len(os.Args) > 3 {
		authToken = os.Args[3]
	}
	server.StartServer(port, authToken)

}
