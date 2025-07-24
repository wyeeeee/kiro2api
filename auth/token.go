package auth

import (
	"fmt"
	"kiro2api/config"
	"kiro2api/logger"
	"kiro2api/types"
	"os"
	"path/filepath"
	"runtime"

	"github.com/bytedance/sonic"
	"github.com/valyala/fasthttp"
)

var fasthttpClient = &fasthttp.Client{}

// getTokenFilePath 获取跨平台的token文件路径
func getTokenFilePath() string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		logger.Error("获取用户目录失败", logger.Err(err))
		os.Exit(1)
	}

	return filepath.Join(homeDir, ".aws", "sso", "cache", "kiro-auth-token.json")
}

// ReadToken 读取并显示token信息
func ReadToken() {
	tokenPath := getTokenFilePath()

	data, err := os.ReadFile(tokenPath)
	if err != nil {
		logger.Error("读取token文件失败", logger.Err(err), logger.String("path", tokenPath))
		os.Exit(1)
	}

	var token types.TokenData
	if err := sonic.Unmarshal(data, &token); err != nil {
		logger.Error("解析token文件失败", logger.Err(err))
		os.Exit(1)
	}

	logger.Println("Token信息:")
	logger.Printf("Access Token: %s\n", token.AccessToken)
	logger.Printf("Refresh Token: %s\n", token.RefreshToken)
	if token.ExpiresAt != "" {
		logger.Printf("过期时间: %s\n", token.ExpiresAt)
	}
}

// RefreshToken 刷新token
func RefreshToken() {
	tokenPath := getTokenFilePath()

	// 读取当前token
	data, err := os.ReadFile(tokenPath)
	if err != nil {
		logger.Error("读取token文件失败", logger.Err(err), logger.String("path", tokenPath))
		os.Exit(1)
	}

	var currentToken types.TokenData
	if err := sonic.Unmarshal(data, &currentToken); err != nil {
		logger.Error("解析token文件失败", logger.Err(err))
		os.Exit(1)
	}

	// 准备刷新请求
	refreshReq := types.RefreshRequest{
		RefreshToken: currentToken.RefreshToken,
	}

	reqBody, err := sonic.Marshal(refreshReq)
	if err != nil {
		logger.Error("序列化请求失败", logger.Err(err))
		os.Exit(1)
	}

	logger.Debug("发送token刷新请求", logger.String("url", "https://prod.us-east-1.auth.desktop.kiro.dev/refreshToken"))

	// 发送刷新请求
	req := fasthttp.AcquireRequest()
	defer fasthttp.ReleaseRequest(req)
	req.SetRequestURI("https://prod.us-east-1.auth.desktop.kiro.dev/refreshToken")
	req.Header.SetMethod(fasthttp.MethodPost)
	req.Header.SetContentType("application/json")
	req.SetBody(reqBody)

	resp := fasthttp.AcquireResponse()
	defer fasthttp.ReleaseResponse(resp)

	if err := fasthttpClient.Do(req, resp); err != nil {
		logger.Error("刷新token请求失败", logger.Err(err))
		os.Exit(1)
	}

	if resp.StatusCode() != fasthttp.StatusOK {
		logger.Error("刷新token失败",
			logger.Int("status_code", resp.StatusCode()),
			logger.String("response", string(resp.Body())))
		os.Exit(1)
	}

	// 解析响应
	var refreshResp types.RefreshResponse
	if err := sonic.Unmarshal(resp.Body(), &refreshResp); err != nil {
		logger.Error("解析刷新响应失败", logger.Err(err))
		os.Exit(1)
	}

	// 更新token文件
	newToken := types.TokenData(refreshResp)

	newData, err := sonic.MarshalIndent(newToken, "", "  ")
	if err != nil {
		logger.Error("序列化新token失败", logger.Err(err))
		os.Exit(1)
	}

	if err := os.WriteFile(tokenPath, newData, 0600); err != nil {
		logger.Error("写入token文件失败", logger.Err(err), logger.String("path", tokenPath))
		os.Exit(1)
	}

	logger.Info("Token刷新成功")
	logger.Debug("新的Access Token", logger.String("access_token", newToken.AccessToken))
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

// ExportEnvVars 导出环境变量
func ExportEnvVars() {
	// 获取实际的token
	token, err := GetToken()
	if err != nil {
		logger.Error("获取token失败", logger.Err(err))
		logger.Printf("使用默认token: %s", config.DefaultAuthToken)
		token.AccessToken = config.DefaultAuthToken
	}

	// 根据操作系统输出不同格式的环境变量设置命令
	if runtime.GOOS == "windows" {
		logger.Println("CMD")
		logger.Printf("set ANTHROPIC_BASE_URL=http://localhost:8080\n")
		logger.Printf("set ANTHROPIC_AUTH_TOKEN=%s\n\n", token.AccessToken)
		logger.Println("Powershell")
		logger.Println(`$env:ANTHROPIC_BASE_URL="http://localhost:8080"`)
		logger.Printf(`$env:ANTHROPIC_AUTH_TOKEN="%s"`, token.AccessToken)
	} else {
		logger.Printf("export ANTHROPIC_BASE_URL=http://localhost:8080\n")
		logger.Printf("export ANTHROPIC_AUTH_TOKEN=\"%s\"\n", token.AccessToken)
	}
}

// GenerateAuthToken 显示认证令牌
func GenerateAuthToken() {
	// 获取实际的token
	token, err := GetToken()
	if err != nil {
		logger.Error("获取token失败", logger.Err(err))
		logger.Printf("使用默认token: %s", config.DefaultAuthToken)
		token.AccessToken = config.DefaultAuthToken
	}

	logger.Printf("AuthToken: %s\n", token.AccessToken)
	logger.Println("\n使用方法:")
	logger.Printf("  kiro2api server 8080 %s\n", token.AccessToken)
	logger.Println("\n或者设置环境变量:")
	if runtime.GOOS == "windows" {
		logger.Println("CMD:")
		logger.Printf("set ANTHROPIC_BASE_URL=http://localhost:8080\n")
		logger.Printf("set ANTHROPIC_AUTH_TOKEN=%s\n\n", token.AccessToken)
		logger.Println("Powershell:")
		logger.Println(`$env:ANTHROPIC_BASE_URL="http://localhost:8080"`)
		logger.Printf(`$env:ANTHROPIC_AUTH_TOKEN="%s"`, token.AccessToken)
	} else {
		logger.Printf("export ANTHROPIC_BASE_URL=http://localhost:8080\n")
		logger.Printf("export ANTHROPIC_AUTH_TOKEN=\"%s\"\n", token.AccessToken)
	}
}
