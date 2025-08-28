package auth

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// AuthConfig 简化的认证配置
type AuthConfig struct {
	AuthType     string `json:"auth"`
	RefreshToken string `json:"refreshToken"`
	ClientID     string `json:"clientId,omitempty"`
	ClientSecret string `json:"clientSecret,omitempty"`
	Disabled     bool   `json:"disabled,omitempty"`
}

// 认证方法常量
const (
	AuthMethodSocial = "Social"
	AuthMethodIdC    = "IdC"
)

// loadConfigs 从环境变量加载配置
func loadConfigs() ([]AuthConfig, error) {
	var allConfigs []AuthConfig

	// 1. 从KIRO_AUTH_TOKEN加载JSON配置
	if jsonData := os.Getenv("KIRO_AUTH_TOKEN"); jsonData != "" {
		jsonConfigs, err := parseJSONConfig(jsonData)
		if err != nil {
			return nil, fmt.Errorf("解析KIRO_AUTH_TOKEN失败: %v", err)
		}
		allConfigs = append(allConfigs, jsonConfigs...)
	}

	// 2. 从传统环境变量加载（向后兼容）
	if refreshToken := os.Getenv("REFRESH_TOKEN"); refreshToken != "" {
		allConfigs = append(allConfigs, AuthConfig{
			AuthType:     AuthMethodSocial,
			RefreshToken: refreshToken,
		})
	}

	// 3. 从AWS_REFRESHTOKEN环境变量加载（老版本兼容）
	if awsRefreshToken := os.Getenv("AWS_REFRESHTOKEN"); awsRefreshToken != "" {
		allConfigs = append(allConfigs, AuthConfig{
			AuthType:     AuthMethodSocial,
			RefreshToken: awsRefreshToken,
		})
	}

	// 4. 从IDC相关环境变量加载（老版本兼容）
	if idcRefreshToken := os.Getenv("IDC_REFRESH_TOKEN"); idcRefreshToken != "" {
		config := AuthConfig{
			AuthType:     AuthMethodIdC,
			RefreshToken: idcRefreshToken,
		}

		// 添加IdC认证所需的客户端信息
		if clientID := os.Getenv("IDC_CLIENT_ID"); clientID != "" {
			config.ClientID = clientID
		}
		if clientSecret := os.Getenv("IDC_CLIENT_SECRET"); clientSecret != "" {
			config.ClientSecret = clientSecret
		}

		allConfigs = append(allConfigs, config)
	}

	// 5. 从批量token环境变量加载
	if bulkTokens := os.Getenv("BULK_REFRESH_TOKENS"); bulkTokens != "" {
		tokens := strings.Split(bulkTokens, ",")
		for i, token := range tokens {
			if strings.TrimSpace(token) != "" {
				allConfigs = append(allConfigs, AuthConfig{
					AuthType:     AuthMethodSocial,
					RefreshToken: strings.TrimSpace(token),
				})
			}
			_ = i // 避免未使用变量警告
		}
	}

	if len(allConfigs) == 0 {
		return nil, fmt.Errorf("未找到有效的认证配置")
	}

	return processConfigs(allConfigs), nil
}

// GetConfigs 公开的配置获取函数，供其他包调用
func GetConfigs() ([]AuthConfig, error) {
	return loadConfigs()
}

// parseJSONConfig 解析JSON配置（支持文件路径）
func parseJSONConfig(jsonData string) ([]AuthConfig, error) {
	var configs []AuthConfig

	// 尝试解析为数组
	if err := json.Unmarshal([]byte(jsonData), &configs); err != nil {
		// 尝试解析为单个对象
		var single AuthConfig
		if err := json.Unmarshal([]byte(jsonData), &single); err != nil {
			// JSON解析失败，尝试作为文件路径读取
			fileConfigs, fileErr := parseJSONConfigFromFile(jsonData)
			if fileErr != nil {
				return nil, fmt.Errorf("JSON格式无效: %v，也不是有效的文件路径: %v", err, fileErr)
			}
			return fileConfigs, nil
		}
		configs = []AuthConfig{single}
	}

	return configs, nil
}

// parseJSONConfigFromFile 从文件读取JSON配置
func parseJSONConfigFromFile(filePath string) ([]AuthConfig, error) {
	// 检查文件是否存在
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return nil, fmt.Errorf("配置文件不存在: %s", filePath)
	}

	// 读取文件内容
	fileData, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("读取配置文件失败: %v", err)
	}

	var configs []AuthConfig

	// 尝试解析为数组
	if err := json.Unmarshal(fileData, &configs); err != nil {
		// 尝试解析为单个对象
		var single AuthConfig
		if err := json.Unmarshal(fileData, &single); err != nil {
			return nil, fmt.Errorf("解析配置文件JSON失败: %v", err)
		}
		configs = []AuthConfig{single}
	}

	return configs, nil
}

// processConfigs 处理和验证配置
func processConfigs(configs []AuthConfig) []AuthConfig {
	var validConfigs []AuthConfig

	for i, config := range configs {
		// 验证必要字段
		if config.RefreshToken == "" {
			continue
		}

		// 设置默认认证类型
		if config.AuthType == "" {
			config.AuthType = AuthMethodSocial
		}

		// 验证IdC认证的必要字段
		if config.AuthType == AuthMethodIdC {
			if config.ClientID == "" || config.ClientSecret == "" {
				continue
			}
		}

		// 跳过禁用的配置
		if config.Disabled {
			continue
		}

		validConfigs = append(validConfigs, config)
		_ = i // 避免未使用变量警告
	}

	return validConfigs
}
