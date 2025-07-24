package server

import (
	"fmt"
	"os"
	"strings"
	"time"

	"kiro2api/auth"
	"kiro2api/config"
	"kiro2api/converter"
	"kiro2api/logger"
	"kiro2api/types"

	"github.com/bytedance/sonic"
	"github.com/fasthttp/router"
	"github.com/valyala/fasthttp"
)

var fasthttpClient = &fasthttp.Client{}

// StartServer 启动HTTP代理服务器
func StartServer(port string, authToken string) {
	r := router.New()

	// GET /v1/models 端点
	r.GET("/v1/models", logMiddleware(func(ctx *fasthttp.RequestCtx) {
		// 认证检查
		providedApiKey := string(ctx.Request.Header.Peek("Authorization"))
		if providedApiKey == "" {
			providedApiKey = string(ctx.Request.Header.Peek("x-api-key"))
		} else {
			providedApiKey = strings.TrimPrefix(providedApiKey, "Bearer ")
		}

		if providedApiKey == "" {
			logger.Warn("请求缺少Authorization或x-api-key头")
			ctx.Error("401", fasthttp.StatusUnauthorized)
			return
		}

		if providedApiKey != authToken {
			logger.Error("authToken验证失败",
				logger.String("expected", "***"),
				logger.String("provided", "***"))
			ctx.Error("401", fasthttp.StatusUnauthorized)
			return
		}

		// 设置响应头
		ctx.Response.Header.Set("Content-Type", "application/json")
		ctx.Response.Header.Set("Access-Control-Allow-Origin", "*")
		ctx.Response.Header.Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		ctx.Response.Header.Set("Access-Control-Allow-Headers", "Content-Type, Authorization, x-api-key")

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

		jsonData, err := sonic.Marshal(response)
		if err != nil {
			logger.Error("序列化模型列表失败", logger.Err(err))
			ctx.Error("内部服务器错误", fasthttp.StatusInternalServerError)
			return
		}

		ctx.Write(jsonData)
	}))

	r.POST("/v1/messages", logMiddleware(func(ctx *fasthttp.RequestCtx) {
		if !ctx.IsPost() {
			logger.Error("不支持的请求方法", logger.String("method", string(ctx.Method())))
			ctx.Error("只支持POST请求", fasthttp.StatusMethodNotAllowed)
			return
		}

		token, err := auth.GetToken()
		if err != nil {
			logger.Error("获取token失败", logger.Err(err))
			ctx.Error(fmt.Sprintf("获取token失败: %v", err), fasthttp.StatusInternalServerError)
			return
		}

		if strings.HasPrefix(string(ctx.Path()), "/v1") {
			// 尝试从Authorization或x-api-key头获取API密钥
			providedApiKey := string(ctx.Request.Header.Peek("Authorization"))
			if providedApiKey == "" {
				providedApiKey = string(ctx.Request.Header.Peek("x-api-key"))
			} else {
				// 如果是Authorization头，移除Bearer前缀
				providedApiKey = strings.TrimPrefix(providedApiKey, "Bearer ")
			}

			if providedApiKey == "" {
				logger.Warn("请求缺少Authorization或x-api-key头")
				ctx.Error("401", fasthttp.StatusUnauthorized)
				return
			}

			if providedApiKey != authToken {
				logger.Error("authToken验证失败",
					logger.String("expected", "***"),
					logger.String("provided", "***"))
				ctx.Error("401", fasthttp.StatusUnauthorized)
				return
			}
		}

		body := ctx.PostBody()
		logger.Debug("收到Anthropic请求",
			logger.String("body", string(body)),
			logger.Int("body_size", len(body)))

		var anthropicReq types.AnthropicRequest
		if err := sonic.Unmarshal(body, &anthropicReq); err != nil {
			logger.Error("解析请求体失败", logger.Err(err))
			ctx.Error(fmt.Sprintf("解析请求体失败: %v", err), fasthttp.StatusBadRequest)
			return
		}

		logger.Debug("请求解析成功",
			logger.String("model", anthropicReq.Model),
			logger.Bool("stream", anthropicReq.Stream),
			logger.Int("max_tokens", anthropicReq.MaxTokens))

		if anthropicReq.Stream {
			handleStreamRequest(ctx, anthropicReq, token.AccessToken)
			return
		}

		handleNonStreamRequest(ctx, anthropicReq, token.AccessToken)
	}))

	// 新增：OpenAI兼容的 /v1/chat/completions 端点
	r.POST("/v1/chat/completions", logMiddleware(func(ctx *fasthttp.RequestCtx) {
		if !ctx.IsPost() {
			logger.Error("不支持的请求方法", logger.String("method", string(ctx.Method())))
			ctx.Error("只支持POST请求", fasthttp.StatusMethodNotAllowed)
			return
		}

		token, err := auth.GetToken()
		if err != nil {
			logger.Error("获取token失败", logger.Err(err))
			ctx.Error(fmt.Sprintf("获取token失败: %v", err), fasthttp.StatusInternalServerError)
			return
		}

		if !validateAPIKey(ctx, authToken) {
			return
		}

		body := ctx.PostBody()
		logger.Debug("收到OpenAI请求",
			logger.String("body", string(body)),
			logger.Int("body_size", len(body)))

		var openaiReq types.OpenAIRequest
		if err := sonic.Unmarshal(body, &openaiReq); err != nil {
			logger.Error("解析OpenAI请求体失败", logger.Err(err))
			ctx.Error(fmt.Sprintf("解析请求体失败: %v", err), fasthttp.StatusBadRequest)
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
			handleOpenAIStreamRequest(ctx, anthropicReq, token.AccessToken)
			return
		}

		handleOpenAINonStreamRequest(ctx, anthropicReq, token.AccessToken)
	}))

	r.GET("/health", logMiddleware(func(ctx *fasthttp.RequestCtx) {
		ctx.SetStatusCode(fasthttp.StatusOK)
		ctx.SetBodyString("OK")
	}))

	r.NotFound = logMiddleware(func(ctx *fasthttp.RequestCtx) {
		logger.Warn("访问未知端点",
			logger.String("path", string(ctx.Path())),
			logger.String("method", string(ctx.Method())))
		ctx.Error("404 未找到", fasthttp.StatusNotFound)
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

	if err := fasthttp.ListenAndServe(":"+port, r.Handler); err != nil {
		logger.Error("启动服务器失败", logger.Err(err), logger.String("port", port))
		os.Exit(1)
	}
}

// logMiddleware 记录所有HTTP请求的中间件
func logMiddleware(next fasthttp.RequestHandler) fasthttp.RequestHandler {
	return func(ctx *fasthttp.RequestCtx) {
		startTime := time.Now()

		logger.Debug("开始处理请求",
			logger.String("method", string(ctx.Method())),
			logger.String("path", string(ctx.Path())),
			logger.String("remote_addr", ctx.RemoteAddr().String()))

		next(ctx)

		duration := time.Since(startTime)
		logger.Debug("请求处理完成",
			logger.Duration("duration", duration),
			logger.Int("status_code", ctx.Response.StatusCode()))
	}
}
