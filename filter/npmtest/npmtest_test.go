package npmtest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Anthoooooooony/gw/filter"
)

func TestMatch(t *testing.T) {
	cases := []struct {
		cmd  string
		args []string
		want bool
	}{
		{"npm", []string{"test"}, true},
		{"npm", []string{"test", "--watch"}, true},
		{"yarn", []string{"test"}, true},
		{"pnpm", []string{"test"}, true},
		{"pnpm", []string{"t"}, true},
		{"npm", []string{"install"}, false},
		{"npm", []string{"run", "test"}, false},  // 自定义脚本不保证 runner 格式
		{"npm", []string{"run", "test:unit"}, false},
		{"pnpm", []string{}, false},
		{"node", []string{"test"}, false},
	}
	f := &Filter{}
	for _, c := range cases {
		if got := f.Match(c.cmd, c.args); got != c.want {
			t.Errorf("Match(%q, %v) = %v, want %v", c.cmd, c.args, got, c.want)
		}
	}
}

func TestApply_Success_AVA(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "toml", "testdata", "npm_test_success.txt"))
	if err != nil {
		t.Fatalf("读取 fixture 失败: %v", err)
	}
	out := (&Filter{}).Apply(filter.FilterInput{
		Cmd:    "npm",
		Args:   []string{"test"},
		Stdout: string(data),
	})
	if !strings.Contains(out.Content, "tests passed") {
		t.Fatalf("应保留 summary 行, got first 200B: %q", out.Content[:min(200, len(out.Content))])
	}
	// 前部 ✔ 进度行应丢弃
	if strings.Contains(out.Content, "✔ instance › the `level` option") {
		t.Error("AVA ✔ 进度行应被丢弃")
	}
	// coverage 表应保留
	if !strings.Contains(out.Content, "% Stmts") {
		t.Error("coverage 表应保留")
	}
}

func TestApply_NonAVA_FallbackTail(t *testing.T) {
	f := &Filter{}
	// 120 行以内时通用尾截断不触发，原文保留
	short := "> my-pkg@1.0.0 test\n> custom-test-runner\n\nall green\n"
	out := f.Apply(filter.FilterInput{Cmd: "npm", Args: []string{"test"}, Stdout: short})
	if out.Content != short {
		t.Errorf("短输出应原文透传, got %q", out.Content)
	}

	// 超过 120 行时截到末 120 行
	var longLines []string
	for i := 0; i < 250; i++ {
		longLines = append(longLines, "line")
	}
	long := strings.Join(longLines, "\n")
	out = f.Apply(filter.FilterInput{Cmd: "npm", Args: []string{"test"}, Stdout: long})
	got := strings.Count(out.Content, "\n") + 1
	if got != 120 {
		t.Errorf("超长非 AVA 应截到 120 行, got %d", got)
	}
}

func TestApplyOnError_AVA(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "toml", "testdata", "npm_test_failure.txt"))
	if err != nil {
		t.Fatalf("读取 fixture 失败: %v", err)
	}
	out := (&Filter{}).ApplyOnError(filter.FilterInput{
		Cmd:      "npm",
		Args:     []string{"test"},
		Stdout:   string(data),
		ExitCode: 1,
	})
	if out == nil {
		t.Fatal("AVA failure 应返回非 nil")
	}
	// 失败详情应保留
	if !strings.Contains(out.Content, "Difference:") {
		t.Error("应保留失败详情")
	}
	if !strings.Contains(out.Content, "1 test failed") {
		t.Error("应保留 failure summary")
	}
	// 前 50 行 ✔ 进度行应丢弃
	if strings.Contains(out.Content, "chalk › support multiple arguments") {
		t.Error("AVA ✔ 进度行应被丢弃")
	}
}

func TestApplyOnError_NonAVA_FallbackTail(t *testing.T) {
	out := (&Filter{}).ApplyOnError(filter.FilterInput{
		Cmd:      "npm",
		Args:     []string{"test"},
		Stdout:   "npm ERR! Test failed.  See above for more details.\n",
		ExitCode: 1,
	})
	// 非 AVA 也返回非 nil，走通用尾截断（短输出原样保留）
	if out == nil {
		t.Fatal("应返回非 nil，让 fallback tail 生效")
	}
	if !strings.Contains(out.Content, "npm ERR! Test failed") {
		t.Errorf("短输出应原文保留, got %q", out.Content)
	}
}
