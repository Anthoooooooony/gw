package internal

import (
	"context"
	"os/exec"
	"testing"
	"time"
)

// TestStartTimeoutKiller_NoTimeout ctx 未设置截止时间时不触发 kill
func TestStartTimeoutKiller_NoTimeout(t *testing.T) {
	cmd := exec.Command("sh", "-c", "echo quick")
	setProcessGroup(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatalf("启动失败: %v", err)
	}

	ctx := context.Background() // 无截止时间
	stop, sigkillFired, timedOut := startTimeoutKiller(ctx, cmd, timeoutKillGrace)
	_ = cmd.Wait()
	stop()

	if timedOut.Load() {
		t.Error("无超时场景不应标记为 timedOut")
	}
	if sigkillFired.Load() {
		t.Error("无超时场景不应发 SIGKILL")
	}
}

// TestStartTimeoutKiller_PreTimeoutExit 进程在超时前自然退出，killer 不动作
func TestStartTimeoutKiller_PreTimeoutExit(t *testing.T) {
	cmd := exec.Command("sh", "-c", "exit 0")
	setProcessGroup(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatalf("启动失败: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stop, sigkillFired, timedOut := startTimeoutKiller(ctx, cmd, timeoutKillGrace)
	if err := cmd.Wait(); err != nil {
		t.Fatalf("wait 失败: %v", err)
	}
	stop()

	if timedOut.Load() {
		t.Error("pre-timeout 退出不应标记 timedOut")
	}
	if sigkillFired.Load() {
		t.Error("pre-timeout 退出不应发 SIGKILL")
	}
}

// TestStartTimeoutKiller_SigtermWorks 截止时间到且进程可被 SIGTERM 杀掉
// （只打 SIGTERM，不需要 SIGKILL）
func TestStartTimeoutKiller_SigtermWorks(t *testing.T) {
	if !procGroupSupportsGraceful {
		t.Skip("平台不支持 SIGTERM 宽限期")
	}
	cmd := exec.Command("sh", "-c", "sleep 10")
	setProcessGroup(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatalf("启动失败: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	start := time.Now()
	stop, sigkillFired, timedOut := startTimeoutKiller(ctx, cmd, 3*time.Second)
	_ = cmd.Wait()
	stop()
	elapsed := time.Since(start)

	if !timedOut.Load() {
		t.Error("应标记 timedOut")
	}
	// 普通 sleep 会响应 SIGTERM，不需进入宽限期
	if sigkillFired.Load() {
		t.Errorf("SIGTERM 可杀的进程不应触发 SIGKILL（elapsed=%v）", elapsed)
	}
	if elapsed > 2*time.Second {
		t.Errorf("SIGTERM 终止耗时过长: %v", elapsed)
	}
}

// TestStartTimeoutKiller_SigkillAfterGrace SIGTERM 被 trap，宽限期后 SIGKILL 生效
func TestStartTimeoutKiller_SigkillAfterGrace(t *testing.T) {
	if !procGroupSupportsGraceful {
		t.Skip("平台不支持 SIGTERM 宽限期")
	}
	cmd := exec.Command("bash", "-c", `trap "" TERM; sleep 10`)
	setProcessGroup(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatalf("启动失败: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	start := time.Now()
	stop, sigkillFired, timedOut := startTimeoutKiller(ctx, cmd, 500*time.Millisecond)
	_ = cmd.Wait()
	stop()
	elapsed := time.Since(start)

	if !timedOut.Load() {
		t.Error("应标记 timedOut")
	}
	if !sigkillFired.Load() {
		t.Error("SIGTERM 被 trap，应触发 SIGKILL")
	}
	// 200ms + 500ms 宽限 + 少许余量
	if elapsed < 600*time.Millisecond {
		t.Errorf("宽限期过短即 SIGKILL: %v", elapsed)
	}
	if elapsed > 2*time.Second {
		t.Errorf("SIGKILL 耗时过长: %v", elapsed)
	}
}
