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

// Transformer 持有 Logger 与累积 Stats。Transformer 实例在 Server 生命周期内共享，
// 多个并发请求同时调用 Transform 时通过 Stats 的 atomic 字段保证计数安全。
type Transformer struct {
	logger Logger
	stats  *Stats
}

// NewTransformer 构造 Transformer 并返回；调用方通过 .Transform 做转换，
// 通过 .Stats() 读取累积观测。
func NewTransformer(logger Logger) *Transformer {
	return &Transformer{logger: logger, stats: &Stats{}}
}

// Stats 暴露累积观测（非快照，持续被 Transform 更新）。
func (t *Transformer) Stats() *Stats { return t.stats }

// Transform 解析 -> 改写 -> 序列化。任何失败都降级为原 body（policy B）。
// 并发安全：内部仅访问 Stats 的 atomic 字段与调用栈局部 state。
func (t *Transformer) Transform(body []byte) []byte {
	t.stats.RequestsProcessed.Add(1)

	var req messagesRequest
	if err := json.Unmarshal(body, &req); err != nil {
		t.logger.Warnf("dcp: 解析失败，透传: %v", err)
		return body
	}

	toolUses, replaced := rewrite(&req, t.logger)
	t.stats.ToolUseScanned.Add(int64(toolUses))
	if replaced == 0 {
		return body
	}

	out, err := json.Marshal(&req)
	if err != nil {
		t.logger.Warnf("dcp: 序列化失败，透传: %v", err)
		return body
	}

	t.stats.ResultsReplaced.Add(int64(replaced))
	t.stats.BytesInput.Add(int64(len(body)))
	t.stats.BytesOutput.Add(int64(len(out)))
	t.logger.Infof("dcp: 替换 %d 条 tool_result，%d -> %d 字节", replaced, len(body), len(out))
	return out
}

// rewrite 扫描 messages，按 DCP 规则标记要裁剪的 tool_use_id，
// 然后重写对应 tool_result 的 content 为占位符。
// 返回 (tool_use 扫描总数, 替换次数)。
func rewrite(req *messagesRequest, logger Logger) (toolUses, replaced int) {
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

	toolUses = len(uses)
	if toolUses == 0 {
		return toolUses, 0
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
		return toolUses, 0
	}

	// 第二遍：在 user 消息里找 tool_result，对 pruned 命中的 tool_use_id 做内容替换。
	replacement, err := json.Marshal(PlaceholderContent)
	if err != nil {
		// string marshal 不可能失败
		logger.Warnf("dcp: 占位符序列化失败: %v", err)
		return toolUses, 0
	}

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
			replaced++
		}
	}

	return toolUses, replaced
}
