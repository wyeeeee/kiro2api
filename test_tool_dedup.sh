#!/bin/bash

# 测试工具去重修复
# 这个脚本测试同一个工具在多个请求中是否能正常工作

echo "开始测试工具去重修复..."

# 确保服务器正在运行
if ! pgrep -f "kiro2api" > /dev/null; then
    echo "启动 kiro2api 服务器..."
    ./kiro2api &
    sleep 3
fi

# 测试请求1 - 使用Read工具
echo "发送第一个请求 (使用Read工具)..."
curl -s -X POST http://localhost:8080/v1/messages \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer 123456" \
  -d '{
    "model": "claude-sonnet-4-20250514",
    "max_tokens": 100,
    "messages": [
      {
        "role": "user", 
        "content": "请使用Read工具读取一个文件"
      }
    ],
    "tools": [
      {
        "name": "Read",
        "description": "读取文件内容",
        "input_schema": {
          "type": "object",
          "properties": {
            "file_path": {"type": "string"}
          }
        }
      }
    ]
  }' > /dev/null

echo "第一个请求完成"

# 等待一秒
sleep 1

# 测试请求2 - 再次使用Read工具
echo "发送第二个请求 (再次使用Read工具)..."
response=$(curl -s -X POST http://localhost:8080/v1/messages \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer 123456" \
  -d '{
    "model": "claude-sonnet-4-20250514", 
    "max_tokens": 100,
    "messages": [
      {
        "role": "user",
        "content": "请使用Read工具读取另一个文件"
      }
    ],
    "tools": [
      {
        "name": "Read", 
        "description": "读取文件内容",
        "input_schema": {
          "type": "object",
          "properties": {
            "file_path": {"type": "string"}
          }
        }
      }
    ]
  }')

echo "第二个请求完成"

# 检查响应中是否包含工具使用
if echo "$response" | grep -q "tool_use"; then
    echo "✅ 测试通过: 第二个请求中Read工具正常工作"
else
    echo "❌ 测试失败: 第二个请求中Read工具被错误跳过"
    echo "响应内容: $response"
fi

echo "测试完成"