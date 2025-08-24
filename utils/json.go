package utils

import (
	"github.com/bytedance/sonic"
)

// 高性能JSON配置
var (
	// FastestConfig 最快的JSON配置，用于性能关键路径
	FastestConfig = sonic.ConfigFastest

	// SafeConfig 安全的JSON配置，带有更多验证
	SafeConfig = sonic.ConfigStd
)

// FastMarshal 高性能JSON序列化
func FastMarshal(v any) ([]byte, error) {
	return FastestConfig.Marshal(v)
}

// FastUnmarshal 高性能JSON反序列化
func FastUnmarshal(data []byte, v any) error {
	return FastestConfig.Unmarshal(data, v)
}

// SafeMarshal 安全JSON序列化（带验证）
func SafeMarshal(v any) ([]byte, error) {
	return SafeConfig.Marshal(v)
}

// SafeUnmarshal 安全JSON反序列化（带验证）
func SafeUnmarshal(data []byte, v any) error {
	return SafeConfig.Unmarshal(data, v)
}

// Valid 验证JSON数据是否有效
func Valid(data []byte) bool {
	return FastestConfig.Valid(data)
}

// MarshalIndent 带缩进的JSON序列化
func MarshalIndent(v any, prefix, indent string) ([]byte, error) {
	// sonic的MarshalIndent
	return SafeConfig.MarshalIndent(v, prefix, indent)
}
