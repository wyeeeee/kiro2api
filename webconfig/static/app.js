// 全局变量
let currentConfig = {};

// DOM元素
const globalMessage = document.getElementById('globalMessage');
const sections = {
    service: document.getElementById('service-section'),
    tokens: document.getElementById('tokens-section'),
    logs: document.getElementById('logs-section'),
    timeouts: document.getElementById('timeouts-section'),
    backup: document.getElementById('backup-section')
};

// 页面加载完成后初始化
document.addEventListener('DOMContentLoaded', function() {
    initializeNavigation();
    initializeForms();
    loadConfig();
    loadTokens();
    loadBackups();
    
    // 初始化时隐藏全局操作按钮（因为默认显示Token管理）
    const globalActions = document.getElementById('globalActions');
    if (globalActions) {
        globalActions.classList.add('hidden');
    }
});

// 初始化导航
function initializeNavigation() {
    const navLinks = document.querySelectorAll('.nav-link[data-section]');

    navLinks.forEach(link => {
        link.addEventListener('click', function(e) {
            e.preventDefault();
            const targetSection = this.dataset.section;
            showSection(targetSection);

            // 更新导航状态
            navLinks.forEach(l => l.classList.remove('active'));
            this.classList.add('active');
        });
    });
}

// 显示指定区域
function showSection(sectionName) {
    Object.keys(sections).forEach(key => {
        sections[key].classList.add('hidden');
    });

    if (sections[sectionName]) {
        sections[sectionName].classList.remove('hidden');
    }

    // 控制全局操作按钮的显示
    const globalActions = document.getElementById('globalActions');
    if (globalActions) {
        // Token管理页面隐藏全局操作按钮
        if (sectionName === 'tokens') {
            globalActions.classList.add('hidden');
        } else {
            globalActions.classList.remove('hidden');
        }
    }
}

// 初始化表单
function initializeForms() {
    // 服务配置表单
    document.getElementById('saveConfigBtn').addEventListener('click', saveConfig);
    document.getElementById('reloadConfigBtn').addEventListener('click', loadConfig);

    // Token管理
    document.getElementById('addTokenForm').addEventListener('submit', addToken);
    document.getElementById('authType').addEventListener('change', toggleIdcFields);
    document.getElementById('refreshTokensBtn').addEventListener('click', refreshTokenInfo);

    // 备份管理
    document.getElementById('createBackupBtn').addEventListener('click', createBackup);
    document.getElementById('refreshBackupsBtn').addEventListener('click', loadBackups);
}

// 切换IdC字段显示
function toggleIdcFields() {
    const authType = document.getElementById('authType').value;
    const idcFields = document.getElementById('idcFields');

    if (authType === 'IdC') {
        idcFields.classList.remove('hidden');
        document.getElementById('clientId').required = true;
        document.getElementById('clientSecret').required = true;
    } else {
        idcFields.classList.add('hidden');
        document.getElementById('clientId').required = false;
        document.getElementById('clientSecret').required = false;
        document.getElementById('clientId').value = '';
        document.getElementById('clientSecret').value = '';
    }
}

// 显示消息
function showMessage(message, type = 'info') {
    globalMessage.textContent = message;
    globalMessage.className = 'message ' + type;
    globalMessage.style.display = 'block';

    // 3秒后自动隐藏成功和信息消息
    if (type === 'success' || type === 'info') {
        setTimeout(() => {
            globalMessage.style.display = 'none';
        }, 3000);
    }
}

// 加载配置
async function loadConfig() {
    try {
        const response = await fetch('/api/config');
        if (!response.ok) {
            throw new Error('加载配置失败');
        }

        const config = await response.json();
        currentConfig = config;

        // 填充服务配置表单
        document.getElementById('port').value = config.serviceConfig.port;
        document.getElementById('ginMode').value = config.serviceConfig.ginMode;
        document.getElementById('clientToken').value = config.serviceConfig.clientToken;

        // 填充日志配置表单
        document.getElementById('logLevel').value = config.logConfig.level;
        document.getElementById('logFormat').value = config.logConfig.format;
        document.getElementById('logFile').value = config.logConfig.file || '';
        document.getElementById('logConsole').checked = config.logConfig.console;
        document.getElementById('logCaller').checked = config.logConfig.enableCaller;
        document.getElementById('callerSkip').value = config.logConfig.callerSkip;

        // 填充超时配置表单
        document.getElementById('requestTimeout').value = config.timeoutConfig.requestMinutes;
        document.getElementById('simpleRequestTimeout').value = config.timeoutConfig.simpleRequestMinutes;
        document.getElementById('streamTimeout').value = config.timeoutConfig.streamMinutes;
        document.getElementById('serverReadTimeout').value = config.timeoutConfig.serverReadMinutes;
        document.getElementById('serverWriteTimeout').value = config.timeoutConfig.serverWriteMinutes;

        showMessage('配置加载成功', 'success');
    } catch (error) {
        showMessage('加载配置失败: ' + error.message, 'error');
    }
}

// 保存配置
async function saveConfig() {
    try {
        // 收集表单数据
        const updatedConfig = {
            ...currentConfig,
            serviceConfig: {
                port: parseInt(document.getElementById('port').value),
                ginMode: document.getElementById('ginMode').value,
                clientToken: document.getElementById('clientToken').value
            },
            logConfig: {
                level: document.getElementById('logLevel').value,
                format: document.getElementById('logFormat').value,
                file: document.getElementById('logFile').value,
                console: document.getElementById('logConsole').checked,
                enableCaller: document.getElementById('logCaller').checked,
                callerSkip: parseInt(document.getElementById('callerSkip').value)
            },
            timeoutConfig: {
                requestMinutes: parseInt(document.getElementById('requestTimeout').value),
                simpleRequestMinutes: parseInt(document.getElementById('simpleRequestTimeout').value),
                streamMinutes: parseInt(document.getElementById('streamTimeout').value),
                serverReadMinutes: parseInt(document.getElementById('serverReadTimeout').value),
                serverWriteMinutes: parseInt(document.getElementById('serverWriteTimeout').value)
            }
        };

        const response = await fetch('/api/config', {
            method: 'PUT',
            headers: {
                'Content-Type': 'application/json',
            },
            body: JSON.stringify(updatedConfig)
        });

        const result = await response.json();
        if (result.success) {
            currentConfig = updatedConfig;
            showMessage('配置保存成功', 'success');
        } else {
            showMessage('配置保存失败: ' + result.error, 'error');
        }
    } catch (error) {
        showMessage('配置保存失败: ' + error.message, 'error');
    }
}

// 加载Token列表
async function loadTokens() {
    try {
        const response = await fetch('/api/tokens');
        if (!response.ok) {
            throw new Error('加载Token失败');
        }

        const tokens = await response.json();
        
        // 获取当前正在使用的token索引
        let currentIndex = -1;
        try {
            const currentResponse = await fetch('/api/tokens/current');
            if (currentResponse.ok) {
                const currentData = await currentResponse.json();
                currentIndex = currentData.currentIndex;
            }
        } catch (e) {
            console.warn('获取当前token索引失败:', e);
        }
        
        renderTokenList(tokens || [], currentIndex);
        updateStatistics(tokens || []);
    } catch (error) {
        showMessage('加载Token失败: ' + error.message, 'error');
        renderTokenList([], -1); // 确保错误情况下也能显示空列表
        updateStatistics([]);
    }
}

// 更新统计数据
function updateStatistics(tokens) {
    const stats = calculateStatistics(tokens);
    
    // 更新统计卡片
    const statsContainer = document.getElementById('tokenStats');
    if (!statsContainer) return;
    
    statsContainer.innerHTML = `
        <div class="stat-card">
            <div class="stat-icon">📊</div>
            <div class="stat-content">
                <div class="stat-label">总Token数</div>
                <div class="stat-value">${stats.totalTokens}</div>
            </div>
        </div>
        <div class="stat-card">
            <div class="stat-icon">✅</div>
            <div class="stat-content">
                <div class="stat-label">启用中</div>
                <div class="stat-value">${stats.activeTokens}</div>
            </div>
        </div>
        <div class="stat-card">
            <div class="stat-icon">🔢</div>
            <div class="stat-content">
                <div class="stat-label">总剩余次数</div>
                <div class="stat-value">${stats.totalRemaining}</div>
            </div>
        </div>
        <div class="stat-card ${stats.lowUsageTokens > 0 ? 'stat-warning' : ''}">
            <div class="stat-icon">⚠️</div>
            <div class="stat-content">
                <div class="stat-label">即将耗尽</div>
                <div class="stat-value">${stats.lowUsageTokens}</div>
            </div>
        </div>
        <div class="stat-card ${stats.errorTokens > 0 ? 'stat-error' : ''}">
            <div class="stat-icon">❌</div>
            <div class="stat-content">
                <div class="stat-label">有错误</div>
                <div class="stat-value">${stats.errorTokens}</div>
            </div>
        </div>
    `;
}

// 渲染Token列表
function renderTokenList(tokens, currentIndex = -1) {
    const tokenList = document.getElementById('tokenList');

    if (!tokens || tokens.length === 0) {
        tokenList.innerHTML = '<p style="text-align: center; color: #666; padding: 20px;">暂无Token配置</p>';
        return;
    }

    tokenList.innerHTML = tokens.map((token, index) => {
        const isCurrentToken = index === currentIndex;
        // 格式化剩余次数显示
        let remainingDisplay = '-';
        if (token.remainingUsage !== undefined && token.remainingUsage !== null) {
            if (token.remainingUsage === 0) {
                remainingDisplay = '<span style="color: #e74c3c;">0 (已耗尽)</span>';
            } else if (token.remainingUsage < 10) {
                remainingDisplay = `<span style="color: #f39c12;">${token.remainingUsage.toFixed(1)} (即将耗尽)</span>`;
            } else {
                remainingDisplay = `<span style="color: #27ae60;">${token.remainingUsage.toFixed(1)}</span>`;
            }
        }

        return `
        <div class="token-item ${!token.enabled ? 'disabled' : ''} ${isCurrentToken ? 'current-token' : ''}">
            <div class="token-header">
                <div class="token-title">
                    ${token.description || '未命名Token'}
                    ${isCurrentToken ? '<span class="current-badge">🔥 使用中</span>' : ''}
                </div>
                <div class="token-status ${token.enabled ? 'enabled' : 'disabled'}">
                    ${token.enabled ? '启用' : '禁用'}
                </div>
            </div>
            <div class="token-details">
                <div class="token-detail">
                    <label>用户ID:</label>
                    <span>${token.userId || token.id || index}</span>
                </div>
                <div class="token-detail">
                    <label>用户邮箱:</label>
                    <span>${maskEmail(token.userEmail || '未知')}</span>
                </div>
                <div class="token-detail">
                    <label>认证方式:</label>
                    <span>${token.auth}</span>
                </div>
                <div class="token-detail">
                    <label>刷新Token:</label>
                    <span>${maskToken(token.refreshToken)}</span>
                </div>
                ${token.auth === 'IdC' ? `
                    <div class="token-detail">
                        <label>客户端ID:</label>
                        <span>${token.clientId || '-'}</span>
                    </div>
                    <div class="token-detail">
                        <label>客户端密钥:</label>
                        <span>${maskToken(token.clientSecret)}</span>
                    </div>
                ` : ''}
                <div class="token-detail">
                    <label>剩余次数:</label>
                    <span>${remainingDisplay}</span>
                </div>
                <div class="token-detail">
                    <label>最后使用:</label>
                    <span>${token.lastUsed ? new Date(token.lastUsed).toLocaleString('zh-CN') : '从未使用'}</span>
                </div>
                <div class="token-detail">
                    <label>错误次数:</label>
                    <span style="color: ${token.errorCount > 0 ? '#e74c3c' : '#95a5a6'};">${token.errorCount || 0}</span>
                </div>
            </div>
            <div class="token-actions">
                <button class="btn btn-info btn-small" onclick="refreshSingleToken('${token.id}', this)" title="刷新此Token信息">🔄</button>
                ${token.enabled && !isCurrentToken ?
                    `<button class="btn btn-primary btn-small" onclick="switchToToken(${index})" title="切换到此Token">切换使用</button>` :
                    ''
                }
                ${!token.enabled ?
                    `<button class="btn btn-success btn-small" onclick="toggleToken('${token.id}', true)">启用</button>` :
                    `<button class="btn btn-secondary btn-small" onclick="toggleToken('${token.id}', false)">禁用</button>`
                }
                <button class="btn btn-danger btn-small" onclick="deleteToken('${token.id}')">删除</button>
            </div>
        </div>
    `;
    }).join('');
}

// 隐藏Token（只显示前后几位）
function maskToken(token) {
    if (!token || token.length <= 8) return token;
    return token.substring(0, 4) + '****' + token.substring(token.length - 4);
}

// 隐藏邮箱（只显示前几位和域名）
function maskEmail(email) {
    if (!email || email === '未知' || email === '已禁用' || email === '获取失败') {
        return email;
    }
    const parts = email.split('@');
    if (parts.length !== 2) return email;
    
    const username = parts[0];
    const domain = parts[1];
    
    if (username.length <= 3) {
        return username[0] + '***@' + domain;
    }
    return username.substring(0, 3) + '***@' + domain;
}

// 计算统计数据
function calculateStatistics(tokens) {
    let totalRemaining = 0;
    let activeCount = 0;
    let errorCount = 0;
    let lowUsageCount = 0;
    
    tokens.forEach(token => {
        if (token.enabled) {
            activeCount++;
            if (token.remainingUsage !== undefined && token.remainingUsage !== null) {
                totalRemaining += token.remainingUsage;
                if (token.remainingUsage > 0 && token.remainingUsage < 10) {
                    lowUsageCount++;
                }
            }
            if (token.errorCount > 0) {
                errorCount++;
            }
        }
    });
    
    return {
        totalTokens: tokens.length,
        activeTokens: activeCount,
        totalRemaining: totalRemaining.toFixed(1),
        errorTokens: errorCount,
        lowUsageTokens: lowUsageCount
    };
}

// 添加Token
async function addToken(e) {
    e.preventDefault();

    const formData = new FormData(e.target);
    const tokenData = {
        auth: formData.get('auth'),
        refreshToken: formData.get('refreshToken'),
        description: formData.get('description') || ''
    };

    if (tokenData.auth === 'IdC') {
        tokenData.clientId = formData.get('clientId');
        tokenData.clientSecret = formData.get('clientSecret');
    }

    try {
        const response = await fetch('/api/tokens', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
            },
            body: JSON.stringify(tokenData)
        });

        const result = await response.json();
        if (result.success) {
            showMessage('Token添加成功', 'success');
            e.target.reset();
            toggleIdcFields();
            loadTokens();
        } else {
            showMessage('Token添加失败: ' + result.error, 'error');
        }
    } catch (error) {
        showMessage('Token添加失败: ' + error.message, 'error');
    }
}

// 切换Token状态
async function toggleToken(tokenId, enabled) {
    try {
        const response = await fetch(`/api/tokens?id=${tokenId}`, {
            method: 'PUT',
            headers: {
                'Content-Type': 'application/json',
            },
            body: JSON.stringify({ enabled: enabled })
        });

        const result = await response.json();
        if (result.success) {
            showMessage(`Token已${enabled ? '启用' : '禁用'}`, 'success');
            // 等待一下再刷新，确保配置已保存
            setTimeout(() => {
                loadTokens();
            }, 500);
        } else {
            showMessage('Token状态更新失败: ' + result.error, 'error');
        }
    } catch (error) {
        showMessage('Token状态更新失败: ' + error.message, 'error');
        console.error('Toggle token error:', error);
    }
}

// 删除Token
async function deleteToken(tokenId) {
    if (!confirm('确定要删除这个Token吗？')) {
        return;
    }

    try {
        const response = await fetch(`/api/tokens?id=${tokenId}`, {
            method: 'DELETE'
        });

        const result = await response.json();
        if (result.success) {
            showMessage('Token删除成功', 'success');
            loadTokens();
        } else {
            showMessage('Token删除失败: ' + result.error, 'error');
        }
    } catch (error) {
        showMessage('Token删除失败: ' + error.message, 'error');
    }
}

// 刷新单个Token信息
async function refreshSingleToken(tokenId, btnElement) {
    const originalText = btnElement.textContent;
    
    try {
        btnElement.disabled = true;
        btnElement.textContent = '🔄';
        
        const response = await fetch(`/api/tokens/refresh-single?id=${tokenId}`, {
            method: 'POST'
        });
        
        const result = await response.json();
        if (result.success) {
            // 等待1秒后重新加载Token列表
            setTimeout(() => {
                loadTokens();
            }, 1000);
        } else {
            showMessage('刷新失败: ' + result.error, 'error');
        }
    } catch (error) {
        showMessage('刷新失败: ' + error.message, 'error');
    } finally {
        setTimeout(() => {
            btnElement.disabled = false;
            btnElement.textContent = originalText;
        }, 1000);
    }
}

// 刷新Token信息
async function refreshTokenInfo() {
    const btn = document.getElementById('refreshTokensBtn');
    const originalText = btn.textContent;
    
    try {
        btn.disabled = true;
        btn.textContent = '🔄 刷新中...';
        
        const response = await fetch('/api/tokens/refresh', {
            method: 'POST'
        });
        
        const result = await response.json();
        if (result.success) {
            showMessage('Token信息刷新已启动，请稍候...', 'info');
            
            // 等待2秒后重新加载Token列表
            setTimeout(() => {
                loadTokens();
            }, 2000);
        } else {
            showMessage('刷新失败: ' + result.error, 'error');
        }
    } catch (error) {
        showMessage('刷新失败: ' + error.message, 'error');
    } finally {
        setTimeout(() => {
            btn.disabled = false;
            btn.textContent = originalText;
        }, 2000);
    }
}

// 创建备份
async function createBackup() {
    try {
        const response = await fetch('/api/backup', {
            method: 'POST'
        });

        const result = await response.json();
        if (result.success) {
            showMessage('备份创建成功', 'success');
            loadBackups();
        } else {
            showMessage('备份创建失败: ' + result.error, 'error');
        }
    } catch (error) {
        showMessage('备份创建失败: ' + error.message, 'error');
    }
}

// 加载备份列表
async function loadBackups() {
    try {
        const response = await fetch('/api/backup');
        if (!response.ok) {
            throw new Error('加载备份列表失败');
        }

        const backups = await response.json();
        renderBackupList(backups || []);
    } catch (error) {
        showMessage('加载备份列表失败: ' + error.message, 'error');
        renderBackupList([]); // 确保错误情况下也能显示空列表
    }
}

// 渲染备份列表
function renderBackupList(backups) {
    const backupList = document.getElementById('backupList');

    if (!backups || backups.length === 0) {
        backupList.innerHTML = '<p style="text-align: center; color: #666; padding: 20px;">暂无备份文件</p>';
        return;
    }

    backupList.innerHTML = backups.map(backup => `
        <div class="backup-item">
            <div class="backup-name">${backup}</div>
            <div class="backup-actions">
                <button class="btn btn-secondary btn-small" onclick="restoreBackup('${backup}')">恢复</button>
            </div>
        </div>
    `).join('');
}

// 切换到指定Token
async function switchToToken(index) {
    if (!confirm('确定要切换到此Token吗？')) {
        return;
    }
    
    try {
        const response = await fetch('/api/tokens/switch', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
            },
            body: JSON.stringify({ index: index })
        });
        
        const result = await response.json();
        if (result.success) {
            showMessage('Token切换成功', 'success');
            // 等待一下再刷新，确保切换已生效
            setTimeout(() => {
                loadTokens();
            }, 500);
        } else {
            showMessage('Token切换失败: ' + result.error, 'error');
        }
    } catch (error) {
        showMessage('Token切换失败: ' + error.message, 'error');
    }
}

// 恢复备份
async function restoreBackup(backupFile) {
    if (!confirm(`确定要恢复备份文件 "${backupFile}" 吗？\n这将覆盖当前配置！`)) {
        return;
    }

    try {
        const response = await fetch('/api/restore', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
            },
            body: JSON.stringify({ backupFile: backupFile })
        });

        const result = await response.json();
        if (result.success) {
            showMessage('配置恢复成功', 'success');
            loadConfig();
            loadTokens();
        } else {
            showMessage('配置恢复失败: ' + result.error, 'error');
        }
    } catch (error) {
        showMessage('配置恢复失败: ' + error.message, 'error');
    }
}