#!/bin/bash

# Token计数接口测试脚本
# 验证本地token估算的准确性和性能

set -e

BASE_URL="http://localhost:8080"
AUTH_TOKEN="123456"

echo "=========================================="
echo "Token计数接口测试"
echo "=========================================="
echo ""

# 测试1: 简单文本消息
echo "测试1: 简单文本消息"
curl -X POST "${BASE_URL}/v1/messages/count_tokens" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer ${AUTH_TOKEN}" \
  -d '{
    "model": "claude-sonnet-4-20250514",
    "messages": [
      {
        "role": "user",
        "content": "Hello, how are you?"
      }
    ]
  }' | jq '.'
echo ""
echo ""

# 测试2: 中文文本消息
echo "测试2: 中文文本消息"
curl -X POST "${BASE_URL}/v1/messages/count_tokens" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer ${AUTH_TOKEN}" \
  -d '{
    "model": "claude-sonnet-4-20250514",
    "messages": [
      {
        "role": "user",
        "content": "你好，今天天气怎么样？"
      }
    ]
  }' | jq '.'
echo ""
echo ""

# 测试3: 包含系统提示词
echo "测试3: 包含系统提示词"
curl -X POST "${BASE_URL}/v1/messages/count_tokens" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer ${AUTH_TOKEN}" \
  -d '{
    "model": "claude-sonnet-4-20250514",
    "system": [
      {
        "type": "text",
        "text": "You are a helpful assistant."
      }
    ],
    "messages": [
      {
        "role": "user",
        "content": "What is the capital of France?"
      }
    ]
  }' | jq '.'
echo ""
echo ""

# 测试4: 包含工具定义
echo "测试4: 包含单个工具定义"
curl -X POST "${BASE_URL}/v1/messages/count_tokens" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer ${AUTH_TOKEN}" \
  -d '{
    "model": "claude-sonnet-4-20250514",
    "messages": [
      {
        "role": "user",
        "content": "What is the weather?"
      }
    ],
    "tools": [
      {
        "name": "get_weather",
        "description": "Get the current weather in a given location",
        "input_schema": {
          "type": "object",
          "properties": {
            "location": {
              "type": "string",
              "description": "The city and state, e.g. San Francisco, CA"
            }
          },
          "required": ["location"]
        }
      }
    ]
  }' | jq '.'
echo ""
echo ""

# 测试5: 多个工具定义
echo "测试5: 多个工具定义"
curl -X POST "${BASE_URL}/v1/messages/count_tokens" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer ${AUTH_TOKEN}" \
  -d '{
    "model": "claude-sonnet-4-20250514",
    "messages": [
      {
        "role": "user",
        "content": "Help me with some tasks"
      }
    ],
    "tools": [
      {
        "name": "get_weather",
        "description": "Get the current weather",
        "input_schema": {
          "type": "object",
          "properties": {
            "location": {"type": "string"}
          }
        }
      },
      {
        "name": "search_web",
        "description": "Search the web",
        "input_schema": {
          "type": "object",
          "properties": {
            "query": {"type": "string"}
          }
        }
      },
      {
        "name": "calculate",
        "description": "Perform calculations",
        "input_schema": {
          "type": "object",
          "properties": {
            "expression": {"type": "string"}
          }
        }
      }
    ]
  }' | jq '.'
echo ""
echo ""

# 测试6: 复杂内容块（文本+工具结果）
echo "测试6: 复杂内容块"
curl -X POST "${BASE_URL}/v1/messages/count_tokens" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer ${AUTH_TOKEN}" \
  -d '{
    "model": "claude-sonnet-4-20250514",
    "messages": [
      {
        "role": "user",
        "content": [
          {
            "type": "text",
            "text": "What is the weather?"
          }
        ]
      },
      {
        "role": "assistant",
        "content": [
          {
            "type": "tool_use",
            "id": "toolu_123",
            "name": "get_weather",
            "input": {"location": "San Francisco"}
          }
        ]
      },
      {
        "role": "user",
        "content": [
          {
            "type": "tool_result",
            "tool_use_id": "toolu_123",
            "content": "The weather in San Francisco is sunny, 72°F"
          }
        ]
      }
    ]
  }' | jq '.'
echo ""
echo ""

# 测试7: 长文本消息
echo "测试7: 长文本消息"
curl -X POST "${BASE_URL}/v1/messages/count_tokens" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer ${AUTH_TOKEN}" \
  -d '{
    "model": "claude-sonnet-4-20250514",
    "messages": [
      {
        "role": "user",
        "content": "Please write a detailed explanation of how neural networks work, including the concepts of forward propagation, backpropagation, gradient descent, activation functions, and how these components work together to enable machine learning. Include examples and explain the mathematical foundations."
      }
    ]
  }' | jq '.'
echo ""
echo ""

# 测试8: 无效模型
echo "测试8: 无效模型（预期失败）"
curl -X POST "${BASE_URL}/v1/messages/count_tokens" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer ${AUTH_TOKEN}" \
  -d '{
    "model": "invalid-model",
    "messages": [
      {
        "role": "user",
        "content": "Test"
      }
    ]
  }' | jq '.'
echo ""
echo ""

# 测试9: 缺少必需字段（预期失败）
echo "测试9: 缺少必需字段（预期失败）"
curl -X POST "${BASE_URL}/v1/messages/count_tokens" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer ${AUTH_TOKEN}" \
  -d '{
    "model": "claude-sonnet-4-20250514"
  }' | jq '.'
echo ""
echo ""

# 性能测试
echo "=========================================="
echo "性能测试: 100次请求"
echo "=========================================="
START_TIME=$(date +%s%N)
for i in {1..100}; do
  curl -s -X POST "${BASE_URL}/v1/messages/count_tokens" \
    -H "Content-Type: application/json" \
    -H "Authorization: Bearer ${AUTH_TOKEN}" \
    -d '{
      "model": "claude-sonnet-4-20250514",
      "messages": [
        {
          "role": "user",
          "content": "Hello, how are you?"
        }
      ]
    }' > /dev/null
done
END_TIME=$(date +%s%N)
ELAPSED=$((($END_TIME - $START_TIME) / 1000000))
AVG_TIME=$(($ELAPSED / 100))

echo "总耗时: ${ELAPSED}ms"
echo "平均响应时间: ${AVG_TIME}ms"
echo ""

if [ $AVG_TIME -lt 5 ]; then
  echo "✅ 性能测试通过（平均响应时间 < 5ms）"
else
  echo "⚠️  性能测试警告（平均响应时间 >= 5ms）"
fi

echo ""
echo "=========================================="
echo "测试完成"
echo "=========================================="
