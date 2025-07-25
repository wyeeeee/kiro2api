# 计划
1.clientToken从运行参数中获取改为系统变量KIRO_CLIENT_TOKEN，默认值为123456
2.优先使用环境变量AWS_REFRESHTOKEN,不再读取~/.aws/sso/cache/kiro-auth-token.json
3.自动加载环境变量文件.env