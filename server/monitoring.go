package server

import (
	"net/http"

	"kiro2api/auth"
	"kiro2api/utils"

	"github.com/gin-gonic/gin"
)

// SetupMonitoringRoutes 设置监控相关路由
func SetupMonitoringRoutes(r *gin.Engine) {
	// 健康检查端点（无需认证）
	r.GET("/health", handleHealth)
	
	// 详细健康检查端点（无需认证）
	r.GET("/health/detailed", handleDetailedHealth)
	
	// 性能指标端点（无需认证，生产环境建议加认证）
	r.GET("/metrics", handleMetrics)
	
	// Token池统计端点（无需认证，生产环境建议加认证）
	r.GET("/stats/token-pool", handleTokenPoolStats)
	
	// 工作池统计端点（无需认证，生产环境建议加认证）
	r.GET("/stats/worker-pool", handleWorkerPoolStats)
	
	// 原子缓存统计端点（无需认证，生产环境建议加认证）
	r.GET("/stats/cache", handleCacheStats)
	
	// 注册pprof性能分析路由（无需认证，调试用途）
	utils.RegisterPProfRoutes(r)
}

// handleHealth 健康检查处理器
func handleHealth(c *gin.Context) {
	healthStatus := utils.GlobalPerformanceMonitor.GetHealthStatus()
	
	statusCode := http.StatusOK
	if healthStatus["status"] == "unhealthy" {
		statusCode = http.StatusServiceUnavailable
	} else if healthStatus["status"] == "degraded" {
		statusCode = http.StatusPartialContent
	}
	
	c.JSON(statusCode, gin.H{
		"status":    healthStatus["status"],
		"timestamp": healthStatus["uptime"],
	})
}

// handleDetailedHealth 详细健康检查处理器
func handleDetailedHealth(c *gin.Context) {
	healthStatus := utils.GlobalPerformanceMonitor.GetHealthStatus()
	
	statusCode := http.StatusOK
	if healthStatus["status"] == "unhealthy" {
		statusCode = http.StatusServiceUnavailable
	} else if healthStatus["status"] == "degraded" {
		statusCode = http.StatusPartialContent
	}
	
	c.JSON(statusCode, healthStatus)
}

// handleMetrics 性能指标处理器
func handleMetrics(c *gin.Context) {
	stats := utils.GlobalPerformanceMonitor.GetStats()
	c.JSON(http.StatusOK, gin.H{
		"performance": stats,
		"timestamp":   utils.GlobalPerformanceMonitor.GetStats()["uptime_seconds"],
	})
}

// handleTokenPoolStats Token池统计处理器
func handleTokenPoolStats(c *gin.Context) {
	stats := auth.GetTokenPoolStats()
	c.JSON(http.StatusOK, gin.H{
		"token_pool": stats,
	})
}

// handleWorkerPoolStats 工作池统计处理器
func handleWorkerPoolStats(c *gin.Context) {
	// 检查全局工作池是否已初始化
	if utils.GlobalWorkerPool != nil {
		stats := utils.GlobalWorkerPool.GetStats()
		c.JSON(http.StatusOK, gin.H{
			"worker_pool": stats,
		})
	} else {
		c.JSON(http.StatusOK, gin.H{
			"worker_pool": gin.H{
				"status": "not_initialized",
				"message": "工作池尚未初始化",
			},
		})
	}
}

// handleCacheStats 缓存统计处理器
func handleCacheStats(c *gin.Context) {
	// 获取原子缓存统计
	atomicStats := utils.GlobalAtomicTokenCache.GetStats()
	
	c.JSON(http.StatusOK, gin.H{
		"atomic_cache": atomicStats,
	})
}