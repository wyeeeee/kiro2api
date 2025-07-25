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

	"github.com/bytedance/sonic"
)

// 移除全局httpClient，使用utils包中的共享客户端

// RefreshTokenForServer 刷新token，用于服务器模式，返回错误而不是退出程序
func RefreshTokenForServer() error {
	_, err := refreshTokenAndReturn()
	return err
}

// refreshTokenAndReturn 刷新token并返回TokenInfo
func refreshTokenAndReturn() (types.TokenInfo, error) {
	// 仅从环境变量获取refreshToken
	refreshToken := os.Getenv("AWS_REFRESHTOKEN")
	if refreshToken == "" {
		logger.Error("AWS_REFRESHTOKEN环境变量未设置")
		return types.TokenInfo{}, fmt.Errorf("AWS_REFRESHTOKEN环境变量未设置，请设置后重新启动服务")
	}

	logger.Debug("使用环境变量AWS_REFRESHTOKEN进行token刷新")
	currentToken := types.TokenInfo{
		RefreshToken: refreshToken,
	}

	// 准备刷新请求
	refreshReq := types.RefreshRequest{
		RefreshToken: currentToken.RefreshToken,
	}

	reqBody, err := sonic.Marshal(refreshReq)
	if err != nil {
		logger.Error("序列化请求失败", logger.Err(err))
		return types.TokenInfo{}, fmt.Errorf("序列化请求失败: %v", err)
	}

	logger.Debug("发送token刷新请求", logger.String("url", config.RefreshTokenURL))

	// 发送刷新请求
	req, err := http.NewRequest("POST", config.RefreshTokenURL, bytes.NewBuffer(reqBody))
	if err != nil {
		logger.Error("创建请求失败", logger.Err(err))
		return types.TokenInfo{}, fmt.Errorf("创建请求失败: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := utils.SharedHTTPClient.Do(req)
	if err != nil {
		logger.Error("刷新token请求失败", logger.Err(err))
		return types.TokenInfo{}, fmt.Errorf("刷新token请求失败: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		logger.Error("刷新token失败",
			logger.Int("status_code", resp.StatusCode),
			logger.String("response", string(body)))
		return types.TokenInfo{}, fmt.Errorf("刷新token失败: 状态码 %d, 响应: %s", resp.StatusCode, string(body))
	}

	// 解析响应
	var refreshResp types.TokenInfo
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		logger.Error("读取响应失败", logger.Err(err))
		return types.TokenInfo{}, fmt.Errorf("读取响应失败: %v", err)
	}

	if err := sonic.Unmarshal(body, &refreshResp); err != nil {
		logger.Error("解析刷新响应失败", logger.Err(err))
		return types.TokenInfo{}, fmt.Errorf("解析刷新响应失败: %v", err)
	}

	logger.Info("Token刷新成功")
	logger.Debug("新的Access Token", logger.String("access_token", refreshResp.AccessToken))
	
	// 返回包含有效AccessToken的TokenInfo
	return types.TokenInfo{
		RefreshToken: refreshToken,
		AccessToken:  refreshResp.AccessToken,
	}, nil
}

// GetToken 获取当前token，仅从环境变量获取，如果AccessToken为空则自动刷新
func GetToken() (types.TokenInfo, error) {
	return refreshTokenAndReturn()
}
