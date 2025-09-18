package parser

import (
	"fmt"
	"kiro2api/logger"
	"strings"
	"sync"
	"time"
)

// ResponseValidator 流式响应验证器
type ResponseValidator struct {
	mu                  sync.RWMutex
	activeValidations   map[string]*ValidationSession
	validationRules     []ValidationRule
	defaultTimeout      time.Duration
	maxValidationErrors int
}

// ValidationSession 验证会话
type ValidationSession struct {
	sessionId         string
	startTime         time.Time
	lastActivity      time.Time
	messageCount      int
	textContentLength int
	toolCallCount     int
	isComplete        bool
	errors            []ValidationError
	status            ValidationStatus
	expectedEndEvents []string
	receivedEndEvents []string
}

// ValidationStatus 验证状态
type ValidationStatus int

const (
	ValidationPending ValidationStatus = iota
	ValidationActive
	ValidationCompleted
	ValidationFailed
)

// ValidationError 验证错误
type ValidationError struct {
	Timestamp time.Time
	ErrorType string
	Message   string
	EventData interface{}
	Severity  ErrorSeverity
}

// ErrorSeverity 错误严重程度
type ErrorSeverity int

const (
	SeverityInfo ErrorSeverity = iota
	SeverityWarning
	SeverityError
	SeverityCritical
)

// ValidationRule 验证规则接口
type ValidationRule interface {
	Name() string
	Validate(session *ValidationSession, event SSEEvent) *ValidationError
}

// NewResponseValidator 创建响应验证器
func NewResponseValidator() *ResponseValidator {
	rv := &ResponseValidator{
		activeValidations:   make(map[string]*ValidationSession),
		defaultTimeout:      30 * time.Second,
		maxValidationErrors: 10,
	}

	// 注册默认验证规则
	rv.registerDefaultRules()

	return rv
}

// registerDefaultRules 注册默认验证规则
func (rv *ResponseValidator) registerDefaultRules() {
	rules := []ValidationRule{
		&MessageStartEndRule{},
		&ContentBlockIntegrityRule{},
		&ToolExecutionFlowRule{},
		&StreamingTimeoutRule{timeout: rv.defaultTimeout},
		&DuplicateEventRule{},
	}

	rv.validationRules = append(rv.validationRules, rules...)

	logger.Debug("注册响应验证规则",
		logger.Int("rule_count", len(rv.validationRules)))
}

// StartValidation 开始验证会话
func (rv *ResponseValidator) StartValidation(sessionId string) {
	rv.mu.Lock()
	defer rv.mu.Unlock()

	session := &ValidationSession{
		sessionId:    sessionId,
		startTime:    time.Now(),
		lastActivity: time.Now(),
		status:       ValidationActive,
		expectedEndEvents: []string{
			"content_block_stop",
			"message_delta",
			"message_stop",
		},
	}

	rv.activeValidations[sessionId] = session

	logger.Debug("开始响应验证会话",
		logger.String("session_id", sessionId))
}

// ValidateEvent 验证单个事件
func (rv *ResponseValidator) ValidateEvent(sessionId string, event SSEEvent) []ValidationError {
	rv.mu.Lock()
	defer rv.mu.Unlock()

	session, exists := rv.activeValidations[sessionId]
	if !exists {
		return []ValidationError{{
			Timestamp: time.Now(),
			ErrorType: "session_not_found",
			Message:   fmt.Sprintf("验证会话不存在: %s", sessionId),
			Severity:  SeverityError,
		}}
	}

	// 更新会话活动时间
	session.lastActivity = time.Now()
	session.messageCount++

	var errors []ValidationError

	// 应用所有验证规则
	for _, rule := range rv.validationRules {
		if err := rule.Validate(session, event); err != nil {
			session.errors = append(session.errors, *err)
			errors = append(errors, *err)

			logger.Warn("响应验证规则失败",
				logger.String("session_id", sessionId),
				logger.String("rule_name", rule.Name()),
				logger.String("error_type", err.ErrorType),
				logger.String("error_message", err.Message))
		}
	}

	// 检查会话是否应该完成
	rv.checkSessionCompletion(session, event)

	return errors
}

// checkSessionCompletion 检查会话是否完成
func (rv *ResponseValidator) checkSessionCompletion(session *ValidationSession, event SSEEvent) {
	if event.Event == "message_stop" {
		session.isComplete = true
		session.status = ValidationCompleted

		// 验证是否收到了所有预期的结束事件
		for _, expectedEvent := range session.expectedEndEvents {
			found := false
			for _, receivedEvent := range session.receivedEndEvents {
				if receivedEvent == expectedEvent {
					found = true
					break
				}
			}
			if !found {
				session.errors = append(session.errors, ValidationError{
					Timestamp: time.Now(),
					ErrorType: "missing_end_event",
					Message:   fmt.Sprintf("缺少预期的结束事件: %s", expectedEvent),
					Severity:  SeverityWarning,
				})
			}
		}

		logger.Debug("验证会话完成",
			logger.String("session_id", session.sessionId),
			logger.Int("message_count", session.messageCount),
			logger.Int("error_count", len(session.errors)),
			logger.Duration("duration", time.Since(session.startTime)))
	}

	// 记录结束事件
	if strings.HasSuffix(event.Event, "_stop") || strings.HasSuffix(event.Event, "_delta") {
		session.receivedEndEvents = append(session.receivedEndEvents, event.Event)
	}
}

// FinishValidation 完成验证会话
func (rv *ResponseValidator) FinishValidation(sessionId string) *ValidationSession {
	rv.mu.Lock()
	defer rv.mu.Unlock()

	session, exists := rv.activeValidations[sessionId]
	if !exists {
		return nil
	}

	session.isComplete = true
	if len(session.errors) == 0 {
		session.status = ValidationCompleted
	} else {
		// 检查是否有严重错误
		hasCriticalError := false
		for _, err := range session.errors {
			if err.Severity >= SeverityError {
				hasCriticalError = true
				break
			}
		}
		if hasCriticalError {
			session.status = ValidationFailed
		} else {
			session.status = ValidationCompleted
		}
	}

	// 从活跃验证中移除
	delete(rv.activeValidations, sessionId)

	logger.Info("验证会话结束",
		logger.String("session_id", sessionId),
		logger.String("status", rv.statusToString(session.status)),
		logger.Int("total_messages", session.messageCount),
		logger.Int("total_errors", len(session.errors)),
		logger.Duration("total_duration", time.Since(session.startTime)))

	return session
}

// CleanupExpiredSessions 清理过期的验证会话
func (rv *ResponseValidator) CleanupExpiredSessions() {
	rv.mu.Lock()
	defer rv.mu.Unlock()

	now := time.Now()
	expiredSessions := make([]string, 0)

	for sessionId, session := range rv.activeValidations {
		if now.Sub(session.lastActivity) > rv.defaultTimeout {
			expiredSessions = append(expiredSessions, sessionId)

			logger.Warn("清理过期的验证会话",
				logger.String("session_id", sessionId),
				logger.Duration("idle_time", now.Sub(session.lastActivity)),
				logger.Int("message_count", session.messageCount))
		}
	}

	for _, sessionId := range expiredSessions {
		delete(rv.activeValidations, sessionId)
	}

	if len(expiredSessions) > 0 {
		logger.Debug("清理过期验证会话完成",
			logger.Int("cleaned_count", len(expiredSessions)),
			logger.Int("remaining_count", len(rv.activeValidations)))
	}
}

// statusToString 将状态转换为字符串
func (rv *ResponseValidator) statusToString(status ValidationStatus) string {
	switch status {
	case ValidationPending:
		return "pending"
	case ValidationActive:
		return "active"
	case ValidationCompleted:
		return "completed"
	case ValidationFailed:
		return "failed"
	default:
		return "unknown"
	}
}

// GetActiveSessionCount 获取活跃会话数量
func (rv *ResponseValidator) GetActiveSessionCount() int {
	rv.mu.RLock()
	defer rv.mu.RUnlock()
	return len(rv.activeValidations)
}

// GetSessionStats 获取会话统计信息
func (rv *ResponseValidator) GetSessionStats(sessionId string) map[string]interface{} {
	rv.mu.RLock()
	defer rv.mu.RUnlock()

	session, exists := rv.activeValidations[sessionId]
	if !exists {
		return nil
	}

	errorsBySeverity := make(map[string]int)
	for _, err := range session.errors {
		severity := rv.severityToString(err.Severity)
		errorsBySeverity[severity]++
	}

	return map[string]interface{}{
		"session_id":          session.sessionId,
		"status":              rv.statusToString(session.status),
		"message_count":       session.messageCount,
		"text_content_length": session.textContentLength,
		"tool_call_count":     session.toolCallCount,
		"is_complete":         session.isComplete,
		"total_errors":        len(session.errors),
		"errors_by_severity":  errorsBySeverity,
		"duration_seconds":    time.Since(session.startTime).Seconds(),
	}
}

// severityToString 将错误严重程度转换为字符串
func (rv *ResponseValidator) severityToString(severity ErrorSeverity) string {
	switch severity {
	case SeverityInfo:
		return "info"
	case SeverityWarning:
		return "warning"
	case SeverityError:
		return "error"
	case SeverityCritical:
		return "critical"
	default:
		return "unknown"
	}
}
