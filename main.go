package main

import (
	"fmt"
	"os"

	"kiro2api/logger"
	"kiro2api/server"
)

func main() {
	// 初始化日志器
	if err := logger.InitLogger(logger.ParseConfig()); err != nil {
		fmt.Printf("初始化日志器失败: %v\n", err)
	}
	defer logger.Close()

	port := "8080"          // 默认端口
	clientToken := "123456" // 默认使用 123456
	if len(os.Args) > 2 {
		port = os.Args[2]
	}
	if len(os.Args) > 3 {
		clientToken = os.Args[3]
	}
	server.StartServer(port, clientToken)

}
