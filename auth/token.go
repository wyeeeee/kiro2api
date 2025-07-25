package auth

import (
	"bytes"
	"fmt"
	"io"
	"kiro2api/config"
	"kiro2api/logger"
	"kiro2api/types"
	"kiro2api/utils"
	"net/http"
	"os"
	"path/filepath"

	"github.com/bytedance/sonic"
)

// 移除全局httpClient，使用utils包中的共享客户端

// getTokenFilePath 获取跨平台的token文件路径
func getTokenFilePath() string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		logger.Error("获取用户目录失败", logger.Err(err))
		os.Exit(1)
	}

	return filepath.Join(homeDir, ".aws", "sso", "cache", "kiro-auth-token.json")
}

// RefreshTokenForServer 刷新token，用于服务器模式，返回错误而不是退出程序
func RefreshTokenForServer() error {
	tokenPath := getTokenFilePath()

	// 读取当前token
	data, err := os.ReadFile(tokenPath)
	if err != nil {
		logger.Error("读取token文件失败", logger.Err(err), logger.String("path", tokenPath))
		return fmt.Errorf("读取token文件失败: %v", err)
	}

	var currentToken types.TokenData
	if err := sonic.Unmarshal(data, &currentToken); err != nil {
		logger.Error("解析token文件失败", logger.Err(err))
		return fmt.Errorf("解析token文件失败: %v", err)
	}

	// 准备刷新请求
	refreshReq := types.RefreshRequest{
		RefreshToken: currentToken.RefreshToken,
	}

	reqBody, err := sonic.Marshal(refreshReq)
	if err != nil {
		logger.Error("序列化请求失败", logger.Err(err))
		return fmt.Errorf("序列化请求失败: %v", err)
	}

	logger.Debug("发送token刷新请求", logger.String("url", config.RefreshTokenURL))

	// 发送刷新请求
	req, err := http.NewRequest("POST", config.RefreshTokenURL, bytes.NewBuffer(reqBody))
	if err != nil {
		logger.Error("创建请求失败", logger.Err(err))
		return fmt.Errorf("创建请求失败: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := utils.SharedHTTPClient.Do(req)
	if err != nil {
		logger.Error("刷新token请求失败", logger.Err(err))
		return fmt.Errorf("刷新token请求失败: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		logger.Error("刷新token失败",
			logger.Int("status_code", resp.StatusCode),
			logger.String("response", string(body)))
		return fmt.Errorf("刷新token失败: 状态码 %d, 响应: %s", resp.StatusCode, string(body))
	}

	// 解析响应
	var refreshResp types.RefreshResponse
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		logger.Error("读取响应失败", logger.Err(err))
		return fmt.Errorf("读取响应失败: %v", err)
	}

	if err := sonic.Unmarshal(body, &refreshResp); err != nil {
		logger.Error("解析刷新响应失败", logger.Err(err))
		return fmt.Errorf("解析刷新响应失败: %v", err)
	}

	// 更新token文件
	newToken := types.TokenData(refreshResp)

	newData, err := sonic.MarshalIndent(newToken, "", "  ")
	if err != nil {
		logger.Error("序列化新token失败", logger.Err(err))
		return fmt.Errorf("序列化新token失败: %v", err)
	}

	if err := os.WriteFile(tokenPath, newData, 0600); err != nil {
		logger.Error("写入token文件失败", logger.Err(err), logger.String("path", tokenPath))
		return fmt.Errorf("写入token文件失败: %v", err)
	}

	logger.Info("Token刷新成功")
	logger.Debug("新的Access Token", logger.String("access_token", newToken.AccessToken))
	return nil
}

// GetToken 获取当前token
func GetToken() (types.TokenData, error) {
	tokenPath := getTokenFilePath()

	logger.Debug("读取token文件", logger.String("path", tokenPath))

	data, err := os.ReadFile(tokenPath)
	if err != nil {
		logger.Error("读取token文件失败", logger.Err(err), logger.String("path", tokenPath))
		return types.TokenData{}, fmt.Errorf("读取token文件失败: %v", err)
	}

	var token types.TokenData
	if err := sonic.Unmarshal(data, &token); err != nil {
		logger.Error("解析token文件失败", logger.Err(err))
		return types.TokenData{}, fmt.Errorf("解析token文件失败: %v", err)
	}

	logger.Debug("Token读取成功")
	return token, nil
}
