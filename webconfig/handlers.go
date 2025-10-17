package webconfig

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"path/filepath"
	"strings"
	"time"
)

// SetupRoutes 设置路由
func (m *Manager) SetupRoutes(r *http.ServeMux) {
	// 公开路由
	r.HandleFunc("/", m.handleRoot)
	r.HandleFunc("/login", m.handleLogin)
	r.HandleFunc("/api/init", m.handleInit)

	// 需要认证的路由
	r.HandleFunc("/logout", m.withAuth(m.handleLogout))
	r.HandleFunc("/config", m.withAuth(m.handleConfig))
	r.HandleFunc("/api/config", m.withAuth(m.handleAPIConfig))
	r.HandleFunc("/api/tokens", m.withAuth(m.handleAPITokens))
	r.HandleFunc("/api/tokens/refresh", m.withAuth(m.handleRefreshTokens))
	r.HandleFunc("/api/tokens/refresh-single", m.withAuth(m.handleRefreshSingleToken))
	r.HandleFunc("/api/tokens/current", m.withAuth(m.handleGetCurrentToken))
	r.HandleFunc("/api/tokens/switch", m.withAuth(m.handleSwitchToken))
	r.HandleFunc("/api/backup", m.withAuth(m.handleBackup))
	r.HandleFunc("/api/restore", m.withAuth(m.handleRestore))

	// 静态文件服务
	r.HandleFunc("/static/", m.handleStatic)
}

// handleRoot 处理根路径
func (m *Manager) handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	if m.IsFirstRun() {
		http.Redirect(w, r, "/login?init=true", http.StatusSeeOther)
		return
	}

	// 检查会话
	sessionCookie, err := r.Cookie("session_id")
	if err != nil || !m.ValidateSession(sessionCookie.Value) {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, "/config", http.StatusSeeOther)
}

// handleLogin 处理登录页面
func (m *Manager) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		isInit := r.URL.Query().Get("init") == "true"
		m.renderLoginPage(w, isInit)
		return
	}

	if r.Method != "POST" {
		http.Error(w, "方法不允许", http.StatusMethodNotAllowed)
		return
	}

	// 处理登录
	if err := r.ParseForm(); err != nil {
		m.renderError(w, "解析表单失败", http.StatusBadRequest)
		return
	}

	password := r.FormValue("password")
	if password == "" {
		m.renderError(w, "密码不能为空", http.StatusBadRequest)
		return
	}

	// 首次初始化
	isInit := r.URL.Query().Get("init") == "true"
	if isInit {
		clientToken := r.FormValue("clientToken")
		if clientToken == "" {
			m.renderError(w, "客户端Token不能为空", http.StatusBadRequest)
			return
		}

		if err := m.InitializeFirstRun(password, clientToken); err != nil {
			m.renderError(w, fmt.Sprintf("初始化失败: %v", err), http.StatusInternalServerError)
			return
		}
	} else {
		// 验证密码
		if !m.VerifyLoginPassword(password) {
			m.renderLoginPage(w, false, "密码错误")
			return
		}
	}

	// 创建会话
	session := m.CreateSession()
	http.SetCookie(w, &http.Cookie{
		Name:     "session_id",
		Value:    session.ID,
		Path:     "/",
		Expires:  session.ExpiresAt,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})

	http.Redirect(w, r, "/config", http.StatusSeeOther)
}

// handleInit 处理初始化API
func (m *Manager) handleInit(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "方法不允许", http.StatusMethodNotAllowed)
		return
	}

	if !m.IsFirstRun() {
		m.writeJSONError(w, "系统已初始化", http.StatusBadRequest)
		return
	}

	var req struct {
		LoginPassword string `json:"loginPassword"`
		ClientToken   string `json:"clientToken"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		m.writeJSONError(w, "JSON解析失败", http.StatusBadRequest)
		return
	}

	if req.LoginPassword == "" || req.ClientToken == "" {
		m.writeJSONError(w, "登录密码和客户端Token不能为空", http.StatusBadRequest)
		return
	}

	if err := m.InitializeFirstRun(req.LoginPassword, req.ClientToken); err != nil {
		m.writeJSONError(w, fmt.Sprintf("初始化失败: %v", err), http.StatusInternalServerError)
		return
	}

	// 创建会话
	session := m.CreateSession()
	http.SetCookie(w, &http.Cookie{
		Name:     "session_id",
		Value:    session.ID,
		Path:     "/",
		Expires:  session.ExpiresAt,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})

	m.writeJSONResponse(w, map[string]interface{}{
		"success": true,
		"message": "初始化成功",
		"session": session.ID,
	})
}

// handleLogout 处理登出
func (m *Manager) handleLogout(w http.ResponseWriter, r *http.Request) {
	sessionCookie, err := r.Cookie("session_id")
	if err == nil {
		m.InvalidateSession(sessionCookie.Value)
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "session_id",
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0),
		HttpOnly: true,
	})

	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// handleConfig 处理配置页面
func (m *Manager) handleConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		m.renderConfigPage(w)
		return
	}

	http.Error(w, "方法不允许", http.StatusMethodNotAllowed)
}

// handleAPIConfig 处理配置API
func (m *Manager) handleAPIConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		config := m.GetConfig()
		// 不返回密码
		config.LoginPassword = ""
		m.writeJSONResponse(w, config)

	case "PUT":
		var newConfig WebConfig
		if err := json.NewDecoder(r.Body).Decode(&newConfig); err != nil {
			m.writeJSONError(w, "JSON解析失败", http.StatusBadRequest)
			return
		}

		// 保持原有的登录密码
		oldConfig := m.GetConfig()
		newConfig.LoginPassword = oldConfig.LoginPassword

		if err := m.UpdateConfig(&newConfig); err != nil {
			m.writeJSONError(w, fmt.Sprintf("更新配置失败: %v", err), http.StatusBadRequest)
			return
		}

		m.writeJSONResponse(w, map[string]interface{}{
			"success": true,
			"message": "配置更新成功",
		})

	default:
		http.Error(w, "方法不允许", http.StatusMethodNotAllowed)
	}
}

// handleRefreshTokens 处理Token刷新请求（全部）
func (m *Manager) handleRefreshTokens(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "方法不允许", http.StatusMethodNotAllowed)
		return
	}
	
	// 强制刷新Token缓存
	go m.ForceRefreshTokenCache()
	
	m.writeJSONResponse(w, map[string]interface{}{
		"success": true,
		"message": "Token刷新已启动",
	})
}

// handleRefreshSingleToken 处理单个Token刷新请求
func (m *Manager) handleRefreshSingleToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "方法不允许", http.StatusMethodNotAllowed)
		return
	}
	
	tokenID := r.URL.Query().Get("id")
	if tokenID == "" {
		m.writeJSONError(w, "Token ID不能为空", http.StatusBadRequest)
		return
	}
	
	// 异步刷新单个Token
	go func() {
		config := m.GetConfig()
		m.providerMutex.RLock()
		provider := m.tokenUsageProvider
		m.providerMutex.RUnlock()
		
		if provider == nil {
			return
		}
		
		for i, token := range config.AuthTokens {
			if token.ID == tokenID {
				tokenInfo := TokenWithUsageInfo{
					AuthToken:      token,
					UserEmail:      "未知",
					RemainingUsage: 0,
					UserId:         fmt.Sprintf("%d", i),
				}
				
				if !token.Enabled {
					tokenInfo.UserEmail = "已禁用"
				} else {
					// 获取实时使用信息，失败时自动重试2次
					var err error
					var userEmail, userId string
					var remainingUsage float64
					var lastUsed *time.Time
					
					maxRetries := 2
					for attempt := 0; attempt <= maxRetries; attempt++ {
						userEmail, userId, remainingUsage, lastUsed, err = provider(token)
						if err == nil {
							// 成功获取信息
							tokenInfo.UserEmail = userEmail
							tokenInfo.UserId = userId
							tokenInfo.RemainingUsage = remainingUsage
							if lastUsed != nil {
								tokenInfo.LastUsed = lastUsed
							}
							break
						}
						
						// 如果不是最后一次尝试，等待一小段时间后重试
						if attempt < maxRetries {
							time.Sleep(time.Second * time.Duration(attempt+1)) // 递增等待时间：1秒、2秒
						}
					}
					
					// 所有重试都失败
					if err != nil {
						tokenInfo.UserEmail = "获取失败"
					}
				}
				
				m.cacheMutex.Lock()
				m.tokenCache[token.ID] = &tokenInfo
				m.cacheMutex.Unlock()
				break
			}
		}
	}()
	
	m.writeJSONResponse(w, map[string]interface{}{
		"success": true,
		"message": "Token刷新已启动",
	})
}

// handleAPITokens 处理Token API
func (m *Manager) handleAPITokens(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		// 获取带有实时使用信息的Token列表（使用缓存）
		tokens := m.GetTokensWithUsageInfo()
		m.writeJSONResponse(w, tokens)

	case "POST":
		var token AuthToken
		if err := json.NewDecoder(r.Body).Decode(&token); err != nil {
			m.writeJSONError(w, "JSON解析失败", http.StatusBadRequest)
			return
		}

		if token.Auth == "" || token.RefreshToken == "" {
			m.writeJSONError(w, "认证方式和刷新Token不能为空", http.StatusBadRequest)
			return
		}

		if token.Auth == "IdC" && (token.ClientID == "" || token.ClientSecret == "") {
			m.writeJSONError(w, "IdC认证需要客户端ID和密钥", http.StatusBadRequest)
			return
		}

		// 生成ID
		token.ID = fmt.Sprintf("%d", time.Now().UnixNano())
		token.Enabled = true

		config := m.GetConfig()
		config.AuthTokens = append(config.AuthTokens, token)

		if err := m.UpdateConfig(config); err != nil {
			m.writeJSONError(w, fmt.Sprintf("添加Token失败: %v", err), http.StatusInternalServerError)
			return
		}

		m.writeJSONResponse(w, map[string]interface{}{
			"success": true,
			"message": "Token添加成功",
			"token":   token,
		})

	case "PUT":
		// 更新Token状态（启用/禁用）
		tokenID := r.URL.Query().Get("id")
		if tokenID == "" {
			m.writeJSONError(w, "Token ID不能为空", http.StatusBadRequest)
			return
		}

		var req struct {
			Enabled bool `json:"enabled"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			m.writeJSONError(w, "JSON解析失败", http.StatusBadRequest)
			return
		}

		config := m.GetConfig()
		found := false

		for i, token := range config.AuthTokens {
			if token.ID == tokenID {
				config.AuthTokens[i].Enabled = req.Enabled
				found = true
				break
			}
		}

		if !found {
			m.writeJSONError(w, "Token不存在", http.StatusNotFound)
			return
		}

		if err := m.UpdateConfig(config); err != nil {
			m.writeJSONError(w, fmt.Sprintf("更新Token失败: %v", err), http.StatusInternalServerError)
			return
		}

		m.writeJSONResponse(w, map[string]interface{}{
			"success": true,
			"message": "Token状态更新成功",
		})

	case "DELETE":
		tokenID := r.URL.Query().Get("id")
		if tokenID == "" {
			m.writeJSONError(w, "Token ID不能为空", http.StatusBadRequest)
			return
		}

		config := m.GetConfig()
		var updatedTokens []AuthToken
		found := false

		for _, token := range config.AuthTokens {
			if token.ID == tokenID {
				found = true
				continue
			}
			updatedTokens = append(updatedTokens, token)
		}

		if !found {
			m.writeJSONError(w, "Token不存在", http.StatusNotFound)
			return
		}

		config.AuthTokens = updatedTokens
		if err := m.UpdateConfig(config); err != nil {
			m.writeJSONError(w, fmt.Sprintf("删除Token失败: %v", err), http.StatusInternalServerError)
			return
		}

		m.writeJSONResponse(w, map[string]interface{}{
			"success": true,
			"message": "Token删除成功",
		})

	default:
		http.Error(w, "方法不允许", http.StatusMethodNotAllowed)
	}
}

// handleBackup 处理备份
func (m *Manager) handleBackup(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		backups, err := m.ListBackups()
		if err != nil {
			m.writeJSONError(w, fmt.Sprintf("获取备份列表失败: %v", err), http.StatusInternalServerError)
			return
		}
		// 确保永远不会返回null
		if backups == nil {
			backups = []string{}
		}
		m.writeJSONResponse(w, backups)

	case "POST":
		if err := m.BackupConfig(); err != nil {
			m.writeJSONError(w, fmt.Sprintf("备份失败: %v", err), http.StatusInternalServerError)
			return
		}

		m.writeJSONResponse(w, map[string]interface{}{
			"success": true,
			"message": "配置备份成功",
		})

	default:
		http.Error(w, "方法不允许", http.StatusMethodNotAllowed)
	}
}

// handleRestore 处理恢复
func (m *Manager) handleRestore(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "方法不允许", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		BackupFile string `json:"backupFile"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		m.writeJSONError(w, "JSON解析失败", http.StatusBadRequest)
		return
	}

	if req.BackupFile == "" {
		m.writeJSONError(w, "备份文件名不能为空", http.StatusBadRequest)
		return
	}

	if err := m.RestoreFromBackup(req.BackupFile); err != nil {
		m.writeJSONError(w, fmt.Sprintf("恢复失败: %v", err), http.StatusInternalServerError)
		return
	}

	m.writeJSONResponse(w, map[string]interface{}{
		"success": true,
		"message": "配置恢复成功",
	})
}

// handleStatic 处理静态文件
func (m *Manager) handleStatic(w http.ResponseWriter, r *http.Request) {
	filePath := filepath.Join("webconfig", "static", strings.TrimPrefix(r.URL.Path, "/static"))
	http.ServeFile(w, r, filePath)
}

// withAuth 认证中间件
func (m *Manager) withAuth(handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sessionCookie, err := r.Cookie("session_id")
		if err != nil || !m.ValidateSession(sessionCookie.Value) {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		handler(w, r)
	}
}

// writeJSONResponse 写入JSON响应
func (m *Manager) writeJSONResponse(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(data); err != nil {
		http.Error(w, "JSON编码失败", http.StatusInternalServerError)
	}
}

// writeJSONError 写入JSON错误响应
func (m *Manager) writeJSONError(w http.ResponseWriter, message string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": false,
		"error":   message,
	})
}

// renderError 渲染错误页面
func (m *Manager) renderError(w http.ResponseWriter, message string, code int) {
	w.WriteHeader(code)
	tmpl := template.Must(template.New("error").Parse(`
<!DOCTYPE html>
<html>
<head>
    <title>错误</title>
    <meta charset="utf-8">
    <style>
        body { font-family: Arial, sans-serif; margin: 50px; }
        .error { color: red; }
    </style>
</head>
<body>
    <h1 class="error">错误</h1>
    <p>{{.Message}}</p>
    <p><a href="/login">返回登录</a></p>
</body>
</html>
`))
	tmpl.Execute(w, map[string]string{"Message": message})
}

// renderLoginPage 渲染登录页面
func (m *Manager) renderLoginPage(w http.ResponseWriter, isInit bool, errorMsg ...string) {
	tmpl := template.Must(template.ParseFiles(filepath.Join("webconfig", "static", "login.html")))

	data := map[string]interface{}{
		"IsInit": isInit,
	}

	if len(errorMsg) > 0 && errorMsg[0] != "" {
		data["Error"] = errorMsg[0]
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.Execute(w, data); err != nil {
		http.Error(w, "渲染页面失败", http.StatusInternalServerError)
	}
}

// handleGetCurrentToken 获取当前正在使用的token索引
func (m *Manager) handleGetCurrentToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "方法不允许", http.StatusMethodNotAllowed)
		return
	}
	
	// 获取当前token索引（通过回调函数）
	currentIndex := -1
	if m.getCurrentTokenIndex != nil {
		currentIndex = m.getCurrentTokenIndex()
	}
	
	m.writeJSONResponse(w, map[string]interface{}{
		"currentIndex": currentIndex,
	})
}

// handleSwitchToken 手动切换到指定的token
func (m *Manager) handleSwitchToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "方法不允许", http.StatusMethodNotAllowed)
		return
	}
	
	var req struct {
		Index int `json:"index"`
	}
	
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		m.writeJSONError(w, "JSON解析失败", http.StatusBadRequest)
		return
	}
	
	// 验证索引有效性
	config := m.GetConfig()
	if req.Index < 0 || req.Index >= len(config.AuthTokens) {
		m.writeJSONError(w, "无效的token索引", http.StatusBadRequest)
		return
	}
	
	// 调用切换函数（通过回调）
	if m.switchToToken != nil {
		if err := m.switchToToken(req.Index); err != nil {
			m.writeJSONError(w, fmt.Sprintf("切换token失败: %v", err), http.StatusInternalServerError)
			return
		}
	} else {
		m.writeJSONError(w, "切换功能未初始化", http.StatusServiceUnavailable)
		return
	}
	
	m.writeJSONResponse(w, map[string]interface{}{
		"success": true,
		"message": "Token切换成功",
		"index":   req.Index,
	})
}

// renderConfigPage 渲染配置页面
func (m *Manager) renderConfigPage(w http.ResponseWriter) {
	tmpl := template.Must(template.ParseFiles(filepath.Join("webconfig", "static", "index.html")))

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.Execute(w, nil); err != nil {
		http.Error(w, "渲染页面失败", http.StatusInternalServerError)
	}
}