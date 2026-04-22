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
		{"npm", []string{"run", "test"}, false}, // 自定义脚本不保证 runner 格式
		{"npm", []string{"run", "test:unit"}, false},
		{"pnpm", []string{}, false},
		{"node", []string{"test"}, false},
		{"vitest", []string{"run"}, true},
		{"vitest", []string{}, true},
		{"vitest", []string{"--run"}, true},
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

func TestApply_Success_Vitest(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "toml", "testdata", "vitest_success.txt"))
	if err != nil {
		t.Fatalf("读取 fixture 失败: %v", err)
	}
	out := (&Filter{}).Apply(filter.FilterInput{
		Cmd:    "vitest",
		Args:   []string{"run"},
		Stdout: string(data),
	})
	if !strings.Contains(out.Content, "Test Files") || !strings.Contains(out.Content, "Tests") {
		t.Fatalf("应保留 vitest 汇总 2 行, got %q", out.Content)
	}
	// 开头的 "RUN  v2.x" 和 ✓ 文件行应被丢弃
	if strings.Contains(out.Content, "RUN  v") {
		t.Error("RUN 头行应被丢弃")
	}
	if strings.Contains(out.Content, " ✓ math.test.js") {
		t.Error("✓ 文件行应被丢弃")
	}
}

func TestApplyOnError_Vitest(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "toml", "testdata", "vitest_failure.txt"))
	if err != nil {
		t.Fatalf("读取 fixture 失败: %v", err)
	}
	out := (&Filter{}).ApplyOnError(filter.FilterInput{
		Cmd:      "vitest",
		Args:     []string{"run"},
		Stdout:   string(data),
		ExitCode: 1,
	})
	if out == nil {
		t.Fatal("vitest failure 应返回非 nil")
	}
	// gw 拼接 stdout + stderr，vitest 的 Test Files/Tests summary 在 stdout 里、
	// Failed Tests 详情分隔符在 stderr 里；切片必须包含两部分。
	if !strings.Contains(out.Content, "Failed Tests") {
		t.Error("应保留 Failed Tests 分隔符")
	}
	if !strings.Contains(out.Content, "AssertionError") {
		t.Error("应保留 AssertionError 详情")
	}
	if !strings.Contains(out.Content, "Test Files  1 failed") {
		t.Error("应保留 Test Files 汇总")
	}
	if !strings.Contains(out.Content, "Tests  2 failed | 4 passed") {
		t.Error("应保留 Tests 通过/失败计数")
	}
	// 切片应从 `❯ file (N | M failed)` 起，包含 × 进度快览
	if !strings.Contains(out.Content, "× math > multiplies broken") {
		t.Error("应保留失败测试的进度快览")
	}
	// RUN 头行和 npm 包装行应在切片之前，不保留
	if strings.Contains(out.Content, "RUN  v") {
		t.Error("RUN 头行应被丢弃")
	}
}

func TestApply_Success_Jest(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "toml", "testdata", "jest_success.txt"))
	if err != nil {
		t.Fatalf("读取 fixture 失败: %v", err)
	}
	out := (&Filter{}).Apply(filter.FilterInput{
		Cmd:    "npm",
		Args:   []string{"test"},
		Stdout: string(data),
	})
	if !strings.Contains(out.Content, "Test Suites:") || !strings.Contains(out.Content, "Tests:") {
		t.Fatalf("应保留 jest 汇总, got %q", out.Content)
	}
	// ✓ 进度行应丢弃
	if strings.Contains(out.Content, "✓ adds") {
		t.Error("✓ 进度行应被丢弃")
	}
	if strings.Contains(out.Content, "PASS ./math.test.js") {
		t.Error("文件结果行应在成功场景被丢弃（jest 成功只留 summary）")
	}
}

func TestApplyOnError_Jest(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "toml", "testdata", "jest_failure.txt"))
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
		t.Fatal("jest failure 应返回非 nil")
	}
	// 起点应为 FAIL 文件行
	if !strings.HasPrefix(out.Content, "FAIL ") {
		t.Errorf("应从 FAIL 文件行开始, got %q", out.Content[:min(80, len(out.Content))])
	}
	// 关键信息验证
	for _, want := range []string{
		"multiplies broken",                         // 失败用例名
		"● math › multiplies broken",                // 失败块 bullet
		"Expected: 7",                               // assertion detail
		"Received: 6",                               // assertion detail
		"Test Suites: 1 failed",                     // 汇总
		"Tests:",                                    // 通过/失败计数
	} {
		if !strings.Contains(out.Content, want) {
			t.Errorf("失败压缩应保留 %q", want)
		}
	}
	// banner `> jest` 和前置的 npm 包装应被丢弃
	if strings.Contains(out.Content, "> jest") {
		t.Error("npm banner 应被丢弃")
	}
}

func TestApply_Success_Mocha(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "toml", "testdata", "mocha_success.txt"))
	if err != nil {
		t.Fatalf("读取 fixture 失败: %v", err)
	}
	out := (&Filter{}).Apply(filter.FilterInput{
		Cmd:    "npm",
		Args:   []string{"test"},
		Stdout: string(data),
	})
	// 只保留 passing 汇总行
	if !strings.Contains(out.Content, "passing") {
		t.Fatalf("应保留 N passing 行, got %q", out.Content)
	}
	if strings.Contains(out.Content, "✔ adds") {
		t.Error("mocha 成功应只留 summary, ✔ 进度行要丢")
	}
}

func TestApplyOnError_Mocha(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "toml", "testdata", "mocha_failure.txt"))
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
		t.Fatal("mocha failure 应返回非 nil")
	}
	// 起点应为 "N failing" 行
	if !strings.HasPrefix(out.Content, "  2 failing") {
		t.Errorf("应从 N failing 行开始, got %q", out.Content[:min(80, len(out.Content))])
	}
	// 关键信息
	for _, want := range []string{
		"1) math",
		"multiplies broken",
		"AssertionError",
		"uppercase broken",
	} {
		if !strings.Contains(out.Content, want) {
			t.Errorf("失败压缩应保留 %q", want)
		}
	}
	// 进度树和 banner 应丢
	if strings.Contains(out.Content, "✔ adds") {
		t.Error("✔ 进度行应被丢弃")
	}
	if strings.Contains(out.Content, "> mocha") {
		t.Error("npm banner 应被丢弃")
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
