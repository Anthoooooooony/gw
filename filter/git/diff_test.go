package git

import (
	"strings"
	"testing"

	"github.com/Anthoooooooony/gw/filter"
)

func TestDiffFilter_Match(t *testing.T) {
	f := &DiffFilter{}

	tests := []struct {
		cmd  string
		args []string
		want bool
	}{
		{"git", []string{"diff"}, true},
		{"git", []string{"diff", "HEAD~1"}, true},
		{"git", []string{"diff", "--stat"}, true},
		{"git", []string{"status"}, false},
		{"git", []string{"log"}, false},
		{"git", nil, false},
		{"diff", []string{"a", "b"}, false},
	}

	for _, tt := range tests {
		got := f.Match(tt.cmd, tt.args)
		if got != tt.want {
			t.Errorf("Match(%q, %v) = %v, want %v", tt.cmd, tt.args, got, tt.want)
		}
	}
}

func TestDiffFilter_Apply(t *testing.T) {
	f := &DiffFilter{}
	fixture := loadFixture(t, "git_diff_mixed.txt")

	out := f.Apply(filter.FilterInput{
		Cmd:    "git",
		Args:   []string{"diff"},
		Stdout: fixture,
	})

	// 关键行全在
	for _, want := range []string{
		"diff --git a/bar.py b/bar.py",
		"diff --git a/baz.md b/baz.md",
		"diff --git a/foo.go b/foo.go",
		"new file mode 100644",
		"--- a/foo.go",
		"+++ b/foo.go",
		"@@ -1,7 +1,7 @@",
		"@@ -9,6 +9,7 @@",
		"-x=1",
		"+x=10",
		"+CHANGED_EARLY",
		"-line4",
		"+INSERTED_MIDDLE",
	} {
		if !strings.Contains(out.Content, want) {
			t.Errorf("应保留关键行 %q\n实际:\n%s", want, out.Content)
		}
	}

	// context 行（以空格开头）应被丢弃
	if strings.Contains(out.Content, "\n y=2") || strings.Contains(out.Content, "\n line1") {
		t.Errorf("context 行应丢弃, got:\n%s", out.Content)
	}

	// 应产生压缩
	if len(out.Content) >= len(fixture) {
		t.Errorf("应当产生压缩: got %d >= fixture %d", len(out.Content), len(fixture))
	}
}

func TestDiffFilter_EmptyFallback(t *testing.T) {
	f := &DiffFilter{}
	// --stat 输出无 `diff --git` 或 `@@` 锚点
	stat := " foo.go | 2 +-\n 1 file changed, 1 insertion(+), 1 deletion(-)\n"
	out := f.Apply(filter.FilterInput{
		Cmd:    "git",
		Args:   []string{"diff", "--stat"},
		Stdout: stat,
	})
	if out.Content != stat {
		t.Errorf("锚点缺失应透传原文\ngot  %q\nwant %q", out.Content, stat)
	}
}

func TestDiffFilter_Empty(t *testing.T) {
	f := &DiffFilter{}
	out := f.Apply(filter.FilterInput{
		Cmd:    "git",
		Args:   []string{"diff"},
		Stdout: "",
	})
	if out.Content != "" {
		t.Errorf("空输入应透传空, got %q", out.Content)
	}
}

func TestDiffFilter_ApplyOnError(t *testing.T) {
	f := &DiffFilter{}
	if out := f.ApplyOnError(filter.FilterInput{ExitCode: 1}); out != nil {
		t.Errorf("ApplyOnError 应返回 nil 透传, got %+v", out)
	}
}
