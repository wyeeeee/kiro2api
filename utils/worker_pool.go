package utils

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"kiro2api/logger"
)

// Job 工作接口定义
type Job interface {
	Execute(ctx context.Context) error
	GetID() string
	GetPriority() int // 优先级：数字越大优先级越高
}

// WorkerPool 工作池，管理goroutine数量和任务队列
type WorkerPool struct {
	// 配置参数
	maxWorkers    int           // 最大工作协程数
	maxQueueSize  int           // 最大队列长度
	workerTimeout time.Duration // 工作协程超时时间

	// 运行时状态
	jobs    chan Job           // 任务队列
	results chan error         // 结果队列
	ctx     context.Context    // 上下文
	cancel  context.CancelFunc // 取消函数
	wg      sync.WaitGroup     // 等待组
	once    sync.Once          // 确保只启动一次

	// 统计信息
	activeWorkers int64 // 活跃工作协程数量
	totalJobs     int64 // 总任务数
	completedJobs int64 // 完成任务数
	failedJobs    int64 // 失败任务数

	mu sync.RWMutex // 用于保护统计信息的读写
}

// WorkerPoolConfig 工作池配置
type WorkerPoolConfig struct {
	MaxWorkers    int           // 默认：CPU核心数*2
	MaxQueueSize  int           // 默认：1000
	WorkerTimeout time.Duration // 默认：30秒
}

// NewWorkerPool 创建新的工作池
func NewWorkerPool(config WorkerPoolConfig) *WorkerPool {
	// 设置默认值
	if config.MaxWorkers <= 0 {
		config.MaxWorkers = runtime.NumCPU() * 2
	}
	if config.MaxQueueSize <= 0 {
		config.MaxQueueSize = 1000
	}
	if config.WorkerTimeout <= 0 {
		config.WorkerTimeout = 30 * time.Second
	}

	ctx, cancel := context.WithCancel(context.Background())

	wp := &WorkerPool{
		maxWorkers:    config.MaxWorkers,
		maxQueueSize:  config.MaxQueueSize,
		workerTimeout: config.WorkerTimeout,
		jobs:          make(chan Job, config.MaxQueueSize),
		results:       make(chan error, config.MaxQueueSize),
		ctx:           ctx,
		cancel:        cancel,
	}

	logger.Info("工作池已创建",
		logger.Int("max_workers", config.MaxWorkers),
		logger.Int("max_queue_size", config.MaxQueueSize),
		logger.String("worker_timeout", config.WorkerTimeout.String()))

	return wp
}

// Start 启动工作池
func (wp *WorkerPool) Start() {
	wp.once.Do(func() {
		logger.Info("启动工作池", logger.Int("workers", wp.maxWorkers))

		// 启动工作协程
		for i := 0; i < wp.maxWorkers; i++ {
			wp.wg.Add(1)
			go wp.worker(i)
		}

		// 启动结果处理协程
		go wp.resultHandler()
	})
}

// Stop 停止工作池
func (wp *WorkerPool) Stop() {
	logger.Info("正在停止工作池...")

	// 关闭任务队列
	close(wp.jobs)

	// 等待所有工作协程完成
	wp.wg.Wait()

	// 取消上下文
	wp.cancel()

	// 关闭结果队列
	close(wp.results)

	logger.Info("工作池已停止")
}

// Submit 提交任务到工作池
func (wp *WorkerPool) Submit(job Job) error {
	select {
	case wp.jobs <- job:
		atomic.AddInt64(&wp.totalJobs, 1)
		logger.Debug("任务已提交", logger.String("job_id", job.GetID()))
		return nil
	case <-wp.ctx.Done():
		return fmt.Errorf("工作池已关闭")
	default:
		return fmt.Errorf("任务队列已满，当前队列大小：%d", wp.maxQueueSize)
	}
}

// TrySubmit 尝试提交任务，不阻塞
func (wp *WorkerPool) TrySubmit(job Job) bool {
	select {
	case wp.jobs <- job:
		atomic.AddInt64(&wp.totalJobs, 1)
		logger.Debug("任务已提交", logger.String("job_id", job.GetID()))
		return true
	default:
		return false
	}
}

// SubmitWithTimeout 带超时的任务提交
func (wp *WorkerPool) SubmitWithTimeout(job Job, timeout time.Duration) error {
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case wp.jobs <- job:
		atomic.AddInt64(&wp.totalJobs, 1)
		logger.Debug("任务已提交", logger.String("job_id", job.GetID()))
		return nil
	case <-timer.C:
		return fmt.Errorf("任务提交超时：%v", timeout)
	case <-wp.ctx.Done():
		return fmt.Errorf("工作池已关闭")
	}
}

// worker 工作协程
func (wp *WorkerPool) worker(id int) {
	defer wp.wg.Done()

	// logger.Debug("工作协程启动", logger.Int("worker_id", id))
	// defer logger.Debug("工作协程停止", logger.Int("worker_id", id))

	for job := range wp.jobs {
		atomic.AddInt64(&wp.activeWorkers, 1)

		// 创建带超时的上下文
		jobCtx, cancel := context.WithTimeout(wp.ctx, wp.workerTimeout)

		// 执行任务
		start := time.Now()
		err := job.Execute(jobCtx)
		duration := time.Since(start)

		cancel()
		atomic.AddInt64(&wp.activeWorkers, -1)

		// 记录结果
		if err != nil {
			atomic.AddInt64(&wp.failedJobs, 1)
			logger.Error("任务执行失败",
				logger.String("job_id", job.GetID()),
				logger.Int("worker_id", id),
				logger.Err(err),
				logger.String("duration", duration.String()))
		} else {
			atomic.AddInt64(&wp.completedJobs, 1)
			logger.Debug("任务执行成功",
				logger.String("job_id", job.GetID()),
				logger.Int("worker_id", id),
				logger.String("duration", duration.String()))
		}

		// 发送结果（非阻塞）
		select {
		case wp.results <- err:
		default:
			// 结果队列满了，忽略
		}
	}
}

// resultHandler 结果处理协程
func (wp *WorkerPool) resultHandler() {
	for {
		select {
		case err, ok := <-wp.results:
			if !ok {
				return // 结果队列已关闭
			}

			if err != nil {
				// 可以在这里添加错误处理逻辑
				// 比如发送到监控系统、重试等
			}

		case <-wp.ctx.Done():
			return
		}
	}
}

// GetStats 获取工作池统计信息
func (wp *WorkerPool) GetStats() map[string]any {
	wp.mu.RLock()
	defer wp.mu.RUnlock()

	return map[string]any{
		"max_workers":    wp.maxWorkers,
		"active_workers": atomic.LoadInt64(&wp.activeWorkers),
		"total_jobs":     atomic.LoadInt64(&wp.totalJobs),
		"completed_jobs": atomic.LoadInt64(&wp.completedJobs),
		"failed_jobs":    atomic.LoadInt64(&wp.failedJobs),
		"queue_size":     len(wp.jobs),
		"max_queue_size": wp.maxQueueSize,
		"worker_timeout": wp.workerTimeout.String(),
	}
}

// GetQueueLength 获取当前队列长度
func (wp *WorkerPool) GetQueueLength() int {
	return len(wp.jobs)
}

// GetActiveWorkers 获取活跃工作协程数量
func (wp *WorkerPool) GetActiveWorkers() int64 {
	return atomic.LoadInt64(&wp.activeWorkers)
}

// 全局工作池实例
var (
	GlobalWorkerPool *WorkerPool
	workerPoolOnce   sync.Once
)

// GetGlobalWorkerPool 获取全局工作池实例
func GetGlobalWorkerPool() *WorkerPool {
	workerPoolOnce.Do(func() {
		config := WorkerPoolConfig{
			MaxWorkers:    runtime.NumCPU() * 2, // CPU核心数*2
			MaxQueueSize:  2000,                 // 2000个任务队列
			WorkerTimeout: 5 * time.Minute,      // 5分钟超时
		}

		GlobalWorkerPool = NewWorkerPool(config)
		GlobalWorkerPool.Start()

		logger.Debug("全局工作池已初始化")
	})

	return GlobalWorkerPool
}

// InitGlobalWorkerPool 初始化全局工作池（可自定义配置）
func InitGlobalWorkerPool(config WorkerPoolConfig) *WorkerPool {
	workerPoolOnce.Do(func() {
		GlobalWorkerPool = NewWorkerPool(config)
		GlobalWorkerPool.Start()

		logger.Debug("全局工作池已初始化（自定义配置）")
	})

	return GlobalWorkerPool
}
