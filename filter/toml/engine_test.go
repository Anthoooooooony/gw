package toml

import (
	"strings"
	"testing"

	"github.com/gw-cli/gw/filter"
)

func makeFilter(rules ...Rule) *TomlFilter {
	return &TomlFilter{Rules: rules}
}

func TestMatch(t *testing.T) {
	f := makeFilter(Rule{Match: "docker ps"})

	if !f.Match("docker", []string{"ps"}) {
		t.Error("应匹配 docker ps")
	}
	if !f.Match("docker", []string{"ps", "-a"}) {
		t.Error("应匹配 docker ps -a（前缀匹配）")
	}
	if f.Match("docker", []string{"build", "."}) {
		t.Error("不应匹配 docker build")
	}
}

func TestMaxLines(t *testing.T) {
	f := makeFilter(Rule{Match: "test", MaxLines: 3})
	input := filter.FilterInput{
		Cmd:    "test",
		Args:   []string{},
		Stdout: "line1\nline2\nline3\nline4\nline5",
	}
	out := f.Apply(input)
	lines := strings.Split(out.Content, "\n")
	if len(lines) != 3 {
		t.Errorf("期望 3 行，得到 %d 行", len(lines))
	}
	if lines[0] != "line1" || lines[2] != "line3" {
		t.Errorf("截断内容不正确: %v", lines)
	}
}

func TestTailLines(t *testing.T) {
	f := makeFilter(Rule{Match: "test", TailLines: 2})
	input := filter.FilterInput{
		Cmd:    "test",
		Args:   []string{},
		Stdout: "line1\nline2\nline3\nline4\nline5",
	}
	out := f.Apply(input)
	lines := strings.Split(out.Content, "\n")
	if len(lines) != 2 {
		t.Errorf("期望 2 行，得到 %d 行", len(lines))
	}
	if lines[0] != "line4" || lines[1] != "line5" {
		t.Errorf("tail 内容不正确: %v", lines)
	}
}

func TestStripLines(t *testing.T) {
	f := makeFilter(Rule{Match: "test", StripLines: []string{"^DEBUG"}})
	input := filter.FilterInput{
		Cmd:    "test",
		Args:   []string{},
		Stdout: "DEBUG: something\nINFO: hello\nDEBUG: another\nERROR: bad",
	}
	out := f.Apply(input)
	lines := strings.Split(out.Content, "\n")
	if len(lines) != 2 {
		t.Errorf("期望 2 行，得到 %d 行: %v", len(lines), lines)
	}
	for _, line := range lines {
		if strings.HasPrefix(line, "DEBUG") {
			t.Errorf("DEBUG 行未被移除: %s", line)
		}
	}
}

func TestKeepLines(t *testing.T) {
	f := makeFilter(Rule{Match: "test", KeepLines: []string{"ERROR"}})
	input := filter.FilterInput{
		Cmd:    "test",
		Args:   []string{},
		Stdout: "INFO: ok\nERROR: bad\nDEBUG: trace\nERROR: worse",
	}
	out := f.Apply(input)
	lines := strings.Split(out.Content, "\n")
	if len(lines) != 2 {
		t.Errorf("期望 2 行，得到 %d 行: %v", len(lines), lines)
	}
	for _, line := range lines {
		if !strings.Contains(line, "ERROR") {
			t.Errorf("非 ERROR 行未被过滤: %s", line)
		}
	}
}

func TestStripAnsi(t *testing.T) {
	f := makeFilter(Rule{Match: "test", StripAnsi: true})
	input := filter.FilterInput{
		Cmd:    "test",
		Args:   []string{},
		Stdout: "\x1b[31mred text\x1b[0m normal",
	}
	out := f.Apply(input)
	if strings.Contains(out.Content, "\x1b[") {
		t.Errorf("ANSI 转义码未被移除: %q", out.Content)
	}
	if out.Content != "red text normal" {
		t.Errorf("内容不正确: %q", out.Content)
	}
}

func TestOnEmpty(t *testing.T) {
	f := makeFilter(Rule{Match: "test", KeepLines: []string{"NOTFOUND"}, OnEmpty: "无匹配输出"})
	input := filter.FilterInput{
		Cmd:    "test",
		Args:   []string{},
		Stdout: "INFO: ok\nDEBUG: trace",
	}
	out := f.Apply(input)
	if out.Content != "无匹配输出" {
		t.Errorf("on_empty 未生效: %q", out.Content)
	}
}

func TestLongestMatch(t *testing.T) {
	f := makeFilter(
		Rule{Match: "docker", MaxLines: 100},
		Rule{Match: "docker ps", MaxLines: 3},
	)
	input := filter.FilterInput{
		Cmd:    "docker",
		Args:   []string{"ps", "-a"},
		Stdout: "line1\nline2\nline3\nline4\nline5",
	}
	out := f.Apply(input)
	lines := strings.Split(out.Content, "\n")
	if len(lines) != 3 {
		t.Errorf("应使用最长匹配规则(max_lines=3)，得到 %d 行", len(lines))
	}
}

func TestApplyOnError(t *testing.T) {
	f := makeFilter(Rule{Match: "test"})
	input := filter.FilterInput{
		Cmd:      "test",
		Args:     []string{},
		Stdout:   "output",
		ExitCode: 1,
	}
	if result := f.ApplyOnError(input); result != nil {
		t.Error("ApplyOnError 应返回 nil")
	}
}

func TestLoadBuiltinRules(t *testing.T) {
	f, err := LoadBuiltinRules()
	if err != nil {
		t.Fatalf("加载内置规则失败: %v", err)
	}
	if len(f.Rules) == 0 {
		t.Error("内置规则为空")
	}
	// 验证 docker ps 规则存在
	if !f.Match("docker", []string{"ps"}) {
		t.Error("内置规则应匹配 docker ps")
	}
	// 验证 kubectl get 规则存在
	if !f.Match("kubectl", []string{"get", "pods"}) {
		t.Error("内置规则应匹配 kubectl get pods")
	}
}
