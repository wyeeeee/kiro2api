package utils

// ObjectPool 通用对象池接口
type ObjectPool[T any] interface {
	Get() T
	Put(T)
}

// PoolStats 对象池统计信息
type PoolStats struct {
	Allocated int64 `json:"allocated"`
	InUse     int64 `json:"in_use"`
	Available int64 `json:"available"`
	Hits      int64 `json:"hits"`
	Misses    int64 `json:"misses"`
}

// StatsAwarePool 支持统计的对象池接口
type StatsAwarePool[T any] interface {
	ObjectPool[T]
	GetPoolStats() PoolStats
}