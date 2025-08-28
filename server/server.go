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

	// 静态资源服务 - 前后端完全分离
	r.Static("/static", "./static")
	r.GET("/", func(c *gin.Context) {
		c.File("./static/index.html")
	})

	// API端点 - 纯数据服务
	r.GET("/api/tokens", handleTokenPoolAPI)

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

		tokenInfo, err := auth.GetToken()
		if err != nil {
			logger.Error("获取token失败", logger.Err(err))
			respondError(c, http.StatusInternalServerError, "获取token失败: %v", err)
			return
		}

		body, err := c.GetRawData()
		if err != nil {
			logger.Error("读取请求体失败", logger.Err(err))
			respondError(c, http.StatusBadRequest, "读取请求体失败: %v", err)
			return
		}

		logger.Debug("收到Anthropic请求",
			logger.String("body", string(body)),
			logger.Int("body_size", len(body)),
			logger.String("remote_addr", c.ClientIP()),
			logger.String("user_agent", c.GetHeader("User-Agent")))

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

		// 如果环境变量禁用了流式传输，则强制设置为false
		if config.IsStreamDisabled() {
			logger.Info("DISABLE_STREAM=true，强制禁用流式响应",
				logger.String("original_stream", fmt.Sprintf("%v", anthropicReq.Stream)))
			anthropicReq.Stream = false
		}

		if anthropicReq.Stream {
			handleStreamRequest(c, anthropicReq, tokenInfo)
			return
		}

		handleNonStreamRequest(c, anthropicReq, tokenInfo)
	})

	// 新增：OpenAI兼容的 /v1/chat/completions 端点
	r.POST("/v1/chat/completions", func(c *gin.Context) {
		tokenInfo, err := auth.GetToken()
		if err != nil {
			logger.Error("获取token失败", logger.Err(err))
			respondError(c, http.StatusInternalServerError, "获取token失败: %v", err)
			return
		}

		body, err := c.GetRawData()
		if err != nil {
			logger.Error("读取请求体失败", logger.Err(err))
			respondError(c, http.StatusBadRequest, "读取请求体失败: %v", err)
			return
		}

		logger.Debug("收到OpenAI请求",
			logger.String("body", string(body)),
			logger.Int("body_size", len(body)))

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

		// 如果环境变量禁用了流式传输，则强制设置为false
		if config.IsStreamDisabled() {
			logger.Info("DISABLE_STREAM=true，强制禁用流式响应",
				logger.String("original_stream", fmt.Sprintf("%v", anthropicReq.Stream)))
			anthropicReq.Stream = false
		}

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
	logger.Info("  GET  /                    - 重定向到静态Dashboard")
	logger.Info("  GET  /static/*            - 静态资源服务")
	logger.Info("  GET  /api/tokens          - Token池状态API")
	logger.Info("  GET  /v1/models           - 模型列表")
	logger.Info("  POST /v1/messages         - Anthropic API代理")
	logger.Info("  POST /v1/chat/completions - OpenAI API代理")
	logger.Info("按Ctrl+C停止服务器")

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
