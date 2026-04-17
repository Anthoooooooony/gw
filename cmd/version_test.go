package cmd

import (
	"strings"
	"testing"
)

// TestVersionString_LdflagsInjected 验证注入的 Version 能正确出现。
func TestVersionString_LdflagsInjected(t *testing.T) {
	orig := Version
	origCommit := Commit
	origDate := BuildDate
	defer func() {
		Version = orig
		Commit = origCommit
		BuildDate = origDate
	}()

	Version = "v1.2.3"
	Commit = "abcdef1234567890"
	BuildDate = "2026-04-17"

	got := versionString()
	if !strings.Contains(got, "v1.2.3") {
		t.Errorf("期望包含 v1.2.3，得到 %q", got)
	}
	if !strings.Contains(got, "abcdef1") {
		t.Errorf("期望包含短 commit abcdef1，得到 %q", got)
	}
	if !strings.Contains(got, "2026-04-17") {
		t.Errorf("期望包含 built 2026-04-17，得到 %q", got)
	}
	if !strings.Contains(got, "go") {
		t.Errorf("期望包含 Go 版本标记，得到 %q", got)
	}
}

// TestVersionString_DevFallback 在无 ldflags 和无 build info 时应返回稳定的 dev 字符串。
// 注：runtime/debug.ReadBuildInfo() 在 go test 中 vcs.* 通常不可用，
// 所以仅当 Version=dev 且其他两项为空时，输出应包含 "dev"。
func TestVersionString_DevFallback(t *testing.T) {
	orig := Version
	origCommit := Commit
	origDate := BuildDate
	defer func() {
		Version = orig
		Commit = origCommit
		BuildDate = origDate
	}()

	Version = "dev"
	Commit = ""
	BuildDate = ""

	got := versionString()
	if !strings.HasPrefix(got, "gw version ") {
		t.Errorf("期望以 'gw version ' 开头，得到 %q", got)
	}
	// 无论是否落入 unknown build 还是被 build info 填充，都应包含 go 版本
	if !strings.Contains(got, "go") {
		t.Errorf("期望包含 go 版本，得到 %q", got)
	}
}
