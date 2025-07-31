package utils

import (
	"context"
	"fmt"
	"net/http"
	_ "net/http/pprof" // 导入pprof包，自动注册handlers
	"runtime"
	"sync/atomic"
	"time"

	"kiro2api/logger"
	
	"github.com/gin-gonic/gin"
)

// PerformanceMonitor 性能监控器
type PerformanceMonitor struct {
	// 请求统计
	totalRequests    int64 // 总请求数
	successRequests  int64 // 成功请求数
	failedRequests   int64 // 失败请求数
	streamRequests   int64 // 流式请求数
	nonStreamRequests int64 // 非流式请求数

	// 延迟统计
	totalLatency     int64 // 总延迟（纳秒）
	minLatency       int64 // 最小延迟（纳秒）
	maxLatency       int64 // 最大延迟（纳秒）
	
	// Token统计
	tokenRefreshes   int64 // Token刷新次数
	tokenCacheHits   int64 // Token缓存命中次数
	tokenCacheMisses int64 // Token缓存未命中次数

	// 内存和GC统计
	startTime        time.Time
	gcCollections    uint32 // GC回收次数
	lastGCTime       time.Time
}

// NewPerformanceMonitor 创建性能监控器
func NewPerformanceMonitor() *PerformanceMonitor {
	return &PerformanceMonitor{
		startTime:    time.Now(),
		minLatency:   int64(^uint64(0) >> 1), // 设置为最大int64值
		lastGCTime:   time.Now(),
	}
}

// RecordRequest 记录请求
func (pm *PerformanceMonitor) RecordRequest(success bool, isStream bool, latency time.Duration) {
	atomic.AddInt64(&pm.totalRequests, 1)
	
	if success {
		atomic.AddInt64(&pm.successRequests, 1)
	} else {
		atomic.AddInt64(&pm.failedRequests, 1)
	}

	if isStream {
		atomic.AddInt64(&pm.streamRequests, 1)
	} else {
		atomic.AddInt64(&pm.nonStreamRequests, 1)
	}

	// 更新延迟统计
	latencyNano := latency.Nanoseconds()
	atomic.AddInt64(&pm.totalLatency, latencyNano)

	// 更新最小延迟
	for {
		oldMin := atomic.LoadInt64(&pm.minLatency)
		if latencyNano >= oldMin || atomic.CompareAndSwapInt64(&pm.minLatency, oldMin, latencyNano) {
			break
		}
	}

	// 更新最大延迟
	for {
		oldMax := atomic.LoadInt64(&pm.maxLatency)
		if latencyNano <= oldMax || atomic.CompareAndSwapInt64(&pm.maxLatency, oldMax, latencyNano) {
			break
		}
	}
}

// RecordTokenRefresh 记录Token刷新
func (pm *PerformanceMonitor) RecordTokenRefresh() {
	atomic.AddInt64(&pm.tokenRefreshes, 1)
}

// RecordTokenCacheHit 记录Token缓存命中
func (pm *PerformanceMonitor) RecordTokenCacheHit() {
	atomic.AddInt64(&pm.tokenCacheHits, 1)
}

// RecordTokenCacheMiss 记录Token缓存未命中
func (pm *PerformanceMonitor) RecordTokenCacheMiss() {
	atomic.AddInt64(&pm.tokenCacheMisses, 1)
}

// GetStats 获取性能统计信息
func (pm *PerformanceMonitor) GetStats() map[string]interface{} {
	totalReq := atomic.LoadInt64(&pm.totalRequests)
	totalLatency := atomic.LoadInt64(&pm.totalLatency)
	
	var avgLatency float64
	if totalReq > 0 {
		avgLatency = float64(totalLatency) / float64(totalReq) / 1e6 // 转换为毫秒
	}

	// 获取内存统计
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	// 计算Token缓存命中率
	totalCacheOps := atomic.LoadInt64(&pm.tokenCacheHits) + atomic.LoadInt64(&pm.tokenCacheMisses)
	var cacheHitRate float64
	if totalCacheOps > 0 {
		cacheHitRate = float64(atomic.LoadInt64(&pm.tokenCacheHits)) / float64(totalCacheOps)
	}

	return map[string]interface{}{
		// 请求统计
		"total_requests":      totalReq,
		"success_requests":    atomic.LoadInt64(&pm.successRequests),
		"failed_requests":     atomic.LoadInt64(&pm.failedRequests),
		"stream_requests":     atomic.LoadInt64(&pm.streamRequests),
		"non_stream_requests": atomic.LoadInt64(&pm.nonStreamRequests),
		
		// 延迟统计（毫秒）
		"avg_latency_ms":      avgLatency,
		"min_latency_ms":      float64(atomic.LoadInt64(&pm.minLatency)) / 1e6,
		"max_latency_ms":      float64(atomic.LoadInt64(&pm.maxLatency)) / 1e6,
		
		// Token统计
		"token_refreshes":     atomic.LoadInt64(&pm.tokenRefreshes),
		"token_cache_hits":    atomic.LoadInt64(&pm.tokenCacheHits),
		"token_cache_misses":  atomic.LoadInt64(&pm.tokenCacheMisses),
		"token_cache_hit_rate": cacheHitRate,
		
		// 系统统计
		"uptime_seconds":      time.Since(pm.startTime).Seconds(),
		"goroutines":          runtime.NumGoroutine(),
		"gc_collections":      m.NumGC,
		"memory_alloc_mb":     float64(m.Alloc) / 1024 / 1024,
		"memory_sys_mb":       float64(m.Sys) / 1024 / 1024,
		"memory_heap_mb":      float64(m.HeapAlloc) / 1024 / 1024,
		"cpu_cores":           runtime.NumCPU(),
	}
}

// GetHealthStatus 获取健康状态
func (pm *PerformanceMonitor) GetHealthStatus() map[string]interface{} {
	totalReq := atomic.LoadInt64(&pm.totalRequests)
	failedReq := atomic.LoadInt64(&pm.failedRequests)
	
	var errorRate float64
	if totalReq > 0 {
		errorRate = float64(failedReq) / float64(totalReq)
	}

	// 获取内存使用情况
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	
	// 判断健康状态
	status := "healthy"
	if errorRate > 0.1 { // 错误率超过10%
		status = "unhealthy"
	} else if errorRate > 0.05 { // 错误率超过5%
		status = "degraded"
	}

	// 检查内存使用
	heapMB := float64(m.HeapAlloc) / 1024 / 1024
	if heapMB > 500 { // 堆内存超过500MB
		status = "degraded"
	}

	return map[string]interface{}{
		"status":     status,
		"error_rate": errorRate,
		"uptime":     time.Since(pm.startTime).String(),
		"memory_mb":  heapMB,
		"goroutines": runtime.NumGoroutine(),
	}
}

// StartMonitoring 启动监控
func (pm *PerformanceMonitor) StartMonitoring(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	logger.Info("性能监控已启动")

	for {
		select {
		case <-ticker.C:
			stats := pm.GetStats()
			logger.Info("性能统计",
				logger.Int64("total_requests", stats["total_requests"].(int64)),
				logger.String("avg_latency_ms", fmt.Sprintf("%.2f", stats["avg_latency_ms"].(float64))),
				logger.String("memory_heap_mb", fmt.Sprintf("%.2f", stats["memory_heap_mb"].(float64))),
				logger.Int("goroutines", stats["goroutines"].(int)),
				logger.String("token_cache_hit_rate", fmt.Sprintf("%.2f", stats["token_cache_hit_rate"].(float64))))
		case <-ctx.Done():
			logger.Info("性能监控已停止")
			return
		}
	}
}

// 全局性能监控实例
var GlobalPerformanceMonitor = NewPerformanceMonitor()

// RegisterPProfRoutes 在指定的gin路由器上注册pprof路由
func RegisterPProfRoutes(router gin.IRouter) {
	pprof := router.Group("/debug/pprof")
	{
		pprof.GET("/", gin.WrapF(http.DefaultServeMux.ServeHTTP))
		pprof.GET("/cmdline", gin.WrapF(http.DefaultServeMux.ServeHTTP))
		pprof.GET("/profile", gin.WrapF(http.DefaultServeMux.ServeHTTP))
		pprof.POST("/symbol", gin.WrapF(http.DefaultServeMux.ServeHTTP))
		pprof.GET("/symbol", gin.WrapF(http.DefaultServeMux.ServeHTTP))
		pprof.GET("/trace", gin.WrapF(http.DefaultServeMux.ServeHTTP))
		pprof.GET("/allocs", gin.WrapF(http.DefaultServeMux.ServeHTTP))
		pprof.GET("/block", gin.WrapF(http.DefaultServeMux.ServeHTTP))
		pprof.GET("/goroutine", gin.WrapF(http.DefaultServeMux.ServeHTTP))
		pprof.GET("/heap", gin.WrapF(http.DefaultServeMux.ServeHTTP))
		pprof.GET("/mutex", gin.WrapF(http.DefaultServeMux.ServeHTTP))
		pprof.GET("/threadcreate", gin.WrapF(http.DefaultServeMux.ServeHTTP))
	}
	
	logger.Info("pprof路由已注册", logger.String("path", "/debug/pprof/*"))
}

// RequestTracker 请求跟踪器
type RequestTracker struct {
	startTime time.Time
	isStream  bool
}

// NewRequestTracker 创建请求跟踪器
func NewRequestTracker(isStream bool) *RequestTracker {
	return &RequestTracker{
		startTime: time.Now(),
		isStream:  isStream,
	}
}

// RecordSuccess 记录成功
func (rt *RequestTracker) RecordSuccess() {
	latency := time.Since(rt.startTime)
	GlobalPerformanceMonitor.RecordRequest(true, rt.isStream, latency)
}

// RecordFailure 记录失败
func (rt *RequestTracker) RecordFailure() {
	latency := time.Since(rt.startTime)
	GlobalPerformanceMonitor.RecordRequest(false, rt.isStream, latency)
}

// GetLatency 获取当前延迟
func (rt *RequestTracker) GetLatency() time.Duration {
	return time.Since(rt.startTime)
}