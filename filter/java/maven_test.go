package java

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
		{"mvn", []string{"spring-boot:run"}, false},          // 长驻进程
		{"mvn", []string{"clean", "spring-boot:run"}, false}, // 长驻进程（混合参数）
		{"mvn", []string{"jetty:run"}, false},                // 长驻进程
		{"mvn", []string{"quarkus:dev"}, false},              // 长驻进程
		{"mvn", []string{"exec:java"}, false},                // 长驻进程
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
	if !strings.Contains(output.Content, "Tests run:") {
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

	// 应保留失败测试名（petclinic PostgresIntegrationTests）
	if !strings.Contains(content, "findAll") {
		t.Error("应该保留失败测试名 findAll")
	}
	if !strings.Contains(content, "ownerDetails") {
		t.Error("应该保留失败测试名 ownerDetails")
	}

	// 应保留异常关键字（ApplicationContext 加载失败 = 典型 Spring 测试失败）
	if !strings.Contains(content, "IllegalState") {
		t.Error("应该保留异常类型 IllegalState")
	}

	// 应保留测试摘要
	if !strings.Contains(content, "Tests run:") {
		t.Error("应该保留测试摘要 Tests run")
	}

	// 不应包含下载日志
	if strings.Contains(content, "Downloading from") {
		t.Error("不应包含下载日志")
	}
	if strings.Contains(content, "Downloaded from") {
		t.Error("不应包含下载完成日志")
	}
}

// TestMavenFilter_ParallelBuild_Fallback 并行 Maven 输出交错多模块行，
// 单线程状态机无法正确分类；必须 fallback 原文，不能输出错误的残缺内容。
func TestMavenFilter_ParallelBuild_Fallback(t *testing.T) {
	// 模拟 `mvn -T 2 test` 典型头部：有 MultiThreadedBuilder 标识行，
	// 接着是交错的模块行。
	parallel := `[INFO] Scanning for projects...
[INFO] ------------------------------------------------------------------------
[INFO] Reactor Build Order:
[INFO]
[INFO] module-a                                                           [jar]
[INFO] module-b                                                           [jar]
[INFO]
[INFO] Using the MultiThreadedBuilder implementation with a thread count of 2
[INFO] ----------------< com.example:module-a >----------------
[INFO] ----------------< com.example:module-b >----------------
[INFO] Building module-a 1.0                                              [1/2]
[INFO] Building module-b 1.0                                              [2/2]
[INFO] BUILD SUCCESS
`
	f := &MavenFilter{}
	out := f.Apply(filter.FilterInput{
		Cmd:    "mvn",
		Args:   []string{"-T", "2", "test"},
		Stdout: parallel,
	})
	if out.Content != parallel {
		t.Fatalf("并行构建应透传原文, 实际压缩了: got %d bytes, want %d bytes",
			len(out.Content), len(parallel))
	}

	// ApplyOnError 也应透传
	errResult := f.ApplyOnError(filter.FilterInput{
		Cmd:      "mvn",
		Args:     []string{"-T", "2", "test"},
		Stdout:   parallel,
		ExitCode: 1,
	})
	if errResult == nil || errResult.Content != parallel {
		t.Fatal("并行构建 ApplyOnError 应透传原文")
	}
}

// TestMavenStream_ParallelBuild_Fallback 流式 stream 处理器同样需要并行检测。
func TestMavenStream_ParallelBuild_Fallback(t *testing.T) {
	f := &MavenFilter{}
	p := f.NewStreamInstance()

	// 检测前的行照常被 state machine 处理
	_, _ = p.ProcessLine("[INFO] Scanning for projects...")

	// 检测到 MultiThreadedBuilder，此行本身 Emit 原文
	act, out := p.ProcessLine("[INFO] Using the MultiThreadedBuilder implementation with a thread count of 4")
	if act != filter.StreamEmit {
		t.Errorf("MultiThreadedBuilder 行应 Emit, got %v", act)
	}
	if !strings.Contains(out, "MultiThreadedBuilder") {
		t.Error("MultiThreadedBuilder 行原文应被保留")
	}

	// 之后的行全部原文透传
	noisy := "[INFO] --- resources:3.3.1:resources (default-resources) @ module-a ---"
	act, out = p.ProcessLine(noisy)
	if act != filter.StreamEmit {
		t.Errorf("并行模式后续 Mojo 行应 Emit, got %v", act)
	}
	if out != noisy {
		t.Errorf("并行模式应 Emit 原文, got %q want %q", out, noisy)
	}
}
