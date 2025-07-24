package auth

import (
	"kiro2api/logger"
	"kiro2api/utils"
	"os"
	"path/filepath"

	"github.com/bytedance/sonic"
)

// SetClaude 设置Claude配置以绕过地区限制
func SetClaude() {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		logger.Error("获取用户目录失败", logger.Err(err))
		os.Exit(1)
	}

	claudeJsonPath := filepath.Join(homeDir, ".claude.json")
	ok, _ := utils.FileExists(claudeJsonPath)
	if !ok {
		logger.Error("未找到Claude配置文件，请确认是否已安装 Claude Code")
		logger.Println("npm install -g @anthropic-ai/claude-code")
		os.Exit(1)
	}

	logger.Debug("读取Claude配置文件", logger.String("path", claudeJsonPath))

	data, err := os.ReadFile(claudeJsonPath)
	if err != nil {
		logger.Error("读取Claude文件失败", logger.Err(err), logger.String("path", claudeJsonPath))
		os.Exit(1)
	}

	var jsonData map[string]any

	err = sonic.Unmarshal(data, &jsonData)

	if err != nil {
		logger.Error("解析JSON文件失败", logger.Err(err))
		os.Exit(1)
	}

	jsonData["hasCompletedOnboarding"] = true
	jsonData["kiro2api"] = true

	newJson, err := sonic.MarshalIndent(jsonData, "", "  ")

	if err != nil {
		logger.Error("生成JSON文件失败", logger.Err(err))
		os.Exit(1)
	}

	err = os.WriteFile(claudeJsonPath, newJson, 0644)

	if err != nil {
		logger.Error("写入JSON文件失败", logger.Err(err), logger.String("path", claudeJsonPath))
		os.Exit(1)
	}

	logger.Info("Claude配置文件已更新", logger.String("path", claudeJsonPath))
}
