package dcp

import (
	"encoding/json"
)

// PlaceholderContent 是替换被 dedup 掉的 tool_result 内容的固定字符串。
// 与 Opencode-DCP 保持一致（lib/messages/prune.ts:9）。
const PlaceholderContent = "[Output removed to save context - information superseded or no longer needed]"

// Logger 最小日志接口（与 internal/apiproxy.Logger 同形，但独立声明避免包循环）。
type Logger interface {
	Infof(format string, args ...any)
	Warnf(format string, args ...any)
}

// NewTransformer 返回一个闭包 transform：把 POST /v1/messages body 做 DCP
// 风格去重后返回新 body。任何失败都降级为原样返回（policy B）。
func NewTransformer(logger Logger) func([]byte) []byte {
	return func(body []byte) []byte {
		return Transform(body, logger)
	}
}

// Transform 对外入口：解析 -> 改写 -> 序列化。
// 失败全部降级为原 body，保证 claude 感知不到 dcp 异常。
func Transform(body []byte, logger Logger) []byte {
	var req messagesRequest
	if err := json.Unmarshal(body, &req); err != nil {
		logger.Warnf("dcp: parse failed, passthrough: %v", err)
		return body
	}

	changed := rewrite(&req, logger)
	if !changed {
		return body
	}

	out, err := json.Marshal(&req)
	if err != nil {
		logger.Warnf("dcp: marshal failed, passthrough: %v", err)
		return body
	}
	return out
}

// rewrite 扫描 messages，按 DCP 规则标记要裁剪的 tool_use_id，
// 然后重写对应 tool_result 的 content 为占位符。
// 返回是否有任何 block 被改动。
func rewrite(req *messagesRequest, logger Logger) bool {
	type toolUseRef struct {
		id  string
		sig string
	}

	// 第一遍：扫描 assistant 消息里的 tool_use，按消息位置顺序收集。
	var uses []toolUseRef
	for _, msg := range req.Messages {
		if msg.Role != "assistant" {
			continue
		}
		for _, raw := range msg.Content {
			var bt blockType
			if err := json.Unmarshal(raw, &bt); err != nil {
				continue
			}
			if bt.Type != "tool_use" {
				continue
			}
			var tu toolUseBlock
			if err := json.Unmarshal(raw, &tu); err != nil {
				continue
			}
			uses = append(uses, toolUseRef{
				id:  tu.ID,
				sig: createSignature(tu.Name, tu.Input),
			})
		}
	}

	if len(uses) == 0 {
		return false
	}

	// 按 sig 分组，保留每组最后一个，其余标记 pruned。
	lastIdxBySig := map[string]int{}
	for i, u := range uses {
		lastIdxBySig[u.sig] = i
	}
	pruned := map[string]bool{}
	for i, u := range uses {
		if lastIdxBySig[u.sig] != i {
			pruned[u.id] = true
		}
	}

	if len(pruned) == 0 {
		return false
	}

	// 第二遍：在 user 消息里找 tool_result，对 pruned 命中的 tool_use_id 做内容替换。
	replacement, err := json.Marshal(PlaceholderContent)
	if err != nil {
		// string marshal 不可能失败
		logger.Warnf("dcp: placeholder marshal: %v", err)
		return false
	}

	changed := false
	for mi := range req.Messages {
		msg := &req.Messages[mi]
		if msg.Role != "user" {
			continue
		}
		for bi, raw := range msg.Content {
			var bt blockType
			if err := json.Unmarshal(raw, &bt); err != nil {
				continue
			}
			if bt.Type != "tool_result" {
				continue
			}
			var tr toolResultBlock
			if err := json.Unmarshal(raw, &tr); err != nil {
				continue
			}
			if !pruned[tr.ToolUseID] {
				continue
			}
			// is_error=true 的 tool_result 不 dedup —— 错误消息对模型诊断更有价值
			if tr.IsError != nil && *tr.IsError {
				continue
			}

			tr.Content = replacement
			newBlock, err := json.Marshal(&tr)
			if err != nil {
				continue
			}
			msg.Content[bi] = newBlock
			msg.mutated = true
			changed = true
		}
	}

	return changed
}
