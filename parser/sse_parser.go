package parser

import (
	"bytes"
	"encoding/binary"
	"io"
	"kiro2api/logger"
	"strings"
	"sync"

	"github.com/bytedance/sonic"
)

type assistantResponseEvent struct {
	Content   string  `json:"content"`
	Input     *string `json:"input,omitempty"`
	Name      string  `json:"name"`
	ToolUseId string  `json:"toolUseId"`
	Stop      bool    `json:"stop"`
}

type toolUseEvent struct {
	Input     string `json:"input,omitempty"`
	Name      string `json:"name"`
	ToolUseId string `json:"toolUseId"`
	Stop      bool   `json:"stop"`
}

type SSEEvent struct {
	Event string `json:"event"`
	Data  any    `json:"data"`
}

func ParseEvents(resp []byte) []SSEEvent {

	events := []SSEEvent{}

	// 维护每个tool_use的内容块索引，确保符合Anthropic规范：每个内容块索引唯一且稳定
	state := newToolIndexState()

	r := bytes.NewReader(resp)
	for {
		if r.Len() < 12 {
			break
		}

		var totalLen, headerLen uint32
		if err := binary.Read(r, binary.BigEndian, &totalLen); err != nil {
			break
		}
		if err := binary.Read(r, binary.BigEndian, &headerLen); err != nil {
			break
		}

		if int(totalLen) > r.Len()+8 {
			logger.Debug("Frame length invalid", logger.Int("total_len", int(totalLen)), logger.Int("remaining", r.Len()))
			break
		}

		// Skip header
		header := make([]byte, headerLen)
		if _, err := io.ReadFull(r, header); err != nil {
			break
		}

		payloadLen := int(totalLen) - int(headerLen) - 12
		payload := make([]byte, payloadLen)
		if _, err := io.ReadFull(r, payload); err != nil {
			break
		}

		// Skip CRC32
		if _, err := r.Seek(4, io.SeekCurrent); err != nil {
			break
		}

		payloadStr := strings.TrimPrefix(string(payload), "vent")

		// 检查事件类型并解析相应的结构
		var evt assistantResponseEvent
		var parseError error

		if strings.Contains(string(header), "toolUseEvent") {
			// 处理toolUseEvent
			var toolEvt toolUseEvent
			if parseError = sonic.Unmarshal([]byte(payloadStr), &toolEvt); parseError == nil {
				// 转换toolUseEvent为assistantResponseEvent格式以复用现有转换逻辑
				evt = assistantResponseEvent{
					Content:   "",
					Name:      toolEvt.Name,
					ToolUseId: toolEvt.ToolUseId,
					Stop:      toolEvt.Stop,
				}
				if toolEvt.Input != "" {
					evt.Input = &toolEvt.Input
				}
			} else {
				logger.Debug("toolUseEvent JSON解析失败", logger.Err(parseError), logger.String("payload", payloadStr))
				continue
			}
		} else {
			// 处理assistantResponseEvent
			if parseError = sonic.Unmarshal([]byte(payloadStr), &evt); parseError != nil {
				logger.Debug("assistantResponseEvent JSON解析失败", logger.Err(parseError), logger.String("payload", payloadStr))
				continue
			}
		}

		converted := convertAssistantEventToSSE(evt, state)
		if len(converted) > 0 {
			events = append(events, converted...)
		}
		addToolUseStopEvent(&events, evt)
	}

	return events
}

// addToolUseStopEvent 添加工具使用停止事件
func addToolUseStopEvent(events *[]SSEEvent, evt assistantResponseEvent) {
	if evt.ToolUseId != "" && evt.Name != "" && evt.Stop {
		*events = append(*events, SSEEvent{
			Event: "message_delta",
			Data: map[string]any{
				"type": "message_delta",
				"delta": map[string]any{
					"stop_reason":   "tool_use",
					"stop_sequence": nil,
				},
				"usage": map[string]any{"output_tokens": 0},
			},
		})
	}
}

// 工具内容块索引状态
type toolIndexState struct {
	toolIndexById map[string]int
	nextIndex     int
}

func newToolIndexState() *toolIndexState {
	return &toolIndexState{toolIndexById: make(map[string]int), nextIndex: 1}
}

func (s *toolIndexState) getOrAssignIndex(toolUseId string) int {
	if idx, ok := s.toolIndexById[toolUseId]; ok {
		return idx
	}
	idx := s.nextIndex
	s.toolIndexById[toolUseId] = idx
	s.nextIndex++
	return idx
}

// convertAssistantEventToSSE 根据事件内容生成符合Anthropic规范的SSE事件序列
func convertAssistantEventToSSE(evt assistantResponseEvent, state *toolIndexState) []SSEEvent {
	// 文本内容增量，固定为索引0
	if evt.Content != "" {
		return []SSEEvent{
			{
				Event: "content_block_delta",
				Data: map[string]any{
					"type":  "content_block_delta",
					"index": 0,
					"delta": map[string]any{
						"type": "text_delta",
						"text": evt.Content,
					},
				},
			},
		}
	}

	// 工具使用：开始与输入增量
	if evt.ToolUseId != "" && evt.Name != "" && !evt.Stop {
		blockIndex := state.getOrAssignIndex(evt.ToolUseId)
		if evt.Input == nil {
			return []SSEEvent{
				{
					Event: "content_block_start",
					Data: map[string]any{
						"type":  "content_block_start",
						"index": blockIndex,
						"content_block": map[string]any{
							"type":  "tool_use",
							"id":    evt.ToolUseId,
							"name":  evt.Name,
							"input": map[string]any{},
						},
					},
				},
			}
		}
		// 输入增量
		return []SSEEvent{
			{
				Event: "content_block_delta",
				Data: map[string]any{
					"type":  "content_block_delta",
					"index": blockIndex,
					"delta": map[string]any{
						"type":         "input_json_delta",
						"partial_json": *evt.Input,
					},
				},
			},
		}
	}

	// 工具结束：停止对应索引
	if evt.Stop && evt.ToolUseId != "" {
		blockIndex := state.getOrAssignIndex(evt.ToolUseId)
		return []SSEEvent{
			{
				Event: "content_block_stop",
				Data: map[string]any{
					"type":  "content_block_stop",
					"index": blockIndex,
				},
			},
		}
	}

	return nil
}

// StreamParser 处理流式EventStream数据
type StreamParser struct {
	buffer []byte
	events []SSEEvent // 预分配事件切片，复用内存
}

// StreamParserPool 对象池，用于复用StreamParser实例，减少内存分配
type StreamParserPool struct {
	pool sync.Pool
}

// 全局StreamParser对象池
var GlobalStreamParserPool = &StreamParserPool{
	pool: sync.Pool{
		New: func() any {
			return &StreamParser{
				buffer: make([]byte, 0, 4096),   // 预分配4KB缓冲区
				events: make([]SSEEvent, 0, 16), // 预分配16个事件空间
			}
		},
	},
}

// Get 从对象池获取StreamParser实例
func (spp *StreamParserPool) Get() *StreamParser {
	return spp.pool.Get().(*StreamParser)
}

// Put 将StreamParser实例放回对象池
func (spp *StreamParserPool) Put(sp *StreamParser) {
	// 重置但保留容量
	sp.buffer = sp.buffer[:0]
	sp.events = sp.events[:0]
	spp.pool.Put(sp)
}

// NewStreamParser 创建新的流式解析器
// 推荐使用 GlobalStreamParserPool.Get() 来获取复用实例
func NewStreamParser() *StreamParser {
	return &StreamParser{
		buffer: make([]byte, 0, 1024),  // 默认1KB缓冲区
		events: make([]SSEEvent, 0, 8), // 默认8个事件空间
	}
}

// Reset 重置StreamParser状态，用于复用
func (sp *StreamParser) Reset() {
	sp.buffer = sp.buffer[:0]
	sp.events = sp.events[:0]
}

// ParseStream 解析流式数据，返回解析出的事件
func (sp *StreamParser) ParseStream(data []byte) []SSEEvent {
	// 将新数据添加到缓冲区
	sp.buffer = append(sp.buffer, data...)

	// 重置事件切片但保留容量
	sp.events = sp.events[:0]

	for {
		if len(sp.buffer) < 12 {
			// 需要更多数据才能读取头部
			break
		}

		// 读取总长度和头部长度
		totalLen := binary.BigEndian.Uint32(sp.buffer[0:4])
		_ = binary.BigEndian.Uint32(sp.buffer[4:8]) // headerLen unused but needed for offset

		if totalLen > uint32(len(sp.buffer)) {
			// 需要更多数据才能读取完整消息
			break
		}

		// 提取单个消息
		messageData := sp.buffer[:totalLen]
		sp.buffer = sp.buffer[totalLen:]

		// 解析这个消息
		messageEvents := ParseEvents(messageData)
		sp.events = append(sp.events, messageEvents...)
	}

	return sp.events
}
