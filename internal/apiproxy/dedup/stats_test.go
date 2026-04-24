package dedup

import (
	"strings"
	"sync"
	"testing"
)

// TestStats_NoOpRequestStillCounts 即使没有 tool_use，RequestsProcessed 也应 +1。
func TestStats_NoOpRequestStillCounts(t *testing.T) {
	tr := NewTransformer(quietLogger{})
	tr.Transform([]byte(`{"model":"x","max_tokens":1,"messages":[{"role":"user","content":"hi"}]}`))
	s := tr.Stats()
	if got := s.RequestsProcessed.Load(); got != 1 {
		t.Errorf("RequestsProcessed = %d, want 1", got)
	}
	if got := s.ResultsReplaced.Load(); got != 0 {
		t.Errorf("ResultsReplaced = %d, want 0", got)
	}
	if got := s.BytesSaved(); got != 0 {
		t.Errorf("BytesSaved = %d, want 0", got)
	}
}

// TestStats_DedupCountsReplacements 验证 dedup 命中时各计数正确。
func TestStats_DedupCountsReplacements(t *testing.T) {
	in := []byte(`{"model":"x","max_tokens":1,"messages":[
		{"role":"assistant","content":[{"type":"tool_use","id":"tu_1","name":"Read","input":{"path":"/a"}}]},
		{"role":"user","content":[{"type":"tool_result","tool_use_id":"tu_1","content":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}]},
		{"role":"assistant","content":[{"type":"tool_use","id":"tu_2","name":"Read","input":{"path":"/a"}}]},
		{"role":"user","content":[{"type":"tool_result","tool_use_id":"tu_2","content":"second"}]}
	]}`)
	tr := NewTransformer(quietLogger{})
	out := tr.Transform(in)
	s := tr.Stats()

	if got := s.RequestsProcessed.Load(); got != 1 {
		t.Errorf("RequestsProcessed = %d, want 1", got)
	}
	if got := s.ToolUseScanned.Load(); got != 2 {
		t.Errorf("ToolUseScanned = %d, want 2", got)
	}
	if got := s.ResultsReplaced.Load(); got != 1 {
		t.Errorf("ResultsReplaced = %d, want 1", got)
	}
	// 输入大、输出小 → 节省为正
	if s.BytesSaved() <= 0 {
		t.Errorf("BytesSaved = %d, want > 0", s.BytesSaved())
	}
	if len(out) >= len(in) {
		t.Errorf("输出应比输入短: in=%d, out=%d", len(in), len(out))
	}
}

// TestStats_ConcurrentSafe 并发 Transform 下计数器总和正确，无竞态。
// race detector 会在有竞态时 fail。
func TestStats_ConcurrentSafe(t *testing.T) {
	tr := NewTransformer(quietLogger{})
	body := []byte(`{"model":"x","max_tokens":1,"messages":[{"role":"user","content":"hi"}]}`)

	const workers = 8
	const perWorker = 50
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < perWorker; j++ {
				tr.Transform(body)
			}
		}()
	}
	wg.Wait()

	want := int64(workers * perWorker)
	if got := tr.Stats().RequestsProcessed.Load(); got != want {
		t.Errorf("RequestsProcessed = %d, want %d", got, want)
	}
}

// TestStats_BytesSavedClampsNegative 验证 BytesSaved 对负差值 clamp 到 0。
// 直接操作 atomic 模拟 BytesOutput > BytesInput 的病态累加场景。
func TestStats_BytesSavedClampsNegative(t *testing.T) {
	s := &Stats{}
	s.BytesInput.Add(100)
	s.BytesOutput.Add(200)
	if got := s.BytesSaved(); got != 0 {
		t.Errorf("BytesSaved() = %d, want 0 (clamp)", got)
	}
}

// TestStats_BytesSavedAccumulates 多次 Transform 的 BytesSaved 应累加。
func TestStats_BytesSavedAccumulates(t *testing.T) {
	in := []byte(`{"model":"x","max_tokens":1,"messages":[
		{"role":"assistant","content":[{"type":"tool_use","id":"tu_1","name":"R","input":{"p":"/a"}}]},
		{"role":"user","content":[{"type":"tool_result","tool_use_id":"tu_1","content":"` + strings.Repeat("x", 500) + `"}]},
		{"role":"assistant","content":[{"type":"tool_use","id":"tu_2","name":"R","input":{"p":"/a"}}]},
		{"role":"user","content":[{"type":"tool_result","tool_use_id":"tu_2","content":"last"}]}
	]}`)
	tr := NewTransformer(quietLogger{})
	tr.Transform(in)
	first := tr.Stats().BytesSaved()
	tr.Transform(in)
	second := tr.Stats().BytesSaved()
	if second <= first {
		t.Errorf("BytesSaved 未累加: first=%d second=%d", first, second)
	}
}
