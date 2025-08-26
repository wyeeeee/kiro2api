#重新设置token管理器
1.KIRO_AUTH_TOKEN环境变量支持json格式的配置文件或者是json格式的字符串，可以配置多个auth设置
2.支持 Social（默认）和 IdC 两种认证方式
3.原AWS_REFRESHTOKEN和IDC_CLIENT_ID、IDC_CLIENT_SECRET、配置方式兼容，启动时自动把相关内容添加到token管理器中
4.token管理器添加认证配置时要能去重
5.获取access token时支持轮询
6.不可用的token要标记为停用，轮询时不要轮询停用的token
#配置示例
```json
[{
  "Auth": "Social",  
  "RerfreshToken": "xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx",
},
{"Auth":"IdC",
 "RerfreshToken": "xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx",
"ClientId":"xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx",
"ClientSecret": "xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx",
}
]
```

#获取access token方法
```go
// tryRefreshIdcToken 使用IdC认证方式刷新token
func tryRefreshIdcToken(refreshToken string) (types.TokenInfo, error) {
	clientId := os.Getenv("IDC_CLIENT_ID")
	clientSecret := os.Getenv("IDC_CLIENT_SECRET")

	if clientId == "" || clientSecret == "" {
		return types.TokenInfo{}, fmt.Errorf("IDC_CLIENT_ID和IDC_CLIENT_SECRET环境变量必须设置")
	}

	// 准备刷新请求
	refreshReq := types.IdcRefreshRequest{
		ClientId:     clientId,
		ClientSecret: clientSecret,
		GrantType:    "refresh_token",
		RefreshToken: refreshToken,
	}

	reqBody, err := utils.FastMarshal(refreshReq)
	if err != nil {
		return types.TokenInfo{}, fmt.Errorf("序列化IdC请求失败: %v", err)
	}

	logger.Debug("发送IdC token刷新请求", logger.String("url", config.IdcRefreshTokenURL))

	// 发送刷新请求
	req, err := http.NewRequest("POST", config.IdcRefreshTokenURL, bytes.NewBuffer(reqBody))
	if err != nil {
		return types.TokenInfo{}, fmt.Errorf("创建IdC请求失败: %v", err)
	}

	// 设置IdC认证所需的特殊headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Host", "oidc.us-east-1.amazonaws.com")
	req.Header.Set("Connection", "keep-alive")
	req.Header.Set("x-amz-user-agent", "aws-sdk-js/3.738.0 ua/2.1 os/other lang/js md/browser#unknown_unknown api/sso-oidc#3.738.0 m/E KiroIDE")
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Language", "*")
	req.Header.Set("sec-fetch-mode", "cors")
	req.Header.Set("User-Agent", "node")
	req.Header.Set("Accept-Encoding", "br, gzip, deflate")

	resp, err := utils.SharedHTTPClient.Do(req)
	if err != nil {
		return types.TokenInfo{}, fmt.Errorf("IdC刷新token请求失败: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return types.TokenInfo{}, fmt.Errorf("IdC刷新token失败: 状态码 %d, 响应: %s", resp.StatusCode, string(body))
	}

	// 解析响应
	var refreshResp types.RefreshResponse
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return types.TokenInfo{}, fmt.Errorf("读取IdC响应失败: %v", err)
	}

	logger.Debug("IdC API响应内容", logger.String("response_body", string(body)))

	if err := utils.SafeUnmarshal(body, &refreshResp); err != nil {
		return types.TokenInfo{}, fmt.Errorf("解析IdC刷新响应失败: %v", err)
	}

	logger.Debug("新的IdC Access Token", logger.String("access_token", refreshResp.AccessToken))
	logger.Debug("IdC Token过期信息", logger.Int("expires_in_seconds", refreshResp.ExpiresIn))

	// 转换为统一的Token结构
	var token types.Token
	token.AccessToken = refreshResp.AccessToken
	token.RefreshToken = refreshToken // 保持原始refresh token
	token.ExpiresIn = refreshResp.ExpiresIn
	token.ExpiresAt = time.Now().Add(time.Duration(refreshResp.ExpiresIn) * time.Second)

	logger.Info("IdC Token过期时间已计算",
		logger.String("expires_at", token.ExpiresAt.Format("2006-01-02 15:04:05")),
		logger.Int("expires_in_seconds", refreshResp.ExpiresIn))

	return token, nil
}

// tryRefreshToken 尝试刷新单个token (social方式)
func tryRefreshToken(refreshToken string) (types.TokenInfo, error) {
	// 准备刷新请求
	refreshReq := types.RefreshRequest{
		RefreshToken: refreshToken,
	}

	reqBody, err := utils.FastMarshal(refreshReq)
	if err != nil {
		return types.TokenInfo{}, fmt.Errorf("序列化请求失败: %v", err)
	}

	logger.Debug("发送token刷新请求", logger.String("url", config.RefreshTokenURL))

	// 发送刷新请求
	req, err := http.NewRequest("POST", config.RefreshTokenURL, bytes.NewBuffer(reqBody))
	if err != nil {
		return types.TokenInfo{}, fmt.Errorf("创建请求失败: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := utils.SharedHTTPClient.Do(req)
	if err != nil {
		return types.TokenInfo{}, fmt.Errorf("刷新token请求失败: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return types.TokenInfo{}, fmt.Errorf("刷新token失败: 状态码 %d, 响应: %s", resp.StatusCode, string(body))
	}

	// 解析响应
	var refreshResp types.RefreshResponse
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return types.TokenInfo{}, fmt.Errorf("读取响应失败: %v", err)
	}

	logger.Debug("API响应内容", logger.String("response_body", string(body)))

	if err := utils.SafeUnmarshal(body, &refreshResp); err != nil {
		return types.TokenInfo{}, fmt.Errorf("解析刷新响应失败: %v", err)
	}

	logger.Debug("新的Access Token", logger.String("access_token", refreshResp.AccessToken))
	logger.Debug("Token过期信息", logger.Int("expires_in_seconds", refreshResp.ExpiresIn))
	logger.Debug("获取到的ProfileArn", logger.String("profile_arn", refreshResp.ProfileArn))

	// 使用新的Token结构进行转换
	var token types.Token
	token.FromRefreshResponse(refreshResp, refreshToken)

	logger.Info("Token过期时间已计算",
		logger.String("expires_at", token.ExpiresAt.Format("2006-01-02 15:04:05")),
		logger.Int("expires_in_seconds", refreshResp.ExpiresIn))

	// 返回兼容的TokenInfo（由于类型别名，这是相同的类型）
	return token, nil
}
```
#获取用户限额
请求：curl 'https://codewhisperer.us-east-1.amazonaws.com/getUsageLimits?isEmailRequired=true&origin=AI_EDITOR&resourceType=AGENTIC_REQUEST' -H 'x-amz-user-agent: aws-sdk-js/1.0.0 KiroIDE-0.2.13-66c23a8c5d15afabec89ef9954ef52a119f10d369df04d548fc6c1eac694b0d1' -H 'user-agent: aws-sdk-js/1.0.0 ua/2.1 os/darwin#24.6.0 lang/js md/nodejs#20.16.0 api/codewhispererruntime#1.0.0 m/E KiroIDE-0.2.13-66c23a8c5d15afabec89ef9954ef52a119f10d369df04d548fc6c1eac694b0d1' -H 'host: codewhisperer.us-east-1.amazonaws.com' -H 'amz-sdk-invocation-id: 54d2396a-270b-4f11-b9e7-e19ef6d63ab5' -H 'amz-sdk-request: attempt=1; max=1' -H 'Authorization: Bearer aoaAAAAAGisIjs3nwB59rPmLBGXroHiLknGSsXp1BOe4jypG_9NfFfl9d8ZgkO7VDLUBSP-oSswANWwOcyaUwQ-r4Bkc0:MGYCMQDM4M//614CWNioCxOXK4VQYTNGZHJPBX/hVetI3MiERYkAdUIXD7LN01aqTV3kSh4CMQCTuRl3rMz4gLbHZuZnpUco54wYnm9UCVF2wQCoqtxQuVoe+zr5RpDONrykh5X/mBU' -H 'Connection: close'
返回：
{
  "limits": [
  ],
  "usageBreakdownList": [
    {
      "nextDateReset": 1756684800,
      "overageCharges": 0,
      "resourceType": "SPEC",
      "unit": "INVOCATIONS",
      "usageLimit": 0,
      "overageRate": 0.2,
      "currentUsage": 0,
      "overageCap": 200,
      "currency": "USD",
      "currentOverages": 0,
      "freeTrialInfo": {
        "freeTrialExpiry": 1757293897.465,
        "freeTrialStatus": "ACTIVE",
        "usageLimit": 100,
        "currentUsage": 0
      }
    },
    {
      "nextDateReset": 1756684800,
      "overageCharges": 0,
      "resourceType": "VIBE",
      "unit": "INVOCATIONS",
      "usageLimit": 50,
      "overageRate": 0.04,
      "currentUsage": 0,
      "overageCap": 1000,
      "currency": "USD",
      "currentOverages": 0,
      "freeTrialInfo": {
        "freeTrialExpiry": 1757293897.452,
        "freeTrialStatus": "ACTIVE",
        "usageLimit": 100,
        "currentUsage": 0
      }
    }
  ],
  "userInfo": {
    "email": "caidaolihz888@sun.edu.pl",
    "userId": "d-9067642ac7.34488428-c081-7036-d4bb-52f6cfcdf729"
  },
  "daysUntilReset": 0,
  "overageConfiguration": {
    "overageStatus": "DISABLED"
  },
  "nextDateReset": 1756684800,
  "subscriptionInfo": {
    "subscriptionManagementTarget": "PURCHASE",
    "overageCapability": "OVERAGE_INCAPABLE",
    "subscriptionTitle": "KIRO FREE",
    "type": "Q_DEVELOPER_STANDALONE_FREE",
    "upgradeCapability": "UPGRADE_CAPABLE"
  },
  "usageBreakdown": null
}
可用数：usageBreakdownList中resourceType=VIBE的usageLimit+freeTrialInfo.usageLimit