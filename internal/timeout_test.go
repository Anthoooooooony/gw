package internal

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"
)

// TestResolveTimeout_Default 默认值为 10 分钟
func TestResolveTimeout_Default(t *testing.T) {
	os.Unsetenv("GW_CMD_TIMEOUT")
	dur, enabled := resolveTimeoutWithStderr(os.Stderr)
	if !enabled {
		t.Fatal("默认应启用超时")
	}
	if dur != 10*time.Minute {
		t.Errorf("期望默认 10m，得到 %v", dur)
	}
}

// TestResolveTimeout_Custom 自定义有效值
func TestResolveTimeout_Custom(t *testing.T) {
	os.Setenv("GW_CMD_TIMEOUT", "30s")
	defer os.Unsetenv("GW_CMD_TIMEOUT")
	dur, enabled := resolveTimeoutWithStderr(os.Stderr)
	if !enabled {
		t.Fatal("30s 应启用超时")
	}
	if dur != 30*time.Second {
		t.Errorf("期望 30s，得到 %v", dur)
	}
}

// TestResolveTimeout_Zero 值为 0 禁用超时
func TestResolveTimeout_Zero(t *testing.T) {
	os.Setenv("GW_CMD_TIMEOUT", "0")
	defer os.Unsetenv("GW_CMD_TIMEOUT")
	_, enabled := resolveTimeoutWithStderr(os.Stderr)
	if enabled {
		t.Error("0 应禁用超时")
	}
}

// TestResolveTimeout_Off 值为 off 禁用超时
func TestResolveTimeout_Off(t *testing.T) {
	os.Setenv("GW_CMD_TIMEOUT", "off")
	defer os.Unsetenv("GW_CMD_TIMEOUT")
	_, enabled := resolveTimeoutWithStderr(os.Stderr)
	if enabled {
		t.Error("off 应禁用超时")
	}
}

// TestResolveTimeout_InvalidFallback 无效值 warning + fallback 到默认
func TestResolveTimeout_InvalidFallback(t *testing.T) {
	os.Setenv("GW_CMD_TIMEOUT", "not-a-duration")
	defer os.Unsetenv("GW_CMD_TIMEOUT")
	var warnBuf bytes.Buffer
	dur, enabled := resolveTimeoutWithStderr(&warnBuf)
	if !enabled {
		t.Fatal("无效值应 fallback 到默认（启用）")
	}
	if dur != 10*time.Minute {
		t.Errorf("期望 fallback 10m，得到 %v", dur)
	}
	if !strings.Contains(warnBuf.String(), "GW_CMD_TIMEOUT") {
		t.Errorf("期望 warning 提到 GW_CMD_TIMEOUT，得到 %q", warnBuf.String())
	}
}
