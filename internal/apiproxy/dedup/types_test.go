package dedup

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestRequest_UnknownTopLevelFieldPreserved 验证未来字段（proxy 不认识的 top-level key）
// 通过 extra 保留并完整回写。
func TestRequest_UnknownTopLevelFieldPreserved(t *testing.T) {
	in := `{"model":"claude-sonnet-4","max_tokens":1024,"messages":[],"future_field":{"a":1,"b":[2,3]}}`
	var req messagesRequest
	if err := json.Unmarshal([]byte(in), &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	out, err := json.Marshal(&req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// 检查 future_field 字节原样保留
	if !strings.Contains(string(out), `"future_field":{"a":1,"b":[2,3]}`) {
		t.Errorf("未知顶层字段丢失或被修改: %s", out)
	}
}

// TestMessage_StringContentNormalized 验证 string 简写的 content 被包成 [text block]。
func TestMessage_StringContentNormalized(t *testing.T) {
	in := `{"role":"user","content":"hello"}`
	var m message
	if err := json.Unmarshal([]byte(in), &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(m.Content) != 1 {
		t.Fatalf("string content 应被包成单 block, got len=%d", len(m.Content))
	}
	var bt blockType
	if err := json.Unmarshal(m.Content[0], &bt); err != nil {
		t.Fatalf("peek type: %v", err)
	}
	if bt.Type != "text" {
		t.Errorf("string 简写应包成 text block, got type=%q", bt.Type)
	}
}

// TestMessage_UnmutatedRoundtripByteExact 验证未被改动的 message 回写字节完全等同原始输入。
// 这是保证 dedup 命中为零时 Transform 返回原 body 语义等价的关键。
func TestMessage_UnmutatedRoundtripByteExact(t *testing.T) {
	in := `{"role":"assistant","content":[{"type":"text","text":"hi","unknown":"x"}]}`
	var m message
	if err := json.Unmarshal([]byte(in), &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	out, err := json.Marshal(&m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(out) != in {
		t.Errorf("未 mutated 的 message 应字节原样:\n in  = %s\n out = %s", in, out)
	}
}

// TestToolResultBlock_ExtraPreserved 验证 cache_control 等 extra 字段被保留。
func TestToolResultBlock_ExtraPreserved(t *testing.T) {
	in := `{"type":"tool_result","tool_use_id":"tu_1","content":"x","cache_control":{"type":"ephemeral"}}`
	var b toolResultBlock
	if err := json.Unmarshal([]byte(in), &b); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	out, err := json.Marshal(&b)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(out), `"cache_control":{"type":"ephemeral"}`) {
		t.Errorf("cache_control 丢失: %s", out)
	}
}

// TestToolResultBlock_IsErrorPreserved 验证 is_error=true 被回写。
func TestToolResultBlock_IsErrorPreserved(t *testing.T) {
	in := `{"type":"tool_result","tool_use_id":"tu_1","content":"oops","is_error":true}`
	var b toolResultBlock
	if err := json.Unmarshal([]byte(in), &b); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	out, err := json.Marshal(&b)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(out), `"is_error":true`) {
		t.Errorf("is_error 丢失: %s", out)
	}
}

// TestToolResultBlock_IsErrorFalsePreserved 验证显式 is_error:false 也被保留（*bool 区分缺省）。
func TestToolResultBlock_IsErrorFalsePreserved(t *testing.T) {
	in := `{"type":"tool_result","tool_use_id":"tu_1","content":"ok","is_error":false}`
	var b toolResultBlock
	if err := json.Unmarshal([]byte(in), &b); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	out, err := json.Marshal(&b)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(out), `"is_error":false`) {
		t.Errorf("显式 is_error:false 丢失: %s", out)
	}
}

// TestToolResultBlock_IsErrorAbsentStaysAbsent 未设置的 is_error 不应出现在输出里。
func TestToolResultBlock_IsErrorAbsentStaysAbsent(t *testing.T) {
	in := `{"type":"tool_result","tool_use_id":"tu_1","content":"ok"}`
	var b toolResultBlock
	if err := json.Unmarshal([]byte(in), &b); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	out, err := json.Marshal(&b)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(out), `is_error`) {
		t.Errorf("未设置 is_error 不应出现: %s", out)
	}
}

// TestMessage_MutatedPreservesExtra 验证 mutated 消息的消息级 extra（如 cache_control）不丢。
func TestMessage_MutatedPreservesExtra(t *testing.T) {
	in := `{"role":"user","content":[{"type":"tool_result","tool_use_id":"tu_1","content":"x"}],"cache_control":{"type":"ephemeral"}}`
	var m message
	if err := json.Unmarshal([]byte(in), &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	m.mutated = true // 模拟 dedup 触发
	out, err := json.Marshal(&m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(out), `"cache_control":{"type":"ephemeral"}`) {
		t.Errorf("消息级 cache_control 丢失: %s", out)
	}
}
