package webconfig

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// TokenUsageProvider Token使用信息提供者接口
type TokenUsageProvider func(token AuthToken) (userEmail string, userId string, remainingUsage float64, err error)

// Manager 配置管理器
type Manager struct {
	storage  *Storage
	config   *WebConfig
	mutex    sync.RWMutex
	sessions map[string]*Session
	sessionMutex sync.RWMutex
	configChangeCallbacks []func() // 配置更新回调函数列表
	callbackMutex sync.RWMutex
	tokenUsageProvider TokenUsageProvider // Token使用信息提供者
	providerMutex sync.RWMutex
}

// Session 会话信息
type Session struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"createdAt"`
	ExpiresAt time.Time `json:"expiresAt"`
	Active    bool      `json:"active"`
}

const (
	sessionDuration = 24 * time.Hour // 会话有效期24小时
	cleanupInterval = time.Hour      // 清理过期会话的间隔
)

// NewManager 创建配置管理器
func NewManager() *Manager {
	m := &Manager{
		storage:  NewStorage(),
		sessions: make(map[string]*Session),
	}

	// 加载配置
	config, err := m.storage.LoadConfig()
	if err != nil {
		panic(fmt.Sprintf("加载配置失败: %v", err))
	}
	m.config = config

	// 启动会话清理协程
	go m.cleanupExpiredSessions()

	return m
}

// GetConfig 获取当前配置（线程安全）
func (m *Manager) GetConfig() *WebConfig {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	return m.config.Clone()
}

// UpdateConfig 更新配置
func (m *Manager) UpdateConfig(newConfig *WebConfig) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	// 验证配置
	if err := newConfig.Validate(); err != nil {
		return err
	}

	// 保存到文件
	if err := m.storage.SaveConfig(newConfig); err != nil {
		return fmt.Errorf("保存配置失败: %w", err)
	}

	// 更新内存中的配置
	m.config = newConfig.Clone()

	// 调用配置更新回调
	m.notifyConfigChange()

	return nil
}

// AddConfigChangeCallback 添加配置更新回调
func (m *Manager) AddConfigChangeCallback(callback func()) {
	m.callbackMutex.Lock()
	defer m.callbackMutex.Unlock()
	m.configChangeCallbacks = append(m.configChangeCallbacks, callback)
}

// notifyConfigChange 通知所有注册的回调函数
func (m *Manager) notifyConfigChange() {
	m.callbackMutex.RLock()
	callbacks := make([]func(), len(m.configChangeCallbacks))
	copy(callbacks, m.configChangeCallbacks)
	m.callbackMutex.RUnlock()

	for _, callback := range callbacks {
		go func(cb func()) {
			defer func() {
				if r := recover(); r != nil {
					// 捕获回调中的panic，避免影响主流程
					fmt.Printf("配置更新回调执行出错: %v\n", r)
				}
			}()
			cb()
		}(callback)
	}
}

// IsFirstRun 检查是否是首次运行
func (m *Manager) IsFirstRun() bool {
	return m.storage.IsFirstRun()
}

// InitializeFirstRun 初始化首次运行配置
func (m *Manager) InitializeFirstRun(loginPassword, clientToken string) error {
	if !m.IsFirstRun() {
		return NewConfigError("配置已存在，不是首次运行")
	}

	config := GetDefaultConfig()
	config.LoginPassword = loginPassword
	config.ServiceConfig.ClientToken = clientToken

	return m.UpdateConfig(config)
}

// VerifyLoginPassword 验证登录密码
func (m *Manager) VerifyLoginPassword(password string) bool {
	config := m.GetConfig()
	return subtle.ConstantTimeCompare([]byte(config.LoginPassword), []byte(password)) == 1
}

// CreateSession 创建会话
func (m *Manager) CreateSession() *Session {
	session := &Session{
		ID:        generateSessionID(),
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(sessionDuration),
		Active:    true,
	}

	m.sessionMutex.Lock()
	m.sessions[session.ID] = session
	m.sessionMutex.Unlock()

	return session
}

// ValidateSession 验证会话
func (m *Manager) ValidateSession(sessionID string) bool {
	m.sessionMutex.RLock()
	defer m.sessionMutex.RUnlock()

	session, exists := m.sessions[sessionID]
	if !exists || !session.Active {
		return false
	}

	// 检查是否过期
	if time.Now().After(session.ExpiresAt) {
		return false
	}

	return true
}

// InvalidateSession 使会话失效
func (m *Manager) InvalidateSession(sessionID string) {
	m.sessionMutex.Lock()
	defer m.sessionMutex.Unlock()

	if session, exists := m.sessions[sessionID]; exists {
		session.Active = false
	}
}

// cleanupExpiredSessions 清理过期会话
func (m *Manager) cleanupExpiredSessions() {
	ticker := time.NewTicker(cleanupInterval)
	defer ticker.Stop()

	for range ticker.C {
		m.sessionMutex.Lock()
		now := time.Now()
		for id, session := range m.sessions {
			if now.After(session.ExpiresAt) {
				delete(m.sessions, id)
			}
		}
		m.sessionMutex.Unlock()
	}
}

// GetAuthTokenString 获取认证Token字符串（兼容原有代码）
func (m *Manager) GetAuthTokenString() string {
	config := m.GetConfig()
	if len(config.AuthTokens) == 0 {
		return "[]"
	}

	// 转换为兼容的JSON格式
	result := "["
	for i, token := range config.AuthTokens {
		if !token.Enabled {
			continue
		}

		if i > 0 {
			result += ","
		}

		result += `{"auth":"` + token.Auth + `","refreshToken":"` + token.RefreshToken + `"`
		if token.ClientID != "" {
			result += `,"clientId":"` + token.ClientID + `"`
		}
		if token.ClientSecret != "" {
			result += `,"clientSecret":"` + token.ClientSecret + `"`
		}
		result += "}"
	}
	result += "]"

	return result
}

// UpdateTokenUsage 更新Token使用状态
func (m *Manager) UpdateTokenUsage(tokenID string, success bool) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	for i, token := range m.config.AuthTokens {
		if token.ID == tokenID {
			if success {
				now := time.Now()
				m.config.AuthTokens[i].LastUsed = &now
				m.config.AuthTokens[i].ErrorCount = 0
			} else {
				m.config.AuthTokens[i].ErrorCount++
			}
			break
		}
	}
}

// GetEnabledTokens 获取启用的Token
func (m *Manager) GetEnabledTokens() []AuthToken {
	config := m.GetConfig()
	var enabled []AuthToken
	for _, token := range config.AuthTokens {
		if token.Enabled {
			enabled = append(enabled, token)
		}
	}
	return enabled
}

// TokenWithUsageInfo Token带使用信息
type TokenWithUsageInfo struct {
	AuthToken
	UserEmail      string  `json:"userEmail"`
	RemainingUsage float64 `json:"remainingUsage"`
	UserId         string  `json:"userId"`
}

// SetTokenUsageProvider 设置Token使用信息提供者
func (m *Manager) SetTokenUsageProvider(provider TokenUsageProvider) {
	m.providerMutex.Lock()
	defer m.providerMutex.Unlock()
	m.tokenUsageProvider = provider
}

// GetTokensWithUsageInfo 获取带有实时使用信息的Token列表
func (m *Manager) GetTokensWithUsageInfo() []TokenWithUsageInfo {
	config := m.GetConfig()
	result := make([]TokenWithUsageInfo, 0, len(config.AuthTokens))
	
	m.providerMutex.RLock()
	provider := m.tokenUsageProvider
	m.providerMutex.RUnlock()
	
	for i, token := range config.AuthTokens {
		tokenInfo := TokenWithUsageInfo{
			AuthToken:      token,
			UserEmail:      "未知",
			RemainingUsage: 0,
			UserId:         fmt.Sprintf("%d", i),
		}
		
		// 如果Token被禁用，直接返回基本信息
		if !token.Enabled {
			tokenInfo.UserEmail = "已禁用"
			result = append(result, tokenInfo)
			continue
		}
		
		// 如果有提供者，获取实时使用信息
		if provider != nil {
			if userEmail, userId, remainingUsage, err := provider(token); err == nil {
				tokenInfo.UserEmail = userEmail
				tokenInfo.UserId = userId
				tokenInfo.RemainingUsage = remainingUsage
			} else {
				tokenInfo.UserEmail = "获取失败"
			}
		}
		
		result = append(result, tokenInfo)
	}
	
	return result
}

// BackupConfig 备份配置
func (m *Manager) BackupConfig() error {
	return m.storage.BackupConfig()
}

// ListBackups 列出备份
func (m *Manager) ListBackups() ([]string, error) {
	return m.storage.ListBackups()
}

// RestoreFromBackup 从备份恢复
func (m *Manager) RestoreFromBackup(backupFile string) error {
	// 加载备份配置
	backupPath := filepath.Join(filepath.Dir(m.storage.GetConfigPath()), backupFile)
	config, err := m.storage.LoadConfigFromPath(backupPath)
	if err != nil {
		return err
	}

	return m.UpdateConfig(config)
}

// 全局配置管理器实例
var globalManager *Manager
var once sync.Once

// GetGlobalManager 获取全局配置管理器（单例模式）
func GetGlobalManager() *Manager {
	once.Do(func() {
		globalManager = NewManager()
	})
	return globalManager
}

// 临时方法，需要在storage.go中添加
func (s *Storage) LoadConfigFromPath(configPath string) (*WebConfig, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("读取配置文件失败: %w", err)
	}

	var config WebConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("解析配置文件失败: %w", err)
	}

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("配置验证失败: %w", err)
	}

	return &config, nil
}

// 生成会话ID
func generateSessionID() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

// AuthMiddleware 认证中间件
func (m *Manager) AuthMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// 检查会话
			sessionCookie, err := r.Cookie("session_id")
			if err != nil || !m.ValidateSession(sessionCookie.Value) {
				http.Redirect(w, r, "/login", http.StatusSeeOther)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}