package utils

import (
	"sync"

	"github.com/bytedance/sonic"
)

// 高性能JSON配置
var (
	// FastestConfig 最快的JSON配置，用于性能关键路径
	FastestConfig = sonic.ConfigFastest

	// SafeConfig 安全的JSON配置，带有更多验证
	SafeConfig = sonic.ConfigStd

	// StreamConfig 流式JSON配置，优化内存使用
	StreamConfig = sonic.Config{
		UseInt64:      true,
		UseNumber:     false,
		EscapeHTML:    false,
		CompactMarshaler: true,
		NoValidateJSONMarshaler: true,
		NoEncoderNewline: true,
	}.Froze()
)

// JSONPool 用于复用JSON编解码器和缓冲区（当前未使用，预留扩展）
type JSONPool struct {
	bufferPool sync.Pool
}

// 全局JSON对象池（简化版）
var GlobalJSONPool = &JSONPool{
	bufferPool: sync.Pool{
		New: func() interface{} {
			return make([]byte, 0, 1024) // 预分配1KB缓冲区
		},
	},
}

// GetBuffer 从池中获取缓冲区
func (jp *JSONPool) GetBuffer() []byte {
	return jp.bufferPool.Get().([]byte)[:0] // 重置长度但保留容量
}

// PutBuffer 将缓冲区放回池中
func (jp *JSONPool) PutBuffer(buf []byte) {
	if cap(buf) <= 32*1024 { // 只回收小于32KB的缓冲区，避免内存泄漏
		jp.bufferPool.Put(buf)
	}
}

// FastMarshal 高性能JSON序列化
func FastMarshal(v interface{}) ([]byte, error) {
	return FastestConfig.Marshal(v)
}

// FastUnmarshal 高性能JSON反序列化
func FastUnmarshal(data []byte, v interface{}) error {
	return FastestConfig.Unmarshal(data, v)
}

// SafeMarshal 安全JSON序列化（带验证）
func SafeMarshal(v interface{}) ([]byte, error) {
	return SafeConfig.Marshal(v)
}

// SafeUnmarshal 安全JSON反序列化（带验证）
func SafeUnmarshal(data []byte, v interface{}) error {
	return SafeConfig.Unmarshal(data, v)
}

// StreamMarshal 流式JSON序列化，优化内存使用
func StreamMarshal(v interface{}) ([]byte, error) {
	return StreamConfig.Marshal(v)
}

// StreamUnmarshal 流式JSON反序列化，优化内存使用
func StreamUnmarshal(data []byte, v interface{}) error {
	return StreamConfig.Unmarshal(data, v)
}

// MarshalToBuffer 序列化到复用缓冲区
func MarshalToBuffer(v interface{}) ([]byte, error) {
	buf := GlobalJSONPool.GetBuffer()
	defer GlobalJSONPool.PutBuffer(buf)
	
	data, err := FastestConfig.Marshal(v)
	if err != nil {
		return nil, err
	}
	
	// 如果数据大小合适，复制到缓冲区
	if len(data) <= cap(buf) {
		buf = buf[:len(data)]
		copy(buf, data)
		// 创建副本返回，因为buf会被回收
		result := make([]byte, len(buf))
		copy(result, buf)
		return result, nil
	}
	
	return data, nil
}