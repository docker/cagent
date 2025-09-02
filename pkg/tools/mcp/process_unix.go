//go:build !windows

package mcp

import (
	"os/exec"
	"syscall"
)

func configureProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func cancelProcess(cmd *exec.Cmd) error {
	return cmd.Process.Signal(syscall.SIGTERM)
}
