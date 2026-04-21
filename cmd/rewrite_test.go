package cmd

import (
	"encoding/json"
	"strings"
	"testing"
)

// decodeRewriteOut 把 rewrite 的 stdout 解析成 hookSpecificOutput 响应。
func decodeRewriteOut(t *testing.T, s string) hookSpecificOutput {
	t.Helper()
	var out hookOutput
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		t.Fatalf("无法解析 rewrite 输出 JSON: %v\n原始: %q", err, s)
	}
	return out.HookSpecificOutput
}

// 空 stdin → 静默放行（stdout 无输出、无错误）
func TestRunRewrite_EmptyStdin(t *testing.T) {
	var stdout strings.Builder
	if err := runRewriteWith(strings.NewReader(""), &stdout, testGwPath); err != nil {
		t.Fatalf("空 stdin 应静默返回，得到错误: %v", err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("空 stdin 不应输出: %q", stdout.String())
	}
}

// 非 Bash 工具 → 静默放行
func TestRunRewrite_NonBashTool(t *testing.T) {
	in := `{"tool_name":"Read","tool_input":{"path":"/etc/hosts"}}`
	var stdout strings.Builder
	if err := runRewriteWith(strings.NewReader(in), &stdout, testGwPath); err != nil {
		t.Fatal(err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("非 Bash 工具不应输出: %q", stdout.String())
	}
}

// 命令不在注册表内 → 静默放行
func TestRunRewrite_UnsupportedCommand(t *testing.T) {
	in := `{"tool_name":"Bash","tool_input":{"command":"cat /etc/passwd"}}`
	var stdout strings.Builder
	if err := runRewriteWith(strings.NewReader(in), &stdout, testGwPath); err != nil {
		t.Fatal(err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("不支持命令不应改写: %q", stdout.String())
	}
}

// mvn 命令 → 改写成 `<gw-abs> exec mvn test`，输出 hookSpecificOutput JSON
func TestRunRewrite_RewritesMvn(t *testing.T) {
	in := `{"tool_name":"Bash","tool_input":{"command":"mvn test","description":"run tests"}}`
	var stdout strings.Builder
	if err := runRewriteWith(strings.NewReader(in), &stdout, testGwPath); err != nil {
		t.Fatal(err)
	}
	got := decodeRewriteOut(t, stdout.String())
	if got.HookEventName != preToolUseEvent {
		t.Errorf("hookEventName 应为 %s, 得到 %s", preToolUseEvent, got.HookEventName)
	}
	if got.PermissionDecision != "allow" {
		t.Errorf("permissionDecision 应为 allow, 得到 %s", got.PermissionDecision)
	}
	cmd, _ := got.UpdatedInput["command"].(string)
	wantPrefix := "'" + testGwPath + "' exec mvn test"
	if cmd != wantPrefix {
		t.Errorf("改写后的 command 不对\n got: %q\nwant: %q", cmd, wantPrefix)
	}
	// 原 tool_input 其他字段必须透传
	if got.UpdatedInput["description"] != "run tests" {
		t.Errorf("description 字段丢失: %v", got.UpdatedInput["description"])
	}
}

// 链式命令（&&）部分段匹配 → 只改写匹配段
func TestRunRewrite_RewritesChainedCommand(t *testing.T) {
	in := `{"tool_name":"Bash","tool_input":{"command":"cd /tmp && mvn test"}}`
	var stdout strings.Builder
	if err := runRewriteWith(strings.NewReader(in), &stdout, testGwPath); err != nil {
		t.Fatal(err)
	}
	if stdout.Len() == 0 {
		t.Fatal("链式命令含 mvn 应被改写，但无输出")
	}
	got := decodeRewriteOut(t, stdout.String())
	cmd, _ := got.UpdatedInput["command"].(string)
	if !strings.Contains(cmd, "' exec mvn test") {
		t.Errorf("mvn 段未被改写: %q", cmd)
	}
	if !strings.HasPrefix(cmd, "cd /tmp") {
		t.Errorf("cd 段应保留: %q", cmd)
	}
}

// 损坏 JSON → 返回 error（调用方会 stderr 打 warn 并静默放行）
func TestRunRewrite_BrokenJSON(t *testing.T) {
	var stdout strings.Builder
	err := runRewriteWith(strings.NewReader("{not-json"), &stdout, testGwPath)
	if err == nil {
		t.Fatal("损坏 JSON 应返回错误")
	}
	if stdout.Len() != 0 {
		t.Fatalf("损坏 JSON 时不应输出: %q", stdout.String())
	}
}
