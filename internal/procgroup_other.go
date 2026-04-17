//go:build !unix

package internal

import (
	"os"
	"os/exec"
	"syscall"
)

// procGroupSupportsGraceful 标记当前平台是否支持 SIGTERM → 宽限期 → SIGKILL
// 的两阶段终止。非 unix 平台（如 Windows、plan9）**不支持**优雅终止：
//   - Windows 上 os.Process.Kill 直接发 TerminateProcess，无法模拟 SIGTERM
//   - 因此调用方应在首个 kill 调用后就视为 SIGKILL 已发生，避免打印
//     误导性的 "SIGTERM, SIGKILL" 两段式日志
const procGroupSupportsGraceful = false

// setProcessGroup 非 unix 平台无进程组概念，留空
func setProcessGroup(cmd *exec.Cmd) {}

// killProcessGroup 非 unix 平台无法按进程组杀，也无法区分信号。
// **sig 参数被忽略**：任何调用都会触发 os.Process.Kill（等价于 SIGKILL）。
// 调用方必须配合 procGroupSupportsGraceful=false 处理 Windows 降级：
// 不再等待宽限期，一次 kill 即为终止。
func killProcessGroup(pid int, sig syscall.Signal) error {
	_ = sig // 非 unix 平台无法区分信号，显式忽略
	p, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return p.Kill()
}
