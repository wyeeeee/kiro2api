package server

import (
	"net/http"

	"kiro2api/logger"

	"github.com/gin-gonic/gin"
)

// logAndRespondError 记录错误日志并返回错误响应
func logAndRespondError(c *gin.Context, status int, operation string, err error) {
	logger.Error(operation+"失败", addReqFields(c, logger.Err(err))...)
	respondError(c, status, operation+"失败: %v", err)
}

// handleParseError 处理请求体解析错误
func handleParseError(c *gin.Context, err error) {
	logger.Error("解析请求体失败", addReqFields(c, logger.Err(err))...)
	respondError(c, http.StatusBadRequest, "解析请求体失败: %v", err)
}

// handleTokenError 处理token获取失败
func handleTokenError(c *gin.Context, err error) {
	logger.Error("获取token失败", addReqFields(c, logger.Err(err))...)
	respondError(c, http.StatusInternalServerError, "获取token失败: %v", err)
}

// handleUpstreamError 处理上游服务错误
func handleUpstreamError(c *gin.Context, err error) {
	logger.Error("上游服务错误", addReqFields(c, logger.Err(err))...)
	respondError(c, http.StatusBadGateway, "上游服务错误: %v", err)
}

// handleBuildRequestError 处理构建请求失败
func handleBuildRequestError(c *gin.Context, err error) {
	logger.Error("构建请求失败", addReqFields(c, logger.Err(err))...)
	respondError(c, http.StatusInternalServerError, "构建请求失败: %v", err)
}
