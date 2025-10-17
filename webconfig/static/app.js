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
}

// 初始化表单
function initializeForms() {
    // 服务配置表单
    document.getElementById('saveConfigBtn').addEventListener('click', saveConfig);
    document.getElementById('reloadConfigBtn').addEventListener('click', loadConfig);

    // Token管理
    document.getElementById('addTokenForm').addEventListener('submit', addToken);
    document.getElementById('authType').addEventListener('change', toggleIdcFields);

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
        renderTokenList(tokens || []);
    } catch (error) {
        showMessage('加载Token失败: ' + error.message, 'error');
        renderTokenList([]); // 确保错误情况下也能显示空列表
    }
}

// 渲染Token列表
function renderTokenList(tokens) {
    const tokenList = document.getElementById('tokenList');

    if (!tokens || tokens.length === 0) {
        tokenList.innerHTML = '<p style="text-align: center; color: #666; padding: 20px;">暂无Token配置</p>';
        return;
    }

    tokenList.innerHTML = tokens.map(token => `
        <div class="token-item ${!token.enabled ? 'disabled' : ''}">
            <div class="token-header">
                <div class="token-title">${token.description || '未命名Token'}</div>
                <div class="token-status ${token.enabled ? 'enabled' : 'disabled'}">
                    ${token.enabled ? '启用' : '禁用'}
                </div>
            </div>
            <div class="token-details">
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
                    <label>最后使用:</label>
                    <span>${token.lastUsed ? new Date(token.lastUsed).toLocaleString() : '从未使用'}</span>
                </div>
                <div class="token-detail">
                    <label>错误次数:</label>
                    <span>${token.errorCount}</span>
                </div>
            </div>
            <div class="token-actions">
                ${token.enabled ?
                    `<button class="btn btn-secondary btn-small" onclick="toggleToken('${token.id}', false)">禁用</button>` :
                    `<button class="btn btn-success btn-small" onclick="toggleToken('${token.id}', true)">启用</button>`
                }
                <button class="btn btn-danger btn-small" onclick="deleteToken('${token.id}')">删除</button>
            </div>
        </div>
    `).join('');
}

// 隐藏Token（只显示前后几位）
function maskToken(token) {
    if (!token || token.length <= 8) return token;
    return token.substring(0, 4) + '****' + token.substring(token.length - 4);
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