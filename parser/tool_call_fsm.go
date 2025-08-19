package parser

import (
	"bytes"
	"fmt"
	"kiro2api/logger"
	"sync"
	"time"
)

// ToolCallState 工具调用状态
type ToolCallState int

const (
	StateInit ToolCallState = iota
	StateStarted
	StateCollecting
	StateValidating
	StateCompleted
	StateError
	StateCancelled
)

func (s ToolCallState) String() string {
	states := []string{
		"Init",
		"Started",
		"Collecting",
		"Validating",
		"Completed",
		"Error",
		"Cancelled",
	}
	if int(s) < len(states) {
		return states[s]
	}
	return "Unknown"
}

// ToolState 工具状态信息
type ToolState struct {
	ID         string
	Name       string
	State      ToolCallState
	Buffer     *bytes.Buffer
	Arguments  map[string]interface{}
	StartTime  time.Time
	LastUpdate time.Time
	BlockIndex int
	Error      error
	Metadata   map[string]interface{}
}

// StateTransition 状态转换
type StateTransition struct {
	From ToolCallState
	To   ToolCallState
}

// TransitionFunc 状态转换函数
type TransitionFunc func(state *ToolState) error

// ToolEvent 工具事件
type ToolEvent struct {
	Type      string
	Data      interface{}
	Timestamp time.Time
}

// ToolCallFSM 工具调用有限状态机
type ToolCallFSM struct {
	mu          sync.RWMutex
	states      map[string]*ToolState
	transitions map[StateTransition]TransitionFunc
	listeners   []StateChangeListener
	metrics     *FSMMetrics
}

// StateChangeListener 状态变化监听器
type StateChangeListener func(toolID string, oldState, newState ToolCallState, state *ToolState)

// FSMMetrics 状态机指标
type FSMMetrics struct {
	TotalTransitions uint64
	StateCount       map[ToolCallState]int
	ErrorCount       uint64
	AvgDuration      time.Duration
}

// NewToolCallFSM 创建新的工具调用状态机
func NewToolCallFSM() *ToolCallFSM {
	fsm := &ToolCallFSM{
		states:      make(map[string]*ToolState),
		transitions: make(map[StateTransition]TransitionFunc),
		metrics: &FSMMetrics{
			StateCount: make(map[ToolCallState]int),
		},
	}

	// 注册状态转换规则
	fsm.registerTransitions()

	return fsm
}

// registerTransitions 注册所有合法的状态转换
func (fsm *ToolCallFSM) registerTransitions() {
	// Init -> Started
	fsm.RegisterTransition(StateInit, StateStarted, fsm.onStart)

	// Started -> Collecting
	fsm.RegisterTransition(StateStarted, StateCollecting, fsm.onCollecting)

	// Collecting -> Collecting (self-loop for incremental data)
	fsm.RegisterTransition(StateCollecting, StateCollecting, fsm.onCollecting)

	// Collecting -> Validating
	fsm.RegisterTransition(StateCollecting, StateValidating, fsm.onValidating)

	// Validating -> Completed
	fsm.RegisterTransition(StateValidating, StateCompleted, fsm.onCompleted)

	// Validating -> Error
	fsm.RegisterTransition(StateValidating, StateError, fsm.onError)

	// Any state -> Cancelled
	for _, state := range []ToolCallState{StateInit, StateStarted, StateCollecting, StateValidating} {
		fsm.RegisterTransition(state, StateCancelled, fsm.onCancelled)
	}

	// Any state -> Error (for unexpected errors)
	for _, state := range []ToolCallState{StateInit, StateStarted, StateCollecting} {
		fsm.RegisterTransition(state, StateError, fsm.onError)
	}
}

// RegisterTransition 注册状态转换
func (fsm *ToolCallFSM) RegisterTransition(from, to ToolCallState, fn TransitionFunc) {
	transition := StateTransition{From: from, To: to}
	fsm.transitions[transition] = fn
}

// ProcessEvent 处理工具事件
func (fsm *ToolCallFSM) ProcessEvent(toolID string, event ToolEvent) error {
	fsm.mu.Lock()
	defer fsm.mu.Unlock()

	// 获取或创建状态
	state, exists := fsm.states[toolID]
	if !exists {
		state = &ToolState{
			ID:         toolID,
			State:      StateInit,
			Buffer:     &bytes.Buffer{},
			Arguments:  make(map[string]interface{}),
			StartTime:  time.Now(),
			LastUpdate: time.Now(),
			Metadata:   make(map[string]interface{}),
		}
		fsm.states[toolID] = state
		fsm.metrics.StateCount[StateInit]++
	}

	// 确定目标状态
	targetState := fsm.determineTargetState(state.State, event)
	if targetState == state.State && targetState != StateCollecting {
		// 没有状态变化（除了Collecting自循环）
		return nil
	}

	// 执行状态转换
	return fsm.transitionTo(state, targetState)
}

// transitionTo 执行状态转换
func (fsm *ToolCallFSM) transitionTo(state *ToolState, targetState ToolCallState) error {
	oldState := state.State
	transition := StateTransition{From: oldState, To: targetState}

	// 查找转换函数
	fn, exists := fsm.transitions[transition]
	if !exists {
		return fmt.Errorf("非法状态转换: %s -> %s", oldState, targetState)
	}

	// 执行转换
	if err := fn(state); err != nil {
		// 转换失败，尝试转到错误状态
		state.State = StateError
		state.Error = err
		fsm.metrics.ErrorCount++
		return err
	}

	// 更新状态
	state.State = targetState
	state.LastUpdate = time.Now()

	// 更新指标
	fsm.metrics.StateCount[oldState]--
	fsm.metrics.StateCount[targetState]++
	fsm.metrics.TotalTransitions++

	// 通知监听器
	fsm.notifyListeners(state.ID, oldState, targetState, state)

	logger.Debug("状态转换成功",
		logger.String("tool_id", state.ID),
		logger.String("from", oldState.String()),
		logger.String("to", targetState.String()))

	return nil
}

// determineTargetState 根据事件确定目标状态
func (fsm *ToolCallFSM) determineTargetState(currentState ToolCallState, event ToolEvent) ToolCallState {
	switch event.Type {
	case "start":
		if currentState == StateInit {
			return StateStarted
		}
	case "input":
		if currentState == StateStarted || currentState == StateCollecting {
			return StateCollecting
		}
	case "complete":
		if currentState == StateCollecting {
			return StateValidating
		}
	case "validate_success":
		if currentState == StateValidating {
			return StateCompleted
		}
	case "validate_fail":
		if currentState == StateValidating {
			return StateError
		}
	case "cancel":
		if currentState != StateCompleted && currentState != StateError {
			return StateCancelled
		}
	case "error":
		return StateError
	}

	return currentState
}

// State transition handlers

func (fsm *ToolCallFSM) onStart(state *ToolState) error {
	logger.Debug("工具调用开始",
		logger.String("tool_id", state.ID),
		logger.String("tool_name", state.Name))

	state.StartTime = time.Now()

	// 确保Buffer已初始化
	if state.Buffer == nil {
		state.Buffer = &bytes.Buffer{}
	} else {
		state.Buffer.Reset()
	}

	if state.Arguments == nil {
		state.Arguments = make(map[string]interface{})
	}

	return nil
}

func (fsm *ToolCallFSM) onCollecting(state *ToolState) error {
	logger.Debug("收集工具输入",
		logger.String("tool_id", state.ID),
		logger.Int("buffer_size", state.Buffer.Len()))

	// 检查缓冲区大小限制
	if state.Buffer.Len() > 10*1024*1024 { // 10MB limit
		return fmt.Errorf("工具输入过大: %d bytes", state.Buffer.Len())
	}

	return nil
}

func (fsm *ToolCallFSM) onValidating(state *ToolState) error {
	logger.Debug("验证工具参数",
		logger.String("tool_id", state.ID),
		logger.String("tool_name", state.Name))

	// 这里可以添加具体的验证逻辑
	// 例如：验证必需参数、类型检查等

	return nil
}

func (fsm *ToolCallFSM) onCompleted(state *ToolState) error {
	duration := time.Since(state.StartTime)

	logger.Info("工具调用完成",
		logger.String("tool_id", state.ID),
		logger.String("tool_name", state.Name),
		logger.Duration("duration", duration))

	// 更新平均执行时间
	if fsm.metrics.AvgDuration == 0 {
		fsm.metrics.AvgDuration = duration
	} else {
		fsm.metrics.AvgDuration = (fsm.metrics.AvgDuration + duration) / 2
	}

	return nil
}

func (fsm *ToolCallFSM) onError(state *ToolState) error {
	logger.Error("工具调用失败",
		logger.String("tool_id", state.ID),
		logger.String("tool_name", state.Name),
		logger.Err(state.Error))

	fsm.metrics.ErrorCount++

	return nil
}

func (fsm *ToolCallFSM) onCancelled(state *ToolState) error {
	logger.Warn("工具调用取消",
		logger.String("tool_id", state.ID),
		logger.String("tool_name", state.Name))

	return nil
}

// Public API methods

// GetState 获取工具状态
func (fsm *ToolCallFSM) GetState(toolID string) (*ToolState, bool) {
	fsm.mu.RLock()
	defer fsm.mu.RUnlock()

	state, exists := fsm.states[toolID]
	return state, exists
}

// GetAllStates 获取所有工具状态
func (fsm *ToolCallFSM) GetAllStates() map[string]*ToolState {
	fsm.mu.RLock()
	defer fsm.mu.RUnlock()

	result := make(map[string]*ToolState)
	for id, state := range fsm.states {
		result[id] = state
	}
	return result
}

// AddListener 添加状态变化监听器
func (fsm *ToolCallFSM) AddListener(listener StateChangeListener) {
	fsm.mu.Lock()
	defer fsm.mu.Unlock()

	fsm.listeners = append(fsm.listeners, listener)
}

// notifyListeners 通知所有监听器
func (fsm *ToolCallFSM) notifyListeners(toolID string, oldState, newState ToolCallState, state *ToolState) {
	for _, listener := range fsm.listeners {
		go listener(toolID, oldState, newState, state)
	}
}

// Reset 重置状态机
func (fsm *ToolCallFSM) Reset() {
	fsm.mu.Lock()
	defer fsm.mu.Unlock()

	fsm.states = make(map[string]*ToolState)
	fsm.metrics = &FSMMetrics{
		StateCount: make(map[ToolCallState]int),
	}
}

// CleanupStaleStates 清理过期状态
func (fsm *ToolCallFSM) CleanupStaleStates(timeout time.Duration) int {
	fsm.mu.Lock()
	defer fsm.mu.Unlock()

	now := time.Now()
	cleaned := 0

	for id, state := range fsm.states {
		// 只清理已完成或出错的状态
		if (state.State == StateCompleted || state.State == StateError || state.State == StateCancelled) &&
			now.Sub(state.LastUpdate) > timeout {
			delete(fsm.states, id)
			fsm.metrics.StateCount[state.State]--
			cleaned++

			logger.Debug("清理过期工具状态",
				logger.String("tool_id", id),
				logger.String("state", state.State.String()),
				logger.Duration("age", now.Sub(state.LastUpdate)))
		}
	}

	return cleaned
}

// GetMetrics 获取状态机指标
func (fsm *ToolCallFSM) GetMetrics() *FSMMetrics {
	fsm.mu.RLock()
	defer fsm.mu.RUnlock()

	// 创建副本
	metrics := &FSMMetrics{
		TotalTransitions: fsm.metrics.TotalTransitions,
		ErrorCount:       fsm.metrics.ErrorCount,
		AvgDuration:      fsm.metrics.AvgDuration,
		StateCount:       make(map[ToolCallState]int),
	}

	for state, count := range fsm.metrics.StateCount {
		metrics.StateCount[state] = count
	}

	return metrics
}

// StartTool 开始工具调用
func (fsm *ToolCallFSM) StartTool(toolID, toolName string) error {
	fsm.mu.Lock()
	state, exists := fsm.states[toolID]
	if !exists {
		state = &ToolState{
			ID:         toolID,
			Name:       toolName,
			State:      StateInit,
			Buffer:     &bytes.Buffer{},
			Arguments:  make(map[string]interface{}),
			StartTime:  time.Now(),
			LastUpdate: time.Now(),
			Metadata:   make(map[string]interface{}),
		}
		fsm.states[toolID] = state
	} else {
		state.Name = toolName
	}
	fsm.mu.Unlock()

	return fsm.ProcessEvent(toolID, ToolEvent{
		Type:      "start",
		Timestamp: time.Now(),
	})
}

// AddInput 添加输入数据
func (fsm *ToolCallFSM) AddInput(toolID string, input string) error {
	fsm.mu.RLock()
	state, exists := fsm.states[toolID]
	fsm.mu.RUnlock()

	if !exists {
		return fmt.Errorf("工具状态不存在: %s", toolID)
	}

	// 确保Buffer已初始化
	if state.Buffer == nil {
		state.Buffer = &bytes.Buffer{}
	}

	// 添加数据到缓冲区
	state.Buffer.WriteString(input)

	return fsm.ProcessEvent(toolID, ToolEvent{
		Type:      "input",
		Data:      input,
		Timestamp: time.Now(),
	})
}

// CompleteTool 完成工具调用
func (fsm *ToolCallFSM) CompleteTool(toolID string) error {
	return fsm.ProcessEvent(toolID, ToolEvent{
		Type:      "complete",
		Timestamp: time.Now(),
	})
}

// CancelTool 取消工具调用
func (fsm *ToolCallFSM) CancelTool(toolID string) error {
	return fsm.ProcessEvent(toolID, ToolEvent{
		Type:      "cancel",
		Timestamp: time.Now(),
	})
}

// ErrorTool 标记工具调用错误
func (fsm *ToolCallFSM) ErrorTool(toolID string, err error) error {
	fsm.mu.RLock()
	state, exists := fsm.states[toolID]
	fsm.mu.RUnlock()

	if exists {
		state.Error = err
	}

	return fsm.ProcessEvent(toolID, ToolEvent{
		Type:      "error",
		Data:      err,
		Timestamp: time.Now(),
	})
}
