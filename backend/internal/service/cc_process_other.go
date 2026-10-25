//go:build !unix

package service

import "os/exec"

func configureClaudeCodeProcess(cmd *exec.Cmd) {}

func killClaudeCodeProcess(cmd *exec.Cmd) {
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}
