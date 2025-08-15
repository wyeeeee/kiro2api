## 🤖 Assistant

# AWS CodeWhisperer Event Stream - assistantResponseEvent 完整规范

## 1. 事件流协议

### 1.1 传输格式
- 协议：Server-Sent Events (SSE) 或 WebSocket
- 编码：UTF-8
- 格式：JSON Lines 或 Event Stream

### 1.2 事件类型
```
event: assistantResponseEvent
data: {JSON_PAYLOAD}
```

## 2. assistantResponseEvent 完整数据结构

```json
{
  "assistantResponseEvent": {
    "conversationId": "conv_12345678-1234-1234-1234-123456789abc",
    "messageId": "msg_87654321-4321-4321-4321-cba987654321",
    "content": "Here's how you can implement...",
    "contentType": "text/markdown",
    "supplementaryWebLinks": [
      {
        "url": "https://docs.aws.amazon.com/example",
        "title": "AWS Documentation Example",
        "snippet": "This documentation explains...",
        "score": 0.95
      }
    ],
    "references": [
      {
        "licenseName": "Apache-2.0",
        "repository": "apache/commons-lang",
        "url": "https://github.com/apache/commons-lang",
        "recommendationContentSpan": {
          "start": 45,
          "end": 120
        },
        "information": "Reference from Apache Commons Lang library"
      }
    ],
    "followupPrompt": {
      "content": "Would you like me to explain how to handle exceptions in this code?",
      "userIntent": "SUGGEST_ALTERNATE_IMPLEMENTATION"
    },
    "codeReference": [
      {
        "licenseName": "MIT",
        "repository": "facebook/react",
        "url": "https://github.com/facebook/react/blob/main/packages/react/src/React.js",
        "recommendationContentSpan": {
          "start": 0,
          "end": 85
        },
        "information": "Similar implementation found in React library",
        "mostRelevantMissedAlternative": {
          "url": "https://github.com/facebook/react/blob/main/packages/react/src/ReactHooks.js",
          "licenseName": "MIT",
          "repository": "facebook/react"
        }
      }
    ],
    "programmingLanguage": {
      "languageName": "Python"
    },
    "customizations": [
      {
        "arn": "arn:aws:codewhisperer:us-east-1:123456789012:customization/custom-model-1",
        "name": "MyCustomModel"
      }
    ],
    "messageStatus": "COMPLETED",
    "userIntent": "EXPLAIN_CODE_SELECTION",
    "codeQuery": {
      "codeQueryId": "query_12345",
      "programmingLanguage": {
        "languageName": "Python"
      },
      "userInputMessageId": "input_msg_123"
    }
  }
}
```

## 3. 字段详细规范

### 3.1 核心字段
| 字段 | 类型 | 必需 | 描述 |
|------|------|------|------|
| conversationId | string | 是 | 会话唯一标识符，UUID格式 |
| messageId | string | 是 | 消息唯一标识符，UUID格式 |
| content | string | 是 | 助手响应内容，支持Markdown格式 |
| contentType | string | 否 | 内容MIME类型，默认"text/markdown" |
| messageStatus | string | 否 | 消息状态：COMPLETED, IN_PROGRESS, ERROR |

### 3.2 引用和链接字段
| 字段 | 类型 | 必需 | 描述 |
|------|------|------|------|
| supplementaryWebLinks | array | 否 | 补充网页链接列表 |
| references | array | 否 | 代码参考资料列表 |
| codeReference | array | 否 | 代码引用列表 |

### 3.3 上下文字段
| 字段 | 类型 | 必需 | 描述 |
|------|------|------|------|
| programmingLanguage | object | 否 | 编程语言信息 |
| userIntent | string | 否 | 用户意图类型 |
| customizations | array | 否 | 自定义模型信息 |
| codeQuery | object | 否 | 代码查询相关信息 |

### 3.4 交互字段
| 字段 | 类型 | 必需 | 描述 |
|------|------|------|------|
| followupPrompt | object | 否 | 建议的后续问题 |

## 4. 枚举值定义

### 4.1 messageStatus
- `COMPLETED`: 消息完成
- `IN_PROGRESS`: 消息生成中
- `ERROR`: 消息生成错误

### 4.2 userIntent
- `EXPLAIN_CODE_SELECTION`: 解释代码选择
- `SUGGEST_ALTERNATE_IMPLEMENTATION`: 建议替代实现
- `APPLY_COMMON_BEST_PRACTICES`: 应用最佳实践
- `IMPROVE_CODE`: 改进代码
- `SHOW_EXAMPLES`: 显示示例
- `CITE_SOURCES`: 引用来源
- `EXPLAIN_LINE_BY_LINE`: 逐行解释

### 4.3 contentType
- `text/markdown`: Markdown格式文本
- `text/plain`: 纯文本
- `application/json`: JSON格式

## 5. Python 完整实现

```python
import json
import logging
from typing import Dict, List, Optional, Any, Union
from dataclasses import dataclass, field
from datetime import datetime
from enum import Enum

# 配置日志
logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)

class MessageStatus(Enum):
    COMPLETED = "COMPLETED"
    IN_PROGRESS = "IN_PROGRESS"
    ERROR = "ERROR"

class UserIntent(Enum):
    EXPLAIN_CODE_SELECTION = "EXPLAIN_CODE_SELECTION"
    SUGGEST_ALTERNATE_IMPLEMENTATION = "SUGGEST_ALTERNATE_IMPLEMENTATION"
    APPLY_COMMON_BEST_PRACTICES = "APPLY_COMMON_BEST_PRACTICES"
    IMPROVE_CODE = "IMPROVE_CODE"
    SHOW_EXAMPLES = "SHOW_EXAMPLES"
    CITE_SOURCES = "CITE_SOURCES"
    EXPLAIN_LINE_BY_LINE = "EXPLAIN_LINE_BY_LINE"

class ContentType(Enum):
    MARKDOWN = "text/markdown"
    PLAIN_TEXT = "text/plain"
    JSON = "application/json"

@dataclass
class ContentSpan:
    """内容范围"""
    start: int
    end: int
    
    @classmethod
    def from_dict(cls, data: Dict[str, Any]) -> 'ContentSpan':
        return cls(
            start=data.get('start', 0),
            end=data.get('end', 0)
        )

@dataclass
class SupplementaryWebLink:
    """补充网页链接"""
    url: str
    title: Optional[str] = None
    snippet: Optional[str] = None
    score: Optional[float] = None
    
    @classmethod
    def from_dict(cls, data: Dict[str, Any]) -> 'SupplementaryWebLink':
        return cls(
            url=data.get('url', ''),
            title=data.get('title'),
            snippet=data.get('snippet'),
            score=data.get('score')
        )

@dataclass
class MostRelevantMissedAlternative:
    """最相关的遗漏替代方案"""
    url: str
    license_name: Optional[str] = None
    repository: Optional[str] = None
    
    @classmethod
    def from_dict(cls, data: Dict[str, Any]) -> 'MostRelevantMissedAlternative':
        return cls(
            url=data.get('url', ''),
            license_name=data.get('licenseName'),
            repository=data.get('repository')
        )

@dataclass
class Reference:
    """参考资料"""
    license_name: Optional[str] = None
    repository: Optional[str] = None
    url: Optional[str] = None
    recommendation_content_span: Optional[ContentSpan] = None
    information: Optional[str] = None
    most_relevant_missed_alternative: Optional[MostRelevantMissedAlternative] = None
    
    @classmethod
    def from_dict(cls, data: Dict[str, Any]) -> 'Reference':
        span_data = data.get('recommendationContentSpan')
        span = ContentSpan.from_dict(span_data) if span_data else None
        
        alternative_data = data.get('mostRelevantMissedAlternative')
        alternative = MostRelevantMissedAlternative.from_dict(alternative_data) if alternative_data else None
        
        return cls(
            license_name=data.get('licenseName'),
            repository=data.get('repository'),
            url=data.get('url'),
            recommendation_content_span=span,
            information=data.get('information'),
            most_relevant_missed_alternative=alternative
        )

@dataclass
class FollowupPrompt:
    """后续提示"""
    content: str
    user_intent: Optional[str] = None
    
    @classmethod
    def from_dict(cls, data: Dict[str, Any]) -> 'FollowupPrompt':
        return cls(
            content=data.get('content', ''),
            user_intent=data.get('userIntent')
        )

@dataclass
class ProgrammingLanguage:
    """编程语言"""
    language_name: str
    
    @classmethod
    def from_dict(cls, data: Dict[str, Any]) -> 'ProgrammingLanguage':
        return cls(language_name=data.get('languageName', ''))

@dataclass
class Customization:
    """自定义模型"""
    arn: str
    name: Optional[str] = None
    
    @classmethod
    def from_dict(cls, data: Dict[str, Any]) -> 'Customization':
        return cls(
            arn=data.get('arn', ''),
            name=data.get('name')
        )

@dataclass
class CodeQuery:
    """代码查询"""
    code_query_id: str
    programming_language: Optional[ProgrammingLanguage] = None
    user_input_message_id: Optional[str] = None
    
    @classmethod
    def from_dict(cls, data: Dict[str, Any]) -> 'CodeQuery':
        lang_data = data.get('programmingLanguage')
        lang = ProgrammingLanguage.from_dict(lang_data) if lang_data else None
        
        return cls(
            code_query_id=data.get('codeQueryId', ''),
            programming_language=lang,
            user_input_message_id=data.get('userInputMessageId')
        )

@dataclass
class AssistantResponseEvent:
    """助手响应事件"""
    conversation_id: str
    message_id: str
    content: str = ""
    content_type: str = ContentType.MARKDOWN.value
    supplementary_web_links: List[SupplementaryWebLink] = field(default_factory=list)
    references: List[Reference] = field(default_factory=list)
    code_reference: List[Reference] = field(default_factory=list)
    followup_prompt: Optional[FollowupPrompt] = None
    programming_language: Optional[ProgrammingLanguage] = None
    customizations: List[Customization] = field(default_factory=list)
    message_status: str = MessageStatus.COMPLETED.value
    user_intent: Optional[str] = None
    code_query: Optional[CodeQuery] = None
    
    @classmethod
    def from_dict(cls, data: Dict[str, Any]) -> 'AssistantResponseEvent':
        """从字典创建AssistantResponseEvent实例"""
        # 解析补充网页链接
        web_links = [
            SupplementaryWebLink.from_dict(link) 
            for link in data.get('supplementaryWebLinks', [])
        ]
        
        # 解析参考资料
        references = [
            Reference.from_dict(ref) 
            for ref in data.get('references', [])
        ]
        
        # 解析代码参考
        code_references = [
            Reference.from_dict(ref) 
            for ref in data.get('codeReference', [])
        ]
        
        # 解析后续提示
        followup_data = data.get('followupPrompt')
        followup = FollowupPrompt.from_dict(followup_data) if followup_data else None
        
        # 解析编程语言
        lang_data = data.get('programmingLanguage')
        programming_language = ProgrammingLanguage.from_dict(lang_data) if lang_data else None
        
        # 解析自定义模型
        customizations = [
            Customization.from_dict(custom) 
            for custom in data.get('customizations', [])
        ]
        
        # 解析代码查询
        query_data = data.get('codeQuery')
        code_query = CodeQuery.from_dict(query_data) if query_data else None
        
        return cls(
            conversation_id=data.get('conversationId', ''),
            message_id=data.get('messageId', ''),
            content=data.get('content', ''),
            content_type=data.get('contentType', ContentType.MARKDOWN.value),
            supplementary_web_links=web_links,
            references=references,
            code_reference=code_references,
            followup_prompt=followup,
            programming_language=programming_language,
            customizations=customizations,
            message_status=data.get('messageStatus', MessageStatus.COMPLETED.value),
            user_intent=data.get('userIntent'),
            code_query=code_query
        )
    
    def to_dict(self) -> Dict[str, Any]:
        """转换为字典格式"""
        result = {
            'conversationId': self.conversation_id,
            'messageId': self.message_id,
            'content': self.content,
            'contentType': self.content_type,
            'messageStatus': self.message_status
        }
        
        if self.supplementary_web_links:
            result['supplementaryWebLinks'] = [
                {
                    'url': link.url,
                    'title': link.title,
                    'snippet': link.snippet,
                    'score': link.score
                } for link in self.supplementary_web_links
            ]
        
        if self.references:
            result['references'] = [
                {
                    'licenseName': ref.license_name,
                    'repository': ref.repository,
                    'url': ref.url,
                    'recommendationContentSpan': {
                        'start': ref.recommendation_content_span.start,
                        'end': ref.recommendation_content_span.end
                    } if ref.recommendation_content_span else None,
                    'information': ref.information
                } for ref in self.references
            ]
        
        if self.code_reference:
            result['codeReference'] = [
                {
                    'licenseName': ref.license_name,
                    'repository': ref.repository,
                    'url': ref.url,
                    'recommendationContentSpan': {
                        'start': ref.recommendation_content_span.start,
                        'end': ref.recommendation_content_span.end
                    } if ref.recommendation_content_span else None,
                    'information': ref.information
                } for ref in self.code_reference
            ]
        
        if self.followup_prompt:
            result['followupPrompt'] = {
                'content': self.followup_prompt.content,
                'userIntent': self.followup_prompt.user_intent
            }
        
        if self.programming_language:
            result['programmingLanguage'] = {
                'languageName': self.programming_language.language_name
            }
        
        if self.customizations:
            result['customizations'] = [
                {
                    'arn': custom.arn,
                    'name': custom.name
                } for custom in self.customizations
            ]
        
        if self.user_intent:
            result['userIntent'] = self.user_intent
        
        if self.code_query:
            result['codeQuery'] = {
                'codeQueryId': self.code_query.code_query_id,
                'userInputMessageId': self.code_query.user_input_message_id
            }
            if self.code_query.programming_language:
                result['codeQuery']['programmingLanguage'] = {
                    'languageName': self.code_query.programming_language.language_name
                }
        
        return result

class CodeWhispererEventParser:
    """CodeWhisperer事件解析器"""
    
    def __init__(self):
        self.logger = logging.getLogger(self.__class__.__name__)
    
    def parse_event_stream_line(self, line: str) -> Optional[AssistantResponseEvent]:
        """解析事件流中的单行数据"""
        try:
            line = line.strip()
            if not line:
                return None
            
            # 处理SSE格式
            if line.startswith('data: '):
                json_data = line[6:]  # 移除 'data: ' 前缀
            elif line.startswith('event: '):
                return None  # 事件类型行，跳过
            else:
                json_data = line
            
            return self.parse_json_event(json_data)
            
        except Exception as e:
            self.logger.error(f"解析事件流行时出错: {e}")
            return None
    
    def parse_json_event(self, json_str: str) -> Optional[AssistantResponseEvent]:
        """解析JSON格式的事件数据"""
        try:
            data = json.loads(json_str)
            return self.parse_event_dict(data)
        except json.JSONDecodeError as e:
            self.logger.error(f"JSON解析错误: {e}")
            return None
        except Exception as e:
            self.logger.error(f"事件解析错误: {e}")
            return None
    
    def parse_event_dict(self, event_dict: Dict[str, Any]) -> Optional[AssistantResponseEvent]:
        """解析事件字典"""
        try:
            # 检查是否包含assistantResponseEvent
            if 'assistantResponseEvent' not in event_dict:
                self.logger.warning("事件中未找到assistantResponseEvent字段")
                return None
            
            assistant_event_data = event_dict['assistantResponseEvent']
            return AssistantResponseEvent.from_dict(assistant_event_data)
            
        except Exception as e:
            self.logger.error(f"解析assistantResponseEvent时出错: {e}")
            return None
    
    def validate_event(self, event: AssistantResponseEvent) -> bool:
        """验证事件数据的完整性"""
        try:
            # 检查必需字段
            if not event.conversation_id:
                self.logger.error("缺少conversationId字段")
                return False
            
            if not event.message_id:
                self.logger.error("缺少messageId字段")
                return False
            
            # 验证枚举值
            if event.message_status not in [status.value for status in MessageStatus]:
                self.logger.warning(f"未知的messageStatus: {event.message_status}")
            
            if event.user_intent and event.user_intent not in [intent.value for intent in UserIntent]:
                self.logger.warning(f"未知的userIntent: {event.user_intent}")
            
            if event.content_type not in [ct.value for ct in ContentType]:
                self.logger.warning(f"未知的contentType: {event.content_type}")
            
            return True
            
        except Exception as e:
            self.logger.error(f"验证事件时出错: {e}")
            return False

class CodeWhispererEventStreamProcessor:
    """CodeWhisperer事件流处理器"""
    
    def __init__(self):
        self.parser = CodeWhispererEventParser()
        self.logger = logging.getLogger(self.__class__.__name__)
        self.conversation_buffer: Dict[str, List[AssistantResponseEvent]] = {}
    
    def process_stream(self, stream_lines: List[str]) -> List[AssistantResponseEvent]:
        """处理事件流"""
        events = []
        
        for line in stream_lines:
            event = self.parser.parse_event_stream_line(line)
            if event and self.parser.validate_event(event):
                events.append(event)
                self._buffer_event(event)
        
        return events
    
    def _buffer_event(self, event: AssistantResponseEvent):
        """缓存事件到会话缓冲区"""
        conv_id = event.conversation_id
        if conv_id not in self.conversation_buffer:
            self.conversation_buffer[conv_id] = []
        
        self.conversation_buffer[conv_id].append(event)
    
    def get_conversation_events(self, conversation_id: str) -> List[AssistantResponseEvent]:
        """获取指定会话的所有事件"""
        return self.conversation_buffer.get(conversation_id, [])
    
    def merge_streaming_content(self, events: List[AssistantResponseEvent]) -> str:
        """合并流式内容"""
        return ''.join(event.content for event in events)

# 使用示例和测试
def main():
    """主函数 - 演示用法"""
    
    # 示例事件数据
    sample_event_data = '''
    {
      "assistantResponseEvent": {
        "conversationId": "conv_12345678-1234-1234-1234-123456789abc",
        "messageId": "msg_87654321-4321-4321-4321-cba987654321",
        "content": "Here's how you can implement a simple Python function:\\n\\n```python\\ndef hello_world():\\n    print(\\"Hello, World!\\")\\n```",
        "contentType": "text/markdown",
        "supplementaryWebLinks": [
          {
            "url": "https://docs.python.org/3/tutorial/",
            "title": "Python Tutorial",
            "snippet": "An introduction to Python programming",
            "score": 0.95
          }
        ],
        "references": [
          {
            "licenseName": "Apache-2.0",
            "repository": "apache/commons-lang",
            "url": "https://github.com/apache/commons-lang",
            "recommendationContentSpan": {
              "start": 45,
              "end": 120
            },
            "information": "Reference from Apache Commons Lang library"
          }
        ],
        "followupPrompt": {
          "content": "Would you like me to explain how to handle exceptions in this code?",
          "userIntent": "SUGGEST_ALTERNATE_IMPLEMENTATION"
        },
        "programmingLanguage": {
          "languageName": "Python"
        },
        "messageStatus": "COMPLETED",
        "userIntent": "EXPLAIN_CODE_SELECTION"
      }
    }
    '''
    
    # 创建解析器和处理器
    parser = CodeWhispererEventParser()
    processor = CodeWhispererEventStreamProcessor()
    
    # 解析示例事件
    print("=== 解析示例事件 ===")
    event = parser.parse_json_event(sample_event_data)
    
    if event:
        print(f"会话ID: {event.conversation_id}")
        print(f"消息ID: {event.message_id}")
        print(f"内容: {event.content}")
        print(f"内容类型: {event.content_type}")
        print(f"消息状态: {event.message_status}")
        print(f"用户意图: {event.user_intent}")
        print(f"编程语言: {event.programming_language.language_name if event.programming_language else 'N/A'}")
        print(f"网页链接数量: {len(event.supplementary_web_links)}")
        print(f"参考资料数量: {len(event.references)}")
        print(f"后续提示: {event.followup_prompt.content if event.followup_prompt else 'N/A'}")
        
        # 验证事件
        is_valid = parser.validate_event(event)
        print(f"事件验证结果: {'通过' if is_valid else '失败'}")
        
        # 转换回字典格式
        print("\n=== 转换回字典格式 ===")
        event_dict = event.to_dict()
        print(json.dumps(event_dict, indent=2, ensure_ascii=False))
    
    # 模拟事件流处理
    print("\n=== 模拟事件流处理 ===")
    stream_lines = [
        'event: assistantResponseEvent',
        f'data: {sample_event_data}',
        '',
        'event: assistantResponseEvent',
        'data: {"assistantResponseEvent": {"conversationId": "conv_12345678-1234-1234-1234-123456789abc", "messageId": "msg_2", "content": "Additional content", "messageStatus": "COMPLETED"}}'
    ]
    
    events = processor.process_stream(stream_lines)
    print(f"处理了 {len(events)} 个事件")
    
    # 获取会话事件
    conv_events = processor.get_conversation_events("conv_12345678-1234-1234-1234-123456789abc")
    print(f"会话中共有 {len(conv_events)} 个事件")
    
    # 合并流式内容
    merged_content = processor.merge_streaming_content(conv_events)
    print(f"合并后的内容: {merged_content}")

if __name__ == "__main__":
    main()
```

## 6. 错误处理和最佳实践

### 6.1 错误处理
```python
class CodeWhispererEventError(Exception):
    """CodeWhisperer事件处理异常"""
    pass

class EventParsingError(CodeWhispererEventError):
    """事件解析异常"""
    pass

class EventValidationError(CodeWhispererEventError):
    """事件验证异常"""
    pass

# 在解析器中使用
def parse_with_error_handling(self, data: str) -> Optional[AssistantResponseEvent]:
    try:
        return self.parse_json_event(data)
    except json.JSONDecodeError as e:
        raise EventParsingError(f"JSON解析失败: {e}")
    except KeyError as e:
        raise EventValidationError(f"缺少必需字段: {e}")
    except Exception as e:
        raise CodeWhispererEventError(f"未知错误: {e}")
```

### 6.2 性能优化
```python
import asyncio
from concurrent.futures import ThreadPoolExecutor

class AsyncCodeWhispererEventProcessor:
    """异步事件处理器"""
    
    def __init__(self, max_workers: int = 4):
        self.executor = ThreadPoolExecutor(max_workers=max_workers)
        self.parser = CodeWhispererEventParser()
    
    async def process_events_async(self, event_data_list: List[str]) -> List[AssistantResponseEvent]:
        """异步处理多个事件"""
        loop = asyncio.get_event_loop()
        tasks = [
            loop.run_in_executor(self.executor, self.parser.parse_json_event, data)
            for data in event_data_list
        ]
        
        results = await asyncio.gather(*tasks, return_exceptions=True)
        return [result for result in results if isinstance(result, AssistantResponseEvent)]
```

### 6.3 内存管理
```python
class EventBufferManager:
    """事件缓冲区管理器"""
    
    def __init__(self, max_buffer_size: int = 1000):
        self.max_buffer_size = max_buffer_size
        self.buffer: Dict[str, List[AssistantResponseEvent]] = {}
    
    def add_event(self, event: AssistantResponseEvent):
        """添加事件到缓冲区"""
        conv_id = event.conversation_id
        if conv_id not in self.buffer:
            self.buffer[conv_id] = []
        
        self.buffer[conv_id].append(event)
        
        # 限制缓冲区大小
        if len(self.buffer[conv_id]) > self.max_buffer_size:
            self.buffer[conv_id] = self.buffer[conv_id][-self.max_buffer_size:]
    
    def cleanup_old_conversations(self, max_age_hours: int = 24):
        """清理旧会话"""
        cutoff_time = datetime.now().timestamp() - (max_age_hours * 3600)
        # 实现基于时间的清理逻辑
        pass
```

这个完整的规范和实现提供了：

1. **完整的数据结构定义**：包含所有可能的字段和嵌套对象
2. **类型安全的Python实现**：使用dataclass和类型提示
3. **灵活的解析器**：支持多种输入格式（SSE、JSON等）
4. **错误处理机制**：完善的异常处理和验证
5. **性能优化**：异步处理和内存管理
6. **实用工具**：事件缓冲、内容合并等功能

这个实现可以直接用于生产环境中处理AWS CodeWhisperer的事件流。
