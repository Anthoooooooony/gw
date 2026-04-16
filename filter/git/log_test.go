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

	// 应包含短哈希（7字符）
	if !strings.Contains(output.Content, "dcd2ec7") {
		t.Error("应该包含短哈希 dcd2ec7")
	}
	if !strings.Contains(output.Content, "cc98397") {
		t.Error("应该包含短哈希 cc98397")
	}
	if !strings.Contains(output.Content, "c1d1511") {
		t.Error("应该包含短哈希 c1d1511")
	}

	// 不应包含完整哈希
	if strings.Contains(output.Content, "dcd2ec767b338e397425187b545f50670990bfe3") {
		t.Error("不应该包含完整哈希")
	}

	// 应保留提交主题
	if !strings.Contains(output.Content, "feat: add git status filter") {
		t.Error("应该保留提交主题")
	}
	if !strings.Contains(output.Content, "refactor: extract filter interface") {
		t.Error("应该保留提交主题")
	}
	if !strings.Contains(output.Content, "init: scaffold gw CLI project") {
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
