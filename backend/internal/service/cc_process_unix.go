//go:build unix

package service

import (
	"os/exec"
	"syscall"
)

// configureClaudeCodeProcess 让 CC 进程自成一个进程组，超时/断连时可以连它拉起的
// 子进程（例如 Bash 工具）一起杀掉，避免 sleep 之类的孙进程占着 stdout。
func configureClaudeCodeProcess(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func killClaudeCodeProcess(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	_ = cmd.Process.Kill()
}
