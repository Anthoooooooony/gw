package cmd

import (
	"bytes"
	"strings"
	"testing"

	tomlfilter "github.com/gw-cli/gw/filter/toml"
)

// TestCollectFilterRows_HasBuiltins 默认注册表应包含 Go 硬编码过滤器和内置 TOML 规则。
func TestCollectFilterRows_HasBuiltins(t *testing.T) {
	rows := collectFilterRows()
	if len(rows) == 0 {
		t.Fatal("未收集到任何过滤器行")
	}

	var hasGo, hasToml bool
	for _, r := range rows {
		if r.Type == "go" {
			hasGo = true
			if r.Source != tomlfilter.SourceBuiltin {
				t.Errorf("Go 过滤器 source 应为 builtin，实际 %s", r.Source)
			}
		}
		if r.Type == "toml" {
			hasToml = true
		}
	}
	if !hasGo {
		t.Error("预期至少一个 go 类型过滤器")
	}
	if !hasToml {
		t.Error("预期至少一个 toml 类型规则")
	}
}

// TestRenderFilters_TableFormat 渲染后的表格应包含表头和关键字段
func TestRenderFilters_TableFormat(t *testing.T) {
	rows := []filterRow{
		{Name: "git/status", Type: "go", Source: "builtin", Match: "git status"},
		{Name: "docker.ps", Type: "toml", Source: "user:///home/u/.config/gw/rules/docker-prod.toml", Match: "docker ps"},
	}
	var buf bytes.Buffer
	renderFilters(&buf, rows)
	out := buf.String()
	for _, want := range []string{
		"NAME", "TYPE", "SOURCE", "MATCH",
		"git/status", "docker.ps",
		"docker-prod.toml", "builtin",
		"toml", "go",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("表格输出缺少 %q\n实际:\n%s", want, out)
		}
	}
}

// TestGoFilterMatchHintFromName 名称到 MATCH 提示的映射
func TestGoFilterMatchHintFromName(t *testing.T) {
	cases := map[string]string{
		"git/status":     "git status",
		"java/maven":     "java maven",
		"java/gradle":    "java gradle",
		"java/springboot": "java springboot",
		"loneword":       "loneword",
	}
	for in, want := range cases {
		if got := goFilterMatchHintFromName(in); got != want {
			t.Errorf("输入 %q：期望 %q 得到 %q", in, want, got)
		}
	}
}

// TestRunFiltersList_OutputsTable 集成测试：运行 list 子命令
func TestRunFiltersList_OutputsTable(t *testing.T) {
	var buf bytes.Buffer
	cmd := filtersListCmd
	cmd.SetOut(&buf)
	cmd.Run(cmd, []string{})
	out := buf.String()
	if !strings.Contains(out, "NAME") || !strings.Contains(out, "SOURCE") {
		t.Errorf("filters list 输出缺少表头:\n%s", out)
	}
}
