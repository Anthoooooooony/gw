package java

import (
	"strings"
	"testing"

	"github.com/gw-cli/gw/filter"
)

// TestMavenStreamFilter_Interface 验证 MavenFilter 实现了 StreamFilter 接口
func TestMavenStreamFilter_Interface(t *testing.T) {
	var _ filter.StreamFilter = (*MavenFilter)(nil)
}

// TestMavenStreamFilter_NoiseLinesDropped 验证噪音行全部被丢弃
func TestMavenStreamFilter_NoiseLinesDropped(t *testing.T) {
	f := &MavenFilter{}
	proc := f.NewStreamInstance()

	noiseLines := []string{
		"[INFO] Scanning for projects...",
		"[INFO] ------------------------------------------------------------------------",
		"[INFO] ",
		"[INFO] Finished at: 2024-01-01T00:00:00+08:00",
		"[INFO] Downloading from central: https://repo.maven.apache.org/maven2/org/foo/1.0/foo.jar",
		"[INFO] Downloaded from central: https://repo.maven.apache.org/maven2/org/foo/1.0/foo.jar",
		"[WARNING] 'dependencies.dependency.version' for org.example:foo is too old",
		"[WARNING] file:///path/to/Foo.kt: (10, 5): Parameter 'x' is never used",
		"[INFO] Compiling 42 source files to /target/classes",
		"[ERROR] To see the full stack trace of the errors, re-run Maven with the -e switch.",
		"",
	}

	for _, line := range noiseLines {
		action, _ := proc.ProcessLine(line)
		if action != filter.StreamDrop {
			t.Errorf("噪音行应该被丢弃: %q", line)
		}
	}
}

// TestMavenStreamFilter_KeyLinesEmitted 验证关键行被输出
func TestMavenStreamFilter_KeyLinesEmitted(t *testing.T) {
	f := &MavenFilter{}
	proc := f.NewStreamInstance()

	// 先推进状态到 Reactor
	setupLines := []string{
		"[INFO] Scanning for projects...",
		"[INFO] ------------------------------------------------------------------------",
		"[INFO] Building my-app 1.0",
		"[INFO] ------------------------------------------------------------------------",
		"[INFO] --- maven-compiler-plugin:3.8.1:compile (default-compile) @ my-app ---",
		"[INFO] Nothing to compile - all classes are up to date",
		"[INFO] Reactor Summary for my-parent 1.0:",
	}
	for _, line := range setupLines {
		proc.ProcessLine(line)
	}

	// Reactor 条目应输出
	action, output := proc.ProcessLine("[INFO] my-module1 ................................... SUCCESS [  1.234 s]")
	if action != filter.StreamEmit {
		t.Error("Reactor 条目应该被输出")
	}
	if !strings.Contains(output, "SUCCESS") {
		t.Error("输出应包含 SUCCESS")
	}

	// BUILD SUCCESS 应输出
	action, output = proc.ProcessLine("[INFO] BUILD SUCCESS")
	if action != filter.StreamEmit {
		t.Error("BUILD SUCCESS 应该被输出")
	}
	if !strings.Contains(output, "BUILD SUCCESS") {
		t.Error("输出应包含 BUILD SUCCESS")
	}

	// Total time 应输出
	action, output = proc.ProcessLine("[INFO] Total time:  5.678 s")
	if action != filter.StreamEmit {
		t.Error("Total time 应该被输出")
	}
	if !strings.Contains(output, "Total time") {
		t.Error("输出应包含 Total time")
	}
}

// TestMavenStreamFilter_ErrorsEmitted 验证错误行被输出且去重
func TestMavenStreamFilter_ErrorsEmitted(t *testing.T) {
	f := &MavenFilter{}
	proc := f.NewStreamInstance()

	// 推进状态到 ErrorReport
	setupLines := []string{
		"[INFO] Scanning for projects...",
		"[INFO] Building my-app 1.0",
		"[INFO] --- maven-compiler-plugin:3.8.1:compile (default-compile) @ my-app ---",
		"[INFO] BUILD FAILURE",
	}
	for _, line := range setupLines {
		proc.ProcessLine(line)
	}

	// 第一个错误应输出
	action1, out1 := proc.ProcessLine("[ERROR] /src/main/kotlin/BusinessLog.kt: (10, 5) Unresolved reference: BusinessLog")
	if action1 != filter.StreamEmit {
		t.Error("第一个错误应该被输出")
	}
	if !strings.Contains(out1, "Unresolved reference") {
		t.Error("输出应包含 Unresolved reference")
	}

	// 重复的相同 Unresolved reference 应被去重
	action2, _ := proc.ProcessLine("[ERROR] /src/main/kotlin/Other.kt: (20, 10) Unresolved reference: BusinessLog")
	if action2 != filter.StreamDrop {
		t.Error("重复的 Unresolved reference: BusinessLog 应该被去重丢弃")
	}

	// 不同的 Unresolved reference 应输出
	action3, _ := proc.ProcessLine("[ERROR] /src/main/kotlin/Other.kt: (30, 10) Unresolved reference: OtherClass")
	if action3 != filter.StreamEmit {
		t.Error("不同的 Unresolved reference 应该被输出")
	}
}

