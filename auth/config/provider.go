package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// AuthConfig 认证配置结构 (避免循环导入)
type AuthConfig struct {
	ID           string `json:"id,omitempty"`
	AuthType     string `json:"auth"`
	RefreshToken string `json:"refreshToken"`
	ClientID     string `json:"ClientId,omitempty"`
	ClientSecret string `json:"ClientSecret,omitempty"`
	Disabled     bool   `json:"disabled,omitempty"`
}

// 认证方法常量 (避免循环导入)
const (
	AuthMethodSocial = "Social"
	AuthMethodIdC    = "IdC"
)

// ConfigProvider 配置提供者接口
type ConfigProvider interface {
	LoadConfigs() ([]AuthConfig, error)
	ValidateConfig(config AuthConfig) error
}

// DefaultConfigProvider 默认配置提供者实现
type DefaultConfigProvider struct {
	validator ConfigValidator
}

// ConfigValidator 配置验证器接口
type ConfigValidator interface {
	Validate(config AuthConfig) error
}

// NewDefaultConfigProvider 创建默认配置提供者
func NewDefaultConfigProvider() ConfigProvider {
	return &DefaultConfigProvider{
		validator: &StandardConfigValidator{},
	}
}

// LoadConfigs 从环境变量加载配置 (遵循DIP和YAGNI)
func (p *DefaultConfigProvider) LoadConfigs() ([]AuthConfig, error) {
	var allConfigs []AuthConfig

	// 1. 尝试从新的JSON配置格式加载 (KIRO_AUTH_TOKEN)
	if jsonData := os.Getenv("KIRO_AUTH_TOKEN"); jsonData != "" {
		jsonConfigs, err := p.parseJSONConfig(jsonData)
		if err != nil {
			return nil, fmt.Errorf("解析KIRO_AUTH_TOKEN失败: %v", err)
		}
		allConfigs = append(allConfigs, jsonConfigs...)
	}

	// 2. 继续从传统环境变量方式加载 (向后兼容)
	legacyConfigs, err := p.loadFromLegacyEnvVars()
	if err == nil {
		allConfigs = append(allConfigs, legacyConfigs...)
	}

	// 3. 检查是否有任何配置
	if len(allConfigs) == 0 {
		return nil, fmt.Errorf("未找到有效的认证配置，请设置KIRO_AUTH_TOKEN或传统环境变量")
	}

	return p.processConfigs(allConfigs), nil
}

// parseJSONConfig 解析JSON配置 (支持单个对象或数组，以及文件路径)
func (p *DefaultConfigProvider) parseJSONConfig(jsonData string) ([]AuthConfig, error) {
	var configs []AuthConfig

	// 尝试解析为数组
	if err := json.Unmarshal([]byte(jsonData), &configs); err != nil {
		// 尝试解析为单个对象
		var single AuthConfig
		if err := json.Unmarshal([]byte(jsonData), &single); err != nil {
			// JSON解析失败，尝试作为文件路径读取
			fileConfigs, fileErr := p.parseJSONConfigFromFile(jsonData)
			if fileErr != nil {
				return nil, fmt.Errorf("JSON格式无效，既不是对象也不是数组: %v，也不是有效的文件路径: %v", err, fileErr)
			}
			return fileConfigs, nil
		}
		configs = []AuthConfig{single}
	}

	return configs, nil
}

// parseJSONConfigFromFile 从文件读取JSON配置
func (p *DefaultConfigProvider) parseJSONConfigFromFile(filePath string) ([]AuthConfig, error) {
	// 读取文件内容
	fileData, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("读取配置文件失败: %v", err)
	}

	var configs []AuthConfig

	// 尝试解析文件内容为数组
	if err := json.Unmarshal(fileData, &configs); err != nil {
		// 尝试解析为单个对象
		var single AuthConfig
		if err := json.Unmarshal(fileData, &single); err != nil {
			return nil, fmt.Errorf("文件中的JSON格式无效: %v", err)
		}
		configs = []AuthConfig{single}
	}

	return configs, nil
}

// loadFromLegacyEnvVars 从传统环境变量加载 (向后兼容)
func (p *DefaultConfigProvider) loadFromLegacyEnvVars() ([]AuthConfig, error) {
	var configs []AuthConfig

	// 检查Social认证方式
	if socialToken := os.Getenv("AWS_REFRESHTOKEN"); socialToken != "" {
		// 支持多个token，用逗号分隔
		tokens := strings.Split(socialToken, ",")
		for i, token := range tokens {
			token = strings.TrimSpace(token)
			if token != "" {
				configs = append(configs, AuthConfig{
					ID:           fmt.Sprintf("social_%d", i),
					AuthType:     AuthMethodSocial,
					RefreshToken: token,
				})
			}
		}
	}

	// 检查IdC认证方式
	if idcToken := os.Getenv("IDC_REFRESH_TOKEN"); idcToken != "" {
		clientID := os.Getenv("IDC_CLIENT_ID")
		clientSecret := os.Getenv("IDC_CLIENT_SECRET")

		if clientID == "" || clientSecret == "" {
			return nil, fmt.Errorf("IdC认证需要设置IDC_CLIENT_ID和IDC_CLIENT_SECRET")
		}

		// 支持多个IDC token，用逗号分隔
		tokens := strings.Split(idcToken, ",")
		for i, token := range tokens {
			token = strings.TrimSpace(token)
			if token != "" {
				configs = append(configs, AuthConfig{
					ID:           fmt.Sprintf("idc_%d", i),
					AuthType:     AuthMethodIdC,
					RefreshToken: token,
					ClientID:     clientID,
					ClientSecret: clientSecret,
				})
			}
		}
	}

	// 如果没有找到任何传统环境变量配置，返回空数组（不是错误）
	// 因为可能只使用JSON配置方式
	return configs, nil
}

// processConfigs 处理配置：去重、生成ID、验证
func (p *DefaultConfigProvider) processConfigs(configs []AuthConfig) []AuthConfig {
	// 1. 去重 (基于AuthType + RefreshToken)
	configs = p.deduplicateConfigs(configs)

	// 2. 生成ID (如果未设置)
	configs = p.generateConfigIDs(configs)

	// 3. 验证配置
	var validConfigs []AuthConfig
	for _, config := range configs {
		if err := p.ValidateConfig(config); err != nil {
			// 跳过无效配置，记录但不中断
			continue
		}
		validConfigs = append(validConfigs, config)
	}

	return validConfigs
}

// deduplicateConfigs 配置去重 (遵循token.md需求)
func (p *DefaultConfigProvider) deduplicateConfigs(configs []AuthConfig) []AuthConfig {
	seen := make(map[string]bool)
	var unique []AuthConfig

	for _, config := range configs {
		// 生成唯一键：认证类型 + refresh token
		key := config.AuthType + ":" + config.RefreshToken
		if config.ClientID != "" {
			key += ":" + config.ClientID // IdC认证包含ClientID
		}

		if !seen[key] {
			seen[key] = true
			unique = append(unique, config)
		}
	}

	return unique
}

// generateConfigIDs 为配置生成ID
func (p *DefaultConfigProvider) generateConfigIDs(configs []AuthConfig) []AuthConfig {
	typeCounters := make(map[string]int)

	for i := range configs {
		if configs[i].ID == "" {
			authType := strings.ToLower(configs[i].AuthType)
			typeCounters[authType]++
			configs[i].ID = fmt.Sprintf("%s_%d", authType, typeCounters[authType])
		}
	}

	return configs
}

// ValidateConfig 验证单个配置
func (p *DefaultConfigProvider) ValidateConfig(config AuthConfig) error {
	return p.validator.Validate(config)
}

// StandardConfigValidator 标准配置验证器
type StandardConfigValidator struct{}

// Validate 验证配置有效性
func (v *StandardConfigValidator) Validate(config AuthConfig) error {
	// 基础字段验证
	if config.AuthType == "" {
		return fmt.Errorf("AuthType不能为空")
	}

	if config.RefreshToken == "" {
		return fmt.Errorf("RefreshToken不能为空")
	}

	// 特定认证类型验证
	switch config.AuthType {
	case AuthMethodSocial:
		// Social认证只需要RefreshToken
		return nil

	case AuthMethodIdC:
		// IdC认证需要额外的ClientID和ClientSecret
		if config.ClientID == "" {
			return fmt.Errorf("IdC认证需要ClientID")
		}
		if config.ClientSecret == "" {
			return fmt.Errorf("IdC认证需要ClientSecret")
		}
		return nil

	default:
		return fmt.Errorf("不支持的认证类型: %s", config.AuthType)
	}
}
