package fs

import (
	"os"
	"strings"
	"testing"

	"github.com/Anthoooooooony/gw/filter"
)

// loadFixture 读取 testdata 目录下的测试数据文件
func loadFixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("无法加载测试数据 %s: %v", name, err)
	}
	return string(data)
}

// --- LsFilter ---

func TestLsFilter_Match(t *testing.T) {
	f := &LsFilter{}
	tests := []struct {
		cmd  string
		args []string
		want bool
	}{
		{"ls", nil, true},
		{"ls", []string{"-la"}, true},
		{"ls", []string{"-a", "/tmp"}, true},
		{"dir", nil, false},
		{"eza", nil, false}, // 替代工具不接管，输出结构不同
	}
	for _, tt := range tests {
		if got := f.Match(tt.cmd, tt.args); got != tt.want {
			t.Errorf("Match(%q, %v) = %v, want %v", tt.cmd, tt.args, got, tt.want)
		}
	}
}

func TestLsFilter_Apply_StripsTotalAndDotEntries(t *testing.T) {
	f := &LsFilter{}
	fixture := loadFixture(t, "ls_la.txt")

	out := f.Apply(filter.FilterInput{
		Cmd:    "ls",
		Args:   []string{"-la"},
		Stdout: fixture,
	})

	// total 行应被丢弃
	if strings.Contains(out.Content, "total ") {
		t.Errorf("应丢弃 total 行, got:\n%s", out.Content)
	}
	// `.` 和 `..` 条目（带元信息）应被丢弃
	for _, line := range strings.Split(out.Content, "\n") {
		trimmed := strings.TrimRight(line, " \t")
		if trimmed == "." || trimmed == ".." ||
			strings.HasSuffix(trimmed, " .") || strings.HasSuffix(trimmed, " ..") {
			t.Errorf("应丢弃 . / .. 条目, got line: %q", line)
		}
	}
	// 真实文件应保留
	for _, want := range []string{"CLAUDE.md", "go.mod", "filter"} {
		if !strings.Contains(out.Content, want) {
			t.Errorf("应保留真实条目 %q", want)
		}
	}
}

func TestLsFilter_Apply_PlainLs(t *testing.T) {
	f := &LsFilter{}
	fixture := loadFixture(t, "ls_plain.txt")

	out := f.Apply(filter.FilterInput{
		Cmd:    "ls",
		Args:   nil,
		Stdout: fixture,
	})

	// 无 total、无 . / ..，透传原文
	if out.Content != fixture {
		t.Errorf("plain ls 应透传\ngot  %q\nwant %q", out.Content, fixture)
	}
}

func TestLsFilter_Apply_StripsDotEntriesPlainAll(t *testing.T) {
	f := &LsFilter{}
	// 模拟 `ls -a` 的纯名字格式（无元信息）
	input := ".\n..\n.editorconfig\nCLAUDE.md\n"
	out := f.Apply(filter.FilterInput{
		Cmd:    "ls",
		Args:   []string{"-a"},
		Stdout: input,
	})

	if strings.Contains(out.Content, "\n.\n") || strings.HasPrefix(out.Content, ".\n") {
		t.Errorf("应丢弃单独的 . 行, got:\n%s", out.Content)
	}
	if !strings.Contains(out.Content, ".editorconfig") {
		t.Errorf("应保留 .editorconfig（隐藏文件但不是 . / ..）, got:\n%s", out.Content)
	}
}

func TestLsFilter_ApplyOnError(t *testing.T) {
	f := &LsFilter{}
	if out := f.ApplyOnError(filter.FilterInput{ExitCode: 1}); out != nil {
		t.Errorf("ApplyOnError 应返回 nil, got %+v", out)
	}
}

// --- FindFilter ---

func TestFindFilter_Match(t *testing.T) {
	f := &FindFilter{}
	if !f.Match("find", []string{".", "-name", "*.go"}) {
		t.Error("应匹配 find")
	}
	if f.Match("fd", []string{"-name", "*.go"}) {
		t.Error("不应匹配 fd（语义不同）")
	}
	if f.Match("locate", []string{"foo"}) {
		t.Error("不应匹配 locate")
	}
}

func TestFindFilter_Apply_PassThrough(t *testing.T) {
	f := &FindFilter{}
	fixture := loadFixture(t, "find_go_files.txt")

	out := f.Apply(filter.FilterInput{
		Cmd:    "find",
		Args:   []string{"filter", "-name", "*.go"},
		Stdout: fixture,
	})
	// 短 fixture（<120 行）应基本透传
	if !strings.Contains(out.Content, "filter/git/ops.go") {
		t.Errorf("应保留典型 .go 条目, got:\n%s", out.Content)
	}
}

func TestFindFilter_Apply_StripsPermissionDenied(t *testing.T) {
	f := &FindFilter{}
	input := "./a.go\nfind: './private': Permission denied\n./b.go\nfind: /root: Permission denied\n./c.go\n"
	out := f.Apply(filter.FilterInput{
		Cmd:    "find",
		Args:   []string{".", "-name", "*.go"},
		Stdout: input,
	})
	if strings.Contains(out.Content, "Permission denied") {
		t.Errorf("应丢弃 Permission denied 噪声, got:\n%s", out.Content)
	}
	for _, want := range []string{"./a.go", "./b.go", "./c.go"} {
		if !strings.Contains(out.Content, want) {
			t.Errorf("应保留 %q", want)
		}
	}
}

func TestFindFilter_Apply_HeadTailTruncates(t *testing.T) {
	f := &FindFilter{}
	var lines []string
	for i := 0; i < 300; i++ {
		lines = append(lines, "./path/to/file")
	}
	input := strings.Join(lines, "\n")
	out := f.Apply(filter.FilterInput{
		Cmd:    "find",
		Args:   []string{".", "-type", "f"},
		Stdout: input,
	})
	if !strings.Contains(out.Content, "行省略") {
		t.Errorf("超过阈值应显示省略标记, got len=%d", len(out.Content))
	}
	got := strings.Count(out.Content, "\n")
	// head 60 + 省略 1 行 + tail 20 ≈ 80 行，远少于 300
	if got > 100 {
		t.Errorf("应压缩到 ~80 行, got %d", got)
	}
}

func TestFindFilter_ApplyOnError(t *testing.T) {
	f := &FindFilter{}
	if out := f.ApplyOnError(filter.FilterInput{ExitCode: 1}); out != nil {
		t.Errorf("ApplyOnError 应返回 nil, got %+v", out)
	}
}

// --- GrepFilter ---

func TestGrepFilter_Match(t *testing.T) {
	f := &GrepFilter{}
	for _, cmd := range []string{"grep", "egrep", "fgrep", "rg", "ripgrep"} {
		if !f.Match(cmd, []string{"pattern", "."}) {
			t.Errorf("应匹配 %q", cmd)
		}
	}
	for _, cmd := range []string{"ag", "ack", "git"} {
		if f.Match(cmd, []string{"pattern"}) {
			t.Errorf("不应匹配 %q", cmd)
		}
	}
}

func TestGrepFilter_Apply_PassThrough(t *testing.T) {
	f := &GrepFilter{}
	fixture := loadFixture(t, "grep_stripansi.txt")
	out := f.Apply(filter.FilterInput{
		Cmd:    "grep",
		Args:   []string{"-rn", "StripANSI"},
		Stdout: fixture,
	})
	if !strings.Contains(out.Content, "StripANSI") {
		t.Errorf("应保留匹配内容, got:\n%s", out.Content)
	}
}

func TestGrepFilter_Apply_TruncatesLongLines(t *testing.T) {
	f := &GrepFilter{}
	longLine := "file.js:42:" + strings.Repeat("x", 500)
	out := f.Apply(filter.FilterInput{
		Cmd:    "grep",
		Args:   []string{"xxx"},
		Stdout: longLine + "\n",
	})
	if !strings.Contains(out.Content, "[截断") {
		t.Errorf("应截断超长行, got:\n%s", out.Content)
	}
	if strings.Count(out.Content, "x") >= 500 {
		t.Error("应真正截断 x 数量")
	}
}

func TestGrepFilter_Apply_HeadTail(t *testing.T) {
	f := &GrepFilter{}
	var lines []string
	for i := 0; i < 300; i++ {
		lines = append(lines, "file.go:1:match")
	}
	out := f.Apply(filter.FilterInput{
		Cmd:    "grep",
		Args:   []string{"match"},
		Stdout: strings.Join(lines, "\n"),
	})
	if !strings.Contains(out.Content, "行省略") {
		t.Errorf("应压缩过长输出, got lines=%d", strings.Count(out.Content, "\n"))
	}
}

func TestGrepFilter_ApplyOnError(t *testing.T) {
	f := &GrepFilter{}
	// grep 没匹配 exit 1，透传（nil）
	if out := f.ApplyOnError(filter.FilterInput{ExitCode: 1}); out != nil {
		t.Errorf("ApplyOnError 应返回 nil, got %+v", out)
	}
}
