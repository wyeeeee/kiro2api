package utils

// IntMin 返回两个整数的最小值
// 遵循KISS原则，简单而高效
func IntMin(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// IntMax 返回两个整数的最大值
// 遵循KISS原则，简单而高效
func IntMax(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// Int64Min 返回两个int64的最小值
func Int64Min(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

// Int64Max 返回两个int64的最大值
func Int64Max(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

// StringSliceContains 检查字符串切片是否包含指定元素
// 优化版本，避免重复查找模式
func StringSliceContains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// StringSliceToMap 将字符串切片转换为map[string]bool
// 用于O(1)查找，替代O(n)查找
func StringSliceToMap(slice []string) map[string]bool {
	result := make(map[string]bool, len(slice))
	for _, s := range slice {
		result[s] = true
	}
	return result
}
