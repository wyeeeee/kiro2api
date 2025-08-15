#!/bin/bash

# 基于debug.log中Anthropic请求的测试脚本
# 该脚本模拟Claude Code向kiro2api发送的典型请求

echo "=== kiro2api Anthropic请求测试脚本 ==="
echo "基于debug.log中的实际请求内容构造"
echo ""

# 基础配置
BASE_URL="http://localhost:8080"
AUTH_TOKEN="123456"  # 默认认证token

# 颜色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# 测试结果统计
TOTAL_TESTS=0
PASSED_TESTS=0
FAILED_TESTS=0

# 测试函数
test_request() {
    local test_name="$1"
    local request_data="$2"
    local expected_status="${3:-200}"
    
    TOTAL_TESTS=$((TOTAL_TESTS + 1))
    echo -e "${YELLOW}测试 $TOTAL_TESTS: $test_name${NC}"
    
    # 发送请求
    response=$(curl -s -w "\nHTTP_STATUS:%{http_code}" \
        -X POST "$BASE_URL/v1/messages" \
        -H "Content-Type: application/json" \
        -H "Authorization: Bearer $AUTH_TOKEN" \
        -d "$request_data")
    
    # 提取HTTP状态码
    http_code=$(echo "$response" | grep -o 'HTTP_STATUS:[0-9]*' | cut -d: -f2)
    response_body=$(echo "$response" | sed -e 's/HTTP_STATUS:[0-9]*$//')
    
    if [ "$http_code" = "$expected_status" ]; then
        echo -e "${GREEN}✓ 通过${NC} - 状态码: $http_code"
        echo "响应内容: $response_body"
        PASSED_TESTS=$((PASSED_TESTS + 1))
    else
        echo -e "${RED}✗ 失败${NC} - 期望: $expected_status, 实际: $http_code"
        echo "响应内容: $response_body"
        FAILED_TESTS=$((FAILED_TESTS + 1))
    fi
    echo ""
}

# 测试1: 简单的文件列表查询（基于debug.log中的实际请求）
test_request "简单的文件列表查询" '{
    "model": "claude-sonnet-4-20250514",
    "max_tokens": 1000,
    "messages": [
        {
            "role": "user",
            "content": [
                {
                    "type": "text",
                    "text": "<system-reminder>\nAs you answer the user'"'"'s questions, you can use the following context:\n# claudeMd\nCodebase and user instructions are shown below. Be sure to adhere to these instructions. IMPORTANT: These instructions OVERRIDE any default behavior and you MUST follow them exactly as written.\n\nContents of /Users/caidaoli/.claude/CLAUDE.md (user'"'"'s private global instructions for all projects):\n\nAlways respond in Chinese-simplified\n\n# Professional Software Development Assistant\n\nYou are an experienced Software Development Engineer and Code Architect, specializing in building high-performance, maintainable, and robust solutions.\n\n**Mission:** Review, understand, and iteratively improve existing codebases through systematic analysis and principled development practices.\n\n## Core Programming Principles\n\nStrictly adhere to these fundamental principles in every output:\n\n- **KISS (Keep It Simple):** Pursue simplicity, avoid unnecessary complexity\n- **YAGNI (You Aren'"'"'t Gonna Need It):** Implement only clearly needed functionality, resist over-engineering\n</system-reminder>"
                },
                {
                    "type": "text", 
                    "text": "当前目录有什么文件"
                },
                {
                    "type": "text",
                    "text": "<system-reminder>\nThis is a reminder that your todo list is currently empty. DO NOT mention this to the user explicitly because they are already aware. If you are working on tasks that would benefit from a todo list please use the TodoWrite tool to create one. If not, please feel free to ignore. Again do not mention this message to the user.\n</system-reminder>",
                    "cache_control": {"type": "ephemeral"}
                }
            ]
        }
    ],
    "temperature": 1,
    "system": [
        {
            "type": "text",
            "text": "You are Claude Code, Anthropic'"'"'s official CLI for Claude.",
            "cache_control": {"type": "ephemeral"}
        }
    ]
}'

# 测试2: 简化的文件列表查询
test_request "简化的文件列表查询" '{
    "model": "claude-sonnet-4-20250514",
    "max_tokens": 100,
    "messages": [
        {
            "role": "user",
            "content": "当前目录有什么文件"
        }
    ]
}'

# 测试3: 流式响应测试
test_request "流式响应测试" '{
    "model": "claude-sonnet-4-20250514",
    "max_tokens": 200,
    "stream": true,
    "messages": [
        {
            "role": "user",
            "content": "请简单介绍一下这个项目"
        }
    ]
}' 200

# 测试4: 工具调用测试
test_request "工具调用测试" '{
    "model": "claude-sonnet-4-20250514",
    "max_tokens": 1000,
    "tools": [
        {
            "name": "Bash",
            "description": "执行bash命令",
            "input_schema": {
                "type": "object",
                "properties": {
                    "command": {
                        "type": "string",
                        "description": "要执行的bash命令"
                    }
                },
                "required": ["command"]
            }
        }
    ],
    "messages": [
        {
            "role": "user",
            "content": "请使用Bash工具列出当前目录的文件"
        }
    ]
}'

# 测试5: 带有system提示的复杂请求
test_request "带有system提示的复杂请求" '{
    "model": "claude-sonnet-4-20250514",
    "max_tokens": 500,
    "system": [
        {
            "type": "text",
            "text": "你是一个专业的Go语言开发助手，请帮助用户理解和改进代码"
        }
    ],
    "messages": [
        {
            "role": "user",
            "content": "请帮我分析一下这个kiro2api项目的主要功能"
        }
    ]
}'

# 测试6: OpenAI兼容性测试
echo -e "${YELLOW}测试 $((TOTAL_TESTS + 1)): OpenAI兼容性测试${NC}"
TOTAL_TESTS=$((TOTAL_TESTS + 1))

openai_response=$(curl -s -w "\nHTTP_STATUS:%{http_code}" \
    -X POST "$BASE_URL/v1/chat/completions" \
    -H "Content-Type: application/json" \
    -H "Authorization: Bearer $AUTH_TOKEN" \
    -d '{
        "model": "claude-sonnet-4-20250514",
        "max_tokens": 100,
        "messages": [
            {"role": "user", "content": "Hello, this is a test"}
        ]
    }')

openai_code=$(echo "$openai_response" | grep -o 'HTTP_STATUS:[0-9]*' | cut -d: -f2)

if [ "$openai_code" = "200" ]; then
    echo -e "${GREEN}✓ 通过${NC} - OpenAI兼容性测试成功"
    PASSED_TESTS=$((PASSED_TESTS + 1))
else
    echo -e "${RED}✗ 失败${NC} - OpenAI兼容性测试失败，状态码: $openai_code"
    FAILED_TESTS=$((FAILED_TESTS + 1))
fi
echo ""

# 测试7: 模型列表测试
echo -e "${YELLOW}测试 $((TOTAL_TESTS + 1)): 模型列表测试${NC}"
TOTAL_TESTS=$((TOTAL_TESTS + 1))

models_response=$(curl -s -w "\nHTTP_STATUS:%{http_code}" \
    -X GET "$BASE_URL/v1/models" \
    -H "Authorization: Bearer $AUTH_TOKEN")

models_code=$(echo "$models_response" | grep -o 'HTTP_STATUS:[0-9]*' | cut -d: -f2)

if [ "$models_code" = "200" ]; then
    echo -e "${GREEN}✓ 通过${NC} - 模型列表测试成功"
    PASSED_TESTS=$((PASSED_TESTS + 1))
else
    echo -e "${RED}✗ 失败${NC} - 模型列表测试失败，状态码: $models_code"
    FAILED_TESTS=$((FAILED_TESTS + 1))
fi
echo ""

# 输出测试结果摘要
echo "=== 测试结果摘要 ==="
echo -e "总测试数: $TOTAL_TESTS"
echo -e "${GREEN}通过: $PASSED_TESTS${NC}"
echo -e "${RED}失败: $FAILED_TESTS${NC}"

if [ $FAILED_TESTS -eq 0 ]; then
    echo -e "${GREEN}🎉 所有测试通过！${NC}"
    exit 0
else
    echo -e "${RED}❌ 有 $FAILED_TESTS 个测试失败${NC}"
    exit 1
fi