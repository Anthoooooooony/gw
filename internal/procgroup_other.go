//go:build !unix

package internal

import (
	"os"
	"os/exec"
	"syscall"
)

// setProcessGroup 非 unix 平台无进程组概念，留空
func setProcessGroup(cmd *exec.Cmd) {}

// killProcessGroup 非 unix 平台无法按进程组杀，仅尽力杀主进程
func killProcessGroup(pid int, sig syscall.Signal) error {
	p, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return p.Kill()
}
