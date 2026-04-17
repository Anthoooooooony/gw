//go:build unix

package internal

import (
	"os/exec"
	"syscall"
)

// setProcessGroup 让子进程成为新进程组的 leader，方便后续整组杀掉
func setProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

// killProcessGroup 向整组发送信号。pid 为进程组 leader 的 PID。
// 内核约定：kill(-pgid, sig) 会将信号发送给整个进程组。
func killProcessGroup(pid int, sig syscall.Signal) error {
	// 注意 Setpgid=true + Pgid 未显式指定 → pgid == pid
	return syscall.Kill(-pid, sig)
}
