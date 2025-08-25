package state

import (
	"sync"
	"time"
)

// TokenState Token状态接口 (避免循环导入)
type TokenState interface {
	IsAvailable() bool
	CanRefresh() bool
	OnSuccess()
	OnFailure()
	StateName() string
}

// TokenStateManager Token状态管理接口
type TokenStateManager interface {
	GetState(tokenID string) TokenState
	UpdateState(tokenID string, state TokenState)
	GetAvailableTokens() []string
	MarkTokenDisabled(tokenID string)
	MarkTokenActive(tokenID string)
}

// ActiveState 活跃状态 - token可用且正常
type ActiveState struct {
	lastSuccess time.Time
}

func NewActiveState() TokenState {
	return &ActiveState{
		lastSuccess: time.Now(),
	}
}

func (s *ActiveState) IsAvailable() bool {
	return true
}

func (s *ActiveState) CanRefresh() bool {
	return true
}

func (s *ActiveState) OnSuccess() {
	s.lastSuccess = time.Now()
}

func (s *ActiveState) OnFailure() {
	// 从活跃状态失败后转为失败状态，由状态管理器处理转换
}

func (s *ActiveState) StateName() string {
	return "Active"
}

// FailedState 失败状态 - token遇到错误，但可以重试
type FailedState struct {
	failures    int
	lastFailure time.Time
	maxRetries  int
}

func NewFailedState(maxRetries int) TokenState {
	return &FailedState{
		failures:    1,
		lastFailure: time.Now(),
		maxRetries:  maxRetries,
	}
}

func (s *FailedState) IsAvailable() bool {
	// 失败次数未达到上限时仍可用，但优先级较低
	return s.failures < s.maxRetries
}

func (s *FailedState) CanRefresh() bool {
	// 有冷却时间，避免频繁重试
	cooldown := time.Duration(s.failures*30) * time.Second // 递增冷却时间
	return time.Since(s.lastFailure) > cooldown
}

func (s *FailedState) OnSuccess() {
	// 成功后重置失败计数，由状态管理器转为活跃状态
	s.failures = 0
}

func (s *FailedState) OnFailure() {
	s.failures++
	s.lastFailure = time.Now()
}

func (s *FailedState) StateName() string {
	return "Failed"
}

func (s *FailedState) GetFailureCount() int {
	return s.failures
}

// DisabledState 停用状态 - token被明确停用，不参与轮询
type DisabledState struct {
	disabledAt time.Time
	reason     string
}

func NewDisabledState(reason string) TokenState {
	return &DisabledState{
		disabledAt: time.Now(),
		reason:     reason,
	}
}

func (s *DisabledState) IsAvailable() bool {
	return false
}

func (s *DisabledState) CanRefresh() bool {
	return false
}

func (s *DisabledState) OnSuccess() {
	// 停用状态不响应成功事件
}

func (s *DisabledState) OnFailure() {
	// 停用状态不响应失败事件
}

func (s *DisabledState) StateName() string {
	return "Disabled"
}

func (s *DisabledState) GetReason() string {
	return s.reason
}

// DefaultTokenStateManager 默认Token状态管理器实现
type DefaultTokenStateManager struct {
	states     map[string]TokenState
	mutex      sync.RWMutex
	maxRetries int
}

// NewDefaultTokenStateManager 创建默认状态管理器
func NewDefaultTokenStateManager(maxRetries int) TokenStateManager {
	return &DefaultTokenStateManager{
		states:     make(map[string]TokenState),
		maxRetries: maxRetries,
	}
}

// GetState 获取token状态
func (m *DefaultTokenStateManager) GetState(tokenID string) TokenState {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	state, exists := m.states[tokenID]
	if !exists {
		// 默认状态为活跃
		return NewActiveState()
	}
	return state
}

// UpdateState 更新token状态
func (m *DefaultTokenStateManager) UpdateState(tokenID string, state TokenState) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.states[tokenID] = state
}

// GetAvailableTokens 获取可用的token列表 (遵循token.md需求：不轮询停用的token)
func (m *DefaultTokenStateManager) GetAvailableTokens() []string {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	var available []string
	for tokenID, state := range m.states {
		if state.IsAvailable() && state.CanRefresh() {
			available = append(available, tokenID)
		}
	}
	return available
}

// MarkTokenDisabled 标记token为停用状态
func (m *DefaultTokenStateManager) MarkTokenDisabled(tokenID string) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.states[tokenID] = NewDisabledState("手动停用")
}

// MarkTokenActive 标记token为活跃状态
func (m *DefaultTokenStateManager) MarkTokenActive(tokenID string) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.states[tokenID] = NewActiveState()
}

// OnTokenSuccess 处理token成功事件 (状态机转换逻辑)
func (m *DefaultTokenStateManager) OnTokenSuccess(tokenID string) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	currentState, exists := m.states[tokenID]
	if !exists {
		currentState = NewActiveState()
	}

	currentState.OnSuccess()

	// 状态转换逻辑
	switch currentState.StateName() {
	case "Failed":
		// 失败状态成功后转为活跃状态
		m.states[tokenID] = NewActiveState()
	case "Active":
		// 活跃状态保持活跃，更新成功时间
		m.states[tokenID] = currentState
	case "Disabled":
		// 停用状态不自动转换
	}
}

// OnTokenFailure 处理token失败事件 (状态机转换逻辑)
func (m *DefaultTokenStateManager) OnTokenFailure(tokenID string) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	currentState, exists := m.states[tokenID]
	if !exists {
		currentState = NewActiveState()
	}

	currentState.OnFailure()

	// 状态转换逻辑
	switch s := currentState.(type) {
	case *ActiveState:
		// 活跃状态失败后转为失败状态
		m.states[tokenID] = NewFailedState(m.maxRetries)
	case *FailedState:
		// 检查是否超过最大重试次数
		if s.GetFailureCount() >= m.maxRetries {
			// 超过重试次数，转为停用状态
			m.states[tokenID] = NewDisabledState("超过最大重试次数")
		} else {
			// 未超过，保持失败状态并更新失败计数
			m.states[tokenID] = s
		}
	case *DisabledState:
		// 停用状态不变
	}
}

// GetTokenStats 获取所有token的状态统计
func (m *DefaultTokenStateManager) GetTokenStats() map[string]interface{} {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	stats := make(map[string]interface{})
	stateCount := make(map[string]int)

	for tokenID, state := range m.states {
		stateName := state.StateName()
		stateCount[stateName]++

		// 详细状态信息
		stats[tokenID] = map[string]interface{}{
			"state":      stateName,
			"available":  state.IsAvailable(),
			"canRefresh": state.CanRefresh(),
		}
	}

	stats["summary"] = stateCount
	return stats
}
