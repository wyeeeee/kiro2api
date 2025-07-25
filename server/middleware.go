package server

import (
	"net/http"
	"strings"

	"kiro2api/logger"

	"github.com/gin-gonic/gin"
)

// AuthMiddleware 创建API密钥验证中间件
func AuthMiddleware(authToken string) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 跳过健康检查端点的认证
		if c.Request.URL.Path == "/health" {
			c.Next()
			return
		}

		if !validateAPIKey(c, authToken) {
			c.Abort()
			return
		}

		c.Next()
	}
}

// extractAPIKey 提取API密钥的通用逻辑
func extractAPIKey(c *gin.Context) string {
	apiKey := c.GetHeader("Authorization")
	if apiKey == "" {
		apiKey = c.GetHeader("x-api-key")
	} else {
		apiKey = strings.TrimPrefix(apiKey, "Bearer ")
	}
	return apiKey
}

// validateAPIKey 验证API密钥 - 重构后的版本
func validateAPIKey(c *gin.Context, authToken string) bool {
	providedApiKey := extractAPIKey(c)

	if providedApiKey == "" {
		logger.Warn("请求缺少Authorization或x-api-key头")
		c.JSON(http.StatusUnauthorized, gin.H{"error": "401"})
		return false
	}

	if providedApiKey != authToken {
		logger.Error("authToken验证失败",
			logger.String("expected", "***"),
			logger.String("provided", "***"))
		c.JSON(http.StatusUnauthorized, gin.H{"error": "401"})
		return false
	}

	return true
}
