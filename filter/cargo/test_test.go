package cargo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Anthoooooooony/gw/filter"
)

func TestMatch(t *testing.T) {
	cases := []struct {
		name string
		cmd  string
		args []string
		want bool
	}{
		{"bare cargo test", "cargo", []string{"test"}, true},
		{"cargo test with flags", "cargo", []string{"test", "--release"}, true},
		{"cargo test with filter", "cargo", []string{"test", "my_module::test1"}, true},
		{"cargo build 不是 test", "cargo", []string{"build"}, false},
		{"cargo nextest run 不匹配", "cargo", []string{"nextest", "run"}, false},
		{"cargo test-util 不匹配", "cargo", []string{"test-util"}, false},
		{"空 args", "cargo", []string{}, false},
		{"非 cargo", "rustc", []string{"test"}, false},
	}
	f := &TestFilter{}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := f.Match(c.cmd, c.args)
			if got != c.want {
				t.Errorf("Match(%q, %v) = %v, want %v", c.cmd, c.args, got, c.want)
			}
		})
	}
}

// 成功场景 fixture（复用 filter/toml/testdata）只保留 `test result: ok.` 行
func TestApply_Success(t *testing.T) {
	fixture := filepath.Join("..", "toml", "testdata", "cargo_test_success.txt")
	data, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatalf("读取 fixture 失败: %v", err)
	}
	f := &TestFilter{}
	out := f.Apply(filter.FilterInput{
		Cmd:    "cargo",
		Args:   []string{"test"},
		Stdout: string(data),
	})
	if !strings.HasPrefix(out.Content, "test result: ok.") {
		t.Fatalf("Apply 应保留 summary 行, got first 80B: %q", out.Content[:min(80, len(out.Content))])
	}
	if strings.Contains(out.Content, "Compiling") {
		t.Error("summary 之外的编译进度行应被丢弃")
	}
	if strings.Contains(out.Content, "running 26 tests") {
		t.Error("逐条 test 进度行应被丢弃")
	}
}

// 找不到 ok summary → 返回原文
func TestApply_NoAnchor_Fallback(t *testing.T) {
	f := &TestFilter{}
	arbitrary := "some arbitrary output\nno cargo summary here\n"
	out := f.Apply(filter.FilterInput{Cmd: "cargo", Args: []string{"test"}, Stdout: arbitrary})
	if out.Content != arbitrary {
		t.Errorf("无 summary 时应原文透传, got %q", out.Content)
	}
}

// 失败场景：保留 failures: 到末尾
func TestApplyOnError_WithFailures(t *testing.T) {
	fixture := filepath.Join("..", "toml", "testdata", "cargo_test_failure.txt")
	data, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatalf("读取 fixture 失败: %v", err)
	}
	f := &TestFilter{}
	out := f.ApplyOnError(filter.FilterInput{
		Cmd:      "cargo",
		Args:     []string{"test"},
		Stdout:   string(data),
		ExitCode: 101,
	})
	if out == nil {
		t.Fatal("ApplyOnError 应返回非 nil")
	}
	if !strings.HasPrefix(out.Content, "failures:") {
		t.Fatalf("应从 failures: 行起, got first 80B: %q", out.Content[:min(80, len(out.Content))])
	}
	if !strings.Contains(out.Content, "test result: FAILED.") {
		t.Error("应保留失败 summary")
	}
	if !strings.Contains(out.Content, "find_cap_ref3") {
		t.Error("应保留具体失败用例的 panic 细节")
	}
	// 必须丢弃前面的编译进度和逐条 ok 行
	if strings.Contains(out.Content, "Compiling grep-matcher") {
		t.Error("failures: 之前的编译行不应保留")
	}
	if strings.Contains(out.Content, "find_cap_ref11 ... ok") {
		t.Error("逐条 ok 行应被丢弃")
	}
}

// 缺 FAILED summary（如 --no-run 等不执行测试的失败）→ nil
func TestApplyOnError_NoFailedSummary_Nil(t *testing.T) {
	f := &TestFilter{}
	// 有 failures: 锚点但没有 "test result: FAILED." 行
	out := f.ApplyOnError(filter.FilterInput{
		Cmd:      "cargo",
		Args:     []string{"test"},
		Stdout:   "failures:\n    some_test\n\n(no result line)\n",
		ExitCode: 1,
	})
	if out != nil {
		t.Errorf("缺 FAILED summary 时应返回 nil, got %+v", out)
	}
}

// 缺 failures: 锚点（只失败摘要）→ nil
func TestApplyOnError_NoFailuresBlock_Nil(t *testing.T) {
	f := &TestFilter{}
	out := f.ApplyOnError(filter.FilterInput{
		Cmd:      "cargo",
		Args:     []string{"test"},
		Stdout:   "test result: FAILED. 0 passed; 1 failed; 0 ignored\n",
		ExitCode: 1,
	})
	if out != nil {
		t.Errorf("缺 failures: 锚点时应返回 nil, got %+v", out)
	}
}
