package utils

import (
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"kiro2api/logger"
	"kiro2api/types"
)

// AtomicTokenCache 使用原子操作的高性能Token缓存
// 减少锁竞争，提升高并发场景下的性能
type AtomicTokenCache struct {
	// hot是最常用的token，使用原子指针避免锁
	hot    unsafe.Pointer // *types.TokenInfo
	hotIdx int64          // 热点token的索引

	// 冷缓存使用sync.Map，适合读多写少的场景
	cache sync.Map // int -> *types.TokenInfo

	// 统计信息
	hits   int64 // 缓存命中次数
	misses int64 // 缓存未命中次数
}

// NewAtomicTokenCache 创建新的原子操作Token缓存
func NewAtomicTokenCache() *AtomicTokenCache {
	return &AtomicTokenCache{
		cache: sync.Map{},
	}
}

// GetHot 获取热点token（最快路径，无锁）
func (atc *AtomicTokenCache) GetHot() (*types.TokenInfo, bool) {
	tokenPtr := atomic.LoadPointer(&atc.hot)
	if tokenPtr == nil {
		atomic.AddInt64(&atc.misses, 1)
		return nil, false
	}

	token := (*types.TokenInfo)(tokenPtr)
	if token.IsExpired() {
		// 原子地清除过期的热点token
		atomic.CompareAndSwapPointer(&atc.hot, tokenPtr, nil)
		atomic.AddInt64(&atc.misses, 1)
		return nil, false
	}

	atomic.AddInt64(&atc.hits, 1)
	return token, true
}

// SetHot 设置热点token（原子操作）
func (atc *AtomicTokenCache) SetHot(idx int, token *types.TokenInfo) {
	atomic.StorePointer(&atc.hot, unsafe.Pointer(token))
	atomic.StoreInt64(&atc.hotIdx, int64(idx))

	// 同时更新冷缓存
	atc.cache.Store(idx, token)
}

// Get 获取指定索引的token（先检查热点，再检查冷缓存）
func (atc *AtomicTokenCache) Get(idx int) (*types.TokenInfo, bool) {
	// 首先检查是否是热点token
	if atomic.LoadInt64(&atc.hotIdx) == int64(idx) {
		if token, ok := atc.GetHot(); ok {
			return token, true
		}
	}

	// 检查冷缓存
	if value, ok := atc.cache.Load(idx); ok {
		token := value.(*types.TokenInfo)
		if !token.IsExpired() {
			atomic.AddInt64(&atc.hits, 1)
			return token, true
		}
		// 清除过期token
		atc.cache.Delete(idx)
	}

	atomic.AddInt64(&atc.misses, 1)
	return nil, false
}

// Set 设置指定索引的token
func (atc *AtomicTokenCache) Set(idx int, token *types.TokenInfo) {
	atc.cache.Store(idx, token)

	// 如果这是第一个token，或者比当前热点token更新，则设为热点
	currentHotIdx := atomic.LoadInt64(&atc.hotIdx)
	if currentHotIdx == 0 || int64(idx) == currentHotIdx {
		atc.SetHot(idx, token)
	}
}

// Delete 删除指定索引的token
func (atc *AtomicTokenCache) Delete(idx int) {
	atc.cache.Delete(idx)

	// 如果删除的是热点token，清除热点缓存
	if atomic.LoadInt64(&atc.hotIdx) == int64(idx) {
		atomic.StorePointer(&atc.hot, nil)
		atomic.StoreInt64(&atc.hotIdx, 0)
	}
}

// Clear 清除所有缓存
func (atc *AtomicTokenCache) Clear() {
	atc.cache.Range(func(key, value any) bool {
		atc.cache.Delete(key)
		return true
	})

	atomic.StorePointer(&atc.hot, nil)
	atomic.StoreInt64(&atc.hotIdx, 0)
}

// CleanupExpired 清理过期的token（后台定期调用）
func (atc *AtomicTokenCache) CleanupExpired() int {
	cleaned := 0

	atc.cache.Range(func(key, value any) bool {
		token := value.(*types.TokenInfo)
		if token.IsExpired() {
			idx := key.(int)
			atc.cache.Delete(idx)

			// 如果是热点token，也清除
			if atomic.LoadInt64(&atc.hotIdx) == int64(idx) {
				atomic.StorePointer(&atc.hot, nil)
				atomic.StoreInt64(&atc.hotIdx, 0)
			}

			cleaned++
		}
		return true
	})

	return cleaned
}

// GetStats 获取缓存统计信息
func (atc *AtomicTokenCache) GetStats() map[string]any {
	hits := atomic.LoadInt64(&atc.hits)
	misses := atomic.LoadInt64(&atc.misses)
	total := hits + misses

	hitRate := float64(0)
	if total > 0 {
		hitRate = float64(hits) / float64(total)
	}

	size := 0
	atc.cache.Range(func(key, value any) bool {
		size++
		return true
	})

	return map[string]any{
		"hits":     hits,
		"misses":   misses,
		"hit_rate": hitRate,
		"size":     size,
		"hot_idx":  atomic.LoadInt64(&atc.hotIdx),
		"has_hot":  atomic.LoadPointer(&atc.hot) != nil,
	}
}

// StartCleanupRoutine 启动后台清理协程
func (atc *AtomicTokenCache) StartCleanupRoutine() {
	go func() {
		ticker := time.NewTicker(5 * time.Minute) // 每5分钟清理一次
		defer ticker.Stop()

		for range ticker.C {
			cleaned := atc.CleanupExpired()
			if cleaned > 0 {
				logger.Debug("缓存清理完成", logger.Int("cleaned_count", cleaned))
			}
		}
	}()
}
