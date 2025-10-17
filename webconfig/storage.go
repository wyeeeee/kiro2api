package webconfig

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	configFileName = "config.json"
)

// Storage 配置存储管理器
type Storage struct {
	configPath string
	mutex      sync.RWMutex
}

// NewStorage 创建新的存储管理器
func NewStorage() *Storage {
	// 确保配置目录存在
	configDir := filepath.Join("webconfig", "data")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		panic(fmt.Sprintf("无法创建配置目录: %v", err))
	}

	configPath := filepath.Join(configDir, configFileName)
	return &Storage{
		configPath: configPath,
	}
}

// LoadConfig 加载配置文件
func (s *Storage) LoadConfig() (*WebConfig, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	// 检查文件是否存在
	if _, err := os.Stat(s.configPath); os.IsNotExist(err) {
		// 文件不存在，返回默认配置
		return GetDefaultConfig(), nil
	}

	// 读取文件
	data, err := os.ReadFile(s.configPath)
	if err != nil {
		return nil, fmt.Errorf("读取配置文件失败: %w", err)
	}

	// 解析JSON
	var config WebConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("解析配置文件失败: %w", err)
	}

	// 验证配置
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("配置验证失败: %w", err)
	}

	return &config, nil
}

// SaveConfig 保存配置文件
func (s *Storage) SaveConfig(config *WebConfig) error {
	if err := config.Validate(); err != nil {
		return fmt.Errorf("配置验证失败: %w", err)
	}

	s.mutex.Lock()
	defer s.mutex.Unlock()

	// 更新时间戳
	config.UpdatedAt = time.Now()

	// 序列化JSON
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化配置失败: %w", err)
	}

	// 创建临时文件
	tempPath := s.configPath + ".tmp"
	if err := os.WriteFile(tempPath, data, 0644); err != nil {
		return fmt.Errorf("写入临时配置文件失败: %w", err)
	}

	// 原子性替换文件
	if err := os.Rename(tempPath, s.configPath); err != nil {
		// 清理临时文件
		os.Remove(tempPath)
		return fmt.Errorf("保存配置文件失败: %w", err)
	}

	return nil
}

// IsFirstRun 检查是否是首次运行
func (s *Storage) IsFirstRun() bool {
	_, err := os.Stat(s.configPath)
	return os.IsNotExist(err)
}

// BackupConfig 备份配置文件
func (s *Storage) BackupConfig() error {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	if _, err := os.Stat(s.configPath); os.IsNotExist(err) {
		return NewConfigError("配置文件不存在，无法备份")
	}

	// 生成备份文件名
	timestamp := time.Now().Format("20060102_150405")
	backupPath := filepath.Join(filepath.Dir(s.configPath), fmt.Sprintf("config_backup_%s.json", timestamp))

	// 复制文件
	data, err := os.ReadFile(s.configPath)
	if err != nil {
		return fmt.Errorf("读取配置文件失败: %w", err)
	}

	if err := os.WriteFile(backupPath, data, 0644); err != nil {
		return fmt.Errorf("创建备份文件失败: %w", err)
	}

	return nil
}

// ListBackups 列出所有备份文件
func (s *Storage) ListBackups() ([]string, error) {
	dir := filepath.Dir(s.configPath)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("读取配置目录失败: %w", err)
	}

	backups := make([]string, 0)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if len(name) > len("config_backup_") && name[:len("config_backup_")] == "config_backup_" &&
		   name[len(name)-5:] == ".json" {
			backups = append(backups, name)
		}
	}

	return backups, nil
}

// RestoreFromBackup 从备份恢复配置
func (s *Storage) RestoreFromBackup(backupFile string) error {
	backupPath := filepath.Join(filepath.Dir(s.configPath), backupFile)

	// 检查备份文件是否存在
	if _, err := os.Stat(backupPath); os.IsNotExist(err) {
		return NewConfigError("备份文件不存在: %s", backupFile)
	}

	// 读取备份文件
	data, err := os.ReadFile(backupPath)
	if err != nil {
		return fmt.Errorf("读取备份文件失败: %w", err)
	}

	// 验证备份配置
	var config WebConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("解析备份文件失败: %w", err)
	}

	if err := config.Validate(); err != nil {
		return fmt.Errorf("备份配置验证失败: %w", err)
	}

	s.mutex.Lock()
	defer s.mutex.Unlock()

	// 先备份当前配置
	if err := s.createAutoBackup(); err != nil {
		return fmt.Errorf("自动备份当前配置失败: %w", err)
	}

	// 恢复配置
	if err := os.WriteFile(s.configPath, data, 0644); err != nil {
		return fmt.Errorf("恢复配置失败: %w", err)
	}

	return nil
}

// createAutoBackup 创建自动备份
func (s *Storage) createAutoBackup() error {
	timestamp := time.Now().Format("20060102_150405")
	autoBackupPath := filepath.Join(filepath.Dir(s.configPath), fmt.Sprintf("auto_backup_%s.json", timestamp))

	data, err := os.ReadFile(s.configPath)
	if err != nil {
		return err
	}

	return os.WriteFile(autoBackupPath, data, 0644)
}

// GetConfigPath 获取配置文件路径
func (s *Storage) GetConfigPath() string {
	return s.configPath
}