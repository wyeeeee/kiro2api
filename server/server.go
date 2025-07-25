package server

import (
	"fmt"
	"net/http"
	"os"

	"kiro2api/auth"
	"kiro2api/config"
	"kiro2api/converter"
	"kiro2api/logger"
	"kiro2api/types"

	"github.com/bytedance/sonic"
	"github.com/gin-gonic/gin"
)

// 移除全局httpClient，使用utils包中的共享客户端

// StartServer 启动HTTP代理服务器
func StartServer(port string, authToken string) {
	// 设置 gin 模式
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()

	// 添加中间件
	r.Use(gin.Logger())
	r.Use(gin.Recovery())
	r.Use(corsMiddleware())
	r.Use(AuthMiddleware(authToken))

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

		var anthropicReq types.AnthropicRequest
		if err := sonic.Unmarshal(body, &anthropicReq); err != nil {
			logger.Error("解析请求体失败", logger.Err(err))
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("解析请求体失败: %v", err)})
			return
		}

		logger.Debug("请求解析成功",
			logger.String("model", anthropicReq.Model),
			logger.Bool("stream", anthropicReq.Stream),
			logger.Int("max_tokens", anthropicReq.MaxTokens))

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
		if err := sonic.Unmarshal(body, &openaiReq); err != nil {
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

	r.GET("/health", func(c *gin.Context) {
		c.String(http.StatusOK, "OK")
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

	if err := r.Run(":" + port); err != nil {
		logger.Error("启动服务器失败", logger.Err(err), logger.String("port", port))
		os.Exit(1)
	}
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
