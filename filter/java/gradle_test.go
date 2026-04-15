package java

import (
	"strings"
	"testing"

	"github.com/gw-cli/gw/filter"
)

func TestGradleFilter_Match(t *testing.T) {
	f := &GradleFilter{}

	tests := []struct {
		cmd  string
		args []string
		want bool
	}{
		{"gradle", []string{"build"}, true},
		{"gradlew", []string{"test"}, true},
		{"./gradlew", []string{"build"}, true},
		{"/home/user/project/gradlew", []string{"test"}, true},
		{"mvn", []string{"test"}, false},
		{"java", []string{"-jar", "app.jar"}, false},
	}

	for _, tt := range tests {
		got := f.Match(tt.cmd, tt.args)
		if got != tt.want {
			t.Errorf("Match(%q, %v) = %v, want %v", tt.cmd, tt.args, got, tt.want)
		}
	}
}

func TestGradleFilter_ApplySuccess(t *testing.T) {
	f := &GradleFilter{}
	fixture := loadFixture(t, "gradle_build_success.txt")

	output := f.Apply(filter.FilterInput{
		Cmd:      "gradle",
		Args:     []string{"build"},
		Stdout:   fixture,
		ExitCode: 0,
	})

	// 应保留 BUILD SUCCESSFUL
	if !strings.Contains(output.Content, "BUILD SUCCESSFUL") {
		t.Error("应该保留 BUILD SUCCESSFUL")
	}

	// 应保留测试结果
	if !strings.Contains(output.Content, "PASSED") {
		t.Error("应该保留测试通过结果")
	}

	// 不应包含 Task 进度行
	if strings.Contains(output.Content, "> Task :") {
		t.Error("不应包含 Task 进度行")
	}

	// 不应包含 Starting Daemon
	if strings.Contains(output.Content, "Starting a Gradle Daemon") {
		t.Error("不应包含 Starting Daemon 行")
	}
}

func TestGradleFilter_ApplyOnError(t *testing.T) {
	f := &GradleFilter{}
	fixture := loadFixture(t, "gradle_test_failure.txt")

	result := f.ApplyOnError(filter.FilterInput{
		Cmd:      "./gradlew",
		Args:     []string{"test"},
		Stdout:   fixture,
		ExitCode: 1,
	})

	if result == nil {
		t.Fatal("ApplyOnError 不应返回 nil")
	}

	content := result.Content

	// 应保留 BUILD FAILED
	if !strings.Contains(content, "BUILD FAILED") {
		t.Error("应该保留 BUILD FAILED")
	}

	// 应保留 FAILED 任务行
	if !strings.Contains(content, "FAILED") {
		t.Error("应该保留 FAILED 信息")
	}

	// 应保留测试失败详情
	if !strings.Contains(content, "401") {
		t.Error("应该保留断言详情(401)")
	}
	if !strings.Contains(content, "NullPointerException") {
		t.Error("应该保留异常信息")
	}

	// 不应包含 Try 建议
	if strings.Contains(content, "> Run with --stacktrace") {
		t.Error("不应包含 Try 建议行")
	}
	if strings.Contains(content, "> Run with --info") {
		t.Error("不应包含 Try 建议行")
	}

	// 不应包含报告链接
	if strings.Contains(content, "See the report at:") {
		t.Error("不应包含报告文件链接")
	}

	// 不应包含普通 Task 进度行
	if strings.Contains(content, "> Task :compileJava") {
		t.Error("不应包含普通 Task 进度行")
	}
}
