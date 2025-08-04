package server

import (
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"kiro2api/auth"
	"kiro2api/config"
	"kiro2api/converter"
	"kiro2api/logger"
	"kiro2api/types"
	"kiro2api/utils"

	"github.com/gin-gonic/gin"
)

// 移除全局httpClient，使用utils包中的共享客户端

// StartServer 启动HTTP代理服务器
func StartServer(port string, authToken string) {
	
	// 初始化全局工作池
	utils.GetGlobalWorkerPool()
	
	// 设置 gin 模式
	ginMode := os.Getenv("GIN_MODE")
	if ginMode == "" {
		ginMode = gin.ReleaseMode
	}
	gin.SetMode(ginMode)
	
	r := gin.New()

	// 添加中间件
	r.Use(gin.Logger())
	r.Use(gin.Recovery())
	r.Use(corsMiddleware())
	// 只对 /v1 开头的端点进行认证
	r.Use(PathBasedAuthMiddleware(authToken, []string{"/v1"}))


	// GET /v1/models 端点
	r.GET("/v1/models", func(c *gin.Context) {
		// 构建模型列表
		models := []types.Model{}
		for anthropicModel := range config.ModelMap {
			model := types.Model{
				ID:          anthropicModel,
				Object:      "model",
				Created:     1234567890,
				OwnedBy:     "anthropic",
				DisplayName: anthropicModel,
				Type:        "text",
				MaxTokens:   200000,
			}
			models = append(models, model)
		}

		response := types.ModelsResponse{
			Object: "list",
			Data:   models,
		}

		c.JSON(http.StatusOK, response)
	})

	r.POST("/v1/messages", func(c *gin.Context) {
		
		token, err := auth.GetToken()
		if err != nil {
			logger.Error("获取token失败", logger.Err(err))
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("获取token失败: %v", err)})
			return
		}

		body, err := c.GetRawData()
		if err != nil {
			logger.Error("读取请求体失败", logger.Err(err))
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("读取请求体失败: %v", err)})
			return
		}

		logger.Debug("收到Anthropic请求",
			logger.String("body", string(body)),
			logger.Int("body_size", len(body)))
		// println("收到Anthropic请求: ", string(body))
		var anthropicReq types.AnthropicRequest
		if err := utils.SafeUnmarshal(body, &anthropicReq); err != nil {
			logger.Error("解析请求体失败", logger.Err(err))
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("解析请求体失败: %v", err)})
			return
		}
		

		logger.Debug("请求解析成功",
			logger.String("model", anthropicReq.Model),
			logger.Bool("stream", anthropicReq.Stream),
			logger.Int("max_tokens", anthropicReq.MaxTokens),
			logger.Int("messages_count", len(anthropicReq.Messages)))

		// 详细记录消息内容以调试
		for i, msg := range anthropicReq.Messages {
			logger.Debug("消息详情",
				logger.Int("index", i),
				logger.String("role", msg.Role),
				logger.String("content_type", fmt.Sprintf("%T", msg.Content)),
				logger.String("content_preview", fmt.Sprintf("%.200v", msg.Content)))
		}

		// 验证请求的有效性
		if len(anthropicReq.Messages) == 0 {
			logger.Error("请求中没有消息")
			print("请求中没有消息")
			c.JSON(http.StatusBadRequest, gin.H{"error": "messages 数组不能为空"})
			return
		}

		// 验证最后一条消息有有效内容
		lastMsg := anthropicReq.Messages[len(anthropicReq.Messages)-1]
		content, err := utils.GetMessageContent(lastMsg.Content)
		if err != nil {
			logger.Error("获取消息内容失败",
				logger.Err(err),
				logger.String("raw_content", fmt.Sprintf("%v", lastMsg.Content)))
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("获取消息内容失败: %v", err)})
			return
		}

		trimmedContent := strings.TrimSpace(content)
		if trimmedContent == "" || trimmedContent == "answer for user question" {
			logger.Error("消息内容为空或无效",
				logger.String("content", content),
				logger.String("trimmed_content", trimmedContent))
			c.JSON(http.StatusBadRequest, gin.H{"error": "消息内容不能为空"})
			return
		}

		if anthropicReq.Stream {
			handleStreamRequest(c, anthropicReq, token.AccessToken)
			return
		}

		handleNonStreamRequest(c, anthropicReq, token.AccessToken)
	})

	// 新增：OpenAI兼容的 /v1/chat/completions 端点
	r.POST("/v1/chat/completions", func(c *gin.Context) {
		token, err := auth.GetToken()
		if err != nil {
			logger.Error("获取token失败", logger.Err(err))
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("获取token失败: %v", err)})
			return
		}

		body, err := c.GetRawData()
		if err != nil {
			logger.Error("读取请求体失败", logger.Err(err))
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("读取请求体失败: %v", err)})
			return
		}

		logger.Debug("收到OpenAI请求",
			logger.String("body", string(body)),
			logger.Int("body_size", len(body)))

		var openaiReq types.OpenAIRequest
		if err := utils.SafeUnmarshal(body, &openaiReq); err != nil {
			logger.Error("解析OpenAI请求体失败", logger.Err(err))
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("解析请求体失败: %v", err)})
			return
		}

		logger.Debug("OpenAI请求解析成功",
			logger.String("model", openaiReq.Model),
			logger.Bool("stream", openaiReq.Stream != nil && *openaiReq.Stream),
			logger.Int("max_tokens", func() int {
				if openaiReq.MaxTokens != nil {
					return *openaiReq.MaxTokens
				}
				return 16384
			}()))

		// 转换为Anthropic格式
		anthropicReq := converter.ConvertOpenAIToAnthropic(openaiReq)

		if anthropicReq.Stream {
			handleOpenAIStreamRequest(c, anthropicReq, token.AccessToken)
			return
		}

		handleOpenAINonStreamRequest(c, anthropicReq, token.AccessToken)
	})

	r.NoRoute(func(c *gin.Context) {
		logger.Warn("访问未知端点",
			logger.String("path", c.Request.URL.Path),
			logger.String("method", c.Request.Method))
		c.JSON(http.StatusNotFound, gin.H{"error": "404 未找到"})
	})

	logger.Info("启动Anthropic API代理服务器",
		logger.String("port", port),
		logger.String("auth_token", "***"))
	logger.Info("AuthToken 验证已启用")
	logger.Println("可用端点:")
	logger.Println("  GET  /v1/models           - 模型列表")
	logger.Println("  POST /v1/messages         - Anthropic API代理")
	logger.Println("  POST /v1/chat/completions - OpenAI API代理")
	logger.Println("  GET  /health              - 健康检查")
	logger.Println("按Ctrl+C停止服务器")

	// 获取服务器超时配置
	readTimeout := getServerTimeoutFromEnv("SERVER_READ_TIMEOUT_MINUTES", 16) * time.Minute
	writeTimeout := getServerTimeoutFromEnv("SERVER_WRITE_TIMEOUT_MINUTES", 16) * time.Minute

	// 创建自定义HTTP服务器以支持长时间请求
	server := &http.Server{
		Addr:           ":" + port,
		Handler:        r,
		ReadTimeout:    readTimeout,       // 读取超时
		WriteTimeout:   writeTimeout,      // 写入超时
		IdleTimeout:    120 * time.Second, // 空闲连接超时
		MaxHeaderBytes: 1 << 20,           // 1MB
	}

	logger.Info("启动HTTP服务器",
		logger.String("port", port),
		logger.Duration("read_timeout", readTimeout),
		logger.Duration("write_timeout", writeTimeout))

	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Error("启动服务器失败", logger.Err(err), logger.String("port", port))
		os.Exit(1)
	}
}

// getServerTimeoutFromEnv 从环境变量获取服务器超时配置（分钟）
func getServerTimeoutFromEnv(envVar string, defaultMinutes int) time.Duration {
	if env := os.Getenv(envVar); env != "" {
		if minutes, err := strconv.Atoi(env); err == nil && minutes > 0 {
			return time.Duration(minutes)
		}
	}
	return time.Duration(defaultMinutes)
}

// corsMiddleware CORS中间件
func corsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization, x-api-key")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(http.StatusOK)
			return
		}

		c.Next()
	}
}
