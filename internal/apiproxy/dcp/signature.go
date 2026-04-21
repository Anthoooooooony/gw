package dcp

import (
	"encoding/json"
	"sort"
)

// createSignature 生成 tool 调用的去重签名，格式为 "<name>::<json>"。
// 归一化规则与 DCP 对齐：
//   - 浅层剥除 input 顶层的 null 值
//   - 递归对 object 的 key 做字典序排序（array 元素顺序保持不变，
//     但 array 里的 object 也要递归排序 key）
//   - 序列化后字符串比较，不做 hash
//
// 参考：/tmp/dcp-real/lib/strategies/deduplication.ts:96-127
func createSignature(name string, input json.RawMessage) string {
	if len(input) == 0 {
		return name
	}
	var parsed any
	if err := json.Unmarshal(input, &parsed); err != nil {
		// input 无法解析：降级为直接用原始字节拼签名，避免中断 dedup 流程
		return name + "::" + string(input)
	}
	normalized := stripNullTopLevel(parsed)
	sorted := sortKeysDeep(normalized)
	out, err := json.Marshal(sorted)
	if err != nil {
		return name + "::" + string(input)
	}
	return name + "::" + string(out)
}

// stripNullTopLevel 仅对 map 的顶层剥除 nil value；array 和非 map 原样返回。
// DCP 是 shallow strip：嵌套的 null 保留。
func stripNullTopLevel(v any) any {
	m, ok := v.(map[string]any)
	if !ok {
		return v
	}
	out := make(map[string]any, len(m))
	for k, val := range m {
		if val == nil {
			continue
		}
		out[k] = val
	}
	return out
}

// sortKeysDeep 返回 key 递归排序后的副本：
//   - map 会按 key 字典序重建
//   - array 元素顺序保留，但每个元素若是 map 也会递归排序（返回新 slice 避免修改 input）
//
// json.Marshal 对 map[string]any 默认按 key 排序输出，所以重建 map 已足够。
func sortKeysDeep(v any) any {
	switch t := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		out := make(map[string]any, len(t))
		for _, k := range keys {
			out[k] = sortKeysDeep(t[k])
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, el := range t {
			out[i] = sortKeysDeep(el)
		}
		return out
	default:
		return v
	}
}
