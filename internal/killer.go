package internal

import (
	"context"
	"errors"
	"os/exec"
	"sync/atomic"
	"syscall"
	"time"
)

// startTimeoutKiller 启动一个后台 goroutine，实现两阶段超时终止：
//  1. 等待 ctx 过期（DeadlineExceeded），或调用方通过 stop() 通知进程已退出
//  2. 过期后对整个进程组发送 SIGTERM，并等待 graceDur 宽限期
//  3. 宽限期后仍未退出则发送 SIGKILL（进程组级别）
//
// 非 unix 平台（procGroupSupportsGraceful=false）上，killProcessGroup 一次 Kill
// 即终止，不会等待宽限期；此时 SIGTERM 阶段结束后立即将 sigkillFired 置为 true
// 以反映真实语义。
//
// 返回值：
//   - stop：调用方在进程自然退出后必须调用，通知 killer 提前返回并等待其结束，
//     避免 goroutine 泄漏
//   - sigkillFired：是否发生 SIGKILL（或非 unix 降级的一次性 Kill）
//   - timedOut：ctx 是否真的因截止时间而过期（true 表示确实发生超时）
//
// 调用约束：
//   - cmd 必须已经 cmd.Start()（否则 cmd.Process 为 nil，killer 将 panic）
//   - 建议先 setProcessGroup(cmd) 以便整组终止
//   - 调用方只负责等待进程（cmd.Wait）与读写 pipe；killer 不介入
func startTimeoutKiller(ctx context.Context, cmd *exec.Cmd, graceDur time.Duration) (stop func(), sigkillFired *atomic.Bool, timedOut *atomic.Bool) {
	sigkillFired = &atomic.Bool{}
	timedOut = &atomic.Bool{}

	procDone := make(chan struct{})
	killerDone := make(chan struct{})

	// 若 ctx 没有截止时间（如未启用超时），则直接返回一个 no-op stop
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		close(killerDone)
		var stopOnce atomic.Bool
		stop = func() {
			if stopOnce.CompareAndSwap(false, true) {
				close(procDone)
			}
		}
		return
	}

	go func() {
		defer close(killerDone)
		select {
		case <-procDone:
			return
		case <-ctx.Done():
			if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return
			}
		}
		timedOut.Store(true)
		pid := cmd.Process.Pid
		_ = killProcessGroup(pid, syscall.SIGTERM)

		// 非 unix 平台（Windows/plan9）killProcessGroup 忽略信号，一次 Kill 即已终止。
		if !procGroupSupportsGraceful {
			sigkillFired.Store(true)
			return
		}

		// 宽限期后若进程仍在，发送 SIGKILL
		graceTimer := time.NewTimer(graceDur)
		defer graceTimer.Stop()
		select {
		case <-procDone:
			return
		case <-graceTimer.C:
			// Race 缓解（见 #58）：graceTimer 到期后二次 peek procDone。
			// 这里是 cmd.Wait() 已 reap pid 到主 goroutine 调 stop() 的窗口期——
			// 若此时 procDone 已被 close，说明进程已自然退出，不应再对可能被复用的 pid 发 SIGKILL。
			// 彻底修复需要 pidfd_send_signal（Linux 5.3+）或等价跨平台 API；此处为纳秒级 best-effort。
			select {
			case <-procDone:
				return
			default:
			}
			sigkillFired.Store(true)
			_ = killProcessGroup(pid, syscall.SIGKILL)
		}
	}()

	var stopOnce atomic.Bool
	stop = func() {
		if stopOnce.CompareAndSwap(false, true) {
			close(procDone)
		}
		<-killerDone
	}
	return
}
