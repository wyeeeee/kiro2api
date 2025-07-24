package parser

import (
	"bytes"
	"encoding/binary"
	"io"
	"kiro2api/logger"
	"strings"

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

		events = append(events, convertAssistantEventToSSE(evt))
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

func convertAssistantEventToSSE(evt assistantResponseEvent) SSEEvent {
	if evt.Content != "" {
		return SSEEvent{
			Event: "content_block_delta",
			Data: map[string]any{
				"type":  "content_block_delta",
				"index": 0,
				"delta": map[string]any{
					"type": "text_delta",
					"text": evt.Content,
				},
			},
		}
	} else if evt.ToolUseId != "" && evt.Name != "" && !evt.Stop {

		if evt.Input == nil {
			return SSEEvent{
				Event: "content_block_start",
				Data: map[string]any{
					"type":  "content_block_start",
					"index": 1,
					"content_block": map[string]any{
						"type":  "tool_use",
						"id":    evt.ToolUseId,
						"name":  evt.Name,
						"input": map[string]any{},
					},
				},
			}
		} else {
			return SSEEvent{
				Event: "content_block_delta",
				Data: map[string]any{
					"type":  "content_block_delta",
					"index": 1,
					"delta": map[string]any{
						"type":         "input_json_delta",
						"id":           evt.ToolUseId,
						"name":         evt.Name,
						"partial_json": *evt.Input,
					},
				},
			}
		}

	} else if evt.Stop {
		return SSEEvent{
			Event: "content_block_stop",
			Data: map[string]any{
				"type":  "content_block_stop",
				"index": 1,
			},
		}
	}

	return SSEEvent{}
}
