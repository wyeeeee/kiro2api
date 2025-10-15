package utils

import (
	"bytes"
	"strings"
	"sync"

	"kiro2api/config"
)

// ObjectPool 通用对象池管理器
type ObjectPool struct {
	// Buffer池 - 用于JSON解析、数据聚合等
	bufferPool *sync.Pool
	// StringBuilder池 - 用于字符串拼接
	stringBuilderPool *sync.Pool
	// ByteSlice池 - 用于网络读取、临时存储
	byteSlicePool *sync.Pool
	// Map池 - 用于临时映射存储
	mapPool *sync.Pool
	// StringSlice池 - 用于临时数组存储
	stringSlicePool *sync.Pool
}

// GlobalPool 全局对象池实例
var GlobalPool = NewObjectPool()

// NewObjectPool 创建新的对象池管理器
func NewObjectPool() *ObjectPool {
	return &ObjectPool{
		bufferPool: &sync.Pool{
			New: func() any {
				return bytes.NewBuffer(make([]byte, 0, config.BufferInitialSize))
			},
		},
		stringBuilderPool: &sync.Pool{
			New: func() any {
				var sb strings.Builder
				sb.Grow(config.StringBuilderInitialSize)
				return &sb
			},
		},
		byteSlicePool: &sync.Pool{
			New: func() any {
				return make([]byte, config.ByteSliceInitialSize)
			},
		},
		mapPool: &sync.Pool{
			New: func() any {
				return make(map[string]any, config.MapInitialCapacity)
			},
		},
		stringSlicePool: &sync.Pool{
			New: func() any {
				return make([]string, 0, config.StringSliceInitialCapacity)
			},
		},
	}
}

// GetBuffer 从池中获取Buffer
func (op *ObjectPool) GetBuffer() *bytes.Buffer {
	buf := op.bufferPool.Get().(*bytes.Buffer)
	buf.Reset() // 确保缓冲区是干净的
	return buf
}

// PutBuffer 将Buffer归还到池中
func (op *ObjectPool) PutBuffer(buf *bytes.Buffer) {
	if buf == nil {
		return
	}
	// 如果缓冲区太大，直接丢弃，避免内存泄漏
	if buf.Cap() > config.BufferMaxRetainSize {
		return
	}
	buf.Reset()
	op.bufferPool.Put(buf)
}

// GetStringBuilder 从池中获取StringBuilder
func (op *ObjectPool) GetStringBuilder() *strings.Builder {
	sb := op.stringBuilderPool.Get().(*strings.Builder)
	sb.Reset()
	return sb
}

// PutStringBuilder 将StringBuilder归还到池中
func (op *ObjectPool) PutStringBuilder(sb *strings.Builder) {
	if sb == nil {
		return
	}
	// 如果StringBuilder太大，直接丢弃
	if sb.Cap() > config.StringBuilderMaxRetainSize {
		return
	}
	sb.Reset()
	op.stringBuilderPool.Put(sb)
}

// GetByteSlice 从池中获取字节数组
func (op *ObjectPool) GetByteSlice() []byte {
	return op.byteSlicePool.Get().([]byte)
}

// PutByteSlice 将字节数组归还到池中
func (op *ObjectPool) PutByteSlice(slice []byte) {
	if slice == nil || cap(slice) > config.ByteSliceMaxRetainSize {
		return
	}
	// 重置长度但保持容量
	slice = slice[:0]
	op.byteSlicePool.Put(slice)
}

// GetMap 从池中获取Map
func (op *ObjectPool) GetMap() map[string]any {
	m := op.mapPool.Get().(map[string]any)
	// 清空map但保持容量
	for k := range m {
		delete(m, k)
	}
	return m
}

// PutMap 将Map归还到池中
func (op *ObjectPool) PutMap(m map[string]any) {
	if m == nil || len(m) > config.MapMaxSize {
		return
	}
	// 清空map
	for k := range m {
		delete(m, k)
	}
	op.mapPool.Put(m)
}


// 便捷的全局函数，直接使用全局池

// GetBuffer 获取Buffer (全局池)
func GetBuffer() *bytes.Buffer {
	return GlobalPool.GetBuffer()
}

// PutBuffer 归还Buffer (全局池)
func PutBuffer(buf *bytes.Buffer) {
	GlobalPool.PutBuffer(buf)
}

// GetStringBuilder 获取StringBuilder (全局池)
func GetStringBuilder() *strings.Builder {
	return GlobalPool.GetStringBuilder()
}

// PutStringBuilder 归还StringBuilder (全局池)
func PutStringBuilder(sb *strings.Builder) {
	GlobalPool.PutStringBuilder(sb)
}

// GetByteSlice 获取字节数组 (全局池)
func GetByteSlice() []byte {
	return GlobalPool.GetByteSlice()
}

// PutByteSlice 归还字节数组 (全局池)
func PutByteSlice(slice []byte) {
	GlobalPool.PutByteSlice(slice)
}

// GetMap 获取Map (全局池)
func GetMap() map[string]any {
	return GlobalPool.GetMap()
}

// PutMap 归还Map (全局池)
func PutMap(m map[string]any) {
	GlobalPool.PutMap(m)
}


// PoolStats 对象池统计信息
type PoolStats struct {
	BufferPoolStats      PoolItemStats `json:"buffer_pool"`
	StringBuilderStats   PoolItemStats `json:"string_builder_pool"`
	ByteSlicePoolStats   PoolItemStats `json:"byte_slice_pool"`
	MapPoolStats         PoolItemStats `json:"map_pool"`
	StringSlicePoolStats PoolItemStats `json:"string_slice_pool"`
}

// PoolItemStats 单个池的统计信息
type PoolItemStats struct {
	Hits   int64 `json:"hits"`   // 命中次数
	Misses int64 `json:"misses"` // 未命中次数
	Puts   int64 `json:"puts"`   // 归还次数
}

// 在实际生产环境中，这里可以添加统计功能
// 由于sync.Pool没有内置统计，这里提供简化的接口供未来扩展
func (op *ObjectPool) GetStats() PoolStats {
	// 简化版本，实际实现需要添加计数器
	return PoolStats{
		BufferPoolStats:      PoolItemStats{},
		StringBuilderStats:   PoolItemStats{},
		ByteSlicePoolStats:   PoolItemStats{},
		MapPoolStats:         PoolItemStats{},
		StringSlicePoolStats: PoolItemStats{},
	}
}
