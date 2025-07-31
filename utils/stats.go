package utils

// StatsProvider 统一的统计信息提供接口
type StatsProvider interface {
	GetStats() map[string]interface{}
}

// MetricsCollector 统一的指标收集器
type MetricsCollector struct {
	providers map[string]StatsProvider
}

// NewMetricsCollector 创建新的指标收集器
func NewMetricsCollector() *MetricsCollector {
	return &MetricsCollector{
		providers: make(map[string]StatsProvider),
	}
}

// Register 注册统计提供者
func (mc *MetricsCollector) Register(name string, provider StatsProvider) {
	mc.providers[name] = provider
}

// CollectAll 收集所有注册的统计信息
func (mc *MetricsCollector) CollectAll() map[string]interface{} {
	result := make(map[string]interface{})
	
	for name, provider := range mc.providers {
		if provider != nil {
			result[name] = provider.GetStats()
		}
	}
	
	return result
}

// GetProvider 获取指定名称的统计提供者
func (mc *MetricsCollector) GetProvider(name string) StatsProvider {
	return mc.providers[name]
}

// 全局指标收集器实例
var GlobalMetricsCollector = NewMetricsCollector()