# GitHub Actions Workflows

本项目包含了以下GitHub Actions工作流：

## 工作流说明

### 1. build.yml - 完整构建流程
- **触发条件**: push到main/master分支或创建pull request
- **功能**:
  - 运行测试
  - 编译应用程序
  - 多平台交叉编译 (Linux, macOS, Windows)
  - 上传构建产物

### 2. simple-build.yml - 简单构建
- **触发条件**: push到main分支或创建pull request
- **功能**:
  - 基本的依赖安装
  - 编译应用程序
  - 验证构建结果

### 3. release-build.yml - 发布构建
- **触发条件**: 创建tag (格式: v*)
- **功能**:
  - 构建多平台二进制文件
  - 自动创建GitHub Release
  - 上传所有平台的可执行文件

## 使用方法

### 日常开发
每次push代码到main分支时，会自动触发构建流程，确保代码可以正常编译。

### 发布新版本
1. 创建并推送tag:
   ```bash
   git tag v1.0.0
   git push origin v1.0.0
   ```

2. GitHub Actions会自动:
   - 构建多平台二进制文件
   - 创建GitHub Release
   - 上传所有可执行文件

### 支持的平台
- Linux (amd64, arm64)
- macOS (amd64, arm64) 
- Windows (amd64)

## 注意事项
- 确保Go版本与go.mod中指定的版本一致
- 所有workflow都会缓存Go模块以加速构建
- 构建产物会保留30天
