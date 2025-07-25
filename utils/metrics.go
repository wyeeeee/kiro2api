package utils

import (
	"sync/atomic"
	"time"
)

// HTTPMetrics HTTP请求性能指标
type HTTPMetrics struct {
	RequestCount    int64         // 总请求数
	SuccessCount    int64         // 成功请求数
	ErrorCount      int64         // 错误请求数
	TotalLatency    int64         // 总延迟(纳秒)
	MaxLatency      int64         // 最大延迟(纳秒)
	MinLatency      int64         // 最小延迟(纳秒)
}

var (
	// GlobalMetrics 全局HTTP指标
	GlobalMetrics HTTPMetrics
)

// RecordRequest 记录HTTP请求指标
func RecordRequest(duration time.Duration, success bool) {
	latencyNs := duration.Nanoseconds()
	
	atomic.AddInt64(&GlobalMetrics.RequestCount, 1)
	atomic.AddInt64(&GlobalMetrics.TotalLatency, latencyNs)
	
	if success {
		atomic.AddInt64(&GlobalMetrics.SuccessCount, 1)
	} else {
		atomic.AddInt64(&GlobalMetrics.ErrorCount, 1)
	}
	
	// 更新最大延迟
	for {
		current := atomic.LoadInt64(&GlobalMetrics.MaxLatency)
		if latencyNs <= current || atomic.CompareAndSwapInt64(&GlobalMetrics.MaxLatency, current, latencyNs) {
			break
		}
	}
	
	// 更新最小延迟
	for {
		current := atomic.LoadInt64(&GlobalMetrics.MinLatency)
		if current == 0 || (latencyNs < current && atomic.CompareAndSwapInt64(&GlobalMetrics.MinLatency, current, latencyNs)) {
			break
		}
		if current != 0 && latencyNs >= current {
			break
		}
	}
}

// GetMetrics 获取当前指标快照
func GetMetrics() HTTPMetrics {
	return HTTPMetrics{
		RequestCount: atomic.LoadInt64(&GlobalMetrics.RequestCount),
		SuccessCount: atomic.LoadInt64(&GlobalMetrics.SuccessCount),
		ErrorCount:   atomic.LoadInt64(&GlobalMetrics.ErrorCount),
		TotalLatency: atomic.LoadInt64(&GlobalMetrics.TotalLatency),
		MaxLatency:   atomic.LoadInt64(&GlobalMetrics.MaxLatency),
		MinLatency:   atomic.LoadInt64(&GlobalMetrics.MinLatency),
	}
}

// AvgLatency 计算平均延迟
func (m HTTPMetrics) AvgLatency() time.Duration {
	if m.RequestCount == 0 {
		return 0
	}
	return time.Duration(m.TotalLatency / m.RequestCount)
}

// SuccessRate 计算成功率
func (m HTTPMetrics) SuccessRate() float64 {
	if m.RequestCount == 0 {
		return 0
	}
	return float64(m.SuccessCount) / float64(m.RequestCount)
}