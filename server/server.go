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
	"kiro2api/webconfig"

	"github.com/gin-gonic/gin"
)

// 移除全局httpClient，使用utils包中的共享客户端

// StartServer 启动HTTP代理服务器
func StartServer(port string, authToken string, authService *auth.AuthService) {
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
	// 注入请求ID，便于日志追踪
	r.Use(RequestIDMiddleware())
	r.Use(corsMiddleware())
	// 只对 /v1 开头的端点进行认证
	r.Use(PathBasedAuthMiddleware(authToken, []string{"/v1", "/api/tokens"}))

	// 静态资源服务 - 前后端完全分离
	r.Static("/static", "./static")
	r.GET("/", func(c *gin.Context) {
		c.File("./static/index.html")
	})

	// API端点 - 纯数据服务
	// 注意：不在这里添加 /api/tokens，避免与Web配置路由冲突

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
		// 检查AuthService是否可用
		if authService == nil {
			respondError(c, http.StatusServiceUnavailable, "%s", "未配置认证Token，请先在Web配置界面中添加Token")
			return
		}

		// 使用RequestContext统一处理token获取和请求体读取
		reqCtx := &RequestContext{
			GinContext:  c,
			AuthService: authService,
			RequestType: "Anthropic",
		}

		tokenInfo, body, err := reqCtx.GetTokenAndBody()
		if err != nil {
			return // 错误已在GetTokenAndBody中处理
		}

		// 先解析为通用map以便处理工具格式
		var rawReq map[string]any
		if err := utils.SafeUnmarshal(body, &rawReq); err != nil {
			logger.Error("解析请求体失败", logger.Err(err))
			respondError(c, http.StatusBadRequest, "解析请求体失败: %v", err)
			return
		}

		// 标准化工具格式处理
		if tools, exists := rawReq["tools"]; exists && tools != nil {
			if toolsArray, ok := tools.([]any); ok {
				normalizedTools := make([]map[string]any, 0, len(toolsArray))
				for _, tool := range toolsArray {
					if toolMap, ok := tool.(map[string]any); ok {
						// 检查是否是简化的工具格式（直接包含name, description, input_schema）
						if name, hasName := toolMap["name"]; hasName {
							if description, hasDesc := toolMap["description"]; hasDesc {
								if inputSchema, hasSchema := toolMap["input_schema"]; hasSchema {
									// 转换为标准Anthropic工具格式
									normalizedTool := map[string]any{
										"name":         name,
										"description":  description,
										"input_schema": inputSchema,
									}
									normalizedTools = append(normalizedTools, normalizedTool)
									continue
								}
							}
						}
						// 如果不是简化格式，保持原样
						normalizedTools = append(normalizedTools, toolMap)
					}
				}
				rawReq["tools"] = normalizedTools
			}
		}

		// 重新序列化并解析为AnthropicRequest
		normalizedBody, err := utils.SafeMarshal(rawReq)
		if err != nil {
			logger.Error("重新序列化请求失败", logger.Err(err))
			respondError(c, http.StatusBadRequest, "处理请求格式失败: %v", err)
			return
		}

		var anthropicReq types.AnthropicRequest
		if err := utils.SafeUnmarshal(normalizedBody, &anthropicReq); err != nil {
			logger.Error("解析标准化请求体失败", logger.Err(err))
			respondError(c, http.StatusBadRequest, "解析请求体失败: %v", err)
			return
		}

		// logger.Debug("请求解析成功",
		// 	logger.String("model", anthropicReq.Model),
		// 	logger.Bool("stream", anthropicReq.Stream),
		// 	logger.Int("max_tokens", anthropicReq.MaxTokens),
		// 	logger.Int("messages_count", len(anthropicReq.Messages)),
		// 	logger.Int("tools_count", len(anthropicReq.Tools)))

		// 详细记录工具信息以调试
		// for i, tool := range anthropicReq.Tools {
		// 	descPreview := tool.Description
		// 	if len(descPreview) > 100 {
		// 		descPreview = descPreview[:100] + "..."
		// 	}
		// 	logger.Debug("工具详情",
		// 		logger.Int("index", i),
		// 		logger.String("name", tool.Name),
		// 		logger.String("description", descPreview))
		// }

		// 验证请求的有效性
		if len(anthropicReq.Messages) == 0 {
			logger.Error("请求中没有消息")
			respondError(c, http.StatusBadRequest, "%s", "messages 数组不能为空")
			return
		}

		// 验证最后一条消息有有效内容
		lastMsg := anthropicReq.Messages[len(anthropicReq.Messages)-1]
		content, err := utils.GetMessageContent(lastMsg.Content)
		if err != nil {
			logger.Error("获取消息内容失败",
				logger.Err(err),
				logger.String("raw_content", fmt.Sprintf("%v", lastMsg.Content)))
			respondError(c, http.StatusBadRequest, "获取消息内容失败: %v", err)
			return
		}

		trimmedContent := strings.TrimSpace(content)
		if trimmedContent == "" || trimmedContent == "answer for user question" {
			logger.Error("消息内容为空或无效",
				logger.String("content", content),
				logger.String("trimmed_content", trimmedContent))
			respondError(c, http.StatusBadRequest, "%s", "消息内容不能为空")
			return
		}

		if anthropicReq.Stream {
			handleStreamRequest(c, anthropicReq, tokenInfo)
			return
		}

		handleNonStreamRequest(c, anthropicReq, tokenInfo)
	})

	// Token计数端点
	r.POST("/v1/messages/count_tokens", handleCountTokens)

	// 新增：OpenAI兼容的 /v1/chat/completions 端点
	r.POST("/v1/chat/completions", func(c *gin.Context) {
		// 检查AuthService是否可用
		if authService == nil {
			respondError(c, http.StatusServiceUnavailable, "%s", "未配置认证Token，请先在Web配置界面中添加Token")
			return
		}

		// 使用RequestContext统一处理token获取和请求体读取
		reqCtx := &RequestContext{
			GinContext:  c,
			AuthService: authService,
			RequestType: "OpenAI",
		}

		tokenInfo, body, err := reqCtx.GetTokenAndBody()
		if err != nil {
			return // 错误已在GetTokenAndBody中处理
		}

		var openaiReq types.OpenAIRequest
		if err := utils.SafeUnmarshal(body, &openaiReq); err != nil {
			logger.Error("解析OpenAI请求体失败", logger.Err(err))
			respondError(c, http.StatusBadRequest, "解析请求体失败: %v", err)
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
			handleOpenAIStreamRequest(c, anthropicReq, tokenInfo)
			return
		}
		handleOpenAINonStreamRequest(c, anthropicReq, tokenInfo)
	})

	r.NoRoute(func(c *gin.Context) {
		logger.Warn("访问未知端点",
			logger.String("path", c.Request.URL.Path),
			logger.String("method", c.Request.Method))
		respondError(c, http.StatusNotFound, "%s", "404 未找到")
	})

	logger.Info("启动Anthropic API代理服务器",
		logger.String("port", port),
		logger.String("auth_token", "***"))
	logger.Info("AuthToken 验证已启用")
	logger.Info("可用端点:")
	logger.Info("  GET  /                          - 重定向到静态Dashboard")
	logger.Info("  GET  /static/*                  - 静态资源服务")
	logger.Info("  GET  /api/tokens                - Token池状态API")
	logger.Info("  GET  /v1/models                 - 模型列表")
	logger.Info("  POST /v1/messages               - Anthropic API代理")
	logger.Info("  POST /v1/messages/count_tokens  - Token计数接口")
	logger.Info("  POST /v1/chat/completions       - OpenAI API代理")
	logger.Info("按Ctrl+C停止服务器")

	// 获取服务器超时配置
	readTimeout := getServerTimeoutFromEnv("SERVER_READ_TIMEOUT_MINUTES", 16) * time.Minute
	writeTimeout := getServerTimeoutFromEnv("SERVER_WRITE_TIMEOUT_MINUTES", 16) * time.Minute

	// 创建自定义HTTP服务器以支持长时间请求
	server := &http.Server{
		Addr:           "0.0.0.0:" + port,
		Handler:        r,
		ReadTimeout:    readTimeout,              // 读取超时
		WriteTimeout:   writeTimeout,             // 写入超时
		IdleTimeout:    config.ServerIdleTimeout, // 空闲连接超时
		MaxHeaderBytes: config.MaxHeaderBytes,    // 最大请求头字节数
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

// StartServerWithConfig 使用Web配置管理器启动HTTP代理服务器
func StartServerWithConfig(port string, authToken string, authService *auth.AuthService, configManager *webconfig.Manager) {
	// 获取配置
	webConfig := configManager.GetConfig()

	// 设置 gin 模式
	gin.SetMode(webConfig.ServiceConfig.GinMode)

	r := gin.New()

	// 添加中间件
	r.Use(gin.Logger())
	r.Use(gin.Recovery())
	// 注入请求ID，便于日志追踪
	r.Use(RequestIDMiddleware())
	r.Use(corsMiddleware())

	// 设置Web配置管理的路由
	setupWebConfigRoutes(r, configManager)

	// 只对 /v1 开头的端点进行认证（不包括 /api 路径，因为 /api 路径由 webconfig 自己管理认证）
	r.Use(PathBasedAuthMiddleware(authToken, []string{"/v1"}))

	// API端点 - 纯数据服务
	// 注意：不在这里添加 /api/tokens，避免与Web配置路由冲突

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
		// 检查AuthService是否可用
		if authService == nil {
			respondError(c, http.StatusServiceUnavailable, "%s", "未配置认证Token，请先在Web配置界面中添加Token")
			return
		}

		// 使用RequestContext统一处理token获取和请求体读取
		reqCtx := &RequestContext{
			GinContext:  c,
			AuthService: authService,
			RequestType: "Anthropic",
		}

		tokenInfo, body, err := reqCtx.GetTokenAndBody()
		if err != nil {
			return // 错误已在GetTokenAndBody中处理
		}

		// 先解析为通用map以便处理工具格式
		var rawReq map[string]any
		if err := utils.SafeUnmarshal(body, &rawReq); err != nil {
			logger.Error("解析请求体失败", logger.Err(err))
			respondError(c, http.StatusBadRequest, "解析请求体失败: %v", err)
			return
		}

		// 标准化工具格式处理
		if tools, exists := rawReq["tools"]; exists && tools != nil {
			if toolsArray, ok := tools.([]any); ok {
				normalizedTools := make([]map[string]any, 0, len(toolsArray))
				for _, tool := range toolsArray {
					if toolMap, ok := tool.(map[string]any); ok {
						// 检查是否是简化的工具格式（直接包含name, description, input_schema）
						if name, hasName := toolMap["name"]; hasName {
							if description, hasDesc := toolMap["description"]; hasDesc {
								if inputSchema, hasSchema := toolMap["input_schema"]; hasSchema {
									// 转换为标准Anthropic工具格式
									normalizedTool := map[string]any{
										"name":         name,
										"description":  description,
										"input_schema": inputSchema,
									}
									normalizedTools = append(normalizedTools, normalizedTool)
									continue
								}
							}
						}
						// 如果不是简化格式，保持原样
						normalizedTools = append(normalizedTools, toolMap)
					}
				}
				rawReq["tools"] = normalizedTools
			}
		}

		// 重新序列化并解析为AnthropicRequest
		normalizedBody, err := utils.SafeMarshal(rawReq)
		if err != nil {
			logger.Error("重新序列化请求失败", logger.Err(err))
			respondError(c, http.StatusBadRequest, "处理请求格式失败: %v", err)
			return
		}

		var anthropicReq types.AnthropicRequest
		if err := utils.SafeUnmarshal(normalizedBody, &anthropicReq); err != nil {
			logger.Error("解析标准化请求体失败", logger.Err(err))
			respondError(c, http.StatusBadRequest, "解析请求体失败: %v", err)
			return
		}

		// 验证请求的有效性
		if len(anthropicReq.Messages) == 0 {
			logger.Error("请求中没有消息")
			respondError(c, http.StatusBadRequest, "%s", "messages 数组不能为空")
			return
		}

		// 验证最后一条消息有有效内容
		lastMsg := anthropicReq.Messages[len(anthropicReq.Messages)-1]
		content, err := utils.GetMessageContent(lastMsg.Content)
		if err != nil {
			logger.Error("获取消息内容失败",
				logger.Err(err),
				logger.String("raw_content", fmt.Sprintf("%v", lastMsg.Content)))
			respondError(c, http.StatusBadRequest, "获取消息内容失败: %v", err)
			return
		}

		trimmedContent := strings.TrimSpace(content)
		if trimmedContent == "" || trimmedContent == "answer for user question" {
			logger.Error("消息内容为空或无效",
				logger.String("content", content),
				logger.String("trimmed_content", trimmedContent))
			respondError(c, http.StatusBadRequest, "%s", "消息内容不能为空")
			return
		}

		if anthropicReq.Stream {
			handleStreamRequest(c, anthropicReq, tokenInfo)
			return
		}

		handleNonStreamRequest(c, anthropicReq, tokenInfo)
	})

	// Token计数端点
	r.POST("/v1/messages/count_tokens", handleCountTokens)

	// 新增：OpenAI兼容的 /v1/chat/completions 端点
	r.POST("/v1/chat/completions", func(c *gin.Context) {
		// 检查AuthService是否可用
		if authService == nil {
			respondError(c, http.StatusServiceUnavailable, "%s", "未配置认证Token，请先在Web配置界面中添加Token")
			return
		}

		// 使用RequestContext统一处理token获取和请求体读取
		reqCtx := &RequestContext{
			GinContext:  c,
			AuthService: authService,
			RequestType: "OpenAI",
		}

		tokenInfo, body, err := reqCtx.GetTokenAndBody()
		if err != nil {
			return // 错误已在GetTokenAndBody中处理
		}

		var openaiReq types.OpenAIRequest
		if err := utils.SafeUnmarshal(body, &openaiReq); err != nil {
			logger.Error("解析OpenAI请求体失败", logger.Err(err))
			respondError(c, http.StatusBadRequest, "解析请求体失败: %v", err)
			return
		}

		// 转换为Anthropic格式
		anthropicReq := converter.ConvertOpenAIToAnthropic(openaiReq)

		if anthropicReq.Stream {
			handleOpenAIStreamRequest(c, anthropicReq, tokenInfo)
			return
		}
		handleOpenAINonStreamRequest(c, anthropicReq, tokenInfo)
	})

	r.NoRoute(func(c *gin.Context) {
		logger.Warn("访问未知端点",
			logger.String("path", c.Request.URL.Path),
			logger.String("method", c.Request.Method))
		respondError(c, http.StatusNotFound, "%s", "404 未找到")
	})

	logger.Info("启动Anthropic API代理服务器",
		logger.String("port", port),
		logger.String("auth_token", "***"))
	logger.Info("AuthToken 验证已启用")
	logger.Info("可用端点:")
	logger.Info("  GET  /                          - Web配置管理页面")
	logger.Info("  GET  /api/tokens                - Token池状态API")
	logger.Info("  GET  /v1/models                 - 模型列表")
	logger.Info("  POST /v1/messages               - Anthropic API代理")
	logger.Info("  POST /v1/messages/count_tokens  - Token计数接口")
	logger.Info("  POST /v1/chat/completions       - OpenAI API代理")
	logger.Info("按Ctrl+C停止服务器")

	// 使用Web配置的超时设置
	readTimeout := time.Duration(webConfig.TimeoutConfig.ServerReadMinutes) * time.Minute
	writeTimeout := time.Duration(webConfig.TimeoutConfig.ServerWriteMinutes) * time.Minute

	// 创建自定义HTTP服务器以支持长时间请求
	server := &http.Server{
		Addr:           "0.0.0.0:" + port,
		Handler:        r,
		ReadTimeout:    readTimeout,              // 读取超时
		WriteTimeout:   writeTimeout,             // 写入超时
		IdleTimeout:    config.ServerIdleTimeout, // 空闲连接超时
		MaxHeaderBytes: config.MaxHeaderBytes,    // 最大请求头字节数
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

// setupWebConfigRoutes 设置Web配置管理路由
func setupWebConfigRoutes(r *gin.Engine, configManager *webconfig.Manager) {
	// 创建路由复用器
	mux := http.NewServeMux()
	configManager.SetupRoutes(mux)

	// 将路由复用器包装为Gin处理器
	r.Any("/", gin.WrapH(mux))
	r.Any("/login", gin.WrapH(mux))
	r.Any("/logout", gin.WrapH(mux))
	r.Any("/config", gin.WrapH(mux))
	// 具体化API路由，避免与通配符冲突
	r.Any("/api/init", gin.WrapH(mux))
	r.Any("/api/config", gin.WrapH(mux))
	r.Any("/api/tokens", gin.WrapH(mux))
	r.Any("/api/tokens/refresh", gin.WrapH(mux))
	r.Any("/api/tokens/refresh-single", gin.WrapH(mux))
	r.Any("/api/tokens/current", gin.WrapH(mux))
	r.Any("/api/tokens/switch", gin.WrapH(mux))
	r.Any("/api/backup", gin.WrapH(mux))
	r.Any("/api/restore", gin.WrapH(mux))
	r.Any("/static/*path", gin.WrapH(mux))
}
