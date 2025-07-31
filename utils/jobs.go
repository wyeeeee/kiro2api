package utils

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"kiro2api/logger"
	"kiro2api/types"
)

// HTTPRequestJob HTTP请求任务
type HTTPRequestJob struct {
	ID          string
	Priority    int
	Request     *http.Request
	AnthropicReq *types.AnthropicRequest
	Client      *http.Client
	OnComplete  func(*http.Response, error)
}

// Execute 执行HTTP请求
func (job *HTTPRequestJob) Execute(ctx context.Context) error {
	start := time.Now()
	
	// 选择合适的客户端
	var client *http.Client
	if job.Client != nil {
		client = job.Client
	} else if job.AnthropicReq != nil {
		client = GetClientForRequest(*job.AnthropicReq)
	} else {
		client = SharedHTTPClient
	}

	// 为请求添加上下文
	reqWithCtx := job.Request.WithContext(ctx)
	
	logger.Debug("开始执行HTTP请求",
		logger.String("job_id", job.ID),
		logger.String("method", reqWithCtx.Method),
		logger.String("url", reqWithCtx.URL.String()))

	// 执行请求
	resp, err := client.Do(reqWithCtx)
	duration := time.Since(start)

	// 记录请求结果
	if err != nil {
		logger.Error("HTTP请求执行失败",
			logger.String("job_id", job.ID),
			logger.Err(err),
			logger.String("duration", duration.String()))
	} else {
		logger.Debug("HTTP请求执行成功",
			logger.String("job_id", job.ID),
			logger.Int("status_code", resp.StatusCode),
			logger.String("duration", duration.String()))
	}

	// 调用完成回调
	if job.OnComplete != nil {
		job.OnComplete(resp, err)
	}

	return err
}

// GetID 获取任务ID
func (job *HTTPRequestJob) GetID() string {
	return job.ID
}

// GetPriority 获取任务优先级
func (job *HTTPRequestJob) GetPriority() int {
	return job.Priority
}

// StreamProcessJob 流处理任务
type StreamProcessJob struct {
	ID         string
	Priority   int
	Data       []byte
	// Parser     *StreamParser
	Parser     interface{} // 临时修复编译错误
	// OnEvent    func(SSEEvent)
	OnEvent    func(interface{}) // 临时修复编译错误
	// OnComplete func([]SSEEvent, error)
	OnComplete func([]interface{}, error) // 临时修复编译错误
}

// Execute 执行流处理
func (job *StreamProcessJob) Execute(ctx context.Context) error {
	if job.Parser == nil {
		err := fmt.Errorf("StreamParser不能为空")
		if job.OnComplete != nil {
			job.OnComplete(nil, err)
		}
		return err
	}

	start := time.Now()
	
	logger.Debug("开始执行流处理",
		logger.String("job_id", job.ID),
		logger.Int("data_size", len(job.Data)))

	// 执行流解析
	// events := job.Parser.ParseStream(job.Data)
	var events []interface{} // 临时修复编译错误
	duration := time.Since(start)

	logger.Debug("流处理完成",
		logger.String("job_id", job.ID),
		logger.Int("event_count", len(events)),
		logger.String("duration", duration.String()))

	// 逐个处理事件
	for _, event := range events {
		if job.OnEvent != nil {
			job.OnEvent(event)
		}
	}

	// 调用完成回调
	if job.OnComplete != nil {
		job.OnComplete(events, nil)
	}

	return nil
}

// GetID 获取任务ID
func (job *StreamProcessJob) GetID() string {
	return job.ID
}

// GetPriority 获取任务优先级
func (job *StreamProcessJob) GetPriority() int {
	return job.Priority
}

// TokenRefreshJob Token刷新任务
type TokenRefreshJob struct {
	ID           string
	Priority     int
	RefreshToken string
	OnComplete   func(types.TokenInfo, error)
}

// Execute 执行Token刷新
func (job *TokenRefreshJob) Execute(ctx context.Context) error {
	start := time.Now()
	
	logger.Debug("开始执行Token刷新",
		logger.String("job_id", job.ID))

	// 模拟Token刷新逻辑（这里应该调用实际的刷新函数）
	// 由于循环依赖问题，这里不直接调用auth包的函数
	// 实际使用时可以通过依赖注入或其他方式解决
	
	duration := time.Since(start)
	err := fmt.Errorf("Token刷新功能待实现")

	logger.Error("Token刷新任务执行失败",
		logger.String("job_id", job.ID),
		logger.Err(err),
		logger.String("duration", duration.String()))

	// 调用完成回调
	if job.OnComplete != nil {
		job.OnComplete(types.TokenInfo{}, err)
	}

	return err
}

// GetID 获取任务ID
func (job *TokenRefreshJob) GetID() string {
	return job.ID
}

// GetPriority 获取任务优先级
func (job *TokenRefreshJob) GetPriority() int {
	return job.Priority
}

// CreateHTTPRequestJob 创建HTTP请求任务的便捷函数
func CreateHTTPRequestJob(id string, req *http.Request, anthropicReq *types.AnthropicRequest, onComplete func(*http.Response, error)) *HTTPRequestJob {
	priority := 5 // 默认优先级
	
	// 根据请求复杂度调整优先级
	if anthropicReq != nil {
		if AnalyzeRequestComplexity(*anthropicReq) == ComplexRequest {
			priority = 3 // 复杂请求优先级较低
		} else {
			priority = 7 // 简单请求优先级较高
		}
	}

	return &HTTPRequestJob{
		ID:          id,
		Priority:    priority,
		Request:     req,
		AnthropicReq: anthropicReq,
		OnComplete:  onComplete,
	}
}

// CreateStreamProcessJob 创建流处理任务的便捷函数
func CreateStreamProcessJob(id string, data []byte, onEvent func(interface{}), onComplete func([]interface{}, error)) *StreamProcessJob {
	// 从对象池获取解析器
	// parser := GlobalStreamParserPool.Get()
	var parser interface{} // 临时修复编译错误
	
	return &StreamProcessJob{
		ID:         id,
		Priority:   8, // 流处理优先级很高
		Data:       data,
		Parser:     parser,
		OnEvent:    onEvent,
		OnComplete: func(events []interface{}, err error) {
			// 归还解析器到对象池
			// GlobalStreamParserPool.Put(parser)
			if onComplete != nil {
				onComplete(events, err)
			}
		},
	}
}