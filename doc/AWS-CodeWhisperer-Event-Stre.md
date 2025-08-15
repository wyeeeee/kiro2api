## 🤖 Assistant

# AWS CodeWhisperer Event Stream 解析完整指南

## 目录
1. [AWS Event Stream 格式概述](#1-aws-event-stream-格式概述)
2. [二进制数据结构详解](#2-二进制数据结构详解)
3. [基础解析器实现](#3-基础解析器实现)
4. [工具调用支持](#4-工具调用支持)
5. [完整示例代码](#5-完整示例代码)
6. [使用指南](#6-使用指南)
7. [错误处理和调试](#7-错误处理和调试)

## 1. AWS Event Stream 格式概述

AWS Event Stream 是一种基于二进制的流式协议，用于在 AWS 服务之间传输结构化数据。CodeWhisperer 使用此格式来传输代码建议、工具调用请求和响应。

### 1.1 消息类型

```python
class MessageTypes:
    """AWS Event Stream 消息类型"""
    EVENT = "event"           # 事件消息
    ERROR = "error"           # 错误消息
    EXCEPTION = "exception"   # 异常消息
    
class EventTypes:
    """CodeWhisperer 事件类型"""
    # 普通代码补全
    COMPLETION = "completion"
    COMPLETION_CHUNK = "completion_chunk"
    
    # 工具调用相关
    TOOL_CALL_REQUEST = "tool_call_request"
    TOOL_CALL_RESULT = "tool_call_result"
    TOOL_CALL_ERROR = "tool_call_error"
    TOOL_EXECUTION_START = "tool_execution_start"
    TOOL_EXECUTION_END = "tool_execution_end"
    
    # 会话管理
    SESSION_START = "session_start"
    SESSION_END = "session_end"
```

## 2. 二进制数据结构详解

### 2.1 消息格式

```
+-------------------+-------------------+-------------------+-------------------+
|   Total Length    |  Headers Length   |      Headers      |      Payload      |
|     (4 bytes)     |     (4 bytes)     |    (variable)     |    (variable)     |
+-------------------+-------------------+-------------------+-------------------+
|       CRC         |
|     (4 bytes)     |
+-------------------+

总长度: 包括整个消息的字节数
头部长度: 头部数据的字节数
头部: 键值对形式的元数据
载荷: 实际的消息内容
CRC: 循环冗余校验码
```

### 2.2 头部格式

```
+-------------+-------------+-------------+-------------+-------------+
| Name Length |    Name     | Value Type  |Value Length |    Value    |
|  (1 byte)   | (variable)  |  (1 byte)   |  (2 bytes)  | (variable)  |
+-------------+-------------+-------------+-------------+-------------+

名称长度: 头部名称的字节长度
名称: 头部名称 (UTF-8 编码)
值类型: 值的数据类型 (7=字符串, 6=字节数组等)
值长度: 值的字节长度
值: 实际的值数据
```

## 3. 基础解析器实现

### 3.1 核心解析器类

```python
import struct
import json
import zlib
import logging
from typing import Dict, List, Optional, Any, Union, Tuple
from dataclasses import dataclass
from enum import Enum

# 配置日志
logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)

class ValueType(Enum):
    """AWS Event Stream 值类型"""
    BOOL_TRUE = 0
    BOOL_FALSE = 1
    BYTE = 2
    SHORT = 3
    INTEGER = 4
    LONG = 5
    BYTE_ARRAY = 6
    STRING = 7
    TIMESTAMP = 8
    UUID = 9

@dataclass
class EventStreamMessage:
    """事件流消息数据结构"""
    headers: Dict[str, Any]
    payload: bytes
    message_type: Optional[str] = None
    event_type: Optional[str] = None
    content_type: Optional[str] = None
    
    def __post_init__(self):
        self.message_type = self.headers.get(':message-type', {}).get('value')
        self.event_type = self.headers.get(':event-type', {}).get('value')
        self.content_type = self.headers.get(':content-type', {}).get('value', 'application/json')

class EventStreamParser:
    """AWS Event Stream 解析器"""
    
    def __init__(self):
        self.buffer = b''
        
    def parse_stream(self, data: bytes) -> List[EventStreamMessage]:
        """解析完整的事件流数据"""
        messages = []
        offset = 0
        
        while offset < len(data):
            try:
                message, consumed = self._parse_single_message(data[offset:])
                if message is None:
                    break
                    
                messages.append(message)
                offset += consumed
                
            except Exception as e:
                logger.error(f"解析消息时出错: {e}")
                break
                
        return messages
    
    def _parse_single_message(self, data: bytes) -> Tuple[Optional[EventStreamMessage], int]:
        """解析单个消息"""
        if len(data) < 12:  # 最小消息长度
            return None, 0
            
        # 读取消息总长度
        total_length = struct.unpack('>I', data[:4])[0]
        
        if len(data) < total_length:
            logger.warning(f"数据不完整: 需要 {total_length} 字节，实际 {len(data)} 字节")
            return None, 0
            
        # 读取头部长度
        headers_length = struct.unpack('>I', data[4:8])[0]
        
        # 验证长度合理性
        if headers_length > total_length - 12:
            logger.error(f"头部长度异常: {headers_length}")
            return None, total_length
            
        # 解析头部
        headers_data = data[8:8 + headers_length]
        headers = self._parse_headers(headers_data)
        
        # 提取载荷
        payload_start = 8 + headers_length
        payload_end = total_length - 4  # 减去 CRC 长度
        payload = data[payload_start:payload_end]
        
        # 验证 CRC (可选)
        expected_crc = struct.unpack('>I', data[payload_end:total_length])[0]
        calculated_crc = self._calculate_crc(data[:payload_end])
        
        if expected_crc != calculated_crc:
            logger.warning(f"CRC 校验失败: 期望 {expected_crc:08x}, 实际 {calculated_crc:08x}")
        
        message = EventStreamMessage(
            headers=headers,
            payload=payload
        )
        
        return message, total_length
    
    def _parse_headers(self, data: bytes) -> Dict[str, Dict[str, Any]]:
        """解析头部数据"""
        headers = {}
        offset = 0
        
        while offset < len(data):
            try:
                # 读取名称长度
                if offset >= len(data):
                    break
                    
                name_length = struct.unpack('B', data[offset:offset+1])[0]
                offset += 1
                
                if offset + name_length > len(data):
                    break
                    
                # 读取名称
                name = data[offset:offset + name_length].decode('utf-8')
                offset += name_length
                
                if offset + 3 > len(data):
                    break
                    
                # 读取值类型
                value_type = struct.unpack('B', data[offset:offset+1])[0]
                offset += 1
                
                # 读取值长度
                value_length = struct.unpack('>H', data[offset:offset+2])[0]
                offset += 2
                
                if offset + value_length > len(data):
                    break
                    
                # 读取值数据
                value_data = data[offset:offset + value_length]
                offset += value_length
                
                # 解析值
                value = self._parse_header_value(value_type, value_data)
                
                headers[name] = {
                    'type': value_type,
                    'value': value
                }
                
            except Exception as e:
                logger.error(f"解析头部时出错: {e}")
                break
                
        return headers
    
    def _parse_header_value(self, value_type: int, data: bytes) -> Any:
        """根据类型解析头部值"""
        try:
            if value_type == ValueType.BOOL_TRUE.value:
                return True
            elif value_type == ValueType.BOOL_FALSE.value:
                return False
            elif value_type == ValueType.BYTE.value:
                return struct.unpack('b', data)[0] if data else 0
            elif value_type == ValueType.SHORT.value:
                return struct.unpack('>h', data)[0] if len(data) >= 2 else 0
            elif value_type == ValueType.INTEGER.value:
                return struct.unpack('>i', data)[0] if len(data) >= 4 else 0
            elif value_type == ValueType.LONG.value:
                return struct.unpack('>q', data)[0] if len(data) >= 8 else 0
            elif value_type == ValueType.BYTE_ARRAY.value:
                return data
            elif value_type == ValueType.STRING.value:
                return data.decode('utf-8')
            elif value_type == ValueType.TIMESTAMP.value:
                return struct.unpack('>q', data)[0] if len(data) >= 8 else 0
            elif value_type == ValueType.UUID.value:
                return data.hex() if len(data) == 16 else data.decode('utf-8', errors='ignore')
            else:
                logger.warning(f"未知的值类型: {value_type}")
                return data
        except Exception as e:
            logger.error(f"解析头部值时出错: {e}")
            return data
    
    def _calculate_crc(self, data: bytes) -> int:
        """计算 CRC32 校验码"""
        return zlib.crc32(data) & 0xffffffff
```

## 4. 工具调用支持

### 4.1 工具调用数据结构

```python
@dataclass
class ToolCall:
    """工具调用请求"""
    id: str
    name: str
    arguments: Dict[str, Any]
    type: str = "function"

@dataclass
class ToolResult:
    """工具调用结果"""
    tool_call_id: str
    result: Any
    error: Optional[str] = None
    execution_time: Optional[float] = None

@dataclass
class CodeCompletion:
    """代码补全结果"""
    content: str
    finish_reason: Optional[str] = None
    tool_calls: List[ToolCall] = None
    
    def __post_init__(self):
        if self.tool_calls is None:
            self.tool_calls = []
```

### 4.2 增强的消息处理器

```python
class CodeWhispererMessageProcessor:
    """CodeWhisperer 消息处理器"""
    
    def __init__(self):
        self.tool_calls = {}
        self.tool_results = {}
        self.completions = []
        
    def process_message(self, message: EventStreamMessage) -> Optional[Dict[str, Any]]:
        """处理单个消息"""
        try:
            # 解析载荷
            payload_data = self._parse_payload(message)
            
            # 根据消息类型处理
            if message.message_type == MessageTypes.EVENT:
                return self._process_event_message(message, payload_data)
            elif message.message_type == MessageTypes.ERROR:
                return self._process_error_message(message, payload_data)
            elif message.message_type == MessageTypes.EXCEPTION:
                return self._process_exception_message(message, payload_data)
            else:
                logger.warning(f"未知消息类型: {message.message_type}")
                return None
                
        except Exception as e:
            logger.error(f"处理消息时出错: {e}")
            return None
    
    def _parse_payload(self, message: EventStreamMessage) -> Any:
        """解析载荷数据"""
        if not message.payload:
            return None
            
        try:
            if message.content_type == 'application/json':
                return json.loads(message.payload.decode('utf-8'))
            elif message.content_type == 'text/plain':
                return message.payload.decode('utf-8')
            else:
                return message.payload
        except (json.JSONDecodeError, UnicodeDecodeError) as e:
            logger.error(f"解析载荷时出错: {e}")
            return message.payload
    
    def _process_event_message(self, message: EventStreamMessage, data: Any) -> Dict[str, Any]:
        """处理事件消息"""
        event_type = message.event_type
        
        if event_type == EventTypes.COMPLETION:
            return self._handle_completion(data)
        elif event_type == EventTypes.COMPLETION_CHUNK:
            return self._handle_completion_chunk(data)
        elif event_type == EventTypes.TOOL_CALL_REQUEST:
            return self._handle_tool_call_request(data)
        elif event_type == EventTypes.TOOL_CALL_RESULT:
            return self._handle_tool_call_result(data)
        elif event_type == EventTypes.TOOL_CALL_ERROR:
            return self._handle_tool_call_error(data)
        elif event_type == EventTypes.TOOL_EXECUTION_START:
            return self._handle_tool_execution_start(data)
        elif event_type == EventTypes.TOOL_EXECUTION_END:
            return self._handle_tool_execution_end(data)
        elif event_type == EventTypes.SESSION_START:
            return self._handle_session_start(data)
        elif event_type == EventTypes.SESSION_END:
            return self._handle_session_end(data)
        else:
            logger.warning(f"未知事件类型: {event_type}")
            return {
                'type': 'unknown_event',
                'event_type': event_type,
                'data': data
            }
    
    def _handle_completion(self, data: Any) -> Dict[str, Any]:
        """处理代码补全"""
        completion = CodeCompletion(
            content=data.get('content', ''),
            finish_reason=data.get('finish_reason'),
            tool_calls=[
                ToolCall(
                    id=tc.get('id'),
                    name=tc.get('function', {}).get('name'),
                    arguments=json.loads(tc.get('function', {}).get('arguments', '{}'))
                )
                for tc in data.get('tool_calls', [])
            ]
        )
        
        self.completions.append(completion)
        
        return {
            'type': 'completion',
            'completion': completion,
            'raw_data': data
        }
    
    def _handle_completion_chunk(self, data: Any) -> Dict[str, Any]:
        """处理流式补全块"""
        return {
            'type': 'completion_chunk',
            'content': data.get('content', ''),
            'delta': data.get('delta', ''),
            'finish_reason': data.get('finish_reason'),
            'raw_data': data
        }
    
    def _handle_tool_call_request(self, data: Any) -> Dict[str, Any]:
        """处理工具调用请求"""
        tool_calls = []
        
        for call_data in data.get('tool_calls', []):
            try:
                arguments_str = call_data.get('function', {}).get('arguments', '{}')
                arguments = json.loads(arguments_str) if isinstance(arguments_str, str) else arguments_str
                
                tool_call = ToolCall(
                    id=call_data.get('id'),
                    name=call_data.get('function', {}).get('name'),
                    arguments=arguments,
                    type=call_data.get('type', 'function')
                )
                
                tool_calls.append(tool_call)
                self.tool_calls[tool_call.id] = tool_call
                
            except (json.JSONDecodeError, KeyError) as e:
                logger.error(f"解析工具调用时出错: {e}")
                continue
        
        return {
            'type': 'tool_call_request',
            'tool_calls': tool_calls,
            'raw_data': data
        }
    
    def _handle_tool_call_result(self, data: Any) -> Dict[str, Any]:
        """处理工具调用结果"""
        tool_call_id = data.get('tool_call_id')
        result = data.get('result')
        execution_time = data.get('execution_time')
        
        if tool_call_id:
            tool_result = ToolResult(
                tool_call_id=tool_call_id,
                result=result,
                execution_time=execution_time
            )
            self.tool_results[tool_call_id] = tool_result
            
            return {
                'type': 'tool_call_result',
                'tool_call_id': tool_call_id,
                'result': result,
                'execution_time': execution_time,
                'tool_result': tool_result
            }
        
        return {
            'type': 'tool_call_result',
            'raw_data': data
        }
    
    def _handle_tool_call_error(self, data: Any) -> Dict[str, Any]:
        """处理工具调用错误"""
        tool_call_id = data.get('tool_call_id')
        error = data.get('error')
        
        if tool_call_id:
            tool_result = ToolResult(
                tool_call_id=tool_call_id,
                result=None,
                error=error
            )
            self.tool_results[tool_call_id] = tool_result
            
            return {
                'type': 'tool_call_error',
                'tool_call_id': tool_call_id,
                'error': error,
                'tool_result': tool_result
            }
        
        return {
            'type': 'tool_call_error',
            'raw_data': data
        }
    
    def _handle_tool_execution_start(self, data: Any) -> Dict[str, Any]:
        """处理工具执行开始"""
        return {
            'type': 'tool_execution_start',
            'tool_call_id': data.get('tool_call_id'),
            'tool_name': data.get('tool_name'),
            'timestamp': data.get('timestamp'),
            'raw_data': data
        }
    
    def _handle_tool_execution_end(self, data: Any) -> Dict[str, Any]:
        """处理工具执行结束"""
        return {
            'type': 'tool_execution_end',
            'tool_call_id': data.get('tool_call_id'),
            'tool_name': data.get('tool_name'),
            'duration': data.get('duration'),
            'timestamp': data.get('timestamp'),
            'raw_data': data
        }
    
    def _handle_session_start(self, data: Any) -> Dict[str, Any]:
        """处理会话开始"""
        return {
            'type': 'session_start',
            'session_id': data.get('session_id'),
            'timestamp': data.get('timestamp'),
            'raw_data': data
        }
    
    def _handle_session_end(self, data: Any) -> Dict[str, Any]:
        """处理会话结束"""
        return {
            'type': 'session_end',
            'session_id': data.get('session_id'),
            'timestamp': data.get('timestamp'),
            'raw_data': data
        }
    
    def _process_error_message(self, message: EventStreamMessage, data: Any) -> Dict[str, Any]:
        """处理错误消息"""
        return {
            'type': 'error',
            'error_code': data.get('__type') if isinstance(data, dict) else None,
            'error_message': data.get('message') if isinstance(data, dict) else str(data),
            'raw_data': data
        }
    
    def _process_exception_message(self, message: EventStreamMessage, data: Any) -> Dict[str, Any]:
        """处理异常消息"""
        return {
            'type': 'exception',
            'exception_type': data.get('__type') if isinstance(data, dict) else None,
            'exception_message': data.get('message') if isinstance(data, dict) else str(data),
            'raw_data': data
        }
```

## 5. 完整示例代码

### 5.1 主解析器类

```python
class CodeWhispererEventStreamParser:
    """CodeWhisperer 事件流完整解析器"""
    
    def __init__(self):
        self.stream_parser = EventStreamParser()
        self.message_processor = CodeWhispererMessageProcessor()
        
    def parse_response(self, stream_data: bytes) -> Dict[str, Any]:
        """解析完整的 CodeWhisperer 响应"""
        try:
            # 解析事件流
            messages = self.stream_parser.parse_stream(stream_data)
            logger.info(f"解析到 {len(messages)} 个消息")
            
            # 处理消息
            processed_messages = []
            for message in messages:
                processed = self.message_processor.process_message(message)
                if processed:
                    processed_messages.append(processed)
            
            # 构建结果
            result = {
                'messages': processed_messages,
                'tool_calls': dict(self.message_processor.tool_calls),
                'tool_results': dict(self.message_processor.tool_results),
                'completions': list(self.message_processor.completions),
                'summary': self._generate_summary(processed_messages)
            }
            
            return result
            
        except Exception as e:
            logger.error(f"解析响应时出错: {e}")
            return {
                'error': str(e),
                'messages': [],
                'tool_calls': {},
                'tool_results': {},
                'completions': []
            }
    
    def _generate_summary(self, messages: List[Dict[str, Any]]) -> Dict[str, Any]:
        """生成解析摘要"""
        summary = {
            'total_messages': len(messages),
            'message_types': {},
            'has_tool_calls': False,
            'has_completions': False,
            'has_errors': False
        }
        
        for message in messages:
            msg_type = message.get('type', 'unknown')
            summary['message_types'][msg_type] = summary['message_types'].get(msg_type, 0) + 1
            
            if msg_type.startswith('tool_'):
                summary['has_tool_calls'] = True
            elif msg_type.startswith('completion'):
                summary['has_completions'] = True
            elif msg_type in ['error', 'exception']:
                summary['has_errors'] = True
        
        return summary
```

### 5.2 使用示例

```python
def example_parse_codewhisperer_response():
    """使用示例：解析 CodeWhisperer 响应"""
    
    # 创建解析器
    parser = CodeWhispererEventStreamParser()
    
    # 模拟从 API 获取的二进制数据
    # 在实际使用中，这将是从 HTTP 响应或 WebSocket 获取的数据
    mock_stream_data = create_mock_stream_data()
    
    # 解析响应
    result = parser.parse_response(mock_stream_data)
    
    # 处理结果
    print("=== CodeWhisperer 响应解析结果 ===")
    print(f"总消息数: {result['summary']['total_messages']}")
    print(f"消息类型分布: {result['summary']['message_types']}")
    print(f"包含工具调用: {result['summary']['has_tool_calls']}")
    print(f"包含代码补全: {result['summary']['has_completions']}")
    print(f"包含错误: {result['summary']['has_errors']}")
    
    # 处理工具调用
    if result['tool_calls']:
        print("\n=== 工具调用 ===")
        for tool_id, tool_call in result['tool_calls'].items():
            print(f"工具 ID: {tool_id}")
            print(f"工具名称: {tool_call.name}")
            print(f"参数: {json.dumps(tool_call.arguments, indent=2)}")
            
            # 检查是否有对应的结果
            if tool_id in result['tool_results']:
                tool_result = result['tool_results'][tool_id]
                if tool_result.error:
                    print(f"执行错误: {tool_result.error}")
                else:
                    print(f"执行结果: {json.dumps(tool_result.result, indent=2)}")
                    if tool_result.execution_time:
                        print(f"执行时间: {tool_result.execution_time}ms")
            print()
    
    # 处理代码补全
    if result['completions']:
        print("=== 代码补全 ===")
        for i, completion in enumerate(result['completions']):
            print(f"补全 {i+1}:")
            print(f"内容: {completion.content}")
            print(f"完成原因: {completion.finish_reason}")
            if completion.tool_calls:
                print(f"关联工具调用: {len(completion.tool_calls)} 个")
            print()
    
    # 处理消息流
    print("=== 消息流详情 ===")
    for i, message in enumerate(result['messages']):
        print(f"消息 {i+1}: {message['type']}")
        if message['type'] == 'completion_chunk':
            print(f"  内容块: '{message.get('content', '')}'")
        elif message['type'] == 'tool_execution_start':
            print(f"  开始执行工具: {message.get('tool_name')}")
        elif message['type'] == 'tool_execution_end':
            print(f"  工具执行完成，耗时: {message.get('duration')}ms")
        elif message['type'] == 'error':
            print(f"  错误: {message.get('error_message')}")

def create_mock_stream_data() -> bytes:
    """创建模拟的事件流数据用于测试"""
    
    def create_message(headers: Dict[str, Any], payload: str) -> bytes:
        """创建单个消息的二进制数据"""
        # 编码头部
        headers_data = b''
        for name, value in headers.items():
            name_bytes = name.encode('utf-8')
            headers_data += struct.pack('B', len(name_bytes))
            headers_data += name_bytes
            
            if isinstance(value, str):
                value_bytes = value.encode('utf-8')
                headers_data += struct.pack('B', ValueType.STRING.value)
                headers_data += struct.pack('>H', len(value_bytes))
                headers_data += value_bytes
            else:
                # 简化处理，实际应根据类型编码
                value_str = str(value)
                value_bytes = value_str.encode('utf-8')
                headers_data += struct.pack('B', ValueType.STRING.value)
                headers_data += struct.pack('>H', len(value_bytes))
                headers_data += value_bytes
        
        # 编码载荷
        payload_bytes = payload.encode('utf-8')
        
        # 计算长度
        headers_length = len(headers_data)
        total_length = 4 + 4 + headers_length + len(payload_bytes) + 4  # 包含CRC
        
        # 构建消息
        message = struct.pack('>I', total_length)  # 总长度
        message += struct.pack('>I', headers_length)  # 头部长度
        message += headers_data  # 头部
        message += payload_bytes  # 载荷
        
        # 计算并添加CRC
        crc = zlib.crc32(message) & 0xffffffff
        message += struct.pack('>I', crc)
        
        return message
    
    # 创建多个消息
    messages = []
    
    # 1. 会话开始
    messages.append(create_message(
        {
            ':message-type': 'event',
            ':event-type': 'session_start',
            ':content-type': 'application/json'
        },
        json.dumps({
            'session_id': 'session_123',
            'timestamp': '2024-01-01T10:00:00Z'
        })
    ))
    
    # 2. 工具调用请求
    messages.append(create_message(
        {
            ':message-type': 'event',
            ':event-type': 'tool_call_request',
            ':content-type': 'application/json'
        },
        json.dumps({
            'tool_calls': [
                {
                    'id': 'call_123',
                    'type': 'function',
                    'function': {
                        'name': 'search_code',
                        'arguments': json.dumps({
                            'query': 'python file handling',
                            'language': 'python',
                            'max_results': 5
                        })
                    }
                }
            ]
        })
    ))
    
    # 3. 工具执行开始
    messages.append(create_message(
        {
            ':message-type': 'event',
            ':event-type': 'tool_execution_start',
            ':content-type': 'application/json'
        },
        json.dumps({
            'tool_call_id': 'call_123',
            'tool_name': 'search_code',
            'timestamp': '2024-01-01T10:00:01Z'
        })
    ))
    
    # 4. 工具调用结果
    messages.append(create_message(
        {
            ':message-type': 'event',
            ':event-type': 'tool_call_result',
            ':content-type': 'application/json'
        },
        json.dumps({
            'tool_call_id': 'call_123',
            'result': {
                'results': [
                    {
                        'file': 'file_handler.py',
                        'content': 'def read_file(filename):\n    with open(filename, "r") as f:\n        return f.read()',
                        'score': 0.95
                    }
                ]
            },
            'execution_time': 150
        })
    ))
    
    # 5. 工具执行结束
    messages.append(create_message(
        {
            ':message-type': 'event',
            ':event-type': 'tool_execution_end',
            ':content-type': 'application/json'
        },
        json.dumps({
            'tool_call_id': 'call_123',
            'tool_name': 'search_code',
            'duration': 150,
            'timestamp': '2024-01-01T10:00:02Z'
        })
    ))
    
    # 6. 代码补全流式响应
    completion_chunks = [
        "基于搜索结果，",
        "这里是一个文件处理的示例:\n\n",
        "```python\n",
        "def read_file(filename):\n",
        "    with open(filename, 'r') as f:\n",
        "        return f.read()\n",
        "```"
    ]
    
    for chunk in completion_chunks:
        messages.append(create_message(
            {
                ':message-type': 'event',
                ':event-type': 'completion_chunk',
                ':content-type': 'application/json'
            },
            json.dumps({
                'content': chunk,
                'delta': chunk
            })
        ))
    
    # 7. 最终完成
    messages.append(create_message(
        {
            ':message-type': 'event',
            ':event-type': 'completion',
            ':content-type': 'application/json'
        },
        json.dumps({
            'content': ''.join(completion_chunks),
            'finish_reason': 'tool_calls_complete',
            'tool_calls': []
        })
    ))
    
    # 8. 会话结束
    messages.append(create_message(
        {
            ':message-type': 'event',
            ':event-type': 'session_end',
            ':content-type': 'application/json'
        },
        json.dumps({
            'session_id': 'session_123',
            'timestamp': '2024-01-01T10:00:03Z'
        })
    ))
    
    return b''.join(messages)
```

## 6. 使用指南

### 6.1 基本使用

```python
# 基本使用示例
def basic_usage_example():
    """基本使用示例"""
    
    # 1. 创建解析器
    parser = CodeWhispererEventStreamParser()
    
    # 2. 从 API 响应获取二进制数据
    # 这里使用模拟数据，实际使用中替换为真实的 API 调用
    stream_data = get_codewhisperer_response_data()
    
    # 3. 解析数据
    result = parser.parse_response(stream_data)
    
    # 4. 处理结果
    if result.get('error'):
        print(f"解析出错: {result['error']}")
        return
    
    # 5. 获取代码补全
    for completion in result['completions']:
        print(f"代码建议: {completion.content}")
    
    # 6. 处理工具调用
    for tool_id, tool_call in result['tool_calls'].items():
        print(f"工具调用: {tool_call.name}")
        if tool_id in result['tool_results']:
            tool_result = result['tool_results'][tool_id]
            print(f"工具结果: {tool_result.result}")

def get_codewhisperer_response_data() -> bytes:
    """模拟获取 CodeWhisperer API 响应数据"""
    # 实际使用中，这里应该是真实的 API 调用
    # 例如：
    # response = requests.post('https://codewhisperer.amazonaws.com/...')
    # return response.content
    
    return create_mock_stream_data()
```

### 6.2 流式处理

```python
class StreamingParser:
    """流式解析器，适用于实时数据流"""
    
    def __init__(self):
        self.parser = CodeWhispererEventStreamParser()
        self.buffer = b''
        
    def feed_data(self, data: bytes) -> List[Dict[str, Any]]:
        """输入数据块，返回解析出的消息"""
        self.buffer += data
        messages = []
        
        while True:
            try:
                # 尝试解析一个完整消息
                if len(self.buffer) < 12:
                    break
                    
                total_length = struct.unpack('>I', self.buffer[:4])[0]
                
                if len(self.buffer) < total_length:
                    break
                    
                # 提取完整消息
                message_data = self.buffer[:total_length]
                self.buffer = self.buffer[total_length:]
                
                # 解析消息
                result = self.parser.parse_response(message_data)
                messages.extend(result.get('messages', []))
                
            except Exception as e:
                logger.error(f"流式解析出错: {e}")
                break
        
        return messages

# 使用流式解析器
def streaming_example():
    """流式解析示例"""
    streaming_parser = StreamingParser()
    
    # 模拟接收数据块
    data_chunks = [
        b'\x00\x00\x00\x50...',  # 第一个数据块
        b'\x00\x00\x00\x60...',  # 第二个数据块
        # ... 更多数据块
    ]
    
    for chunk in data_chunks:
        messages = streaming_parser.feed_data(chunk)
        
        for message in messages:
            print(f"收到消息: {message['type']}")
            
            if message['type'] == 'completion_chunk':
                print(f"代码块: {message.get('content', '')}")
            elif message['type'] == 'tool_call_request':
                print(f"工具调用请求: {len(message.get('tool_calls', []))} 个工具")
```

### 6.3 异步处理

```python
import asyncio
from typing import AsyncGenerator

class AsyncCodeWhispererParser:
    """异步 CodeWhisperer 解析器"""
    
    def __init__(self):
        self.parser = CodeWhispererEventStreamParser()
    
    async def parse_stream_async(self, data_stream: AsyncGenerator[bytes, None]) -> AsyncGenerator[Dict[str, Any], None]:
        """异步解析数据流"""
        buffer = b''
        
        async for chunk in data_stream:
            buffer += chunk
            
            # 解析完整消息
            while len(buffer) >= 12:
                try:
                    total_length = struct.unpack('>I', buffer[:4])[0]
                    
                    if len(buffer) < total_length:
                        break
                    
                    message_data = buffer[:total_length]
                    buffer = buffer[total_length:]
                    
                    result = self.parser.parse_response(message_data)
                    
                    for message in result.get('messages', []):
                        yield message
                        
                except Exception as e:
                    logger.error(f"异步解析出错: {e}")
                    break

# 异步使用示例
async def async_example():
    """异步使用示例"""
    parser = AsyncCodeWhispererParser()
    
    async def mock_data_stream():
        """模拟异步数据流"""
        chunks = [create_mock_stream_data()[i:i+100] for i in range(0, len(create_mock_stream_data()), 100)]
        for chunk in chunks:
            await asyncio.sleep(0.1)  # 模拟网络延迟
            yield chunk
    
    async for message in parser.parse_stream_async(mock_data_stream()):
        print(f"异步收到消息: {message['type']}")
        
        if message['type'] == 'tool_call_request':
            print("处理工具调用...")
            # 这里可以异步执行工具调用
            await asyncio.sleep(0.1)
```

## 7. 错误处理和调试

### 7.1 错误处理

```python
class ParseError(Exception):
    """解析错误基类"""
    pass

class InvalidMessageError(ParseError):
    """无效消息错误"""
    pass

class CRCError(ParseError):
    """CRC 校验错误"""
    pass

class RobustEventStreamParser(EventStreamParser):
    """增强的错误处理解析器"""
    
    def __init__(self, strict_mode: bool = False):
        super().__init__()
        self.strict_mode = strict_mode
        self.error_count = 0
        self.max_errors = 10
    
    def parse_stream(self, data: bytes) -> List[EventStreamMessage]:
        """带错误恢复的解析"""
        messages = []
        offset = 0
        
        while offset < len(data) and self.error_count < self.max_errors:
            try:
                message, consumed = self._parse_single_message(data[offset:])
                
                if message is None:
                    if self.strict_mode:
                        raise InvalidMessageError("无法解析消息")
                    else:
                        # 尝试跳过损坏的数据
                        offset += 1
                        continue
                
                messages.append(message)
                offset += consumed
                
            except CRCError as e:
                logger.warning(f"CRC 校验失败: {e}")
                if self.strict_mode:
                    raise
                else:
                    # 在非严格模式下继续处理
                    offset += 1
                    self.error_count += 1
                    
            except Exception as e:
                logger.error(f"解析错误: {e}")
                if self.strict_mode:
                    raise ParseError(f"解析失败: {e}")
                else:
                    # 尝试恢复
                    offset += 1
                    self.error_count += 1
        
        if self.error_count >= self.max_errors:
            logger.error("错误次数过多，停止解析")
        
        return messages
```

### 7.2 调试工具

```python
class DebugEventStreamParser(CodeWhispererEventStreamParser):
    """调试版本的解析器"""
    
    def __init__(self, debug: bool = True):
        super().__init__()
        self.debug = debug
        self.parse_stats = {
            'total_bytes': 0,
            'total_messages': 0,
            'message_types': {},
            'errors': []
        }
    
    def parse_response(self, stream_data: bytes) -> Dict[str, Any]:
        """带调试信息的解析"""
        self.parse_stats['total_bytes'] = len(stream_data)
        
        if self.debug:
            print(f"开始解析 {len(stream_data)} 字节的数据")
            self._dump_hex(stream_data[:100])  # 显示前100字节的十六进制
        
        result = super().parse_response(stream_data)
        
        self.parse_stats['total_messages'] = len(result.get('messages', []))
        
        for message in result.get('messages', []):
            msg_type = message.get('type', 'unknown')
            self.parse_stats['message_types'][msg_type] = \
                self.parse_stats['message_types'].get(msg_type, 0) + 1
        
        if self.debug:
            self._print_stats()
        
        return result
    
    def _dump_hex(self, data: bytes, width: int = 16):
        """以十六进制格式显示数据"""
        print("十六进制数据:")
        for i in range(0, len(data), width):
            hex_part = ' '.join(f'{b:02x}' for b in data[i:i+width])
            ascii_part = ''.join(chr(b) if 32 <= b < 127 else '.' for b in data[i:i+width])
            print(f"{i:08x}: {hex_part:<48} |{ascii_part}|")
    
    def _print_stats(self):
        """打印解析统计信息"""
        print("\n=== 解析统计 ===")
        print(f"总字节数: {self.parse_stats['total_bytes']}")
        print(f"总消息数: {self.parse_stats['total_messages']}")
        print("消息类型分布:")
        for msg_type, count in self.parse_stats['message_types'].items():
            print(f"  {msg_type}: {count}")
        
        if self.parse_stats['errors']:
            print("错误:")
            for error in self.parse_stats['errors']:
                print(f"  {error}")

# 调试使用示例
def debug_example():
    """调试示例"""
    debug_parser = DebugEventStreamParser(debug=True)
    
    # 使用调试解析器
    stream_data = create_mock_stream_data()
    result = debug_parser.parse_response(stream_data)
    
    # 显示详细信息
    print("\n=== 详细消息信息 ===")
    for i, message in enumerate(result['messages']):
        print(f"消息 {i+1}:")
        print(f"  类型: {message['type']}")
        if 'raw_data' in message:
            print(f"  原始数据: {json.dumps(message['raw_data'], indent=2)}")
        print()
```

### 7.3 性能监控

```python
import time
from contextlib import contextmanager

class PerformanceMonitor:
    """性能监控器"""
    
    def __init__(self):
        self.metrics = {}
    
    @contextmanager
    def measure(self, operation: str):
        """测量操作耗时"""
        start_time = time.time()
        try:
            yield
        finally:
            duration = time.time() - start_time
            if operation not in self.metrics:
                self.metrics[operation] = []
            self.metrics[operation].append(duration)
    
    def get_stats(self) -> Dict[str, Dict[str, float]]:
        """获取性能统计"""
        stats = {}
        for operation, times in self.metrics.items():
            stats[operation] = {
                'count': len(times),
                'total': sum(times),
                'average': sum(times) / len(times),
                'min': min(times),
                'max': max(times)
            }
        return stats

class PerformanceAwareParser(CodeWhispererEventStreamParser):
    """带性能监控的解析器"""
    
    def __init__(self):
        super().__init__()
        self.monitor = PerformanceMonitor()
    
    def parse_response(self, stream_data: bytes) -> Dict[str, Any]:
        """带性能监控的解析"""
        with self.monitor.measure('total_parse'):
            with self.monitor.measure('stream_parse'):
                messages = self.stream_parser.parse_stream(stream_data)
            
            processed_messages = []
            for message in messages:
                with self.monitor.measure('message_process'):
                    processed = self.message_processor.process_message(message)
                    if processed:
                        processed_messages.append(processed)
            
            result = {
                'messages': processed_messages,
                'tool_calls': dict(self.message_processor.tool_calls),
                'tool_results': dict(self.message_processor.tool_results),
                'completions': list(self.message_processor.completions),
                'summary': self._generate_summary(processed_messages),
                'performance': self.monitor.get_stats()
            }
            
            return result

# 性能测试示例
def performance_test():
    """性能测试示例"""
    parser = PerformanceAwareParser()
    
    # 测试不同大小的数据
    test_sizes = [1024, 10240, 102400]  # 1KB, 10KB, 100KB
    
    for size in test_sizes:
        # 创建测试数据
        test_data = create_mock_stream_data() * (size // len(create_mock_stream_data()) + 1)
        test_data = test_data[:size]
        
        print(f"\n测试数据大小: {size} 字节")
        
        # 解析数据
        result = parser.parse_response(test_data)
        
        # 显示性能统计
        perf_stats = result['performance']
        for operation, stats in perf_stats.items():
            print(f"{operation}:")
            print(f"  次数: {stats['count']}")
            print(f"  总耗时: {stats['total']:.4f}s")
            print(f"  平均耗时: {stats['average']:.4f}s")
            print(f"  最小耗时: {stats['min']:.4f}s")
            print(f"  最大耗时: {stats['max']:.4f}s")
```

## 总结

这个完整的 CodeWhisperer Event Stream 解析器提供了以下功能：

1. **完整的二进制格式解析**：支持 AWS Event Stream 的所有数据类型
2. **工具调用支持**：完整处理工具调用的生命周期
3. **流式处理**：支持实时数据流解析
4. **错误处理**：robust 的错误恢复机制
5. **性能监控**：内置性能分析工具
6. **调试支持**：详细的调试和诊断功能

使用时只需要：

```python
# 创建解析器
parser = CodeWhispererEventStreamParser()

# 解析响应
result = parser.parse_response(stream_data)

# 处理结果
for completion in result['completions']:
    print(completion.content)

for tool_call in result['tool_calls'].values():
    print(f"工具: {tool_call.name}, 参数: {tool_call.arguments}")
```

这个解析器可以处理 CodeWhisperer 的所有响应类型，包括普通代码补全和复杂的工具调用场景。
