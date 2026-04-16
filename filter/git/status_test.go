package git

import (
	"os"
	"strings"
	"testing"

	"github.com/gw-cli/gw/filter"
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

func TestStatusFilter_Match(t *testing.T) {
	f := &StatusFilter{}

	tests := []struct {
		cmd  string
		args []string
		want bool
	}{
		{"git", []string{"status"}, true},
		{"git", []string{"status", "--short"}, true},
		{"git", []string{"log"}, false},
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

func TestStatusFilter_Clean(t *testing.T) {
	f := &StatusFilter{}
	fixture := loadFixture(t, "git_status_clean.txt")

	output := f.Apply(filter.FilterInput{
		Cmd:    "git",
		Args:   []string{"status"},
		Stdout: fixture,
	})

	if !strings.Contains(output.Content, "nothing to commit") {
		t.Error("clean status 应该保留 'nothing to commit'")
	}
}

func TestStatusFilter_Dirty(t *testing.T) {
	f := &StatusFilter{}
	fixture := loadFixture(t, "git_status_dirty.txt")

	output := f.Apply(filter.FilterInput{
		Cmd:    "git",
		Args:   []string{"status"},
		Stdout: fixture,
	})

	// 应保留分支名
	if !strings.Contains(output.Content, "feature/add-filters") {
		t.Error("应该保留分支名")
	}

	// 应保留文件名
	for _, file := range []string{"filter/git/status.go", "filter/registry.go", "cmd/exec.go", "filter/git/log.go"} {
		if !strings.Contains(output.Content, file) {
			t.Errorf("应该保留文件名 %s", file)
		}
	}

	// 应去除教学提示
	if strings.Contains(output.Content, "(use \"git") {
		t.Error("应该去除教学提示行")
	}

	// 过滤后应该比原始输出短
	if len(output.Content) >= len(fixture) {
		t.Errorf("过滤后输出(%d)应该比原始输出(%d)短", len(output.Content), len(fixture))
	}
}

func TestStatusFilter_ApplyOnError(t *testing.T) {
	f := &StatusFilter{}
	result := f.ApplyOnError(filter.FilterInput{})
	if result != nil {
		t.Error("ApplyOnError 应该返回 nil")
	}
}
