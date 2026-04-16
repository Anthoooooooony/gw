package java

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

func TestMavenFilter_Match(t *testing.T) {
	f := &MavenFilter{}

	tests := []struct {
		cmd  string
		args []string
		want bool
	}{
		{"mvn", []string{"test"}, true},
		{"mvn", []string{"clean", "install"}, true},
		{"mvn", []string{"package", "-DskipTests"}, true},
		{"mvn", nil, true},
		{"gradle", []string{"build"}, false},
		{"java", []string{"-jar", "app.jar"}, false},
	}

	for _, tt := range tests {
		got := f.Match(tt.cmd, tt.args)
		if got != tt.want {
			t.Errorf("Match(%q, %v) = %v, want %v", tt.cmd, tt.args, got, tt.want)
		}
	}
}

func TestMavenFilter_ApplySuccess(t *testing.T) {
	f := &MavenFilter{}
	fixture := loadFixture(t, "mvn_test_success.txt")

	output := f.Apply(filter.FilterInput{
		Cmd:      "mvn",
		Args:     []string{"test"},
		Stdout:   fixture,
		ExitCode: 0,
	})

	// 应保留 BUILD SUCCESS
	if !strings.Contains(output.Content, "BUILD SUCCESS") {
		t.Error("应该保留 BUILD SUCCESS")
	}

	// 应保留测试计数
	if !strings.Contains(output.Content, "Tests run: 12") {
		t.Error("应该保留测试计数")
	}

	// 应保留 Total time
	if !strings.Contains(output.Content, "Total time") {
		t.Error("应该保留 Total time")
	}

	// 不应包含下载日志
	if strings.Contains(output.Content, "Downloading from") {
		t.Error("不应包含下载日志")
	}
	if strings.Contains(output.Content, "Downloaded from") {
		t.Error("不应包含下载完成日志")
	}

	// 不应包含插件执行行
	if strings.Contains(output.Content, "--- maven-") {
		t.Error("不应包含插件执行行")
	}

	// 压缩率应大于 70%
	ratio := 1.0 - float64(len(output.Content))/float64(len(fixture))
	if ratio < 0.70 {
		t.Errorf("压缩率 %.1f%% 低于 70%%", ratio*100)
	}
}

func TestMavenFilter_ApplyOnError(t *testing.T) {
	f := &MavenFilter{}
	fixture := loadFixture(t, "mvn_test_failure.txt")

	result := f.ApplyOnError(filter.FilterInput{
		Cmd:      "mvn",
		Args:     []string{"test"},
		Stdout:   fixture,
		ExitCode: 1,
	})

	if result == nil {
		t.Fatal("ApplyOnError 不应返回 nil")
	}

	content := result.Content

	// 应保留 BUILD FAILURE
	if !strings.Contains(content, "BUILD FAILURE") {
		t.Error("应该保留 BUILD FAILURE")
	}

	// 应保留失败测试名
	if !strings.Contains(content, "testAuthentication") {
		t.Error("应该保留失败测试名 testAuthentication")
	}
	if !strings.Contains(content, "testGetUserProfile") {
		t.Error("应该保留失败测试名 testGetUserProfile")
	}

	// 应保留断言详情
	if !strings.Contains(content, "401") {
		t.Error("应该保留断言详情(401)")
	}

	// 应保留测试摘要
	if !strings.Contains(content, "Tests run: 12") {
		t.Error("应该保留测试摘要")
	}

	// 不应包含下载日志
	if strings.Contains(content, "Downloading from") {
		t.Error("不应包含下载日志")
	}
	if strings.Contains(content, "Downloaded from") {
		t.Error("不应包含下载完成日志")
	}
}

func TestMavenFilter_RealProject_Failure(t *testing.T) {
	f := &MavenFilter{}
	fixture := loadFixture(t, "mvn_compile_real_failure.txt")

	result := f.ApplyOnError(filter.FilterInput{
		Cmd:      "mvn",
		Args:     []string{"compile"},
		Stdout:   fixture,
		ExitCode: 1,
	})

	if result == nil {
		t.Fatal("ApplyOnError 不应返回 nil")
	}

	content := result.Content
	fixtureLines := len(strings.Split(fixture, "\n"))
	contentLines := len(strings.Split(content, "\n"))
	compression := 1.0 - float64(contentLines)/float64(fixtureLines)

	t.Logf("原始行数: %d, 过滤后行数: %d, 压缩率: %.1f%%", fixtureLines, contentLines, compression*100)
	t.Logf("--- 过滤后内容 ---\n%s\n--- 结束 ---", content)

	// 应包含 BUILD FAILURE
	if !strings.Contains(content, "BUILD FAILURE") {
		t.Error("应该保留 BUILD FAILURE")
	}

	// 应包含 Total time
	if !strings.Contains(content, "Total time:") {
		t.Error("应该保留 Total time")
	}

	// 应包含 Reactor 条目（SUCCESS 或 FAILURE）
	if !strings.Contains(content, "SUCCESS") && !strings.Contains(content, "FAILURE") {
		t.Error("应该保留 Reactor Summary 条目")
	}

	// 应包含至少一个 Unresolved reference 错误
	if !strings.Contains(content, "Unresolved reference") {
		t.Error("应该保留 Unresolved reference 错误")
	}

	// 不应包含 LATEST or RELEASE 警告
	if strings.Contains(content, "LATEST or RELEASE") {
		t.Error("不应包含 LATEST or RELEASE 警告")
	}

	// 不应包含 Kotlin 编译器 WARNING（file:/// 开头的）
	if strings.Contains(content, "file:///") && strings.Contains(content, "WARNING") {
		t.Error("不应包含 Kotlin 编译器 WARNING")
	}

	// 不应包含帮助建议
	if strings.Contains(content, "To see the full stack trace") {
		t.Error("不应包含帮助建议")
	}
	if strings.Contains(content, "[Help 1]") {
		t.Error("不应包含 [Help 1]")
	}

	// 不应包含下载行
	if strings.Contains(content, "Downloading from") || strings.Contains(content, "Downloaded from") {
		t.Error("不应包含下载行")
	}

	// 压缩率应大于 90%
	if compression < 0.90 {
		t.Errorf("压缩率 %.1f%% 低于 90%%", compression*100)
	}
}

func TestMavenFilter_RealProject_Apply(t *testing.T) {
	f := &MavenFilter{}
	fixture := loadFixture(t, "mvn_compile_real_failure.txt")

	output := f.Apply(filter.FilterInput{
		Cmd:      "mvn",
		Args:     []string{"compile"},
		Stdout:   fixture,
		ExitCode: 0,
	})

	fixtureLines := len(strings.Split(fixture, "\n"))
	contentLines := len(strings.Split(output.Content, "\n"))
	compression := 1.0 - float64(contentLines)/float64(fixtureLines)

	t.Logf("原始行数: %d, 过滤后行数: %d, 压缩率: %.1f%%", fixtureLines, contentLines, compression*100)
	t.Logf("--- 过滤后内容（成功模式）---\n%s\n--- 结束 ---", output.Content)

	// 压缩率应大于 90%
	if compression < 0.90 {
		t.Errorf("压缩率 %.1f%% 低于 90%%", compression*100)
	}
}
