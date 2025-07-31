package utils

import (
	"context"
	"fmt"
	"net/http"
	"runtime"
	"sync"
	"testing"
	"time"

	"kiro2api/parser"
	"kiro2api/types"
)

// BenchmarkStreamParserPool 测试StreamParser对象池性能
func BenchmarkStreamParserPool(b *testing.B) {
	// 模拟EventStream数据
	testData := []byte{
		0x00, 0x00, 0x00, 0x30, // totalLen = 48
		0x00, 0x00, 0x00, 0x10, // headerLen = 16
		// 16字节header
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		// payload (event data)
		'e', 'v', 'e', 'n', 't', '{', '"', 'c', 'o', 'n', 't', 'e', 'n', 't', '"', ':', '"', 't', 'e', 's', 't', '"', '}',
		0x00, // padding
		// CRC32
		0x00, 0x00, 0x00, 0x00,
	}

	b.Run("WithPool", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			sp := parser.GlobalStreamParserPool.Get()
			_ = sp.ParseStream(testData)
			parser.GlobalStreamParserPool.Put(sp)
		}
	})

	b.Run("WithoutPool", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			sp := parser.NewStreamParser()
			_ = sp.ParseStream(testData)
		}
	})
}

// BenchmarkJSONProcessing 测试JSON处理性能
func BenchmarkJSONProcessing(b *testing.B) {
	testObj := map[string]interface{}{
		"model":      "claude-sonnet-4-20250514",
		"max_tokens": 1000,
		"messages": []map[string]interface{}{
			{
				"role":    "user",
				"content": "Hello, this is a test message for benchmarking JSON performance",
			},
		},
		"tools": []map[string]interface{}{
			{
				"name":        "test_tool",
				"description": "A test tool for benchmarking",
				"input_schema": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"param1": map[string]interface{}{
							"type":        "string",
							"description": "Test parameter",
						},
					},
				},
			},
		},
	}

	b.Run("FastJSON", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			data, _ := FastMarshal(testObj)
			var result map[string]interface{}
			_ = FastUnmarshal(data, &result)
		}
	})

	b.Run("SafeJSON", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			data, _ := SafeMarshal(testObj)
			var result map[string]interface{}
			_ = SafeUnmarshal(data, &result)
		}
	})
}

// BenchmarkAtomicTokenCache 测试原子Token缓存性能
func BenchmarkAtomicTokenCache(b *testing.B) {
	cache := NewAtomicTokenCache()
	
	// 准备测试token
	testToken := &types.TokenInfo{
		AccessToken: "test_token",
		ExpiresAt:   time.Now().Add(time.Hour),
	}

	b.Run("AtomicCacheRead", func(b *testing.B) {
		cache.SetHot(1, testToken)
		b.ResetTimer()
		
		for i := 0; i < b.N; i++ {
			_, _ = cache.GetHot()
		}
	})

	b.Run("AtomicCacheWrite", func(b *testing.B) {
		b.ResetTimer()
		
		for i := 0; i < b.N; i++ {
			cache.SetHot(i%10, testToken)
		}
	})

	b.Run("TraditionalCacheRead", func(b *testing.B) {
		traditionalCache := types.NewTokenCache()
		traditionalCache.Set(*testToken)
		b.ResetTimer()
		
		for i := 0; i < b.N; i++ {
			_, _ = traditionalCache.Get()
		}
	})
}

// BenchmarkWorkerPool 测试工作池性能
func BenchmarkWorkerPool(b *testing.B) {
	config := WorkerPoolConfig{
		MaxWorkers:    runtime.NumCPU(),
		MaxQueueSize:  1000,
		WorkerTimeout: 30 * time.Second,
	}
	
	wp := NewWorkerPool(config)
	wp.Start()
	defer wp.Stop()

	// 简单任务
	createJob := func(id int) Job {
		return &HTTPRequestJob{
			ID:       fmt.Sprintf("job_%d", id),
			Priority: 5,
			OnComplete: func(*http.Response, error) {
				// 空回调
			},
		}
	}

	b.Run("WorkerPoolSubmit", func(b *testing.B) {
		b.ResetTimer()
		
		for i := 0; i < b.N; i++ {
			job := createJob(i)
			_ = wp.Submit(job)
		}
		
		// 等待所有任务完成
		for wp.GetQueueLength() > 0 {
			time.Sleep(time.Millisecond)
		}
	})

	b.Run("DirectExecution", func(b *testing.B) {
		b.ResetTimer()
		
		for i := 0; i < b.N; i++ {
			job := createJob(i)
			_ = job.Execute(context.Background())
		}
	})
}

// BenchmarkConcurrentPerformance 测试并发性能
func BenchmarkConcurrentPerformance(b *testing.B) {
	const numGoroutines = 100

	b.Run("AtomicCacheConcurrent", func(b *testing.B) {
		cache := NewAtomicTokenCache()
		testToken := &types.TokenInfo{
			AccessToken: "test_token",
			ExpiresAt:   time.Now().Add(time.Hour),
		}

		b.ResetTimer()
		
		var wg sync.WaitGroup
		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				for j := 0; j < b.N/numGoroutines; j++ {
					cache.SetHot(id, testToken)
					_, _ = cache.GetHot()
				}
			}(i)
		}
		wg.Wait()
	})

	b.Run("StreamParserPoolConcurrent", func(b *testing.B) {
		testData := make([]byte, 1024)
		for i := range testData {
			testData[i] = byte(i % 256)
		}

		b.ResetTimer()
		
		var wg sync.WaitGroup
		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := 0; j < b.N/numGoroutines; j++ {
					sp := parser.GlobalStreamParserPool.Get()
					_ = sp.ParseStream(testData)
					parser.GlobalStreamParserPool.Put(sp)
				}
			}()
		}
		wg.Wait()
	})
}

// MemoryUsageBenchmark 内存使用基准测试
func MemoryUsageBenchmark(b *testing.B) {
	b.Run("StreamParserMemory", func(b *testing.B) {
		var m1, m2 runtime.MemStats
		
		// 测试对象池版本
		runtime.GC()
		runtime.ReadMemStats(&m1)
		
		for i := 0; i < b.N; i++ {
			sp := parser.GlobalStreamParserPool.Get()
			parser.GlobalStreamParserPool.Put(sp)
		}
		
		runtime.GC()
		runtime.ReadMemStats(&m2)
		
		b.Logf("对象池版本 - 分配: %d bytes, 回收: %d bytes", 
			m2.TotalAlloc-m1.TotalAlloc, m2.Frees-m1.Frees)
	})

	b.Run("RegularStreamParserMemory", func(b *testing.B) {
		var m1, m2 runtime.MemStats
		
		// 测试常规版本
		runtime.GC()
		runtime.ReadMemStats(&m1)
		
		for i := 0; i < b.N; i++ {
			_ = parser.NewStreamParser()
		}
		
		runtime.GC()  
		runtime.ReadMemStats(&m2)
		
		b.Logf("常规版本 - 分配: %d bytes, 回收: %d bytes", 
			m2.TotalAlloc-m1.TotalAlloc, m2.Frees-m1.Frees)
	})
}