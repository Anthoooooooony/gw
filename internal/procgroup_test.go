package internal

import (
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestProcGroupSupportsGraceful 验证不同平台常量正确：
//   - unix 平台：支持 SIGTERM 宽限期
//   - 非 unix 平台（如 Windows）：仅支持直接 Kill，不区分 SIGTERM/SIGKILL
func TestProcGroupSupportsGraceful(t *testing.T) {
	switch runtime.GOOS {
	case "windows", "plan9":
		if procGroupSupportsGraceful {
			t.Errorf("%s 不应支持 SIGTERM 宽限期", runtime.GOOS)
		}
	default:
		// linux / darwin / *bsd / solaris 等 unix 家族
		if !procGroupSupportsGraceful {
			t.Errorf("%s 应支持 SIGTERM 宽限期", runtime.GOOS)
		}
	}
}

// TestFormatTimeoutMessage_PlatformAware 验证降级平台不输出误导性的
// "SIGTERM[, SIGKILL]"，改用 "(killed)" 反映一次性 Kill 的真实语义。
func TestFormatTimeoutMessage_PlatformAware(t *testing.T) {
	dur := 300 * time.Millisecond
	msg := formatTimeoutMessage(dur, true)
	if !strings.Contains(msg, "gw: command timed out after") {
		t.Errorf("消息应包含标准前缀，得到 %q", msg)
	}

	if procGroupSupportsGraceful {
		// unix：保留两阶段语义
		if !strings.Contains(msg, "SIGTERM") {
			t.Errorf("unix 平台消息应含 SIGTERM，得到 %q", msg)
		}
	} else {
		// 非 unix：不应含 SIGTERM/SIGKILL 字样
		if strings.Contains(msg, "SIGTERM") || strings.Contains(msg, "SIGKILL") {
			t.Errorf("非 unix 平台消息不应含 SIGTERM/SIGKILL，得到 %q", msg)
		}
		if !strings.Contains(msg, "killed") {
			t.Errorf("非 unix 平台消息应含 killed，得到 %q", msg)
		}
	}
}
