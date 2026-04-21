package git

import (
	"strings"
	"testing"

	"github.com/gw-cli/gw/filter"
)

func TestLogFilter_Match(t *testing.T) {
	f := &LogFilter{}

	tests := []struct {
		cmd  string
		args []string
		want bool
	}{
		{"git", []string{"log"}, true},
		{"git", []string{"log", "--oneline"}, true},
		{"git", []string{"status"}, false},
		{"ls", []string{"-la"}, false},
		{"git", []string{}, false},
	}

	for _, tt := range tests {
		got := f.Match(tt.cmd, tt.args)
		if got != tt.want {
			t.Errorf("Match(%q, %v) = %v, want %v", tt.cmd, tt.args, got, tt.want)
		}
	}
}

func TestLogFilter_Apply(t *testing.T) {
	f := &LogFilter{}
	fixture := loadFixture(t, "git_log_default.txt")

	output := f.Apply(filter.FilterInput{
		Cmd:    "git",
		Args:   []string{"log"},
		Stdout: fixture,
	})

	// 应包含短哈希（7 字符，fixture 取自 gw 仓库真实 log）
	if !strings.Contains(output.Content, "9273922") {
		t.Error("应该包含短哈希 9273922")
	}
	if !strings.Contains(output.Content, "990556c") {
		t.Error("应该包含短哈希 990556c")
	}
	if !strings.Contains(output.Content, "24c338b") {
		t.Error("应该包含短哈希 24c338b")
	}

	// 不应包含完整哈希
	if strings.Contains(output.Content, "9273922ef6b548b8015b64d6407def209587d2f2") {
		t.Error("不应该包含完整哈希")
	}

	// 应保留提交主题
	if !strings.Contains(output.Content, "test: 场景化压缩率回归") {
		t.Error("应该保留提交主题")
	}
	if !strings.Contains(output.Content, "fix(bump): 支持 BREAKING CHANGE footer") {
		t.Error("应该保留提交主题")
	}
	if !strings.Contains(output.Content, "docs: 加 PR 模板") {
		t.Error("应该保留提交主题")
	}

	// 应去除 trailer
	if strings.Contains(output.Content, "Signed-off-by") {
		t.Error("应该去除 Signed-off-by trailer")
	}
	if strings.Contains(output.Content, "Co-authored-by") {
		t.Error("应该去除 Co-authored-by trailer")
	}

	// 压缩率应该超过 40%
	compressionRatio := 1.0 - float64(len(output.Content))/float64(len(fixture))
	if compressionRatio <= 0.4 {
		t.Errorf("压缩率 %.1f%% 应该超过 40%%", compressionRatio*100)
	}
}

func TestLogFilter_ApplyOnError(t *testing.T) {
	f := &LogFilter{}
	result := f.ApplyOnError(filter.FilterInput{})
	if result != nil {
		t.Error("ApplyOnError 应该返回 nil")
	}
}
