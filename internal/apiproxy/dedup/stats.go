package dedup

import "sync/atomic"

// Stats 汇总 dedup 在 Transformer 生命周期内的观测数据。
// 所有字段通过 atomic 操作读写，可在多请求并发下安全累加。
type Stats struct {
	// RequestsProcessed 所有进入 Transform 的请求数（含解析失败透传）。
	RequestsProcessed atomic.Int64
	// ToolUseScanned 扫到的 tool_use 块总数。
	ToolUseScanned atomic.Int64
	// ResultsReplaced 内容被替换为占位符的 tool_result 总数。
	ResultsReplaced atomic.Int64
	// BytesInput 所有请求的输入 body 字节总和（仅在解析成功时累加）。
	BytesInput atomic.Int64
	// BytesOutput 对应的输出 body 字节总和。
	BytesOutput atomic.Int64
}

// BytesSaved 返回输入减输出的字节数（节省量）。
// 理论上可能为负：当被替换的原 tool_result 内容比固定 PlaceholderContent 还短时，
// 单个 block 的输出长度反而增加。为避免向用户展示"节省 -N 字节"的困惑输出，
// 负值一律 clamp 到 0（表达"净未节省"）。
func (s *Stats) BytesSaved() int64 {
	v := s.BytesInput.Load() - s.BytesOutput.Load()
	if v < 0 {
		return 0
	}
	return v
}
