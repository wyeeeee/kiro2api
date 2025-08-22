# 测试系统架构说明

## 概述

本文档描述了 `kiro2api` 项目的测试系统架构，特别是 `raw_data_replay` 测试机制的实现和使用方法。

## 架构重构

### 2025年8月重构：移除 StreamRequestSimulator

在最新的重构中，我们移除了 `StreamRequestSimulator` 组件，统一使用 `RealIntegrationTester` 来处理所有测试场景。这个决策基于以下原因：

- **消除代码重复**：两个组件实现了类似的功能，增加了维护成本
- **提高测试真实性**：使用真实的 HTTP 服务器进行端到端测试更能反映生产环境
- **简化架构**：统一的测试接口降低了系统复杂度

## 核心组件

### 1. RealIntegrationTester

**文件位置**: `test/real_integration.go`

**职责**:
- 启动真实的 HTTP 测试服务器
- 模拟 Anthropic API 端点
- 处理流式和非流式请求
- 集成 Mock AWS 服务器处理原始数据

**主要方法**:
- `TestWithRawData(rawDataFile string)`: 使用原始数据文件进行完整测试
- `TestWithRawDataDirect(binaryData []byte)`: 直接使用二进制数据进行测试（替代原有的 `SimulateStreamProcessing`）

### 2. ValidationFramework

**文件位置**: `test/validation_framework.go`

**职责**:
- 比较预期输出与实际结果
- 支持容错配置
- 生成详细的验证报告

**新增方法**:
- `ValidateResultsDirect(*ExpectedOutput, *RealIntegrationResult)`: 直接验证 RealIntegrationResult

### 3. 类型定义

为了保持向后兼容性，我们在 `validation_framework.go` 中保留了以下类型：

```go
// EventCapture 事件捕获
type EventCapture struct {
    Timestamp   time.Time              `json:"timestamp"`
    EventType   string                 `json:"event_type"`
    Data        map[string]interface{} `json:"data"`
    Source      string                 `json:"source"`
    Index       int                    `json:"index"`
}

// SimulationResult 模拟结果（保留用于兼容性）
type SimulationResult struct {
    CapturedSSEEvents []interface{}     `json:"captured_sse_events"`
    OutputText        string            `json:"output_text"`
    ProcessingTime    time.Duration     `json:"processing_time"`
    ErrorsEncountered []error           `json:"errors_encountered"`
    Stats             SimulationStats   `json:"stats"`
    RawOutput         string            `json:"raw_output"`
    EventSequence     []EventCapture    `json:"event_sequence"`
}

// SimulationStats 模拟统计（保留用于兼容性）
type SimulationStats struct {
    TotalBytesProcessed int               `json:"total_bytes_processed"`
    TotalEventsEmitted  int               `json:"total_events_emitted"`
    EventsByType        map[string]int    `json:"events_by_type"`
    ProcessingLatency   time.Duration     `json:"processing_latency"`
    ToolCallsDetected   int               `json:"tool_calls_detected"`
    ContentBlocksCount  int               `json:"content_blocks_count"`
    DeduplicationSkips  int               `json:"deduplication_skips"`
}
```

## 测试流程

### 1. Raw Data Replay 测试

```go
// 创建测试器
tester := NewRealIntegrationTester(nil) // 使用默认配置
defer tester.Close()

// 使用原始数据文件进行测试
result, err := tester.TestWithRawData("testdata/sample_stream.hex")
if err != nil {
    t.Fatalf("测试失败: %v", err)
}

// 验证结果
if !result.Success {
    t.Errorf("测试未通过: %s", result.ErrorMessage)
}
```

### 2. 直接二进制数据测试

```go
// 加载二进制数据
binaryData := loadTestData("testdata/sample.bin")

// 直接测试
result, err := tester.TestWithRawDataDirect(binaryData)
if err != nil {
    t.Fatalf("直接测试失败: %v", err)
}

// 检查事件
if len(result.StreamEvents) == 0 {
    t.Error("未捕获到流事件")
}
```

### 3. 配置测试环境

```go
config := &RealTestConfig{
    ServerTimeout: time.Minute * 2,
    EnableMockAWS: true,
    EnableAuth:    false,
    DebugMode:     true,
}

tester := NewRealIntegrationTester(config)
defer tester.Close()
```

## 测试数据格式

### HEX 文件格式

测试数据以十六进制格式存储，包含：
- AWS Event Stream 格式的二进制数据
- 每行为十六进制编码的字节序列
- 支持注释行（以 `#` 开头）

示例：
```
# AWS Event Stream 数据示例
000000210000000555766656e743a203a6d6573736167652d747970650700...
```

### 期望输出格式

期望输出包含：
- 预期的 SSE 事件序列
- 最终文本输出
- 工具调用信息
- 性能指标

## 性能测试

项目包含基准测试以监控性能：

```bash
# 运行性能测试
go test ./test -bench=BenchmarkRealIntegration -benchmem

# 运行所有测试
go test ./test -v
```

## 错误处理

测试系统包含全面的错误处理：

1. **数据加载错误**: HEX 文件格式错误或文件不存在
2. **网络错误**: HTTP 请求失败或超时
3. **解析错误**: 响应数据格式不正确
4. **验证错误**: 实际结果与期望不符

## 调试技巧

### 启用调试模式

```go
config := &RealTestConfig{
    DebugMode: true,
}
```

### 查看详细日志

```go
// 获取详细的验证结果
if result.ValidationResult != nil {
    for _, diff := range result.ValidationResult.Differences {
        fmt.Printf("差异: %s\n", diff.Description)
    }
}
```

### 检查事件序列

```go
// 打印捕获的事件
for i, event := range result.StreamEvents {
    fmt.Printf("事件 %d: %+v\n", i, event)
}
```

## 最佳实践

1. **总是在测试后关闭测试器**: 使用 `defer tester.Close()`
2. **使用适当的超时设置**: 根据测试数据大小调整 `ServerTimeout`
3. **启用 Mock AWS**: 对于离线测试，始终启用 `EnableMockAWS`
4. **验证测试结果**: 检查 `result.Success` 和 `result.ValidationResult`
5. **处理错误**: 正确处理所有可能的错误情况

## 迁移指南

如果你之前使用 `StreamRequestSimulator`，请按以下步骤迁移：

### 替换测试器创建

**之前**:
```go
simulator := NewStreamRequestSimulator(config)
```

**现在**:
```go
tester := NewRealIntegrationTester(config)
defer tester.Close()
```

### 替换测试方法调用

**之前**:
```go
result, err := simulator.SimulateStreamProcessing(binaryData)
```

**现在**:
```go
result, err := tester.TestWithRawDataDirect(binaryData)
```

### 更新验证调用

**之前**:
```go
validationResult := validator.ValidateResults(expected, &result)
```

**现在**:
```go
validationResult := validator.ValidateResultsDirect(expected, result)
```

## 故障排除

### 常见问题

1. **编译错误**: 确保已移除所有对 `StreamRequestSimulator` 的引用
2. **测试超时**: 增加 `ServerTimeout` 配置值
3. **事件解析失败**: 检查 HEX 文件格式是否正确
4. **Mock AWS 未响应**: 确保 `EnableMockAWS` 设置为 `true`

### 日志分析

测试器会输出详细的调试信息，包括：
- 服务器启动和关闭事件
- HTTP 请求和响应详情
- 事件解析过程
- 验证结果详情

查看这些日志可以帮助诊断问题。

## 总结

新的测试架构通过统一使用 `RealIntegrationTester` 简化了系统设计，提高了测试的真实性和可维护性。所有现有功能都得到了保留，同时提供了更好的性能和更清晰的接口。