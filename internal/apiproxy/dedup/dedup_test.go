package dedup

import (
	"bytes"
	"strings"
	"testing"
)

// quietLogger 测试里屏蔽 dedup 的 warn 输出。
type quietLogger struct{}

func (quietLogger) Infof(string, ...any) {}
func (quietLogger) Warnf(string, ...any) {}

// TestTransform_NoToolUse 无 tool_use 时 body 字节原样返回。
func TestTransform_NoToolUse(t *testing.T) {
	in := []byte(`{"model":"x","max_tokens":1,"messages":[{"role":"user","content":"hi"}]}`)
	out := NewTransformer(quietLogger{}).Transform(in)
	if !bytes.Equal(in, out) {
		t.Errorf("no tool_use 应透传:\n in  = %s\n out = %s", in, out)
	}
}

// TestTransform_SingleToolNoDedup 单次调用不触发 dedup。
func TestTransform_SingleToolNoDedup(t *testing.T) {
	in := []byte(`{"model":"x","max_tokens":1,"messages":[
		{"role":"assistant","content":[{"type":"tool_use","id":"tu_1","name":"Read","input":{"path":"/a"}}]},
		{"role":"user","content":[{"type":"tool_result","tool_use_id":"tu_1","content":"file content"}]}
	]}`)
	out := NewTransformer(quietLogger{}).Transform(in)
	if strings.Contains(string(out), PlaceholderContent) {
		t.Errorf("单次调用不应替换:\n out = %s", out)
	}
}

// TestTransform_DuplicateTwoOccurrences 同参数 + 同内容重复两次，
// 第一次 tool_result 被替换，第二次保留。
// 同参数但 content 不同的场景由 TestTransform_ContentChangedNoDedup 覆盖。
func TestTransform_DuplicateTwoOccurrences(t *testing.T) {
	in := []byte(`{"model":"x","max_tokens":1,"messages":[
		{"role":"assistant","content":[{"type":"tool_use","id":"tu_1","name":"Read","input":{"path":"/a"}}]},
		{"role":"user","content":[{"type":"tool_result","tool_use_id":"tu_1","content":"file content"}]},
		{"role":"assistant","content":[{"type":"tool_use","id":"tu_2","name":"Read","input":{"path":"/a"}}]},
		{"role":"user","content":[{"type":"tool_result","tool_use_id":"tu_2","content":"file content"}]}
	]}`)
	out := string(NewTransformer(quietLogger{}).Transform(in))
	if strings.Count(out, PlaceholderContent) != 1 {
		t.Errorf("占位符应出现恰好 1 次:\n %s", out)
	}
	if strings.Count(out, `"file content"`) != 1 {
		t.Errorf("原 content 最后一次应保留（恰出现 1 次）:\n %s", out)
	}
}

// TestTransform_ThreeOccurrences 同参数 + 同内容重复三次，前两个替换，最后一个保留。
func TestTransform_ThreeOccurrences(t *testing.T) {
	in := []byte(`{"model":"x","max_tokens":1,"messages":[
		{"role":"assistant","content":[{"type":"tool_use","id":"tu_1","name":"Read","input":{"path":"/a"}}]},
		{"role":"user","content":[{"type":"tool_result","tool_use_id":"tu_1","content":"payload"}]},
		{"role":"assistant","content":[{"type":"tool_use","id":"tu_2","name":"Read","input":{"path":"/a"}}]},
		{"role":"user","content":[{"type":"tool_result","tool_use_id":"tu_2","content":"payload"}]},
		{"role":"assistant","content":[{"type":"tool_use","id":"tu_3","name":"Read","input":{"path":"/a"}}]},
		{"role":"user","content":[{"type":"tool_result","tool_use_id":"tu_3","content":"payload"}]}
	]}`)
	out := string(NewTransformer(quietLogger{}).Transform(in))
	if n := strings.Count(out, PlaceholderContent); n != 2 {
		t.Errorf("占位符出现次数 = %d, want 2:\n %s", n, out)
	}
	if n := strings.Count(out, `"payload"`); n != 1 {
		t.Errorf("原 content 最后一次应保留（恰出现 1 次）, got %d:\n %s", n, out)
	}
}

// TestTransform_IsErrorSkipped is_error=true 的 tool_result 不参与 dedup（即使重复也不替换）。
func TestTransform_IsErrorSkipped(t *testing.T) {
	in := []byte(`{"model":"x","max_tokens":1,"messages":[
		{"role":"assistant","content":[{"type":"tool_use","id":"tu_1","name":"Bash","input":{"cmd":"x"}}]},
		{"role":"user","content":[{"type":"tool_result","tool_use_id":"tu_1","content":"err msg","is_error":true}]},
		{"role":"assistant","content":[{"type":"tool_use","id":"tu_2","name":"Bash","input":{"cmd":"x"}}]},
		{"role":"user","content":[{"type":"tool_result","tool_use_id":"tu_2","content":"fresh output"}]}
	]}`)
	out := string(NewTransformer(quietLogger{}).Transform(in))
	if strings.Contains(out, PlaceholderContent) {
		t.Errorf("is_error 的 tool_result 不应被替换:\n %s", out)
	}
	if !strings.Contains(out, "err msg") {
		t.Errorf("is_error 原内容应保留:\n %s", out)
	}
}

// TestTransform_DifferentParamsNoDedup 同 tool 不同参数不 dedup。
func TestTransform_DifferentParamsNoDedup(t *testing.T) {
	in := []byte(`{"model":"x","max_tokens":1,"messages":[
		{"role":"assistant","content":[{"type":"tool_use","id":"tu_1","name":"Read","input":{"path":"/a"}}]},
		{"role":"user","content":[{"type":"tool_result","tool_use_id":"tu_1","content":"A"}]},
		{"role":"assistant","content":[{"type":"tool_use","id":"tu_2","name":"Read","input":{"path":"/b"}}]},
		{"role":"user","content":[{"type":"tool_result","tool_use_id":"tu_2","content":"B"}]}
	]}`)
	out := string(NewTransformer(quietLogger{}).Transform(in))
	if strings.Contains(out, PlaceholderContent) {
		t.Errorf("不同参数不应 dedup:\n %s", out)
	}
}

// TestTransform_MultipleToolsIndependently 多个 tool 各自独立 dedup，互不影响。
// 同 tool 两次调用的 content 相同（否则按内容指纹不会 dedup）。
func TestTransform_MultipleToolsIndependently(t *testing.T) {
	in := []byte(`{"model":"x","max_tokens":1,"messages":[
		{"role":"assistant","content":[{"type":"tool_use","id":"r1","name":"Read","input":{"path":"/a"}}]},
		{"role":"user","content":[{"type":"tool_result","tool_use_id":"r1","content":"READ"}]},
		{"role":"assistant","content":[{"type":"tool_use","id":"b1","name":"Bash","input":{"cmd":"ls"}}]},
		{"role":"user","content":[{"type":"tool_result","tool_use_id":"b1","content":"BASH"}]},
		{"role":"assistant","content":[{"type":"tool_use","id":"r2","name":"Read","input":{"path":"/a"}}]},
		{"role":"user","content":[{"type":"tool_result","tool_use_id":"r2","content":"READ"}]},
		{"role":"assistant","content":[{"type":"tool_use","id":"b2","name":"Bash","input":{"cmd":"ls"}}]},
		{"role":"user","content":[{"type":"tool_result","tool_use_id":"b2","content":"BASH"}]}
	]}`)
	out := string(NewTransformer(quietLogger{}).Transform(in))
	// 每个 tool 的前一次替换 -> 占位符共出现 2 次
	if n := strings.Count(out, PlaceholderContent); n != 2 {
		t.Errorf("占位符应出现 2 次, got %d:\n %s", n, out)
	}
	// 每个 tool 的最后一次 content 各保留 1 次
	if n := strings.Count(out, `"READ"`); n != 1 {
		t.Errorf(`"READ" 应保留 1 次, got %d`, n)
	}
	if n := strings.Count(out, `"BASH"`); n != 1 {
		t.Errorf(`"BASH" 应保留 1 次, got %d`, n)
	}
}

// TestTransform_UnknownBlockPreserved 验证 thinking 等未识别的 block 类型字节级保留。
func TestTransform_UnknownBlockPreserved(t *testing.T) {
	// 用一个包含特殊字段（signature 模拟 thinking block 的不可修改字节）的 block。
	// 两次 tool_result 内容一致，才会触发替换（否则按内容指纹不 dedup）。
	in := []byte(`{"model":"x","max_tokens":1,"messages":[
		{"role":"assistant","content":[
			{"type":"thinking","thinking":"pondering","signature":"abc=="},
			{"type":"tool_use","id":"tu_1","name":"Read","input":{"path":"/a"}}
		]},
		{"role":"user","content":[{"type":"tool_result","tool_use_id":"tu_1","content":"same"}]},
		{"role":"assistant","content":[{"type":"tool_use","id":"tu_2","name":"Read","input":{"path":"/a"}}]},
		{"role":"user","content":[{"type":"tool_result","tool_use_id":"tu_2","content":"same"}]}
	]}`)
	out := string(NewTransformer(quietLogger{}).Transform(in))
	// thinking block 的 signature 字段必须保留
	if !strings.Contains(out, `"signature":"abc=="`) {
		t.Errorf("thinking signature 字段丢失:\n %s", out)
	}
}

// TestTransform_MalformedJSONPassthrough 解析失败降级为原样透传。
func TestTransform_MalformedJSONPassthrough(t *testing.T) {
	in := []byte(`{not json`)
	out := NewTransformer(quietLogger{}).Transform(in)
	if !bytes.Equal(in, out) {
		t.Errorf("无法解析时应原样透传:\n in  = %s\n out = %s", in, out)
	}
}

// TestTransform_StringContentMessagePreserved 验证 string 简写 content 的消息参与流程不出错。
// 两次 tool_result 内容一致才会触发替换（否则按内容指纹不 dedup）。
func TestTransform_StringContentMessagePreserved(t *testing.T) {
	in := []byte(`{"model":"x","max_tokens":1,"messages":[
		{"role":"user","content":"hi"},
		{"role":"assistant","content":[{"type":"tool_use","id":"tu_1","name":"Read","input":{"path":"/a"}}]},
		{"role":"user","content":[{"type":"tool_result","tool_use_id":"tu_1","content":"payload"}]},
		{"role":"assistant","content":[{"type":"tool_use","id":"tu_2","name":"Read","input":{"path":"/a"}}]},
		{"role":"user","content":[{"type":"tool_result","tool_use_id":"tu_2","content":"payload"}]}
	]}`)
	out := string(NewTransformer(quietLogger{}).Transform(in))
	if n := strings.Count(out, PlaceholderContent); n != 1 {
		t.Errorf("占位符应出现 1 次, got %d:\n %s", n, out)
	}
	if n := strings.Count(out, `"payload"`); n != 1 {
		t.Errorf(`"payload" 最后一次应保留（恰出现 1 次）, got %d:\n %s`, n, out)
	}
}

// TestTransform_NoDedupReturnsOriginalBytes 无 dedup 命中时应返回原始 body 字节。
// 这是 Transform 的 fast-path：避免无意义的 re-marshal 造成 upstream 收到的 body 与
// 原始字节不同，间接影响 Anthropic 的 prompt cache 命中。
func TestTransform_NoDedupReturnsOriginalBytes(t *testing.T) {
	in := []byte(`{"model":"x","max_tokens":1,"messages":[{"role":"user","content":"hello"}]}`)
	out := NewTransformer(quietLogger{}).Transform(in)
	if !bytes.Equal(in, out) {
		t.Errorf("无命中应原字节返回:\n in  = %s\n out = %s", in, out)
	}
}

// TestTransform_NoHTMLEscapeInflation 验证命中 dedup 后，输出 body 不会被 Go
// 默认 json.Marshal 的 HTML escape 污染（< > & 保持原样，不变成 < 等）。
// 该行为保证：
//  1. request body 只减不增（除必要的占位符替换外），小替换场景 BytesSaved 不再被吞；
//  2. 未被 dedup 的字段（system prompt、tool schema、其他 message）字节级原样转发，
//     不破坏 Anthropic prompt cache。
func TestTransform_NoHTMLEscapeInflation(t *testing.T) {
	// 1) system / tools / messages 都含 < > &；
	// 2) 两次 tool_use 同参数 + 同内容触发一次替换（按内容指纹 dedup）；
	// 3) 被替换的 tool_result 原文足够长，保证 Transform 后字节严格 < 原 body。
	longOrig := `<pre>` + strings.Repeat("x", 200) + `</pre>`
	in := []byte(`{"system":"<policy>use tools & reply promptly</policy>","model":"x","max_tokens":1,"messages":[
		{"role":"assistant","content":[{"type":"tool_use","id":"tu_1","name":"Read","input":{"path":"/a"}}]},
		{"role":"user","content":[{"type":"tool_result","tool_use_id":"tu_1","content":"` + longOrig + `"}]},
		{"role":"assistant","content":[{"type":"tool_use","id":"tu_2","name":"Read","input":{"path":"/a"}}]},
		{"role":"user","content":[{"type":"tool_result","tool_use_id":"tu_2","content":"` + longOrig + `"}]}
	]}`)
	out := NewTransformer(quietLogger{}).Transform(in)

	// 任何一种 HTML escape 形式出现都视为回归
	for _, bad := range []string{"\\u003c", "\\u003e", "\\u0026"} {
		if bytes.Contains(out, []byte(bad)) {
			t.Errorf("输出不应包含 HTMLEscape %q:\n%s", bad, out)
		}
	}
	// system 字段应字节级保留（含 < > &）
	if !bytes.Contains(out, []byte(`"<policy>use tools & reply promptly</policy>"`)) {
		t.Errorf("system 原文被篡改:\n%s", out)
	}
	// 第二次（保留的）tool_result 的 longOrig（含 <pre> </pre>）应保留原字符
	if !bytes.Contains(out, []byte(`<pre>`)) {
		t.Errorf("未 dedup 的 tool_result 原文被篡改:\n%s", out)
	}
	// 命中 dedup 且原文 > 占位符时，输出字节必然小于输入
	if len(out) >= len(in) {
		t.Errorf("dedup 应净减字节: in=%d out=%d", len(in), len(out))
	}
}

// TestTransform_ContentChangedNoDedup 同 tool + 同 input 但两次 tool_result 内容不同（文件被改、
// Bash 输出随时间变化、WebFetch 拉到不同页面等），不能 dedup ——
// 否则会 silent 丢掉前一次的信息，让模型看不到"变化"。
func TestTransform_ContentChangedNoDedup(t *testing.T) {
	in := []byte(`{"model":"x","max_tokens":1,"messages":[
		{"role":"assistant","content":[{"type":"tool_use","id":"tu_1","name":"Read","input":{"path":"/a"}}]},
		{"role":"user","content":[{"type":"tool_result","tool_use_id":"tu_1","content":"OLD state of file"}]},
		{"role":"assistant","content":[{"type":"tool_use","id":"tu_2","name":"Read","input":{"path":"/a"}}]},
		{"role":"user","content":[{"type":"tool_result","tool_use_id":"tu_2","content":"NEW state of file"}]}
	]}`)
	out := string(NewTransformer(quietLogger{}).Transform(in))
	if strings.Contains(out, PlaceholderContent) {
		t.Errorf("内容不同不应 dedup:\n %s", out)
	}
	if !strings.Contains(out, "OLD state of file") {
		t.Errorf("OLD 内容应保留:\n %s", out)
	}
	if !strings.Contains(out, "NEW state of file") {
		t.Errorf("NEW 内容应保留:\n %s", out)
	}
}

// TestTransform_MixedContentPartialDedup 同 tool + 同 input 的三次调用：
// 第 1/3 次 content 相同、第 2 次不同。第 1 次应被替换（和 3 一组），第 2 次保留。
// 证明按内容指纹分组的正确性 —— 不受中间一次内容变化的干扰。
func TestTransform_MixedContentPartialDedup(t *testing.T) {
	in := []byte(`{"model":"x","max_tokens":1,"messages":[
		{"role":"assistant","content":[{"type":"tool_use","id":"tu_1","name":"Read","input":{"path":"/a"}}]},
		{"role":"user","content":[{"type":"tool_result","tool_use_id":"tu_1","content":"SAME_AB"}]},
		{"role":"assistant","content":[{"type":"tool_use","id":"tu_2","name":"Read","input":{"path":"/a"}}]},
		{"role":"user","content":[{"type":"tool_result","tool_use_id":"tu_2","content":"MIDDLE_DIFFERENT"}]},
		{"role":"assistant","content":[{"type":"tool_use","id":"tu_3","name":"Read","input":{"path":"/a"}}]},
		{"role":"user","content":[{"type":"tool_result","tool_use_id":"tu_3","content":"SAME_AB"}]}
	]}`)
	out := string(NewTransformer(quietLogger{}).Transform(in))
	if n := strings.Count(out, PlaceholderContent); n != 1 {
		t.Errorf("占位符应出现 1 次（仅第 1 次被 3 替代）, got %d:\n %s", n, out)
	}
	if !strings.Contains(out, "MIDDLE_DIFFERENT") {
		t.Errorf("中间一次内容不同，应保留:\n %s", out)
	}
	if n := strings.Count(out, `"SAME_AB"`); n != 1 {
		t.Errorf(`SAME_AB 应保留 1 次（最后一次）, got %d`, n)
	}
}
