// Package dcp 实现 DCP 风格的 Anthropic Messages 请求上下文裁剪：
// 对同签名 tool_use 的历史 tool_result 内容做去重，只保留最后一次。
//
// 设计思路参考 Opencode-DCP/opencode-dynamic-context-pruning，核心为
// lib/strategies/deduplication.ts + lib/messages/prune.ts。
//
// v0.2（PR2）：无状态 per-request 扫描（Claude Code 每次请求带完整 history）。
// 未来可按 X-Claude-Code-Session-Id 加 session LRU 做跨请求增量优化。
package dcp

import (
	"bytes"
	"encoding/json"
)

// messagesRequest 是 POST /v1/messages 请求体的最小解析面。
// 只显式解析 dedup 必需字段，其余通过 extra 保留原始字节实现 round-trip 无损。
type messagesRequest struct {
	Messages []message
	// extra 收纳所有非 Messages 的顶层字段，Marshal 回写时按原字节拼回。
	extra map[string]json.RawMessage
}

func (r *messagesRequest) UnmarshalJSON(data []byte) error {
	raw := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if m, ok := raw["messages"]; ok {
		if err := json.Unmarshal(m, &r.Messages); err != nil {
			return err
		}
		delete(raw, "messages")
	}
	r.extra = raw
	return nil
}

// MarshalJSON 按原字节回写 extra，再拼回 messages。
// Go map 迭代顺序不稳定，但 JSON object 字段本身就无序，Anthropic 不依赖顺序。
func (r *messagesRequest) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	first := true

	writeField := func(key string, value []byte) {
		if !first {
			buf.WriteByte(',')
		}
		first = false
		buf.WriteByte('"')
		buf.WriteString(key)
		buf.WriteString(`":`)
		buf.Write(value)
	}

	msgBytes, err := json.Marshal(r.Messages)
	if err != nil {
		return nil, err
	}
	writeField("messages", msgBytes)

	for k, v := range r.extra {
		writeField(k, v)
	}

	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// message 是 messages 数组的元素。content 做成统一的 []json.RawMessage，
// 原本可能是 string 简写，在 Unmarshal 时包成单 text block，下游同构处理。
type message struct {
	Role    string
	Content []json.RawMessage
	// extra 收纳 role/content 以外的消息级字段（如 cache_control），
	// mutated 时由 Marshal 回写，保证未来字段 round-trip 无损。
	extra map[string]json.RawMessage
	// raw 保留原始字节，Marshal 时若未被 dedup 触动直接原样吐回。
	raw []byte
	// mutated 由 dedup 流程在改动 Content 时置 true。
	mutated bool
}

func (m *message) UnmarshalJSON(data []byte) error {
	m.raw = append([]byte(nil), data...)

	raw := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if r, ok := raw["role"]; ok {
		if err := json.Unmarshal(r, &m.Role); err != nil {
			return err
		}
	}
	content, hasContent := raw["content"]
	if hasContent {
		trimmed := bytes.TrimSpace(content)
		if len(trimmed) > 0 && trimmed[0] == '"' {
			// content 是字符串：包成单 text block 供下游同构处理
			textBlock, err := json.Marshal(map[string]json.RawMessage{
				"type": json.RawMessage(`"text"`),
				"text": content,
			})
			if err != nil {
				return err
			}
			m.Content = []json.RawMessage{textBlock}
		} else {
			if err := json.Unmarshal(content, &m.Content); err != nil {
				return err
			}
		}
	}
	m.extra = map[string]json.RawMessage{}
	for k, v := range raw {
		if k == "role" || k == "content" {
			continue
		}
		m.extra[k] = v
	}
	return nil
}

func (m *message) MarshalJSON() ([]byte, error) {
	if m.raw != nil && !m.mutated {
		return m.raw, nil
	}
	// mutated：按 array 形式重建，同时保留未来/消息级未知字段
	contentBytes, err := json.Marshal(m.Content)
	if err != nil {
		return nil, err
	}
	out := map[string]json.RawMessage{
		"role":    mustMarshal(m.Role),
		"content": contentBytes,
	}
	for k, v := range m.extra {
		out[k] = v
	}
	return json.Marshal(out)
}

// blockType 只用来 peek content 数组元素的 type 字段。
type blockType struct {
	Type string `json:"type"`
}

// toolUseBlock 是 assistant 消息里的 tool_use 块。
// 只需 id/name/input 做 signature，其他字段 dedup 不关心。
type toolUseBlock struct {
	Type  string          `json:"type"`
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

// toolResultBlock 是 user 消息里的 tool_result 块。
// Content 是 RawMessage，可能是 string / array / null；替换时直接重写。
// IsError 用 *bool 区分"显式 false"与"未设置"，保证 round-trip 无损。
type toolResultBlock struct {
	Type      string
	ToolUseID string
	Content   json.RawMessage
	IsError   *bool
	// extra 收纳 cache_control 等 dedup 不关心的字段
	extra map[string]json.RawMessage
}

func (b *toolResultBlock) UnmarshalJSON(data []byte) error {
	raw := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	known := map[string]bool{"type": true, "tool_use_id": true, "content": true, "is_error": true}
	for k, v := range raw {
		switch k {
		case "type":
			if err := json.Unmarshal(v, &b.Type); err != nil {
				return err
			}
		case "tool_use_id":
			if err := json.Unmarshal(v, &b.ToolUseID); err != nil {
				return err
			}
		case "content":
			b.Content = v
		case "is_error":
			var v2 bool
			if err := json.Unmarshal(v, &v2); err != nil {
				return err
			}
			b.IsError = &v2
		}
	}
	b.extra = map[string]json.RawMessage{}
	for k, v := range raw {
		if !known[k] {
			b.extra[k] = v
		}
	}
	return nil
}

func (b *toolResultBlock) MarshalJSON() ([]byte, error) {
	out := map[string]json.RawMessage{
		"type": mustMarshal(b.Type),
	}
	if b.ToolUseID != "" {
		out["tool_use_id"] = mustMarshal(b.ToolUseID)
	}
	if len(b.Content) > 0 {
		out["content"] = b.Content
	}
	// 仅在原始 payload 显式设置过 is_error 时回写，包括 false
	if b.IsError != nil {
		out["is_error"] = mustMarshal(*b.IsError)
	}
	for k, v := range b.extra {
		out[k] = v
	}
	return json.Marshal(out)
}

func mustMarshal(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		// 仅接 string/bool 基础类型，理论不可达
		panic("dcp: marshal failed: " + err.Error())
	}
	return b
}
