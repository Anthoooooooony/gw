package dedup

import (
	"encoding/json"
	"testing"
)

// TestSignature_DeterministicKeyOrder 验证 input 里的 key 顺序不同，签名相同。
func TestSignature_DeterministicKeyOrder(t *testing.T) {
	sigA := createSignature("Read", json.RawMessage(`{"b":1,"a":2}`))
	sigB := createSignature("Read", json.RawMessage(`{"a":2,"b":1}`))
	if sigA != sigB {
		t.Errorf("签名应一致:\n A = %s\n B = %s", sigA, sigB)
	}
}

// TestSignature_NullTopLevelStripped 验证顶层 null 字段被剥除。
func TestSignature_NullTopLevelStripped(t *testing.T) {
	sigA := createSignature("Read", json.RawMessage(`{"a":null,"b":1}`))
	sigB := createSignature("Read", json.RawMessage(`{"b":1}`))
	if sigA != sigB {
		t.Errorf("顶层 null 应被剥除:\n A = %s\n B = %s", sigA, sigB)
	}
}

// TestSignature_NestedObjectKeysSorted 验证嵌套 object 的 key 也会被排序。
func TestSignature_NestedObjectKeysSorted(t *testing.T) {
	sigA := createSignature("Read", json.RawMessage(`{"x":{"z":1,"y":2}}`))
	sigB := createSignature("Read", json.RawMessage(`{"x":{"y":2,"z":1}}`))
	if sigA != sigB {
		t.Errorf("嵌套 object 的 key 应排序后签名一致:\n A = %s\n B = %s", sigA, sigB)
	}
}

// TestSignature_ArrayOrderPreserved 验证 array 元素顺序影响签名（不排序 array）。
func TestSignature_ArrayOrderPreserved(t *testing.T) {
	sigA := createSignature("Bash", json.RawMessage(`{"cmd":["ls","-la"]}`))
	sigB := createSignature("Bash", json.RawMessage(`{"cmd":["-la","ls"]}`))
	if sigA == sigB {
		t.Errorf("array 元素顺序不同应产生不同签名:\n A = %s\n B = %s", sigA, sigB)
	}
}

// TestSignature_ObjectInsideArrayKeysSorted 验证 array 里的 object 也会递归排序 key。
func TestSignature_ObjectInsideArrayKeysSorted(t *testing.T) {
	sigA := createSignature("X", json.RawMessage(`{"list":[{"b":1,"a":2}]}`))
	sigB := createSignature("X", json.RawMessage(`{"list":[{"a":2,"b":1}]}`))
	if sigA != sigB {
		t.Errorf("array 里 object 的 key 应排序:\n A = %s\n B = %s", sigA, sigB)
	}
}

// TestSignature_DifferentParamsDifferentSig 基础区分度检查。
func TestSignature_DifferentParamsDifferentSig(t *testing.T) {
	sigA := createSignature("Read", json.RawMessage(`{"path":"/a"}`))
	sigB := createSignature("Read", json.RawMessage(`{"path":"/b"}`))
	if sigA == sigB {
		t.Errorf("不同参数签名应不同:\n A = %s\n B = %s", sigA, sigB)
	}
}

// TestSignature_EmptyInput 验证空 input 不会 panic，签名即工具名。
func TestSignature_EmptyInput(t *testing.T) {
	got := createSignature("Noop", nil)
	if got != "Noop" {
		t.Errorf("空 input 签名应为工具名, got %q", got)
	}
}

// TestSignature_MalformedInputFallback 验证无法解析的 input 降级为原样拼接，不 panic。
func TestSignature_MalformedInputFallback(t *testing.T) {
	got := createSignature("X", json.RawMessage(`{broken`))
	if got == "" {
		t.Error("降级签名不应为空")
	}
}
