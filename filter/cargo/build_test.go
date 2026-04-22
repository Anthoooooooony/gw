package cargo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Anthoooooooony/gw/filter"
)

func TestBuildMatch(t *testing.T) {
	cases := []struct {
		cmd  string
		args []string
		want bool
	}{
		{"cargo", []string{"build"}, true},
		{"cargo", []string{"build", "--release"}, true},
		{"cargo", []string{"check"}, true},
		{"cargo", []string{"clippy", "--all-targets"}, true},
		{"cargo", []string{"test"}, false},
		{"cargo", []string{"run"}, false},
		{"cargo", []string{"nextest", "build"}, false},
		{"cargo", []string{}, false},
		{"rustc", []string{"build"}, false},
	}
	f := &BuildFilter{}
	for _, c := range cases {
		if got := f.Match(c.cmd, c.args); got != c.want {
			t.Errorf("Match(%q, %v) = %v, want %v", c.cmd, c.args, got, c.want)
		}
	}
}

func TestBuildSubname(t *testing.T) {
	f := &BuildFilter{}
	if got := f.Subname("cargo", []string{"build"}); got != "build" {
		t.Errorf("want build, got %q", got)
	}
	if got := f.Subname("cargo", []string{"clippy"}); got != "clippy" {
		t.Errorf("want clippy, got %q", got)
	}
	if got := f.Subname("cargo", []string{}); got != "" {
		t.Errorf("want empty, got %q", got)
	}
}

func TestBuildApply_Success(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "toml", "testdata", "cargo_build_success.txt"))
	if err != nil {
		t.Fatalf("读取 fixture 失败: %v", err)
	}
	out := (&BuildFilter{}).Apply(filter.FilterInput{
		Cmd:    "cargo",
		Args:   []string{"build"},
		Stdout: string(data),
	})
	if !strings.Contains(out.Content, "Finished") || !strings.Contains(out.Content, "target(s) in") {
		t.Fatalf("应保留 Finished 行, got %q", out.Content)
	}
	if strings.Contains(out.Content, "Compiling memchr") {
		t.Error("Compiling 进度行应被丢弃")
	}
	if strings.Contains(out.Content, "Downloaded") {
		t.Error("Downloaded 进度行应被丢弃")
	}
}

func TestBuildApply_NoAnchor_Fallback(t *testing.T) {
	out := (&BuildFilter{}).Apply(filter.FilterInput{
		Cmd:    "cargo",
		Args:   []string{"build"},
		Stdout: "some unrelated output\n",
	})
	if out.Content != "some unrelated output\n" {
		t.Errorf("无 Finished 行应原文透传, got %q", out.Content)
	}
}

func TestBuildApplyOnError_CouldNotCompile(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "toml", "testdata", "cargo_build_failure.txt"))
	if err != nil {
		t.Fatalf("读取 fixture 失败: %v", err)
	}
	out := (&BuildFilter{}).ApplyOnError(filter.FilterInput{
		Cmd:      "cargo",
		Args:     []string{"build"},
		Stdout:   string(data),
		ExitCode: 101,
	})
	if out == nil {
		t.Fatal("应返回非 nil")
	}
	if !strings.HasPrefix(out.Content, "error:") {
		t.Fatalf("应从首个 error 行开始, got %q", out.Content[:min(80, len(out.Content))])
	}
	if !strings.Contains(out.Content, "could not compile") {
		t.Error("应保留总结行")
	}
	if strings.Contains(out.Content, "Compiling grep-matcher") {
		t.Error("error 之前的 Compiling 行应被丢弃")
	}
}

func TestBuildApplyOnError_NoCouldNotCompile_Nil(t *testing.T) {
	out := (&BuildFilter{}).ApplyOnError(filter.FilterInput{
		Cmd:      "cargo",
		Args:     []string{"build"},
		Stdout:   "error: unexpected thing\n(no summary line)\n",
		ExitCode: 1,
	})
	if out != nil {
		t.Errorf("缺 could not compile 总结行应返回 nil, got %+v", out)
	}
}

func TestBuildApplyOnError_OnlySummary_Nil(t *testing.T) {
	out := (&BuildFilter{}).ApplyOnError(filter.FilterInput{
		Cmd:      "cargo",
		Args:     []string{"build"},
		Stdout:   "error: could not compile `x` (lib) due to 1 previous error\n",
		ExitCode: 1,
	})
	// 这里只有 could-not-compile 行本身；first-error 会命中同一行，合法 → 应返回非 nil
	if out == nil {
		t.Error("总结行本身既是 first error 又是 summary, 应可单独保留")
	}
}
